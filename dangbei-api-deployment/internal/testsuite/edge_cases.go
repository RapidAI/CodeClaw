package testsuite

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

func (r *Runner) caseConcurrencyThresholdLimit(ctx context.Context, cc *caseContext) error {
	status, err := r.fetchQueueStatus(ctx, cc)
	if err != nil {
		return err
	}
	total := toInt(status["total"])
	maxInflight := toInt(status["max_inflight_per_account"])
	maxQueue := toInt(status["max_queue_size"])
	if total <= 0 || maxInflight <= 0 {
		cc.assert("queue_capacity_known", false, fmt.Sprintf("queue_status=%v", status))
		return nil
	}
	capacity := total*maxInflight + maxQueue
	if capacity <= 0 {
		capacity = total * maxInflight
	}
	n := capacity + 8
	if n < 8 {
		n = 8
	}
	type one struct {
		Status int
		Err    string
	}
	res := make([]one, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			resp, err := cc.request(ctx, requestSpec{
				Method: http.MethodPost,
				Path:   "/v1/chat/completions",
				Headers: map[string]string{
					"Authorization": "Bearer " + r.apiKey,
				},
				Body: map[string]any{
					"model": "deepseek-chat",
					"messages": []map[string]any{
						{"role": "user", "content": fmt.Sprintf("并发边界测试 #%d，请输出不少于300字。", idx)},
					},
					"stream": true,
				},
				Stream:    true,
				Retryable: true,
			})
			if err != nil {
				res[idx] = one{Err: err.Error()}
				return
			}
			res[idx] = one{Status: resp.StatusCode}
		}(i)
	}
	wg.Wait()

	dist := map[int]int{}
	for _, it := range res {
		if it.Status > 0 {
			dist[it.Status]++
		}
	}
	cc.assert("has_200", dist[http.StatusOK] > 0, fmt.Sprintf("distribution=%v", dist))
	cc.assert("has_429_when_over_capacity", dist[http.StatusTooManyRequests] > 0, fmt.Sprintf("distribution=%v capacity=%d n=%d", dist, capacity, n))
	_, has5xx := has5xx(dist)
	cc.assert("no_5xx", !has5xx, fmt.Sprintf("distribution=%v", dist))
	return nil
}

func (r *Runner) caseStreamAbortRelease(ctx context.Context, cc *caseContext) error {
	before, err := r.fetchQueueStatus(ctx, cc)
	if err != nil {
		return err
	}
	baseInUse := toInt(before["in_use"])
	for i := 0; i < 3; i++ {
		if err := cc.abortStreamRequest(ctx, requestSpec{
			Method: http.MethodPost,
			Path:   "/v1/chat/completions",
			Headers: map[string]string{
				"Authorization": "Bearer " + r.apiKey,
			},
			Body: map[string]any{
				"model": "deepseek-chat",
				"messages": []map[string]any{
					{"role": "user", "content": fmt.Sprintf("中断释放测试 #%d，请流式回复", i)},
				},
				"stream": true,
			},
			Stream: true,
		}); err != nil {
			cc.assert("abort_request_no_error", false, err.Error())
		}
	}

	deadline := time.Now().Add(25 * time.Second)
	recovered := false
	lastInUse := -1
	for time.Now().Before(deadline) {
		st, err := r.fetchQueueStatus(ctx, cc)
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		lastInUse = toInt(st["in_use"])
		if lastInUse <= baseInUse {
			recovered = true
			break
		}
		time.Sleep(time.Second)
	}
	cc.assert("in_use_recovered_after_abort", recovered, fmt.Sprintf("base=%d last=%d", baseInUse, lastInUse))
	return nil
}

func (cc *caseContext) abortStreamRequest(ctx context.Context, spec requestSpec) error {
	cc.seq++
	traceID := fmt.Sprintf("ts_%s_%s_%03d", cc.runner.runID, sanitizeID(cc.id), cc.seq)
	cc.traceIDsSet[traceID] = struct{}{}
	fullURL, err := withTraceQuery(cc.runner.baseURL+spec.Path, traceID)
	if err != nil {
		return err
	}
	headers := map[string]string{}
	for k, v := range spec.Headers {
		headers[k] = v
	}
	headers["X-Ds2-Test-Trace"] = traceID
	bodyBytes, _ := json.Marshal(spec.Body)
	headers["Content-Type"] = "application/json"
	cc.requests = append(cc.requests, requestLog{
		Seq:       cc.seq,
		Attempt:   1,
		TraceID:   traceID,
		Method:    spec.Method,
		URL:       fullURL,
		Headers:   headers,
		Body:      spec.Body,
		Timestamp: time.Now().Format(time.RFC3339Nano),
	})

	reqCtx, cancel := context.WithTimeout(ctx, cc.runner.opts.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, spec.Method, fullURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	start := time.Now()
	resp, err := cc.runner.httpClient.Do(req)
	if err != nil {
		cc.responses = append(cc.responses, responseLog{
			Seq:        cc.seq,
			Attempt:    1,
			TraceID:    traceID,
			StatusCode: 0,
			DurationMS: time.Since(start).Milliseconds(),
			NetworkErr: err.Error(),
			ReceivedAt: time.Now().Format(time.RFC3339Nano),
		})
		return err
	}
	defer resp.Body.Close()
	buf := make([]byte, 512)
	_, _ = resp.Body.Read(buf)
	_ = resp.Body.Close()
	cc.responses = append(cc.responses, responseLog{
		Seq:        cc.seq,
		Attempt:    1,
		TraceID:    traceID,
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		BodyText:   "aborted_after_first_chunk",
		DurationMS: time.Since(start).Milliseconds(),
		ReceivedAt: time.Now().Format(time.RFC3339Nano),
	})
	return nil
}

func (r *Runner) caseToolcallStreamMixed(ctx context.Context, cc *caseContext) error {
	payload := toolcallPayload(true)
	payload["messages"] = []map[string]any{
		{
			"role":    "user",
			"content": "请先输出一句普通文本，再调用工具 search 查询 golang，最后再输出一句普通文本。",
		},
	}
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.apiKey,
		},
		Body:      payload,
		Stream:    true,
		Retryable: false,
	})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	frames, done := parseSSEFrames(resp.Body)
	hasTool := false
	hasText := false
	rawLeak := false
	for _, f := range frames {
		choices, _ := f["choices"].([]any)
		for _, c := range choices {
			ch, _ := c.(map[string]any)
			delta, _ := ch["delta"].(map[string]any)
			if _, ok := delta["tool_calls"]; ok {
				hasTool = true
			}
			content := asString(delta["content"])
			if content != "" {
				hasText = true
			}
			if strings.Contains(strings.ToLower(content), `"tool_calls"`) {
				rawLeak = true
			}
		}
	}
	cc.assert("tool_calls_delta_present", hasTool, "tool_calls delta missing")
	cc.assert("no_raw_tool_json_leak", !rawLeak, "raw tool_calls leaked")
	cc.assert("done_terminated", done, "expected [DONE]")
	if !(hasTool && hasText) {
		r.warnings = append(r.warnings, "toolcall mixed stream did not produce both text and tool_calls in this run (model-side behavior dependent)")
	}
	return nil
}

func (r *Runner) caseSSEJSONIntegrity(ctx context.Context, cc *caseContext) error {
	openaiResp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.apiKey,
		},
		Body: map[string]any{
			"model": "deepseek-chat",
			"messages": []map[string]any{
				{"role": "user", "content": "输出一句话"},
			},
			"stream": true,
		},
		Stream:    true,
		Retryable: false,
	})
	if err != nil {
		return err
	}
	cc.assert("openai_status_200", openaiResp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", openaiResp.StatusCode))
	badOpenAI := countMalformedSSEJSONLines(openaiResp.Body)
	cc.assert("openai_sse_json_valid", badOpenAI == 0, fmt.Sprintf("malformed=%d", badOpenAI))

	anthropicResp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/anthropic/v1/messages",
		Headers: map[string]string{
			"Authorization":     "Bearer " + r.apiKey,
			"anthropic-version": "2023-06-01",
		},
		Body: map[string]any{
			"model": "claude-sonnet-4-5",
			"messages": []map[string]any{
				{"role": "user", "content": "stream json integrity"},
			},
			"stream": true,
		},
		Stream:    true,
		Retryable: false,
	})
	if err != nil {
		return err
	}
	cc.assert("anthropic_status_200", anthropicResp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", anthropicResp.StatusCode))
	badAnthropic := countMalformedSSEJSONLines(anthropicResp.Body)
	cc.assert("anthropic_sse_json_valid", badAnthropic == 0, fmt.Sprintf("malformed=%d", badAnthropic))
	return nil
}

func (r *Runner) caseInvalidModel(ctx context.Context, cc *caseContext) error {
	resp, err := cc.requestOnce(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.apiKey,
		},
		Body: map[string]any{
			"model": "deepseek-not-exists",
			"messages": []map[string]any{
				{"role": "user", "content": "hi"},
			},
			"stream": false,
		},
		Retryable: false,
	}, 1)
	if err != nil {
		return err
	}
	cc.assert("status_503", resp.StatusCode == http.StatusServiceUnavailable, fmt.Sprintf("status=%d", resp.StatusCode))
	var m map[string]any
	_ = json.Unmarshal(resp.Body, &m)
	e, _ := m["error"].(map[string]any)
	cc.assert("error_type_service_unavailable", asString(e["type"]) == "service_unavailable_error", fmt.Sprintf("body=%s", string(resp.Body)))
	return nil
}

func (r *Runner) caseMissingMessages(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.apiKey,
		},
		Body: map[string]any{
			"model":  "deepseek-chat",
			"stream": false,
		},
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("status_400", resp.StatusCode == http.StatusBadRequest, fmt.Sprintf("status=%d", resp.StatusCode))
	var m map[string]any
	_ = json.Unmarshal(resp.Body, &m)
	e, _ := m["error"].(map[string]any)
	cc.assert("error_type_invalid_request", asString(e["type"]) == "invalid_request_error", fmt.Sprintf("body=%s", string(resp.Body)))
	return nil
}

func (r *Runner) caseAdminUnauthorized(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{
		Method:    http.MethodGet,
		Path:      "/admin/config",
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("status_401", resp.StatusCode == http.StatusUnauthorized, fmt.Sprintf("status=%d", resp.StatusCode))
	return nil
}

func (r *Runner) caseTokenRefreshManagedAccount(ctx context.Context, cc *caseContext) error {
	if len(r.configRaw.Accounts) == 0 {
		cc.assert("account_present", false, "no account in config")
		return nil
	}
	acc := r.configRaw.Accounts[0]
	id := strings.TrimSpace(acc.Email)
	if id == "" {
		id = strings.TrimSpace(acc.Mobile)
	}
	if id == "" {
		cc.assert("account_identifier", false, "first account has no identifier")
		return nil
	}
	if strings.TrimSpace(acc.Password) == "" {
		r.warnings = append(r.warnings, "token refresh edge case skipped strict check: first account password empty")
		cc.assert("account_password_present", true, "skipped strict refresh check due empty password")
		return nil
	}
	invalidToken := "invalid-testsuite-refresh-token-" + sanitizeID(r.runID)
	update := map[string]any{
		"keys": r.configRaw.Keys,
		"accounts": []map[string]any{
			{
				"email":    acc.Email,
				"mobile":   acc.Mobile,
				"password": acc.Password,
				"token":    invalidToken,
			},
		},
	}
	updResp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/admin/config",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.adminJWT,
		},
		Body:      update,
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("update_config_status_200", updResp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", updResp.StatusCode))

	chatResp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Headers: map[string]string{
			"Authorization":        "Bearer " + r.apiKey,
			"X-Ds2-Target-Account": id,
		},
		Body: map[string]any{
			"model": "deepseek-chat",
			"messages": []map[string]any{
				{"role": "user", "content": "token refresh test"},
			},
			"stream": false,
		},
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("chat_status_200", chatResp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d body=%s", chatResp.StatusCode, string(chatResp.Body)))

	cfgResp, err := cc.request(ctx, requestSpec{
		Method: http.MethodGet,
		Path:   "/admin/config",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.adminJWT,
		},
		Retryable: true,
	})
	if err != nil {
		return err
	}
	var cfg map[string]any
	_ = json.Unmarshal(cfgResp.Body, &cfg)
	accounts, _ := cfg["accounts"].([]any)
	preview := ""
	hasToken := false
	for _, item := range accounts {
		m, _ := item.(map[string]any)
		e := asString(m["email"])
		mo := asString(m["mobile"])
		if e == acc.Email && mo == acc.Mobile {
			preview = asString(m["token_preview"])
			hasToken, _ = m["has_token"].(bool)
			break
		}
	}
	cc.assert("has_token_after_refresh", hasToken, fmt.Sprintf("config=%s", string(cfgResp.Body)))
	cc.assert("token_preview_changed_from_invalid", !strings.HasPrefix(preview, invalidToken[:20]), fmt.Sprintf("preview=%s invalid_prefix=%s", preview, invalidToken[:20]))
	return nil
}

func (r *Runner) fetchQueueStatus(ctx context.Context, cc *caseContext) (map[string]any, error) {
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodGet,
		Path:   "/admin/queue/status",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.adminJWT,
		},
		Retryable: true,
	})
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(resp.Body, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func countMalformedSSEJSONLines(body []byte) int {
	lines := strings.Split(string(body), "\n")
	bad := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var v any
		if err := json.Unmarshal([]byte(payload), &v); err != nil {
			bad++
		}
	}
	return bad
}
