package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func registerStaticRoutes(mux *http.ServeMux, staticDir string, routePrefix string) {
	staticDir = strings.TrimSpace(staticDir)
	if staticDir == "" {
		return
	}
	if !strings.HasPrefix(routePrefix, "/") {
		routePrefix = "/" + routePrefix
	}
	routePrefix = strings.TrimRight(routePrefix, "/")
	indexPath := filepath.Join(staticDir, "index.html")

	mux.HandleFunc("GET "+routePrefix, func(w http.ResponseWriter, r *http.Request) {
		serveStaticIndexFallback(w, r, staticDir, indexPath, routePrefix)
	})
	mux.HandleFunc("GET "+routePrefix+"/{rest...}", func(w http.ResponseWriter, r *http.Request) {
		serveStaticIndexFallback(w, r, staticDir, indexPath, routePrefix)
	})
}

// registerAdminStaticRoutes is an alias for registerStaticRoutes with /admin default.
func registerAdminStaticRoutes(mux *http.ServeMux, staticDir string, routePrefix string) {
	if routePrefix == "" {
		routePrefix = "/admin"
	}
	registerStaticRoutes(mux, staticDir, routePrefix)
}

func serveStaticIndexFallback(w http.ResponseWriter, r *http.Request, staticDir string, indexPath string, routePrefix string) {
	relPath := strings.TrimPrefix(r.URL.Path, routePrefix)
	relPath = strings.TrimPrefix(relPath, "/")
	if relPath == "" {
		relPath = "index.html"
	}

	candidate := filepath.Join(staticDir, filepath.FromSlash(relPath))
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		http.ServeFile(w, r, candidate)
		return
	}

	if _, err := os.Stat(indexPath); err == nil {
		http.ServeFile(w, r, indexPath)
		return
	}

	http.NotFound(w, r)
}
