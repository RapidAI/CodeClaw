package webui

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"

	"ds2api/internal/config"
)

const welcomeHTML = `<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"><title>DS2API</title>
<style>body{font-family:Inter,system-ui,sans-serif;background:#030712;color:#f9fafb;display:flex;min-height:100vh;align-items:center;justify-content:center;margin:0}a{color:#f59e0b;text-decoration:none}main{max-width:700px;padding:24px;text-align:center}h1{font-size:48px;margin:0 0 12px}.links{display:flex;gap:16px;justify-content:center;margin-top:20px;flex-wrap:wrap}</style>
</head><body><main><h1>DS2API</h1><p>DeepSeek to OpenAI & Claude Compatible API</p><div class="links"><a href="/admin">管理面板</a><a href="/v1/models">API 状态</a><a href="https://github.com/CJackHwang/ds2api" target="_blank">GitHub</a></div></main></body></html>`

type Handler struct {
	StaticDir string
}

func NewHandler() *Handler {
	return &Handler{StaticDir: resolveStaticAdminDir(config.StaticAdminDir())}
}

func RegisterRoutes(r chi.Router, h *Handler) {
	r.Get("/", h.index)
	r.Get("/admin", h.admin)
}

func (h *Handler) HandleAdminFallback(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	if !strings.HasPrefix(r.URL.Path, "/admin/") {
		return false
	}
	h.admin(w, r)
	return true
}

func (h *Handler) index(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(welcomeHTML))
}

func (h *Handler) admin(w http.ResponseWriter, r *http.Request) {
	staticDir := resolveStaticAdminDir(h.StaticDir)
	if fi, err := os.Stat(staticDir); err == nil && fi.IsDir() {
		h.serveFromDisk(w, r, staticDir)
		return
	}
	http.Error(w, "WebUI not built. Run `cd webui && npm run build` first.", http.StatusNotFound)
}

func (h *Handler) serveFromDisk(w http.ResponseWriter, r *http.Request, staticDir string) {
	path := strings.TrimPrefix(r.URL.Path, "/admin")
	path = strings.TrimPrefix(path, "/")
	if path != "" && strings.Contains(path, ".") {
		full := filepath.Join(staticDir, filepath.Clean(path))
		if !strings.HasPrefix(full, staticDir) {
			http.NotFound(w, r)
			return
		}
		if _, err := os.Stat(full); err == nil {
			if strings.HasPrefix(path, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-store, must-revalidate")
			}
			http.ServeFile(w, r, full)
			return
		}
		http.NotFound(w, r)
		return
	}
	index := filepath.Join(staticDir, "index.html")
	if _, err := os.Stat(index); err != nil {
		http.Error(w, "index.html not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Cache-Control", "no-store, must-revalidate")
	http.ServeFile(w, r, index)
}

func resolveStaticAdminDir(preferred string) string {
	if strings.TrimSpace(os.Getenv("DS2API_STATIC_ADMIN_DIR")) != "" {
		return filepath.Clean(preferred)
	}
	candidates := []string{preferred}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "static/admin"))
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, "static/admin"),
			filepath.Join(filepath.Dir(exeDir), "static/admin"),
		)
	}
	// Common serverless locations.
	candidates = append(candidates, "/var/task/static/admin", "/var/task/user/static/admin")

	seen := map[string]struct{}{}
	for _, c := range candidates {
		c = filepath.Clean(strings.TrimSpace(c))
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			return c
		}
	}
	return filepath.Clean(preferred)
}
