// Package configfile provides atomic file write utilities for tool configuration files.
// Inspired by cc-switch's atomic_write pattern: write to temp file then rename,
// preventing half-written config corruption.
package configfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// AtomicWrite writes data to path atomically: write temp file → rename.
// On Windows, removes target first since rename over existing file fails.
func AtomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}

	tmp := fmt.Sprintf("%s.tmp.%d", path, time.Now().UnixNano())
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write temp %s: %w", tmp, err)
	}

	if runtime.GOOS == "windows" {
		_ = os.Remove(path) // best-effort remove before rename
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("atomic rename %s → %s: %w", tmp, path, err)
	}
	return nil
}

// AtomicWriteJSON writes a JSON value atomically with pretty-printing.
func AtomicWriteJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	data = append(data, '\n')
	return AtomicWrite(path, data)
}
