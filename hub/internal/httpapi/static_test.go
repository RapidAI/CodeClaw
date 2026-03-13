package httpapi

import (
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "testing"
)

func TestRegisterPWAStaticRoutesServesIndexAndAssets(t *testing.T) {
    dir := t.TempDir()
    if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("index-page"), 0644); err != nil {
        t.Fatalf("write index: %v", err)
    }
    if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log('ok');"), 0644); err != nil {
        t.Fatalf("write asset: %v", err)
    }

    mux := http.NewServeMux()
    registerPWAStaticRoutes(mux, dir, "/app")

    indexReq := httptest.NewRequest(http.MethodGet, "/app", nil)
    indexRec := httptest.NewRecorder()
    mux.ServeHTTP(indexRec, indexReq)
    if indexRec.Code != http.StatusOK {
        t.Fatalf("index status = %d", indexRec.Code)
    }
    if body := indexRec.Body.String(); body != "index-page" {
        t.Fatalf("index body = %q", body)
    }

    assetReq := httptest.NewRequest(http.MethodGet, "/app/app.js", nil)
    assetRec := httptest.NewRecorder()
    mux.ServeHTTP(assetRec, assetReq)
    if assetRec.Code != http.StatusOK {
        t.Fatalf("asset status = %d", assetRec.Code)
    }
    if body := assetRec.Body.String(); body != "console.log('ok');" {
        t.Fatalf("asset body = %q", body)
    }

    spaReq := httptest.NewRequest(http.MethodGet, "/app/session/123", nil)
    spaRec := httptest.NewRecorder()
    mux.ServeHTTP(spaRec, spaReq)
    if spaRec.Code != http.StatusOK {
        t.Fatalf("spa fallback status = %d", spaRec.Code)
    }
    if body := spaRec.Body.String(); body != "index-page" {
        t.Fatalf("spa fallback body = %q", body)
    }
}

func TestRegisterAdminStaticRoutesServesIndexAndAssets(t *testing.T) {
    dir := t.TempDir()
    if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("admin-index"), 0644); err != nil {
        t.Fatalf("write index: %v", err)
    }
    if err := os.WriteFile(filepath.Join(dir, "admin.js"), []byte("console.log('admin');"), 0644); err != nil {
        t.Fatalf("write asset: %v", err)
    }

    mux := http.NewServeMux()
    registerAdminStaticRoutes(mux, dir, "/admin")

    indexReq := httptest.NewRequest(http.MethodGet, "/admin", nil)
    indexRec := httptest.NewRecorder()
    mux.ServeHTTP(indexRec, indexReq)
    if indexRec.Code != http.StatusOK {
        t.Fatalf("index status = %d", indexRec.Code)
    }
    if body := indexRec.Body.String(); body != "admin-index" {
        t.Fatalf("index body = %q", body)
    }

    assetReq := httptest.NewRequest(http.MethodGet, "/admin/admin.js", nil)
    assetRec := httptest.NewRecorder()
    mux.ServeHTTP(assetRec, assetReq)
    if assetRec.Code != http.StatusOK {
        t.Fatalf("asset status = %d", assetRec.Code)
    }
    if body := assetRec.Body.String(); body != "console.log('admin');" {
        t.Fatalf("asset body = %q", body)
    }

    spaReq := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
    spaRec := httptest.NewRecorder()
    mux.ServeHTTP(spaRec, spaReq)
    if spaRec.Code != http.StatusOK {
        t.Fatalf("spa fallback status = %d", spaRec.Code)
    }
    if body := spaRec.Body.String(); body != "admin-index" {
        t.Fatalf("spa fallback body = %q", body)
    }
}
