package deepseek

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ds2api/internal/auth"
	"ds2api/internal/config"
	trans "ds2api/internal/deepseek/transport"
	"ds2api/internal/util"

	"github.com/andybalholm/brotli"
)

// intFrom is a package-internal alias for the shared util version.
var intFrom = util.IntFrom

type Client struct {
	Store      *config.Store
	Auth       *auth.Resolver
	regular    trans.Doer
	stream     trans.Doer
	fallback   *http.Client
	fallbackS  *http.Client
	powSolver  *PowSolver
	maxRetries int
}

func NewClient(store *config.Store, resolver *auth.Resolver) *Client {
	return &Client{
		Store:      store,
		Auth:       resolver,
		regular:    trans.New(60 * time.Second),
		stream:     trans.New(0),
		fallback:   &http.Client{Timeout: 60 * time.Second},
		fallbackS:  &http.Client{Timeout: 0},
		powSolver:  NewPowSolver(config.WASMPath()),
		maxRetries: 3,
	}
}

func (c *Client) PreloadPow(ctx context.Context) error {
	return c.powSolver.init(ctx)
}

func (c *Client) Login(ctx context.Context, acc config.Account) (string, error) {
	payload := map[string]any{
		"password":  strings.TrimSpace(acc.Password),
		"device_id": "deepseek_to_api",
		"os":        "android",
	}
	if email := strings.TrimSpace(acc.Email); email != "" {
		payload["email"] = email
	} else if mobile := strings.TrimSpace(acc.Mobile); mobile != "" {
		payload["mobile"] = mobile
		payload["area_code"] = nil
	} else {
		return "", errors.New("missing email/mobile")
	}
	resp, err := c.postJSON(ctx, c.regular, DeepSeekLoginURL, BaseHeaders, payload)
	if err != nil {
		return "", err
	}
	code := intFrom(resp["code"])
	if code != 0 {
		return "", fmt.Errorf("login failed: %v", resp["msg"])
	}
	data, _ := resp["data"].(map[string]any)
	if intFrom(data["biz_code"]) != 0 {
		return "", fmt.Errorf("login failed: %v", data["biz_msg"])
	}
	bizData, _ := data["biz_data"].(map[string]any)
	user, _ := bizData["user"].(map[string]any)
	token, _ := user["token"].(string)
	if strings.TrimSpace(token) == "" {
		return "", errors.New("missing login token")
	}
	return token, nil
}

func (c *Client) CreateSession(ctx context.Context, a *auth.RequestAuth, maxAttempts int) (string, error) {
	if maxAttempts <= 0 {
		maxAttempts = c.maxRetries
	}
	attempts := 0
	refreshed := false
	for attempts < maxAttempts {
		headers := c.authHeaders(a.DeepSeekToken)
		resp, status, err := c.postJSONWithStatus(ctx, c.regular, DeepSeekCreateSessionURL, headers, map[string]any{"agent": "chat"})
		if err != nil {
			config.Logger.Warn("[create_session] request error", "error", err, "account", a.AccountID)
			attempts++
			continue
		}
		code := intFrom(resp["code"])
		if status == http.StatusOK && code == 0 {
			data, _ := resp["data"].(map[string]any)
			bizData, _ := data["biz_data"].(map[string]any)
			sessionID, _ := bizData["id"].(string)
			if sessionID != "" {
				return sessionID, nil
			}
		}
		msg, _ := resp["msg"].(string)
		config.Logger.Warn("[create_session] failed", "status", status, "code", code, "msg", msg, "use_config_token", a.UseConfigToken, "account", a.AccountID)
		if a.UseConfigToken {
			if isTokenInvalid(status, code, msg) && !refreshed {
				if c.Auth.RefreshToken(ctx, a) {
					refreshed = true
					continue
				}
			}
			if c.Auth.SwitchAccount(ctx, a) {
				refreshed = false
				attempts++
				continue
			}
		}
		attempts++
	}
	return "", errors.New("create session failed")
}

func (c *Client) GetPow(ctx context.Context, a *auth.RequestAuth, maxAttempts int) (string, error) {
	if maxAttempts <= 0 {
		maxAttempts = c.maxRetries
	}
	attempts := 0
	for attempts < maxAttempts {
		headers := c.authHeaders(a.DeepSeekToken)
		resp, status, err := c.postJSONWithStatus(ctx, c.regular, DeepSeekCreatePowURL, headers, map[string]any{"target_path": "/api/v0/chat/completion"})
		if err != nil {
			config.Logger.Warn("[get_pow] request error", "error", err, "account", a.AccountID)
			attempts++
			continue
		}
		code := intFrom(resp["code"])
		if status == http.StatusOK && code == 0 {
			data, _ := resp["data"].(map[string]any)
			bizData, _ := data["biz_data"].(map[string]any)
			challenge, _ := bizData["challenge"].(map[string]any)
			answer, err := c.powSolver.Compute(ctx, challenge)
			if err != nil {
				attempts++
				continue
			}
			return BuildPowHeader(challenge, answer)
		}
		msg, _ := resp["msg"].(string)
		config.Logger.Warn("[get_pow] failed", "status", status, "code", code, "msg", msg, "use_config_token", a.UseConfigToken, "account", a.AccountID)
		if a.UseConfigToken {
			if isTokenInvalid(status, code, msg) {
				if c.Auth.RefreshToken(ctx, a) {
					continue
				}
			}
			if c.Auth.SwitchAccount(ctx, a) {
				attempts++
				continue
			}
		}
		attempts++
	}
	return "", errors.New("get pow failed")
}

func (c *Client) CallCompletion(ctx context.Context, a *auth.RequestAuth, payload map[string]any, powResp string, maxAttempts int) (*http.Response, error) {
	if maxAttempts <= 0 {
		maxAttempts = c.maxRetries
	}
	headers := c.authHeaders(a.DeepSeekToken)
	headers["x-ds-pow-response"] = powResp
	attempts := 0
	for attempts < maxAttempts {
		resp, err := c.streamPost(ctx, DeepSeekCompletionURL, headers, payload)
		if err != nil {
			attempts++
			time.Sleep(time.Second)
			continue
		}
		if resp.StatusCode == http.StatusOK {
			return resp, nil
		}
		_ = resp.Body.Close()
		attempts++
		time.Sleep(time.Second)
	}
	return nil, errors.New("completion failed")
}

func (c *Client) postJSON(ctx context.Context, doer trans.Doer, url string, headers map[string]string, payload any) (map[string]any, error) {
	body, status, err := c.postJSONWithStatus(ctx, doer, url, headers, payload)
	if err != nil {
		return nil, err
	}
	if status == 0 {
		return nil, errors.New("request failed")
	}
	return body, nil
}

func (c *Client) postJSONWithStatus(ctx context.Context, doer trans.Doer, url string, headers map[string]string, payload any) (map[string]any, int, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, 0, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := doer.Do(req)
	if err != nil {
		config.Logger.Warn("[deepseek] fingerprint request failed, fallback to std transport", "url", url, "error", err)
		req2, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
		if reqErr != nil {
			return nil, 0, err
		}
		for k, v := range headers {
			req2.Header.Set(k, v)
		}
		resp, err = c.fallback.Do(req2)
		if err != nil {
			return nil, 0, err
		}
	}
	defer resp.Body.Close()
	payloadBytes, err := readResponseBody(resp)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	out := map[string]any{}
	if len(payloadBytes) > 0 {
		if err := json.Unmarshal(payloadBytes, &out); err != nil {
			config.Logger.Warn("[deepseek] json parse failed", "url", url, "status", resp.StatusCode, "content_encoding", resp.Header.Get("Content-Encoding"), "preview", preview(payloadBytes))
		}
	}
	return out, resp.StatusCode, nil
}

func (c *Client) streamPost(ctx context.Context, url string, headers map[string]string, payload any) (*http.Response, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.stream.Do(req)
	if err != nil {
		config.Logger.Warn("[deepseek] fingerprint stream request failed, fallback to std transport", "url", url, "error", err)
		req2, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
		if reqErr != nil {
			return nil, err
		}
		for k, v := range headers {
			req2.Header.Set(k, v)
		}
		return c.fallbackS.Do(req2)
	}
	return resp, nil
}

func (c *Client) authHeaders(token string) map[string]string {
	headers := make(map[string]string, len(BaseHeaders)+1)
	for k, v := range BaseHeaders {
		headers[k] = v
	}
	headers["authorization"] = "Bearer " + token
	return headers
}

func isTokenInvalid(status int, code int, msg string) bool {
	msg = strings.ToLower(msg)
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return true
	}
	if code == 40001 || code == 40002 || code == 40003 {
		return true
	}
	return strings.Contains(msg, "token") || strings.Contains(msg, "unauthorized")
}

func readResponseBody(resp *http.Response) ([]byte, error) {
	encoding := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding")))
	var reader io.Reader = resp.Body
	switch encoding {
	case "gzip":
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		reader = gz
	case "br":
		reader = brotli.NewReader(resp.Body)
	}
	return io.ReadAll(reader)
}

func preview(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 160 {
		return s[:160]
	}
	return s
}

func ScanSSELines(resp *http.Response, onLine func([]byte) bool) error {
	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)
	for scanner.Scan() {
		if !onLine(scanner.Bytes()) {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}
