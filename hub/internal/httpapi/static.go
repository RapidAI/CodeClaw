package httpapi

import (
    "net/http"
    "os"
    "path/filepath"
    "strings"

    "github.com/RapidAI/CodeClaw/corelib/brand"
)

func registerPWAStaticRoutes(mux *http.ServeMux, staticDir string, routePrefix string) {
    registerStaticRoutes(mux, staticDir, routePrefix)
}

func registerAdminStaticRoutes(mux *http.ServeMux, staticDir string, routePrefix string) {
    brandName := brand.Current().DisplayName
    if brandName == "" || brandName == "MaClaw" {
        // Default brand — serve files as-is, no replacement needed.
        registerStaticRoutes(mux, staticDir, routePrefix)
        return
    }

    staticDir = strings.TrimSpace(staticDir)
    if staticDir == "" {
        return
    }
    if routePrefix == "" {
        routePrefix = "/admin"
    }
    if !strings.HasPrefix(routePrefix, "/") {
        routePrefix = "/" + routePrefix
    }
    routePrefix = strings.TrimRight(routePrefix, "/")
    indexPath := filepath.Join(staticDir, "index.html")

    serve := func(w http.ResponseWriter, r *http.Request) {
        serveAdminBranded(w, r, staticDir, indexPath, routePrefix, brandName)
    }
    mux.HandleFunc("GET "+routePrefix, serve)
    mux.HandleFunc("GET "+routePrefix+"/{rest...}", serve)
}

// serveAdminBranded serves admin static files, replacing "MaClaw" with the
// current brand name in index.html responses.
func serveAdminBranded(w http.ResponseWriter, r *http.Request, staticDir, indexPath, routePrefix, brandName string) {
    relPath := strings.TrimPrefix(r.URL.Path, routePrefix)
    relPath = strings.TrimPrefix(relPath, "/")

    // For non-index static assets, serve directly.
    if relPath != "" {
        candidate := filepath.Join(staticDir, filepath.FromSlash(relPath))
        if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
            http.ServeFile(w, r, candidate)
            return
        }
        ext := strings.ToLower(filepath.Ext(relPath))
        if staticAssetExtensions[ext] {
            http.NotFound(w, r)
            return
        }
    }

    // Serve index.html with brand replacement.
    data, err := os.ReadFile(indexPath)
    if err != nil {
        http.NotFound(w, r)
        return
    }
    replaced := strings.ReplaceAll(string(data), "MaClaw", brandName)
    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    w.Write([]byte(replaced))
}

func registerBindStaticRoutes(mux *http.ServeMux, staticDir string, routePrefix string) {
    staticDir = strings.TrimSpace(staticDir)
    if staticDir == "" {
        return
    }
    if routePrefix == "" {
        routePrefix = "/bind"
    }
    if !strings.HasPrefix(routePrefix, "/") {
        routePrefix = "/" + routePrefix
    }
    routePrefix = strings.TrimRight(routePrefix, "/")
    indexPath := filepath.Join(staticDir, "index.html")

    allowIframe := func(next http.HandlerFunc) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
            w.Header().Set("X-Frame-Options", "ALLOWALL")
            w.Header().Set("Content-Security-Policy", "frame-ancestors *")
            w.Header().Set("Access-Control-Allow-Origin", "*")
            next(w, r)
        }
    }

    mux.HandleFunc("GET "+routePrefix, allowIframe(func(w http.ResponseWriter, r *http.Request) {
        serveStaticIndexFallback(w, r, staticDir, indexPath, routePrefix)
    }))
    mux.HandleFunc("GET "+routePrefix+"/{rest...}", allowIframe(func(w http.ResponseWriter, r *http.Request) {
        serveStaticIndexFallback(w, r, staticDir, indexPath, routePrefix)
    }))
}

func registerStaticRoutes(mux *http.ServeMux, staticDir string, routePrefix string) {
    staticDir = strings.TrimSpace(staticDir)
    if staticDir == "" {
        return
    }

    if routePrefix == "" {
        routePrefix = "/app"
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

// staticAssetExtensions lists file extensions that should never fall back to
// index.html. When a browser requests a .js or .css file and it doesn't exist
// on disk, returning the SPA index causes the browser to silently parse HTML
// as JavaScript, breaking the entire page.
var staticAssetExtensions = map[string]bool{
    ".js": true, ".mjs": true, ".css": true, ".map": true,
    ".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true, ".ico": true, ".webp": true,
    ".woff": true, ".woff2": true, ".ttf": true, ".eot": true, ".otf": true,
    ".json": true, ".xml": true, ".txt": true, ".webmanifest": true,
    ".wasm": true, ".mp4": true, ".webm": true, ".mp3": true, ".ogg": true, ".pdf": true,
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

    // For known static asset extensions, return 404 instead of falling back
    // to index.html. This prevents the browser from parsing HTML as JS/CSS.
    ext := strings.ToLower(filepath.Ext(relPath))
    if staticAssetExtensions[ext] {
        http.NotFound(w, r)
        return
    }

    if _, err := os.Stat(indexPath); err == nil {
        http.ServeFile(w, r, indexPath)
        return
    }

    http.NotFound(w, r)
}
