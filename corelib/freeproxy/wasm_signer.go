package freeproxy

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

const signWasmURL = "https://ai.dangbei.com/_next/static/media/sign_bg.6ff3d844.wasm"

// pooledSignModule holds a pre-instantiated WASM module with cached function references.
type pooledSignModule struct {
	mod    api.Module
	refs   *externRefStore
	malloc api.Function
	sign   api.Function
}

// WasmSigner loads the dangbei sign_bg.wasm and calls get_sign to produce request signatures.
//
// Design inspired by ds2api's PowSolver:
//   - sync.Once for lazy initialization
//   - Precompiled WASM module for fast instantiation
//   - Module pool for concurrent signing (no more global mutex serialization)
//   - Embedded WASM with file/network fallback
type WasmSigner struct {
	once     sync.Once
	err      error
	rt       wazero.Runtime
	compiled wazero.CompiledModule
	pool     chan *pooledSignModule
	poolSize int
	cacheDir string
	debug    bool

	// refRegistry maps module names to their externref stores.
	// Needed because the host module ("wbg") is registered once per runtime,
	// but each instantiated module needs its own refs store.
	refRegistry sync.Map // map[string]*externRefStore
}

// NewWasmSigner creates a signer that caches the WASM in cacheDir.
func NewWasmSigner(cacheDir string) *WasmSigner {
	return &WasmSigner{cacheDir: cacheDir}
}

// Init downloads (or loads cached) WASM, compiles it, and pre-instantiates a pool of modules.
// Safe to call multiple times; only the first call does work (sync.Once).
func (s *WasmSigner) Init(ctx context.Context) error {
	s.once.Do(func() {
		s.err = s.doInit(ctx)
	})
	return s.err
}

func (s *WasmSigner) doInit(ctx context.Context) error {
	wasmBytes, err := s.loadWasm()
	if err != nil {
		return fmt.Errorf("load wasm: %w", err)
	}

	rt := wazero.NewRuntime(ctx)

	// Register the "wbg" host module once for the runtime.
	// Host functions use refRegistry to find the correct refs store per module.
	if err := s.registerHostModule(ctx, rt); err != nil {
		rt.Close(ctx)
		return fmt.Errorf("register host module: %w", err)
	}

	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		rt.Close(ctx)
		return fmt.Errorf("compile wasm: %w", err)
	}

	s.rt = rt
	s.compiled = compiled
	s.poolSize = signerPoolSize()
	s.pool = make(chan *pooledSignModule, s.poolSize)

	for range s.poolSize {
		pm, err := s.createModule(ctx)
		if err != nil {
			rt.Close(ctx)
			return fmt.Errorf("create pooled module: %w", err)
		}
		s.pool <- pm
	}
	return nil
}

// refsForModule returns the externref store for the given module.
func (s *WasmSigner) refsForModule(mod api.Module) *externRefStore {
	if v, ok := s.refRegistry.Load(mod.Name()); ok {
		return v.(*externRefStore)
	}
	// This should never happen — indicates a bug in module lifecycle management.
	log.Printf("[wasm-signer] WARNING: no refs registered for module %q, creating fallback", mod.Name())
	refs := newExternRefStore()
	s.refRegistry.Store(mod.Name(), refs)
	return refs
}

var moduleCounter atomic.Uint64

func nextModuleName() string {
	n := moduleCounter.Add(1)
	return fmt.Sprintf("sign_bg_%d", n)
}

// createModule instantiates a new WASM module from the precompiled binary.
func (s *WasmSigner) createModule(ctx context.Context) (*pooledSignModule, error) {
	refs := newExternRefStore()
	name := nextModuleName()

	// Register refs before instantiation so host functions can find it
	s.refRegistry.Store(name, refs)

	mod, err := s.rt.InstantiateModule(ctx, s.compiled,
		wazero.NewModuleConfig().WithName(name))
	if err != nil {
		s.refRegistry.Delete(name)
		return nil, fmt.Errorf("instantiate wasm: %w", err)
	}

	malloc := mod.ExportedFunction("__wbindgen_malloc")
	sign := mod.ExportedFunction("get_sign")
	if malloc == nil || sign == nil {
		mod.Close(ctx)
		s.refRegistry.Delete(name)
		return nil, fmt.Errorf("required wasm exports missing (malloc=%v, get_sign=%v)", malloc != nil, sign != nil)
	}

	return &pooledSignModule{
		mod:    mod,
		refs:   refs,
		malloc: malloc,
		sign:   sign,
	}, nil
}

func (s *WasmSigner) acquireModule(ctx context.Context) (*pooledSignModule, error) {
	if s.pool != nil {
		select {
		case pm := <-s.pool:
			if pm != nil {
				return pm, nil
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.createModule(ctx)
}

func (s *WasmSigner) releaseModule(pm *pooledSignModule) {
	if pm == nil || pm.mod == nil {
		return
	}
	if s.pool != nil {
		select {
		case s.pool <- pm:
			return
		default:
		}
	}
	// Pool full or closed — discard this module
	s.refRegistry.Delete(pm.mod.Name())
	// Guard against runtime already closed
	func() {
		defer func() { recover() }()
		pm.mod.Close(context.Background())
	}()
}

// SignResult holds the three values returned by the WASM get_sign function.
type SignResult struct {
	Sign      string
	Nonce     string
	Timestamp int64
}

// Sign calls get_sign(body, url) and returns the sign/nonce/timestamp.
// Thread-safe: acquires a module from the pool, so multiple goroutines can sign concurrently.
func (s *WasmSigner) Sign(ctx context.Context, body, url string) (SignResult, error) {
	if err := s.Init(ctx); err != nil {
		return SignResult{}, err
	}

	pm, err := s.acquireModule(ctx)
	if err != nil {
		return SignResult{}, fmt.Errorf("acquire module: %w", err)
	}
	defer s.releaseModule(pm)

	// Allocate and write body string into WASM memory
	bodyPtr, err := writeWasmString(ctx, pm.malloc, pm.mod.Memory(), body)
	if err != nil {
		return SignResult{}, fmt.Errorf("write body: %w", err)
	}
	// Allocate and write url string into WASM memory
	urlPtr, err := writeWasmString(ctx, pm.malloc, pm.mod.Memory(), url)
	if err != nil {
		return SignResult{}, fmt.Errorf("write url: %w", err)
	}

	// Call get_sign(body_ptr, body_len, url_ptr, url_len)
	// Wrapped in recover to catch __wbindgen_throw panics from WASM.
	var results []uint64
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("wasm panic: %v", r)
			}
		}()
		results, err = pm.sign.Call(ctx, uint64(bodyPtr), uint64(len(body)), uint64(urlPtr), uint64(len(url)))
	}()
	if err != nil {
		return SignResult{}, fmt.Errorf("get_sign call: %w", err)
	}
	if len(results) == 0 {
		return SignResult{}, fmt.Errorf("get_sign returned no results")
	}

	// The result is a Map externref containing "sign", "nonce", "timestamp" keys.
	ref := uint32(results[0])
	v := pm.refs.get(ref)
	obj, ok := v.(*hostObject)
	if !ok || obj.kind != "map" || obj.mapData == nil {
		return SignResult{}, fmt.Errorf("get_sign returned non-map ref %d (type=%T val=%v)", ref, v, v)
	}

	var sr SignResult
	if sign, ok := obj.mapData["sign"].(string); ok {
		sr.Sign = sign
	}
	if nonce, ok := obj.mapData["nonce"].(string); ok {
		sr.Nonce = nonce
	}
	if ts, ok := obj.mapData["timestamp"].(float64); ok {
		sr.Timestamp = int64(ts)
	}
	if sr.Sign == "" {
		return sr, fmt.Errorf("empty sign in result map: %v", obj.mapData)
	}
	return sr, nil
}

// Close releases all WASM resources. Safe to call even if Init was never called or failed.
func (s *WasmSigner) Close(ctx context.Context) {
	if s.pool != nil {
		ch := s.pool
		s.pool = nil
		close(ch)
		for pm := range ch {
			if pm != nil && pm.mod != nil {
				s.refRegistry.Delete(pm.mod.Name())
				pm.mod.Close(ctx)
			}
		}
	}
	if s.rt != nil {
		s.rt.Close(ctx)
		s.rt = nil
		s.compiled = nil
	}
}

// writeWasmString allocates memory in WASM and writes a UTF-8 string into it.
func writeWasmString(ctx context.Context, malloc api.Function, mem api.Memory, str string) (uint32, error) {
	data := []byte(str)
	results, err := malloc.Call(ctx, uint64(len(data)), 1)
	if err != nil || len(results) == 0 {
		return 0, fmt.Errorf("malloc failed: %w", err)
	}
	ptr := uint32(results[0])
	if !mem.Write(ptr, data) {
		return 0, fmt.Errorf("memory write failed at %d len %d", ptr, len(data))
	}
	return ptr, nil
}

func (s *WasmSigner) loadWasm() ([]byte, error) {
	// 1. Try cache first
	if s.cacheDir != "" {
		cachePath := filepath.Join(s.cacheDir, "sign_bg.wasm")
		if data, err := os.ReadFile(cachePath); err == nil && len(data) > 1000 {
			return data, nil
		}
	}
	// 2. Try embedded WASM (if available via go:embed)
	if len(embeddedSignWASM) > 0 {
		return embeddedSignWASM, nil
	}
	// 3. Download from remote
	dlClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := dlClient.Get(signWasmURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	// Cache for next time
	if s.cacheDir != "" {
		cachePath := filepath.Join(s.cacheDir, "sign_bg.wasm")
		os.MkdirAll(filepath.Dir(cachePath), 0700)
		os.WriteFile(cachePath, data, 0600)
	}
	return data, nil
}

// signerPoolSize returns the pool size based on CPU count, capped at 8.
func signerPoolSize() int {
	n := runtime.GOMAXPROCS(0)
	if n < 1 {
		n = 2
	}
	if raw := os.Getenv("FREEPROXY_SIGNER_POOL_SIZE"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			n = v
		}
	}
	if n > 8 {
		return 8
	}
	return n
}

// externRefStore is a simple store for externref values used by wasm-bindgen.
type externRefStore struct {
	mu     sync.Mutex
	values map[uint32]interface{}
	nextID uint32
}

func newExternRefStore() *externRefStore {
	return &externRefStore{
		values: map[uint32]interface{}{
			0: nil, // null
			1: "undefined",
		},
		nextID: 2,
	}
}

func (e *externRefStore) put(val interface{}) uint32 {
	e.mu.Lock()
	defer e.mu.Unlock()
	id := e.nextID
	e.nextID++
	e.values[id] = val
	return id
}

func (e *externRefStore) get(id uint32) interface{} {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.values[id]
}

func (e *externRefStore) getString(id uint32) string {
	v := e.get(id)
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
