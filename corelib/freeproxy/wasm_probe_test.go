package freeproxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

const wasmURL = "https://ai.dangbei.com/_next/static/media/sign_bg.6ff3d844.wasm"

func downloadWasm(t *testing.T) []byte {
	t.Helper()
	// Cache locally
	home, _ := os.UserHomeDir()
	cachePath := filepath.Join(home, ".maclaw", "freeproxy", "sign_bg.wasm")
	if data, err := os.ReadFile(cachePath); err == nil && len(data) > 1000 {
		t.Logf("Using cached WASM: %s (%d bytes)", cachePath, len(data))
		return data
	}

	t.Logf("Downloading WASM from %s ...", wasmURL)
	resp, err := http.Get(wasmURL)
	if err != nil {
		t.Fatalf("Download WASM: %v", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Read WASM: %v", err)
	}
	t.Logf("Downloaded %d bytes", len(data))

	os.MkdirAll(filepath.Dir(cachePath), 0700)
	os.WriteFile(cachePath, data, 0600)
	return data
}

// TestWasmProbeExports downloads the WASM and lists all exports and imports.
func TestWasmProbeExports(t *testing.T) {
	wasmBytes := downloadWasm(t)

	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	// First, decode the module to see imports
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	t.Log("=== EXPORTS ===")
	for _, exp := range compiled.ExportedFunctions() {
		params := exp.ParamTypes()
		results := exp.ResultTypes()
		t.Logf("  func %s(%v) -> %v", exp.Name(), fmtTypes(params), fmtTypes(results))
	}

	t.Log("=== IMPORTED FUNCTIONS ===")
	for _, imp := range compiled.ImportedFunctions() {
		mod, name, _ := imp.Import()
		params := imp.ParamTypes()
		results := imp.ResultTypes()
		t.Logf("  %s.%s(%v) -> %v", mod, name, fmtTypes(params), fmtTypes(results))
	}

	// Check for memory export
	for _, exp := range compiled.ExportedMemories() {
		min, hasMax := exp.Min(), false
		max := uint32(0)
		_ = hasMax
		_ = max
		t.Logf("  memory: min=%d pages", min)
	}
}

func fmtTypes(types []api.ValueType) string {
	names := make([]string, len(types))
	for i, vt := range types {
		switch vt {
		case api.ValueTypeI32:
			names[i] = "i32"
		case api.ValueTypeI64:
			names[i] = "i64"
		case api.ValueTypeF32:
			names[i] = "f32"
		case api.ValueTypeF64:
			names[i] = "f64"
		default:
			names[i] = fmt.Sprintf("0x%x", vt)
		}
	}
	return fmt.Sprintf("[%s]", joinStr(names, ", "))
}

func joinStr(s []string, sep string) string {
	result := ""
	for i, v := range s {
		if i > 0 {
			result += sep
		}
		result += v
	}
	return result
}
