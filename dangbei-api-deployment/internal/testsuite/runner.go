package testsuite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Options struct {
	ConfigPath  string
	AdminKey    string
	OutputDir   string
	Port        int
	Timeout     time.Duration
	Retries     int
	NoPreflight bool
	MaxKeepRuns int
}

type runSummary struct {
	RunID       string         `json:"run_id"`
	StartedAt   string         `json:"started_at"`
	EndedAt     string         `json:"ended_at"`
	DurationMS  int64          `json:"duration_ms"`
	Stats       map[string]any `json:"stats"`
	Environment map[string]any `json:"environment"`
	Cases       []caseResult   `json:"cases"`
	Warnings    []string       `json:"warnings,omitempty"`
}

type caseResult struct {
	CaseID       string            `json:"case_id"`
	Passed       bool              `json:"passed"`
	DurationMS   int64             `json:"duration_ms"`
	TraceIDs     []string          `json:"trace_ids"`
	StatusCodes  []int             `json:"status_codes"`
	Error        string            `json:"error,omitempty"`
	ArtifactPath string            `json:"artifact_path"`
	Assertions   []assertionResult `json:"assertions"`
}

type assertionResult struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

type requestLog struct {
	Seq       int               `json:"seq"`
	Attempt   int               `json:"attempt"`
	TraceID   string            `json:"trace_id"`
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	Body      any               `json:"body,omitempty"`
	Timestamp string            `json:"timestamp"`
}

type responseLog struct {
	Seq        int                 `json:"seq"`
	Attempt    int                 `json:"attempt"`
	TraceID    string              `json:"trace_id"`
	StatusCode int                 `json:"status_code"`
	Headers    map[string][]string `json:"headers"`
	BodyText   string              `json:"body_text"`
	DurationMS int64               `json:"duration_ms"`
	NetworkErr string              `json:"network_error,omitempty"`
	ReceivedAt string              `json:"received_at"`
}

type caseContext struct {
	runner      *Runner
	id          string
	dir         string
	startedAt   time.Time
	mu          sync.Mutex
	seq         int
	assertions  []assertionResult
	requests    []requestLog
	responses   []responseLog
	streamRaw   strings.Builder
	traceIDsSet map[string]struct{}
}

type requestSpec struct {
	Method    string
	Path      string
	Headers   map[string]string
	Body      any
	Stream    bool
	Retryable bool
}

type responseResult struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
	TraceID    string
	URL        string
}

type Runner struct {
	opts Options

	runID        string
	runDir       string
	serverLog    string
	preflightLog string

	baseURL     string
	httpClient  *http.Client
	serverCmd   *exec.Cmd
	serverLogFd *os.File

	configCopyPath     string
	originalConfigPath string
	originalConfigHash string

	configRaw runConfig
	apiKey    string
	adminKey  string
	adminJWT  string
	accountID string

	warnings []string
	results  []caseResult
}

type runConfig struct {
	Keys     []string `json:"keys"`
	Accounts []struct {
		Email    string `json:"email,omitempty"`
		Mobile   string `json:"mobile,omitempty"`
		Password string `json:"password,omitempty"`
		Token    string `json:"token,omitempty"`
	} `json:"accounts"`
}

func DefaultOptions() Options {
	return Options{
		ConfigPath:  "config.json",
		AdminKey:    strings.TrimSpace(os.Getenv("DS2API_ADMIN_KEY")),
		OutputDir:   "artifacts/testsuite",
		Port:        0,
		Timeout:     120 * time.Second,
		Retries:     2,
		NoPreflight: false,
		MaxKeepRuns: 5,
	}
}

func Run(ctx context.Context, opts Options) error {
	r, err := newRunner(opts)
	if err != nil {
		return err
	}
	start := time.Now()
	defer func() {
		_ = r.stopServer()
	}()

	if err := r.prepareRunDir(); err != nil {
		return err
	}

	if !r.opts.NoPreflight {
		if err := r.runPreflight(ctx); err != nil {
			_ = r.writeSummary(start, time.Now())
			return err
		}
	}

	if err := r.prepareConfigIsolation(); err != nil {
		_ = r.writeSummary(start, time.Now())
		return err
	}

	if err := r.startServer(ctx); err != nil {
		_ = r.writeSummary(start, time.Now())
		return err
	}

	if err := r.prepareAuth(ctx); err != nil {
		r.warnings = append(r.warnings, "auth prepare failed: "+err.Error())
	}

	for _, c := range r.cases() {
		r.runCase(ctx, c)
	}

	if err := r.ensureOriginalConfigUntouched(); err != nil {
		r.warnings = append(r.warnings, err.Error())
	}

	end := time.Now()
	if err := r.writeSummary(start, end); err != nil {
		return err
	}

	// Prune old test runs, keeping only the most recent N.
	if err := r.pruneOldRuns(); err != nil {
		r.warnings = append(r.warnings, "prune old runs: "+err.Error())
	}

	failed := 0
	for _, cs := range r.results {
		if !cs.Passed {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("testsuite failed: %d case(s) failed, see %s", failed, filepath.Join(r.runDir, "summary.md"))
	}
	return nil
}

func newRunner(opts Options) (*Runner, error) {
	if strings.TrimSpace(opts.ConfigPath) == "" {
		opts.ConfigPath = "config.json"
	}
	if strings.TrimSpace(opts.OutputDir) == "" {
		opts.OutputDir = "artifacts/testsuite"
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 120 * time.Second
	}
	if opts.Retries < 0 {
		opts.Retries = 0
	}
	adminKey := strings.TrimSpace(opts.AdminKey)
	if adminKey == "" {
		adminKey = strings.TrimSpace(os.Getenv("DS2API_ADMIN_KEY"))
	}
	if adminKey == "" {
		adminKey = "admin"
	}
	opts.AdminKey = adminKey

	return &Runner{
		opts: opts,
		httpClient: &http.Client{
			Timeout: 0,
		},
		runID:    time.Now().UTC().Format("20060102T150405Z"),
		adminKey: adminKey,
	}, nil
}

func (r *Runner) prepareRunDir() error {
	r.runDir = filepath.Join(r.opts.OutputDir, r.runID)
	if err := os.MkdirAll(r.runDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(r.runDir, "cases"), 0o755); err != nil {
		return err
	}
	r.serverLog = filepath.Join(r.runDir, "server.log")
	r.preflightLog = filepath.Join(r.runDir, "preflight.log")
	return nil
}

// pruneOldRuns removes old test run directories, keeping the most recent MaxKeepRuns.
// Run IDs use the format "20060102T150405Z", so alphabetical order == chronological order.
func (r *Runner) pruneOldRuns() error {
	keep := r.opts.MaxKeepRuns
	if keep <= 0 {
		return nil // 0 or negative means no pruning
	}

	entries, err := os.ReadDir(r.opts.OutputDir)
	if err != nil {
		return err
	}

	// Collect only directories (each run is a directory).
	var runDirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		runDirs = append(runDirs, e.Name())
	}

	sort.Strings(runDirs)

	if len(runDirs) <= keep {
		return nil
	}

	// Remove oldest runs (those at the beginning of the sorted list).
	toRemove := runDirs[:len(runDirs)-keep]
	var errs []string
	for _, name := range toRemove {
		dirPath := filepath.Join(r.opts.OutputDir, name)
		if err := os.RemoveAll(dirPath); err != nil {
			errs = append(errs, fmt.Sprintf("remove %s: %v", name, err))
		} else {
			fmt.Fprintf(os.Stdout, "pruned old test run: %s\n", name)
		}
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (r *Runner) runPreflight(ctx context.Context) error {
	steps := [][]string{
		{"go", "test", "./...", "-count=1"},
		{"node", "--check", "api/chat-stream.js"},
		{"node", "--check", "api/helpers/stream-tool-sieve.js"},
		{"node", "--test", "api/helpers/stream-tool-sieve.test.js", "api/chat-stream.test.js"},
		{"npm", "run", "build", "--prefix", "webui"},
	}
	f, err := os.OpenFile(r.preflightLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, step := range steps {
		if _, err := fmt.Fprintf(f, "\n$ %s\n", strings.Join(step, " ")); err != nil {
			return err
		}
		cmd := exec.CommandContext(ctx, step[0], step[1:]...)
		cmd.Stdout = f
		cmd.Stderr = f
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("preflight failed at `%s`: %w", strings.Join(step, " "), err)
		}
	}
	return nil
}

func (r *Runner) prepareConfigIsolation() error {
	abs, err := filepath.Abs(r.opts.ConfigPath)
	if err != nil {
		return err
	}
	r.originalConfigPath = abs
	raw, err := os.ReadFile(abs)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	r.originalConfigHash = hex.EncodeToString(sum[:])

	tmpDir := filepath.Join(r.runDir, "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return err
	}
	r.configCopyPath = filepath.Join(tmpDir, "config.json")
	if err := os.WriteFile(r.configCopyPath, raw, 0o644); err != nil {
		return err
	}
	var cfg runConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("parse config failed: %w", err)
	}
	r.configRaw = cfg
	if len(cfg.Keys) > 0 {
		r.apiKey = strings.TrimSpace(cfg.Keys[0])
	}
	for _, acc := range cfg.Accounts {
		id := strings.TrimSpace(acc.Email)
		if id == "" {
			id = strings.TrimSpace(acc.Mobile)
		}
		if id != "" {
			r.accountID = id
			break
		}
	}
	return nil
}

func (r *Runner) startServer(ctx context.Context) error {
	port := r.opts.Port
	if port <= 0 {
		p, err := findFreePort()
		if err != nil {
			return err
		}
		port = p
	}
	r.baseURL = "http://127.0.0.1:" + strconv.Itoa(port)

	logFd, err := os.OpenFile(r.serverLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	r.serverLogFd = logFd
	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/ds2api")
	cmd.Stdout = logFd
	cmd.Stderr = logFd
	cmd.Env = prepareServerEnv(os.Environ(), map[string]string{
		"PORT":                    strconv.Itoa(port),
		"DS2API_CONFIG_PATH":      r.configCopyPath,
		"DS2API_AUTO_BUILD_WEBUI": "false",
		"DS2API_CONFIG_JSON":      "",
		"CONFIG_JSON":             "",
	})
	if err := cmd.Start(); err != nil {
		_ = logFd.Close()
		return err
	}
	r.serverCmd = cmd

	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if r.ping("/healthz") == nil && r.ping("/readyz") == nil {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("server readiness timeout")
}

func (r *Runner) stopServer() error {
	var errs []string
	if r.serverCmd != nil && r.serverCmd.Process != nil {
		_ = r.serverCmd.Process.Signal(os.Interrupt)
		done := make(chan error, 1)
		go func() { done <- r.serverCmd.Wait() }()
		select {
		case <-time.After(5 * time.Second):
			_ = r.serverCmd.Process.Kill()
			<-done
		case <-done:
		}
	}
	if r.serverLogFd != nil {
		if err := r.serverLogFd.Close(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (r *Runner) ping(path string) error {
	resp, err := r.httpClient.Get(r.baseURL + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status=%d", resp.StatusCode)
	}
	return nil
}

func (r *Runner) prepareAuth(ctx context.Context) error {
	reqBody := map[string]any{
		"admin_key":    r.adminKey,
		"expire_hours": 24,
	}
	resp, err := r.doSimpleJSON(ctx, http.MethodPost, "/admin/login", nil, reqBody)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("admin login status=%d body=%s", resp.StatusCode, string(resp.Body))
	}
	var m map[string]any
	if err := json.Unmarshal(resp.Body, &m); err != nil {
		return err
	}
	token, _ := m["token"].(string)
	if strings.TrimSpace(token) == "" {
		return errors.New("empty admin jwt token")
	}
	r.adminJWT = token
	return nil
}

func (r *Runner) ensureOriginalConfigUntouched() error {
	raw, err := os.ReadFile(r.originalConfigPath)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	current := hex.EncodeToString(sum[:])
	if current != r.originalConfigHash {
		return fmt.Errorf("original config changed unexpectedly: %s", r.originalConfigPath)
	}
	return nil
}

func (r *Runner) runCase(ctx context.Context, c caseDef) {
	caseDir := filepath.Join(r.runDir, "cases", c.ID)
	_ = os.MkdirAll(caseDir, 0o755)
	cc := &caseContext{
		runner:      r,
		id:          c.ID,
		dir:         caseDir,
		startedAt:   time.Now(),
		traceIDsSet: map[string]struct{}{},
	}
	err := c.Run(ctx, cc)
	duration := time.Since(cc.startedAt).Milliseconds()

	if err != nil {
		cc.assertions = append(cc.assertions, assertionResult{
			Name:   "case_error",
			Passed: false,
			Detail: err.Error(),
		})
	}
	passed := err == nil
	for _, a := range cc.assertions {
		if !a.Passed {
			passed = false
			break
		}
	}

	traceIDs := make([]string, 0, len(cc.traceIDsSet))
	for t := range cc.traceIDsSet {
		traceIDs = append(traceIDs, t)
	}
	sort.Strings(traceIDs)
	statuses := uniqueStatusCodes(cc.responses)
	cs := caseResult{
		CaseID:       c.ID,
		Passed:       passed,
		DurationMS:   duration,
		TraceIDs:     traceIDs,
		StatusCodes:  statuses,
		ArtifactPath: caseDir,
		Assertions:   cc.assertions,
	}
	if err != nil {
		cs.Error = err.Error()
	}
	_ = cc.flushArtifacts(cs)
	r.results = append(r.results, cs)
}

func (cc *caseContext) assert(name string, ok bool, detail string) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.assertions = append(cc.assertions, assertionResult{
		Name:   name,
		Passed: ok,
		Detail: detail,
	})
}

func (cc *caseContext) request(ctx context.Context, spec requestSpec) (*responseResult, error) {
	retries := cc.runner.opts.Retries
	if !spec.Retryable {
		retries = 0
	}
	var lastErr error
	for attempt := 1; attempt <= retries+1; attempt++ {
		resp, err := cc.requestOnce(ctx, spec, attempt)
		if err == nil && resp.StatusCode < 500 {
			return resp, nil
		}
		if err != nil {
			lastErr = err
		} else if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("status=%d", resp.StatusCode)
		}
		if attempt <= retries {
			sleep := time.Duration(300*(1<<(attempt-1))) * time.Millisecond
			time.Sleep(sleep)
		}
	}
	return nil, lastErr
}

func (cc *caseContext) requestOnce(ctx context.Context, spec requestSpec, attempt int) (*responseResult, error) {
	cc.mu.Lock()
	cc.seq++
	seq := cc.seq
	traceID := fmt.Sprintf("ts_%s_%s_%03d", cc.runner.runID, sanitizeID(cc.id), seq)
	cc.traceIDsSet[traceID] = struct{}{}
	cc.mu.Unlock()

	fullURL, err := withTraceQuery(cc.runner.baseURL+spec.Path, traceID)
	if err != nil {
		return nil, err
	}

	headers := map[string]string{}
	for k, v := range spec.Headers {
		headers[k] = v
	}
	headers["X-Ds2-Test-Trace"] = traceID

	var bodyBytes []byte
	var bodyAny any
	if spec.Body != nil {
		b, err := json.Marshal(spec.Body)
		if err != nil {
			return nil, err
		}
		bodyBytes = b
		bodyAny = spec.Body
		headers["Content-Type"] = "application/json"
	}
	cc.mu.Lock()
	cc.requests = append(cc.requests, requestLog{
		Seq:       seq,
		Attempt:   attempt,
		TraceID:   traceID,
		Method:    spec.Method,
		URL:       fullURL,
		Headers:   headers,
		Body:      bodyAny,
		Timestamp: time.Now().Format(time.RFC3339Nano),
	})
	cc.mu.Unlock()

	reqCtx, cancel := context.WithTimeout(ctx, cc.runner.opts.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, spec.Method, fullURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	start := time.Now()
	resp, err := cc.runner.httpClient.Do(req)
	if err != nil {
		cc.mu.Lock()
		cc.responses = append(cc.responses, responseLog{
			Seq:        seq,
			Attempt:    attempt,
			TraceID:    traceID,
			StatusCode: 0,
			DurationMS: time.Since(start).Milliseconds(),
			NetworkErr: err.Error(),
			ReceivedAt: time.Now().Format(time.RFC3339Nano),
		})
		cc.mu.Unlock()
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	cc.mu.Lock()
	cc.responses = append(cc.responses, responseLog{
		Seq:        seq,
		Attempt:    attempt,
		TraceID:    traceID,
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		BodyText:   string(body),
		DurationMS: time.Since(start).Milliseconds(),
		ReceivedAt: time.Now().Format(time.RFC3339Nano),
	})

	if spec.Stream {
		cc.streamRaw.WriteString(fmt.Sprintf("### trace=%s url=%s\n", traceID, fullURL))
		cc.streamRaw.Write(body)
		cc.streamRaw.WriteString("\n\n")
	}
	cc.mu.Unlock()

	return &responseResult{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       body,
		TraceID:    traceID,
		URL:        fullURL,
	}, nil
}

func (cc *caseContext) flushArtifacts(cs caseResult) error {
	requestPath := filepath.Join(cc.dir, "request.json")
	headersPath := filepath.Join(cc.dir, "response.headers")
	bodyPath := filepath.Join(cc.dir, "response.body")
	streamPath := filepath.Join(cc.dir, "stream.raw")
	assertPath := filepath.Join(cc.dir, "assertions.json")
	metaPath := filepath.Join(cc.dir, "meta.json")

	if err := writeJSONFile(requestPath, cc.requests); err != nil {
		return err
	}
	respHeaders := make([]map[string]any, 0, len(cc.responses))
	respBodies := make([]map[string]any, 0, len(cc.responses))
	for _, r := range cc.responses {
		respHeaders = append(respHeaders, map[string]any{
			"seq":         r.Seq,
			"attempt":     r.Attempt,
			"trace_id":    r.TraceID,
			"status_code": r.StatusCode,
			"headers":     r.Headers,
		})
		respBodies = append(respBodies, map[string]any{
			"seq":           r.Seq,
			"attempt":       r.Attempt,
			"trace_id":      r.TraceID,
			"status_code":   r.StatusCode,
			"body_text":     r.BodyText,
			"network_error": r.NetworkErr,
			"duration_ms":   r.DurationMS,
		})
	}
	if err := writeJSONFile(headersPath, respHeaders); err != nil {
		return err
	}
	if err := writeJSONFile(bodyPath, respBodies); err != nil {
		return err
	}
	if err := os.WriteFile(streamPath, []byte(cc.streamRaw.String()), 0o644); err != nil {
		return err
	}
	if err := writeJSONFile(assertPath, cc.assertions); err != nil {
		return err
	}
	meta := map[string]any{
		"case_id":       cs.CaseID,
		"trace_id":      strings.Join(cs.TraceIDs, ","),
		"attempt":       len(cc.responses),
		"duration_ms":   cs.DurationMS,
		"status":        map[bool]string{true: "passed", false: "failed"}[cs.Passed],
		"status_codes":  cs.StatusCodes,
		"assertions":    cs.Assertions,
		"artifact_path": cs.ArtifactPath,
	}
	return writeJSONFile(metaPath, meta)
}

type caseDef struct {
	ID  string
	Run func(context.Context, *caseContext) error
}

func (r *Runner) cases() []caseDef {
	return []caseDef{
		{ID: "healthz_ok", Run: r.caseHealthz},
		{ID: "readyz_ok", Run: r.caseReadyz},
		{ID: "models_openai", Run: r.caseModelsOpenAI},
		{ID: "model_openai_by_id", Run: r.caseModelOpenAIByID},
		{ID: "models_claude", Run: r.caseModelsClaude},
		{ID: "admin_login_verify", Run: r.caseAdminLoginVerify},
		{ID: "admin_queue_status", Run: r.caseAdminQueueStatus},
		{ID: "chat_nonstream_basic", Run: r.caseChatNonstream},
		{ID: "chat_stream_basic", Run: r.caseChatStream},
		{ID: "responses_nonstream_basic", Run: r.caseResponsesNonstream},
		{ID: "responses_stream_basic", Run: r.caseResponsesStream},
		{ID: "embeddings_contract", Run: r.caseEmbeddings},
		{ID: "reasoner_stream", Run: r.caseReasonerStream},
		{ID: "toolcall_nonstream", Run: r.caseToolcallNonstream},
		{ID: "toolcall_stream", Run: r.caseToolcallStream},
		{ID: "anthropic_messages_nonstream", Run: r.caseAnthropicNonstream},
		{ID: "anthropic_messages_stream", Run: r.caseAnthropicStream},
		{ID: "anthropic_count_tokens", Run: r.caseAnthropicCountTokens},
		{ID: "admin_account_test_single", Run: r.caseAdminAccountTest},
		{ID: "concurrency_burst", Run: r.caseConcurrencyBurst},
		{ID: "concurrency_threshold_limit", Run: r.caseConcurrencyThresholdLimit},
		{ID: "stream_abort_release", Run: r.caseStreamAbortRelease},
		{ID: "toolcall_stream_mixed", Run: r.caseToolcallStreamMixed},
		{ID: "sse_json_integrity", Run: r.caseSSEJSONIntegrity},
		{ID: "error_contract_invalid_model", Run: r.caseInvalidModel},
		{ID: "error_contract_missing_messages", Run: r.caseMissingMessages},
		{ID: "admin_unauthorized_contract", Run: r.caseAdminUnauthorized},
		{ID: "config_write_isolated", Run: r.caseConfigWriteIsolated},
		{ID: "token_refresh_managed_account", Run: r.caseTokenRefreshManagedAccount},
		{ID: "error_contract_invalid_key", Run: r.caseInvalidKey},
	}
}

func (r *Runner) caseHealthz(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{Method: http.MethodGet, Path: "/healthz", Retryable: true})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	var m map[string]any
	_ = json.Unmarshal(resp.Body, &m)
	cc.assert("status_ok", asString(m["status"]) == "ok", fmt.Sprintf("body=%s", string(resp.Body)))
	return nil
}

func (r *Runner) caseReadyz(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{Method: http.MethodGet, Path: "/readyz", Retryable: true})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	var m map[string]any
	_ = json.Unmarshal(resp.Body, &m)
	cc.assert("status_ready", asString(m["status"]) == "ready", fmt.Sprintf("body=%s", string(resp.Body)))
	return nil
}

func (r *Runner) caseModelsOpenAI(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{Method: http.MethodGet, Path: "/v1/models", Retryable: true})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	ids := extractModelIDs(resp.Body)
	cc.assert("has_deepseek_chat", contains(ids, "deepseek-chat"), strings.Join(ids, ","))
	cc.assert("has_deepseek_reasoner", contains(ids, "deepseek-reasoner"), strings.Join(ids, ","))
	return nil
}

func (r *Runner) caseModelOpenAIByID(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{Method: http.MethodGet, Path: "/v1/models/gpt-4o", Retryable: true})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	var m map[string]any
	_ = json.Unmarshal(resp.Body, &m)
	cc.assert("object_model", asString(m["object"]) == "model", fmt.Sprintf("body=%s", string(resp.Body)))
	cc.assert("id_deepseek_chat", asString(m["id"]) == "deepseek-chat", fmt.Sprintf("body=%s", string(resp.Body)))
	return nil
}

func (r *Runner) caseModelsClaude(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{Method: http.MethodGet, Path: "/anthropic/v1/models", Retryable: true})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	ids := extractModelIDs(resp.Body)
	cc.assert("non_empty", len(ids) > 0, fmt.Sprintf("models=%v", ids))
	return nil
}

func (r *Runner) caseAdminLoginVerify(ctx context.Context, cc *caseContext) error {
	loginResp, err := cc.request(ctx, requestSpec{
		Method:    http.MethodPost,
		Path:      "/admin/login",
		Body:      map[string]any{"admin_key": r.adminKey, "expire_hours": 24},
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("login_status_200", loginResp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", loginResp.StatusCode))
	var payload map[string]any
	_ = json.Unmarshal(loginResp.Body, &payload)
	token := asString(payload["token"])
	cc.assert("token_exists", token != "", fmt.Sprintf("body=%s", string(loginResp.Body)))
	if token == "" {
		return nil
	}
	verifyResp, err := cc.request(ctx, requestSpec{
		Method: http.MethodGet,
		Path:   "/admin/verify",
		Headers: map[string]string{
			"Authorization": "Bearer " + token,
		},
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("verify_status_200", verifyResp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", verifyResp.StatusCode))
	var v map[string]any
	_ = json.Unmarshal(verifyResp.Body, &v)
	valid, _ := v["valid"].(bool)
	cc.assert("verify_valid_true", valid, fmt.Sprintf("body=%s", string(verifyResp.Body)))
	return nil
}

func (r *Runner) caseAdminQueueStatus(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodGet,
		Path:   "/admin/queue/status",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.adminJWT,
		},
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	var m map[string]any
	_ = json.Unmarshal(resp.Body, &m)
	_, hasRec := m["recommended_concurrency"]
	_, hasQueue := m["max_queue_size"]
	cc.assert("has_recommended_concurrency", hasRec, fmt.Sprintf("body=%s", string(resp.Body)))
	cc.assert("has_max_queue_size", hasQueue, fmt.Sprintf("body=%s", string(resp.Body)))
	return nil
}

func (r *Runner) caseChatNonstream(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.apiKey,
		},
		Body: map[string]any{
			"model": "deepseek-chat",
			"messages": []map[string]any{
				{"role": "user", "content": "请简单回复一句话"},
			},
			"stream": false,
		},
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	var m map[string]any
	_ = json.Unmarshal(resp.Body, &m)
	cc.assert("object_chat_completion", asString(m["object"]) == "chat.completion", fmt.Sprintf("body=%s", string(resp.Body)))
	choices, _ := m["choices"].([]any)
	cc.assert("choices_non_empty", len(choices) > 0, fmt.Sprintf("body=%s", string(resp.Body)))
	return nil
}

func (r *Runner) caseChatStream(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.apiKey,
		},
		Body: map[string]any{
			"model": "deepseek-chat",
			"messages": []map[string]any{
				{"role": "user", "content": "请流式回复一句话"},
			},
			"stream": true,
		},
		Stream:    true,
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	frames, done := parseSSEFrames(resp.Body)
	cc.assert("frames_non_empty", len(frames) > 0, fmt.Sprintf("len=%d", len(frames)))
	cc.assert("done_terminated", done, "expected [DONE]")
	return nil
}

func (r *Runner) caseResponsesNonstream(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/responses",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.apiKey,
		},
		Body: map[string]any{
			"model": "gpt-4o",
			"input": "请简要回答 hello",
		},
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	var m map[string]any
	_ = json.Unmarshal(resp.Body, &m)
	cc.assert("object_response", asString(m["object"]) == "response", fmt.Sprintf("body=%s", string(resp.Body)))
	responseID := asString(m["id"])
	cc.assert("response_id_present", responseID != "", fmt.Sprintf("body=%s", string(resp.Body)))
	if responseID != "" {
		getResp, getErr := cc.request(ctx, requestSpec{
			Method: http.MethodGet,
			Path:   "/v1/responses/" + responseID,
			Headers: map[string]string{
				"Authorization": "Bearer " + r.apiKey,
			},
			Retryable: true,
		})
		if getErr != nil {
			return getErr
		}
		cc.assert("get_status_200", getResp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", getResp.StatusCode))
	}
	return nil
}

func (r *Runner) caseResponsesStream(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/responses",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.apiKey,
		},
		Body: map[string]any{
			"model":  "gpt-4o",
			"input":  "请流式回答 hello",
			"stream": true,
		},
		Stream:    true,
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	frames, done := parseSSEFrames(resp.Body)
	cc.assert("frames_non_empty", len(frames) > 0, fmt.Sprintf("len=%d", len(frames)))
	hasCreated := false
	hasCompleted := false
	for _, f := range frames {
		switch asString(f["type"]) {
		case "response.created":
			hasCreated = true
		case "response.completed":
			hasCompleted = true
		}
	}
	cc.assert("has_response_created", hasCreated, fmt.Sprintf("body=%s", string(resp.Body)))
	cc.assert("has_response_completed", hasCompleted, fmt.Sprintf("body=%s", string(resp.Body)))
	cc.assert("done_terminated", done, "expected [DONE]")
	return nil
}

func (r *Runner) caseEmbeddings(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/embeddings",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.apiKey,
		},
		Body: map[string]any{
			"model": "gpt-4o",
			"input": []string{"hello", "world"},
		},
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("status_200_or_501", resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotImplemented, fmt.Sprintf("status=%d", resp.StatusCode))
	var m map[string]any
	_ = json.Unmarshal(resp.Body, &m)
	if resp.StatusCode == http.StatusOK {
		cc.assert("object_list", asString(m["object"]) == "list", fmt.Sprintf("body=%s", string(resp.Body)))
		data, _ := m["data"].([]any)
		cc.assert("data_non_empty", len(data) > 0, fmt.Sprintf("body=%s", string(resp.Body)))
		return nil
	}
	errObj, _ := m["error"].(map[string]any)
	_, hasCode := errObj["code"]
	_, hasParam := errObj["param"]
	cc.assert("error_has_code", hasCode, fmt.Sprintf("body=%s", string(resp.Body)))
	cc.assert("error_has_param", hasParam, fmt.Sprintf("body=%s", string(resp.Body)))
	return nil
}

func (r *Runner) caseReasonerStream(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.apiKey,
		},
		Body: map[string]any{
			"model": "deepseek-reasoner",
			"messages": []map[string]any{
				{"role": "user", "content": "先思考后回答：1+1"},
			},
			"stream": true,
		},
		Stream:    true,
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	frames, done := parseSSEFrames(resp.Body)
	hasReasoning := false
	for _, f := range frames {
		choices, _ := f["choices"].([]any)
		for _, c := range choices {
			ch, _ := c.(map[string]any)
			delta, _ := ch["delta"].(map[string]any)
			if asString(delta["reasoning_content"]) != "" {
				hasReasoning = true
			}
		}
	}
	cc.assert("has_reasoning_content", hasReasoning, "reasoning_content not found")
	cc.assert("done_terminated", done, "expected [DONE]")
	return nil
}

func (r *Runner) caseToolcallNonstream(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.apiKey,
		},
		Body:      toolcallPayload(false),
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	var m map[string]any
	_ = json.Unmarshal(resp.Body, &m)
	choices, _ := m["choices"].([]any)
	if len(choices) == 0 {
		cc.assert("choices_non_empty", false, fmt.Sprintf("body=%s", string(resp.Body)))
		return nil
	}
	c0, _ := choices[0].(map[string]any)
	cc.assert("finish_reason_tool_calls", asString(c0["finish_reason"]) == "tool_calls", fmt.Sprintf("body=%s", string(resp.Body)))
	msg, _ := c0["message"].(map[string]any)
	tc, _ := msg["tool_calls"].([]any)
	cc.assert("tool_calls_present", len(tc) > 0, fmt.Sprintf("body=%s", string(resp.Body)))
	return nil
}

func (r *Runner) caseToolcallStream(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.apiKey,
		},
		Body:      toolcallPayload(true),
		Stream:    true,
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	frames, done := parseSSEFrames(resp.Body)
	hasTool := false
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
			if strings.Contains(strings.ToLower(content), `"tool_calls"`) {
				rawLeak = true
			}
		}
	}
	cc.assert("tool_calls_delta_present", hasTool, "tool_calls delta missing")
	cc.assert("no_raw_tool_json_leak", !rawLeak, "raw tool_calls JSON leaked in content")
	cc.assert("done_terminated", done, "expected [DONE]")
	return nil
}

func (r *Runner) caseAnthropicNonstream(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/anthropic/v1/messages",
		Headers: map[string]string{
			"Authorization":     "Bearer " + r.apiKey,
			"anthropic-version": "2023-06-01",
			"content-type":      "application/json",
		},
		Body: map[string]any{
			"model": "claude-sonnet-4-5",
			"messages": []map[string]any{
				{"role": "user", "content": "hello"},
			},
			"stream": false,
		},
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	var m map[string]any
	_ = json.Unmarshal(resp.Body, &m)
	cc.assert("type_message", asString(m["type"]) == "message", fmt.Sprintf("body=%s", string(resp.Body)))
	return nil
}

func (r *Runner) caseAnthropicStream(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/anthropic/v1/messages",
		Headers: map[string]string{
			"Authorization":     "Bearer " + r.apiKey,
			"anthropic-version": "2023-06-01",
			"content-type":      "application/json",
		},
		Body: map[string]any{
			"model": "claude-sonnet-4-5",
			"messages": []map[string]any{
				{"role": "user", "content": "stream hello"},
			},
			"stream": true,
		},
		Stream:    true,
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	events := parseClaudeStreamEvents(resp.Body)
	cc.assert("has_message_start", contains(events, "message_start"), fmt.Sprintf("events=%v", events))
	cc.assert("has_message_stop", contains(events, "message_stop"), fmt.Sprintf("events=%v", events))
	return nil
}

func (r *Runner) caseAnthropicCountTokens(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/anthropic/v1/messages/count_tokens",
		Headers: map[string]string{
			"Authorization":     "Bearer " + r.apiKey,
			"anthropic-version": "2023-06-01",
			"content-type":      "application/json",
		},
		Body: map[string]any{
			"model": "claude-sonnet-4-5",
			"messages": []map[string]any{
				{"role": "user", "content": "count me"},
			},
		},
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	var m map[string]any
	_ = json.Unmarshal(resp.Body, &m)
	v := toInt(m["input_tokens"])
	cc.assert("input_tokens_gt_zero", v > 0, fmt.Sprintf("body=%s", string(resp.Body)))
	return nil
}

func (r *Runner) caseAdminAccountTest(ctx context.Context, cc *caseContext) error {
	if strings.TrimSpace(r.accountID) == "" {
		cc.assert("account_present", false, "no account in config")
		return nil
	}
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/admin/accounts/test",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.adminJWT,
		},
		Body: map[string]any{
			"identifier": r.accountID,
			"model":      "deepseek-chat",
			"message":    "ping",
		},
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	var m map[string]any
	_ = json.Unmarshal(resp.Body, &m)
	ok, _ := m["success"].(bool)
	cc.assert("success_true", ok, fmt.Sprintf("body=%s", string(resp.Body)))
	return nil
}

func (r *Runner) caseConcurrencyBurst(ctx context.Context, cc *caseContext) error {
	accountCount := len(r.configRaw.Accounts)
	n := accountCount*2 + 2
	if n < 2 {
		n = 2
	}
	type one struct {
		Status int
		Err    string
	}
	results := make([]one, n)
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
						{"role": "user", "content": fmt.Sprintf("并发请求 #%d，请回复ok", idx)},
					},
					"stream": true,
				},
				Stream:    true,
				Retryable: true,
			})
			if err != nil {
				results[idx] = one{Err: err.Error()}
				return
			}
			results[idx] = one{Status: resp.StatusCode}
		}(i)
	}
	wg.Wait()

	dist := map[int]int{}
	success := 0
	for _, it := range results {
		if it.Status > 0 {
			dist[it.Status]++
			if it.Status == http.StatusOK {
				success++
			}
		}
	}
	cc.assert("success_gt_zero", success > 0, fmt.Sprintf("distribution=%v", dist))
	_, has5xx := has5xx(dist)
	cc.assert("no_5xx", !has5xx, fmt.Sprintf("distribution=%v", dist))
	if err := r.ping("/healthz"); err != nil {
		cc.assert("server_alive", false, err.Error())
	} else {
		cc.assert("server_alive", true, "")
	}
	return nil
}

func (r *Runner) caseConfigWriteIsolated(ctx context.Context, cc *caseContext) error {
	k := "testsuite-temp-" + sanitizeID(r.runID)
	add, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/admin/keys",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.adminJWT,
		},
		Body:      map[string]any{"key": k},
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("add_key_status_200", add.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", add.StatusCode))

	cfg1, err := cc.request(ctx, requestSpec{
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
	containsAdded := strings.Contains(string(cfg1.Body), k)
	cc.assert("key_present_in_isolated_config", containsAdded, "added key not found in isolated config")

	delPath := "/admin/keys/" + url.PathEscape(k)
	del, err := cc.request(ctx, requestSpec{
		Method: http.MethodDelete,
		Path:   delPath,
		Headers: map[string]string{
			"Authorization": "Bearer " + r.adminJWT,
		},
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("delete_key_status_200", del.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", del.StatusCode))

	cfg2, err := cc.request(ctx, requestSpec{
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
	cc.assert("key_removed_in_isolated_config", !strings.Contains(string(cfg2.Body), k), "temporary key still present")

	if err := r.ensureOriginalConfigUntouched(); err != nil {
		cc.assert("original_config_unchanged", false, err.Error())
	} else {
		cc.assert("original_config_unchanged", true, "")
	}
	return nil
}

func (r *Runner) caseInvalidKey(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Headers: map[string]string{
			"Authorization": "Bearer invalid-testsuite-key-" + sanitizeID(r.runID),
		},
		Body: map[string]any{
			"model": "deepseek-chat",
			"messages": []map[string]any{
				{"role": "user", "content": "hi"},
			},
			"stream": false,
		},
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("status_401", resp.StatusCode == http.StatusUnauthorized, fmt.Sprintf("status=%d", resp.StatusCode))
	var m map[string]any
	_ = json.Unmarshal(resp.Body, &m)
	e, _ := m["error"].(map[string]any)
	cc.assert("error_object_present", len(e) > 0, fmt.Sprintf("body=%s", string(resp.Body)))
	cc.assert("error_message_present", asString(e["message"]) != "", fmt.Sprintf("body=%s", string(resp.Body)))
	return nil
}

func (r *Runner) doSimpleJSON(ctx context.Context, method, path string, headers map[string]string, body any) (*responseResult, error) {
	cc := &caseContext{
		runner:      r,
		id:          "auth_prepare",
		traceIDsSet: map[string]struct{}{},
	}
	return cc.request(ctx, requestSpec{
		Method:    method,
		Path:      path,
		Headers:   headers,
		Body:      body,
		Retryable: true,
	})
}

func (r *Runner) writeSummary(start, end time.Time) error {
	passed := 0
	failed := 0
	for _, cs := range r.results {
		if cs.Passed {
			passed++
		} else {
			failed++
		}
	}
	summary := runSummary{
		RunID:      r.runID,
		StartedAt:  start.Format(time.RFC3339Nano),
		EndedAt:    end.Format(time.RFC3339Nano),
		DurationMS: end.Sub(start).Milliseconds(),
		Stats: map[string]any{
			"total":  len(r.results),
			"passed": passed,
			"failed": failed,
		},
		Environment: map[string]any{
			"go_version":      runtime.Version(),
			"os":              runtime.GOOS,
			"arch":            runtime.GOARCH,
			"base_url":        r.baseURL,
			"config_source":   r.originalConfigPath,
			"config_isolated": r.configCopyPath,
			"server_log":      r.serverLog,
			"preflight_log":   r.preflightLog,
			"retries":         r.opts.Retries,
			"timeout_seconds": int(r.opts.Timeout.Seconds()),
		},
		Cases:    r.results,
		Warnings: r.warnings,
	}
	if err := writeJSONFile(filepath.Join(r.runDir, "summary.json"), summary); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.runDir, "summary.md"), []byte(r.summaryMarkdown(summary)), 0o644)
}

func (r *Runner) summaryMarkdown(s runSummary) string {
	var b strings.Builder
	b.WriteString("# DS2API Live Testsuite Summary\n\n")
	b.WriteString("**Sensitive Notice:** this run stores full raw request/response logs. Do not share artifacts publicly.\n\n")
	fmt.Fprintf(&b, "- Run ID: `%s`\n", s.RunID)
	fmt.Fprintf(&b, "- Started: `%s`\n", s.StartedAt)
	fmt.Fprintf(&b, "- Ended: `%s`\n", s.EndedAt)
	fmt.Fprintf(&b, "- Duration: `%d ms`\n", s.DurationMS)
	fmt.Fprintf(&b, "- Passed/Failed: `%d/%d`\n\n", s.Stats["passed"], s.Stats["failed"])
	if len(s.Warnings) > 0 {
		b.WriteString("## Warnings\n\n")
		for _, w := range s.Warnings {
			fmt.Fprintf(&b, "- %s\n", w)
		}
		b.WriteString("\n")
	}
	b.WriteString("## Failed Cases\n\n")
	hasFailed := false
	for _, c := range s.Cases {
		if c.Passed {
			continue
		}
		hasFailed = true
		fmt.Fprintf(&b, "- `%s`: %s\n", c.CaseID, c.Error)
		if len(c.TraceIDs) > 0 {
			fmt.Fprintf(&b, "  - trace_ids: `%s`\n", strings.Join(c.TraceIDs, ", "))
			fmt.Fprintf(&b, "  - grep: `rg \"%s\" %s`\n", c.TraceIDs[0], filepath.Join(r.runDir, "server.log"))
		}
		fmt.Fprintf(&b, "  - artifact: `%s`\n", c.ArtifactPath)
	}
	if !hasFailed {
		b.WriteString("- none\n")
	}
	b.WriteString("\n## Case Table\n\n")
	b.WriteString("| case_id | status | duration_ms | statuses | artifact |\n")
	b.WriteString("|---|---:|---:|---|---|\n")
	for _, c := range s.Cases {
		status := "PASS"
		if !c.Passed {
			status = "FAIL"
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %v | `%s` |\n", c.CaseID, status, c.DurationMS, c.StatusCodes, c.ArtifactPath)
	}
	return b.String()
}

func toolcallPayload(stream bool) map[string]any {
	return map[string]any{
		"model": "deepseek-chat",
		"messages": []map[string]any{
			{
				"role":    "user",
				"content": "你必须调用工具 search 查询 golang，并仅返回工具调用。",
			},
		},
		"tools": []map[string]any{
			{
				"type": "function",
				"function": map[string]any{
					"name":        "search",
					"description": "search documents",
					"parameters": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"q": map[string]any{
								"type": "string",
							},
						},
						"required": []string{"q"},
					},
				},
			},
		},
		"stream": stream,
	}
}

func parseSSEFrames(body []byte) ([]map[string]any, bool) {
	lines := strings.Split(string(body), "\n")
	frames := make([]map[string]any, 0, len(lines))
	done := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			done = true
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(payload), &m); err == nil {
			frames = append(frames, m)
		}
	}
	return frames, done
}

func parseClaudeStreamEvents(body []byte) []string {
	events := []string{}
	seen := map[string]bool{}
	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(payload), &m); err != nil {
			continue
		}
		t := asString(m["type"])
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		events = append(events, t)
	}
	return events
}

func extractModelIDs(body []byte) []string {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil
	}
	out := []string{}
	data, _ := m["data"].([]any)
	for _, it := range data {
		item, _ := it.(map[string]any)
		id := asString(item["id"])
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

func withTraceQuery(rawURL, traceID string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("__trace_id", traceID)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func writeJSONFile(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func prepareServerEnv(base []string, overrides map[string]string) []string {
	out := make([]string, 0, len(base)+len(overrides))
	skip := map[string]struct{}{}
	for k := range overrides {
		skip[k] = struct{}{}
	}
	for _, e := range base {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if _, ok := skip[parts[0]]; ok {
			continue
		}
		out = append(out, e)
	}
	for k, v := range overrides {
		out = append(out, k+"="+v)
	}
	return out
}

func findFreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("failed to detect tcp port")
	}
	return addr.Port, nil
}

func uniqueStatusCodes(in []responseLog) []int {
	set := map[int]struct{}{}
	for _, it := range in {
		if it.StatusCode > 0 {
			set[it.StatusCode] = struct{}{}
		}
	}
	out := make([]int, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

func has5xx(dist map[int]int) (int, bool) {
	for k := range dist {
		if k >= 500 {
			return k, true
		}
	}
	return 0, false
}

func sanitizeID(s string) string {
	s = strings.ReplaceAll(s, ":", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, " ", "_")
	return s
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func toInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case float32:
		return int(x)
	case int:
		return x
	case int64:
		return int(x)
	default:
		return 0
	}
}

func contains(xs []string, target string) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}
	return false
}
