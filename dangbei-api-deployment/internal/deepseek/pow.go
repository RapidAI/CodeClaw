package deepseek

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"os"
	stdruntime "runtime"
	"strconv"
	"sync"

	"ds2api/internal/config"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

type PowSolver struct {
	wasmPath string
	once     sync.Once
	err      error

	runtime  wazero.Runtime
	compiled wazero.CompiledModule
	pool     chan *pooledModule
	poolSize int
}

type pooledModule struct {
	mod     api.Module
	stackFn api.Function
	allocFn api.Function
	freeFn  api.Function
	solveFn api.Function
}

func NewPowSolver(wasmPath string) *PowSolver {
	return &PowSolver{wasmPath: wasmPath}
}

func (p *PowSolver) init(ctx context.Context) error {
	p.once.Do(func() {
		wasmBytes, err := os.ReadFile(p.wasmPath)
		if err != nil {
			if len(embeddedWASM) == 0 {
				p.err = err
				return
			}
			wasmBytes = embeddedWASM
		}
		p.runtime = wazero.NewRuntime(ctx)
		p.compiled, p.err = p.runtime.CompileModule(ctx, wasmBytes)
		if p.err == nil {
			p.poolSize = powPoolSizeFromEnv()
			p.pool = make(chan *pooledModule, p.poolSize)
			for range p.poolSize {
				inst, err := p.createModule(ctx)
				if err != nil {
					p.err = err
					return
				}
				p.pool <- inst
			}
		}
	})
	return p.err
}

func (p *PowSolver) Compute(ctx context.Context, challenge map[string]any) (int64, error) {
	if err := p.init(ctx); err != nil {
		return 0, err
	}
	algo, _ := challenge["algorithm"].(string)
	if algo != "DeepSeekHashV1" {
		return 0, errors.New("unsupported algorithm")
	}
	challengeStr, _ := challenge["challenge"].(string)
	salt, _ := challenge["salt"].(string)
	signature, _ := challenge["signature"].(string)
	targetPath, _ := challenge["target_path"].(string)
	_ = signature
	_ = targetPath

	difficulty := toFloat64(challenge["difficulty"], 144000)
	expireAt := toInt64(challenge["expire_at"], 1680000000)
	prefix := salt + "_" + itoa(expireAt) + "_"

	pm, err := p.acquireModule(ctx)
	if err != nil {
		return 0, err
	}
	defer p.releaseModule(pm)

	mem := pm.mod.Memory()
	if mem == nil {
		return 0, errors.New("wasm memory missing")
	}
	retPtrs, err := pm.stackFn.Call(ctx, uint64(uint32(^uint32(15)))) // -16 i32
	if err != nil || len(retPtrs) == 0 {
		return 0, errors.New("stack alloc failed")
	}
	retptr := uint32(retPtrs[0])
	defer func() {
		_, _ = pm.stackFn.Call(context.Background(), 16)
	}()

	chPtr, chLen, err := writeUTF8(ctx, pm.allocFn, mem, challengeStr)
	if err != nil {
		return 0, err
	}
	defer freeUTF8(pm.freeFn, chPtr, chLen)

	prefixPtr, prefixLen, err := writeUTF8(ctx, pm.allocFn, mem, prefix)
	if err != nil {
		return 0, err
	}
	defer freeUTF8(pm.freeFn, prefixPtr, prefixLen)

	if _, err := pm.solveFn.Call(ctx,
		uint64(retptr),
		uint64(chPtr), uint64(chLen),
		uint64(prefixPtr), uint64(prefixLen),
		math.Float64bits(difficulty),
	); err != nil {
		return 0, err
	}

	statusBytes, ok := mem.Read(retptr, 4)
	if !ok {
		return 0, errors.New("read status failed")
	}
	status := int32(binary.LittleEndian.Uint32(statusBytes))
	valueBytes, ok := mem.Read(retptr+8, 8)
	if !ok {
		return 0, errors.New("read value failed")
	}
	value := math.Float64frombits(binary.LittleEndian.Uint64(valueBytes))
	if status == 0 {
		return 0, errors.New("pow solve failed")
	}
	return int64(value), nil
}

func (p *PowSolver) createModule(ctx context.Context) (*pooledModule, error) {
	mod, err := p.runtime.InstantiateModule(ctx, p.compiled, wazero.NewModuleConfig())
	if err != nil {
		return nil, err
	}
	stackFn := mod.ExportedFunction("__wbindgen_add_to_stack_pointer")
	allocFn := mod.ExportedFunction("__wbindgen_export_0")
	solveFn := mod.ExportedFunction("wasm_solve")
	if stackFn == nil || allocFn == nil || solveFn == nil {
		_ = mod.Close(context.Background())
		return nil, errors.New("required wasm exports missing")
	}
	return &pooledModule{
		mod:     mod,
		stackFn: stackFn,
		allocFn: allocFn,
		freeFn:  mod.ExportedFunction("__wbindgen_export_2"),
		solveFn: solveFn,
	}, nil
}

func (p *PowSolver) acquireModule(ctx context.Context) (*pooledModule, error) {
	if p.pool != nil {
		for {
			select {
			case pm := <-p.pool:
				if pm != nil {
					return pm, nil
				}
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return p.createModule(ctx)
}

func (p *PowSolver) releaseModule(pm *pooledModule) {
	if pm == nil || pm.mod == nil {
		return
	}
	if p.pool != nil {
		select {
		case p.pool <- pm:
			return
		default:
		}
	}
	_ = pm.mod.Close(context.Background())
}

func writeUTF8(ctx context.Context, allocFn api.Function, mem api.Memory, text string) (uint32, uint32, error) {
	data := []byte(text)
	res, err := allocFn.Call(ctx, uint64(len(data)), 1)
	if err != nil || len(res) == 0 {
		return 0, 0, errors.New("alloc failed")
	}
	ptr := uint32(res[0])
	if !mem.Write(ptr, data) {
		return 0, 0, errors.New("mem write failed")
	}
	return ptr, uint32(len(data)), nil
}

func freeUTF8(freeFn api.Function, ptr, size uint32) {
	if freeFn == nil || ptr == 0 || size == 0 {
		return
	}
	_, _ = freeFn.Call(context.Background(), uint64(ptr), uint64(size), 1)
}

func BuildPowHeader(challenge map[string]any, answer int64) (string, error) {
	payload := map[string]any{
		"algorithm":   challenge["algorithm"],
		"challenge":   challenge["challenge"],
		"salt":        challenge["salt"],
		"answer":      answer,
		"signature":   challenge["signature"],
		"target_path": challenge["target_path"],
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func toFloat64(v any, d float64) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return d
	}
}

func toInt64(v any, d int64) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	default:
		return d
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

func powPoolSizeFromEnv() int {
	const fallback = 4
	n := fallback
	if cpus := stdruntime.GOMAXPROCS(0); cpus > 0 {
		n = cpus
	}
	if raw := os.Getenv("DS2API_POW_POOL_SIZE"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			n = v
		}
	}
	if n > 64 {
		return 64
	}
	return n
}

func PreloadWASM(wasmPath string) {
	solver := NewPowSolver(wasmPath)
	if err := solver.init(context.Background()); err != nil {
		config.Logger.Warn("[WASM] preload failed", "error", err)
		return
	}
	config.Logger.Info("[WASM] module preloaded", "path", wasmPath)
}
