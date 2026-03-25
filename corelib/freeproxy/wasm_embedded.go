package freeproxy

// embeddedSignWASM holds the embedded sign_bg.wasm binary.
// When the WASM file is available at build time, replace this with:
//
//	//go:embed sign_bg.wasm
//	var embeddedSignWASM []byte
//
// For now, fallback to empty (will download from remote or use cache).
var embeddedSignWASM []byte
