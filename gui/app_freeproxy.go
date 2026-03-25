package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/freeproxy"
)

var (
	freeProxyServer *freeproxy.Server
	freeProxyMu     sync.Mutex
	freeProxyCancel context.CancelFunc
)

const (
	freeProxyAddr    = ":18099"
	freeProviderName = "免费"
)

var freeProxyConfigOnce sync.Once
var freeProxyConfigPath string

// freeProxyConfigDir returns the directory for persisting dangbei auth data.
func freeProxyConfigDir() string {
	freeProxyConfigOnce.Do(func() {
		home, _ := os.UserHomeDir()
		freeProxyConfigPath = filepath.Join(home, ".maclaw", "freeproxy")
		os.MkdirAll(freeProxyConfigPath, 0700)
	})
	return freeProxyConfigPath
}

// StartFreeProxy starts the local free proxy server backed by 当贝 AI.
// It waits briefly to confirm the server actually bound the port before returning.
func (a *App) StartFreeProxy() (string, error) {
	freeProxyMu.Lock()
	defer freeProxyMu.Unlock()

	if freeProxyServer != nil {
		return "already running", nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	srv := freeproxy.NewServer(freeProxyAddr, freeProxyConfigDir())

	// Use a channel to get the startup result from the goroutine.
	startErr := make(chan error, 1)
	go func() {
		err := srv.Start(ctx)
		startErr <- err // signal startup result (nil if shut down normally)
		freeProxyMu.Lock()
		freeProxyServer = nil
		freeProxyCancel = nil
		freeProxyMu.Unlock()
	}()

	// Wait briefly for the server to either start listening or fail.
	// Server.Start calls net.Listen synchronously before entering Serve loop,
	// so if it fails (port in use), the error comes back quickly.
	select {
	case err := <-startErr:
		// Server exited immediately — startup failed
		cancel()
		if err != nil {
			return "", fmt.Errorf("代理启动失败: %w", err)
		}
		return "", fmt.Errorf("代理启动失败: 服务器意外退出")
	case <-time.After(300 * time.Millisecond):
		// Server is still running after 300ms — assume it started OK
		freeProxyServer = srv
		freeProxyCancel = cancel
		return "started on " + freeProxyAddr, nil
	}
}

// StopFreeProxy stops the local free proxy server.
func (a *App) StopFreeProxy() string {
	freeProxyMu.Lock()
	defer freeProxyMu.Unlock()

	if freeProxyCancel != nil {
		freeProxyCancel()
		freeProxyCancel = nil
	}
	if freeProxyServer != nil {
		freeProxyServer.Stop()
		freeProxyServer = nil
	}
	return "stopped"
}

// IsFreeProxyRunning returns whether the free proxy server is running.
func (a *App) IsFreeProxyRunning() bool {
	freeProxyMu.Lock()
	defer freeProxyMu.Unlock()
	return freeProxyServer != nil
}

// IsDangbeiLoggedIn returns whether the user has a valid 当贝 AI cookie.
func (a *App) IsDangbeiLoggedIn() bool {
	freeProxyMu.Lock()
	srv := freeProxyServer
	freeProxyMu.Unlock()

	if srv != nil {
		return srv.Auth().HasAuth()
	}
	// Check persisted auth even if server isn't running
	auth := freeproxy.NewAuthStore(freeProxyConfigDir())
	auth.Load()
	return auth.HasAuth()
}

// DangbeiEnsureAuth checks if a valid persisted cookie exists.
// Returns "authenticated" if the cookie is valid, "need_login" otherwise.
// This allows the frontend to skip the browser login flow when a valid cookie exists.
func (a *App) DangbeiEnsureAuth() string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// If server is running and has a cookie, validate it directly
	freeProxyMu.Lock()
	srv := freeProxyServer
	freeProxyMu.Unlock()

	if srv != nil && srv.Auth().HasAuth() {
		if srv.Client().IsAuthenticated(ctx) {
			return "authenticated"
		}
		// Server cookie is stale — fall through to try persisted cookie
	}

	// Try loading persisted cookie from disk
	auth := freeproxy.NewAuthStore(freeProxyConfigDir())
	if err := auth.Load(); err != nil || !auth.HasAuth() {
		return "need_login"
	}

	// Skip re-validation if the persisted cookie is the same as the server's
	// (already proven invalid above)
	if srv != nil && srv.Auth().GetCookie() == auth.GetCookie() {
		return "need_login"
	}

	// Validate the persisted cookie via API
	client := freeproxy.NewDangbeiClient(auth)
	if !client.IsAuthenticated(ctx) {
		return "need_login"
	}

	// Cookie is valid — sync to running server if needed
	if srv != nil {
		srv.Auth().SetCookie(auth.GetCookie())
	}

	return "authenticated"
}

// ensureFreeProxyIfNeeded starts the free proxy server if the current
// LLM provider is "免费" and the server is not already running.
// It also auto-loads persisted cookies so the user doesn't need to
// re-login every time the app starts.
func (a *App) ensureFreeProxyIfNeeded() {
	data := a.GetMaclawLLMProviders()
	if data.Current != freeProviderName {
		return
	}
	if a.IsFreeProxyRunning() {
		return
	}
	// Try to load persisted cookie and start proxy automatically
	auth := freeproxy.NewAuthStore(freeProxyConfigDir())
	if err := auth.Load(); err == nil && auth.HasAuth() {
		a.StartFreeProxy()
		// Sync persisted model selection to the running server
		freeProxyMu.Lock()
		srv := freeProxyServer
		freeProxyMu.Unlock()
		if srv != nil {
			if m := a.GetFreeProxyModel(); m != "" {
				srv.SetDefaultModel(m)
			}
		}
	}
}

// GetFreeProxyModels returns the available models for the free proxy (当贝 AI).
func (a *App) GetFreeProxyModels() []map[string]string {
	models := freeproxy.AvailableModels()
	result := make([]map[string]string, len(models))
	for i, m := range models {
		result[i] = map[string]string{"id": m.ID, "name": m.Name}
	}
	return result
}

// GetFreeProxyModel returns the currently selected free proxy model ID.
func (a *App) GetFreeProxyModel() string {
	cfg, err := a.LoadConfig()
	if err != nil || cfg.FreeProxyModel == "" {
		return "deepseek_r1"
	}
	return cfg.FreeProxyModel
}

// SetFreeProxyModel persists the selected model and syncs to the running server.
func (a *App) SetFreeProxyModel(modelID string) error {
	// Validate model ID
	valid := false
	for _, m := range freeproxy.AvailableModels() {
		if m.ID == modelID {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("unknown model: %s", modelID)
	}

	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	cfg.FreeProxyModel = modelID
	if err := a.SaveConfig(cfg); err != nil {
		return err
	}
	// Sync to running server
	freeProxyMu.Lock()
	srv := freeProxyServer
	freeProxyMu.Unlock()
	if srv != nil {
		srv.SetDefaultModel(modelID)
	}
	return nil
}

// DetectBrowser returns info about the detected browser (Chrome/Edge).
func (a *App) DetectBrowser() map[string]string {
	bi := freeproxy.DetectBrowser()
	if bi == nil {
		return map[string]string{"found": "false"}
	}
	return map[string]string{
		"found": "true",
		"name":  bi.Name,
		"path":  bi.Path,
	}
}

// DangbeiLogin launches a dedicated browser for the user to log in to 当贝 AI.
// Returns "ok" on success. Wails bindings work more reliably with (string, error)
// than bare error returns.
func (a *App) DangbeiLogin() (string, error) {
	if err := freeproxy.LoginViaBrowser(); err != nil {
		return "", err
	}
	return "ok", nil
}

// DangbeiFinishLogin extracts cookies from the browser profile after user login,
// saves them, and optionally syncs to the running proxy server.
func (a *App) DangbeiFinishLogin() (string, error) {
	cookie, err := freeproxy.FinishLogin()
	if err != nil {
		return "", fmt.Errorf("提取 cookie 失败: %w", err)
	}

	// Save cookie to the running server's auth store (if running)
	freeProxyMu.Lock()
	srv := freeProxyServer
	freeProxyMu.Unlock()

	if srv != nil {
		srv.Auth().SetCookie(cookie)
		if err := srv.Auth().Save(); err != nil {
			return "", fmt.Errorf("保存登录信息失败: %w", err)
		}
	} else {
		// Save to disk even if server isn't running yet
		auth := freeproxy.NewAuthStore(freeProxyConfigDir())
		auth.SetCookie(cookie)
		if err := auth.Save(); err != nil {
			return "", fmt.Errorf("保存登录信息失败: %w", err)
		}
	}

	return "登录成功", nil
}

// DetectChrome is kept for backward compatibility. Returns Chrome path or "".
func (a *App) DetectChrome() string {
	bi := freeproxy.DetectBrowser()
	if bi == nil {
		return ""
	}
	return bi.Path
}

// LaunchChromeDebug is kept for backward compatibility.
// Now launches a dedicated browser instance for 当贝 login.
func (a *App) LaunchChromeDebug() (string, error) {
	return a.DangbeiLogin()
}
