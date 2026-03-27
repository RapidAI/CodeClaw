package httpapi

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// ModelDownloadHandler serves model files from the Hub's data/ directory.
// Only allows downloading files with .gguf extension for safety.
// GET /api/v1/models/{filename}
func ModelDownloadHandler(dataDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filename := r.PathValue("filename")
		if filename == "" {
			http.Error(w, "missing filename", http.StatusBadRequest)
			return
		}
		// Security: only allow .gguf files, no path traversal
		if strings.Contains(filename, "/") || strings.Contains(filename, "\\") || strings.Contains(filename, "..") {
			http.Error(w, "invalid filename", http.StatusBadRequest)
			return
		}
		if !strings.HasSuffix(filename, ".gguf") {
			http.Error(w, "only .gguf files are allowed", http.StatusForbidden)
			return
		}

		filePath := filepath.Join(dataDir, filename)
		fi, err := os.Stat(filePath)
		if err != nil {
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", fi.Size()))

		http.ServeFile(w, r, filePath)
	}
}
