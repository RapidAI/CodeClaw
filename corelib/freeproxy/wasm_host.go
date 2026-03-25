package freeproxy

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// hostObject represents a typed JS-like object in our externref store.
type hostObject struct {
	kind    string      // "memory", "buffer", "uint8array", "map", "array", "object", "function", "crypto", "globalThis", "date", "error"
	data    interface{} // backing data (e.g. []byte for uint8array, mod api.Module for memory)
	mapData map[string]interface{}
	memMod  api.Module // non-nil if this uint8array is a view into WASM memory
}

// registerHostModule registers the "wbg" module that the sign_bg.wasm imports.
// Implements all 44 imported functions based on the JS glue code.
//
// Host functions use s.refsForModule(mod) to look up the correct externref store
// for each module instance, enabling pooled concurrent execution.
func (s *WasmSigner) registerHostModule(ctx context.Context, rt wazero.Runtime) error {
	b := rt.NewHostModuleBuilder("wbg")

	dbg := func(format string, args ...interface{}) {
		if s.debug {
			fmt.Printf("[wasm-host] "+format+"\n", args...)
		}
	}

	// Helper: get refs for the calling module
	refs := func(mod api.Module) *externRefStore {
		return s.refsForModule(mod)
	}

	// ==================== String / type helpers ====================

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			ptr := uint32(stack[0])
			length := uint32(stack[1])
			buf, ok := mod.Memory().Read(ptr, length)
			if !ok {
				stack[0] = uint64(r.put(""))
				return
			}
			str := string(buf)
			dbg("string_new(%d,%d) = %q", ptr, length, str)
			stack[0] = uint64(r.put(str))
		}),
		[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32},
		[]api.ValueType{api.ValueTypeExternref},
	).Export("__wbindgen_string_new")

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			ptr := uint32(stack[0])
			length := uint32(stack[1])
			buf, _ := mod.Memory().Read(ptr, length)
			msg := string(buf)
			dbg("error_new: %q", msg)
			stack[0] = uint64(r.put(&hostObject{kind: "error", data: msg}))
		}),
		[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32},
		[]api.ValueType{api.ValueTypeExternref},
	).Export("__wbindgen_error_new")

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			ref := uint32(stack[0])
			v := r.get(ref)
			result := uint64(0)
			if obj, ok := v.(*hostObject); ok {
				switch obj.kind {
				case "error", "map", "object", "crypto", "globalThis", "memory", "buffer", "uint8array", "date":
					result = 1
				}
			}
			stack[0] = result
		}),
		[]api.ValueType{api.ValueTypeExternref},
		[]api.ValueType{api.ValueTypeI32},
	).Export("__wbindgen_is_object")

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			ref := uint32(stack[0])
			v := r.get(ref)
			if _, ok := v.(string); ok {
				stack[0] = 1
			} else {
				stack[0] = 0
			}
		}),
		[]api.ValueType{api.ValueTypeExternref},
		[]api.ValueType{api.ValueTypeI32},
	).Export("__wbindgen_is_string")

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			ref := uint32(stack[0])
			v := r.get(ref)
			result := uint64(0)
			if obj, ok := v.(*hostObject); ok && obj.kind == "function" {
				result = 1
			}
			stack[0] = result
		}),
		[]api.ValueType{api.ValueTypeExternref},
		[]api.ValueType{api.ValueTypeI32},
	).Export("__wbindgen_is_function")

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			ref := uint32(stack[0])
			v := r.get(ref)
			if v == nil {
				stack[0] = 1
			} else {
				stack[0] = 0
			}
		}),
		[]api.ValueType{api.ValueTypeExternref},
		[]api.ValueType{api.ValueTypeI32},
	).Export("__wbindgen_is_undefined")

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			stack[0] = uint64(r.put(api.DecodeF64(stack[0])))
		}),
		[]api.ValueType{api.ValueTypeF64},
		[]api.ValueType{api.ValueTypeExternref},
	).Export("__wbindgen_number_new")

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			stack[0] = uint64(r.put(int64(stack[0])))
		}),
		[]api.ValueType{api.ValueTypeI64},
		[]api.ValueType{api.ValueTypeExternref},
	).Export("__wbindgen_bigint_from_i64")

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			stack[0] = uint64(r.put(stack[0]))
		}),
		[]api.ValueType{api.ValueTypeI64},
		[]api.ValueType{api.ValueTypeExternref},
	).Export("__wbindgen_bigint_from_u64")

	// ==================== Object property access ====================

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// e[t] = n — no-op for our purposes
		}),
		[]api.ValueType{api.ValueTypeExternref, api.ValueTypeExternref, api.ValueTypeExternref},
		[]api.ValueType{},
	).Export("__wbg_set_3f1d0b984ed272ed")

	// Map.set(key, val) -> map
	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			mapRef := uint32(stack[0])
			keyRef := uint32(stack[1])
			valRef := uint32(stack[2])

			mapVal := r.get(mapRef)
			if obj, ok := mapVal.(*hostObject); ok && obj.kind == "map" {
				if obj.mapData == nil {
					obj.mapData = make(map[string]interface{})
				}
				keyStr := ""
				switch k := r.get(keyRef).(type) {
				case string:
					keyStr = k
				default:
					keyStr = fmt.Sprintf("%v", k)
				}
				obj.mapData[keyStr] = r.get(valRef)
			}
			// return the map itself (like JS Map.set)
			// stack[0] already holds mapRef, no reassignment needed
		}),
		[]api.ValueType{api.ValueTypeExternref, api.ValueTypeExternref, api.ValueTypeExternref},
		[]api.ValueType{api.ValueTypeExternref},
	).Export("__wbg_set_8fc6bf8a5b1071d1")

	// e[t>>>0] = n (array set)
	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {}),
		[]api.ValueType{api.ValueTypeExternref, api.ValueTypeI32, api.ValueTypeExternref},
		[]api.ValueType{},
	).Export("__wbg_set_37837023f3d740e8")

	// Uint8Array.set(source, offset)
	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			dstRef := uint32(stack[0])
			srcRef := uint32(stack[1])
			offset := uint32(stack[2])

			srcVal := r.get(srcRef)
			var srcBytes []byte
			if obj, ok := srcVal.(*hostObject); ok && obj.kind == "uint8array" {
				srcBytes, _ = obj.data.([]byte)
			}
			if srcBytes == nil {
				return
			}

			dstVal := r.get(dstRef)
			if obj, ok := dstVal.(*hostObject); ok && obj.kind == "uint8array" {
				if obj.memMod != nil {
					obj.memMod.Memory().Write(offset, srcBytes)
				} else if dstBuf, ok := obj.data.([]byte); ok {
					copy(dstBuf[offset:], srcBytes)
				}
			}
		}),
		[]api.ValueType{api.ValueTypeExternref, api.ValueTypeExternref, api.ValueTypeI32},
		[]api.ValueType{},
	).Export("__wbg_set_65595bdd868b3009")

	// ==================== DOM stubs ====================

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			stack[0] = 0
		}),
		[]api.ValueType{api.ValueTypeExternref},
		[]api.ValueType{api.ValueTypeI32},
	).Export("__wbg_instanceof_Window_def73ea0955fc569")

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			stack[0] = 0
		}),
		[]api.ValueType{api.ValueTypeExternref},
		[]api.ValueType{api.ValueTypeI32},
	).Export("__wbg_document_d249400bd7bd996d")

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			stack[0] = 0
		}),
		[]api.ValueType{api.ValueTypeExternref, api.ValueTypeI32, api.ValueTypeI32},
		[]api.ValueType{api.ValueTypeI32},
	).Export("__wbg_getElementById_f827f0d6648718a8")

	// ==================== Crypto ====================

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			stack[0] = uint64(r.put(&hostObject{kind: "crypto", data: nil}))
		}),
		[]api.ValueType{api.ValueTypeExternref},
		[]api.ValueType{api.ValueTypeExternref},
	).Export("__wbg_crypto_ed58b8e10a292839")

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			stack[0] = uint64(r.put(nil))
		}),
		[]api.ValueType{api.ValueTypeExternref},
		[]api.ValueType{api.ValueTypeExternref},
	).Export("__wbg_process_5c1d670bc53614b8")

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			stack[0] = uint64(r.put(nil))
		}),
		[]api.ValueType{api.ValueTypeExternref},
		[]api.ValueType{api.ValueTypeExternref},
	).Export("__wbg_versions_c71aa1626a93e0a1")

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			stack[0] = uint64(r.put(nil))
		}),
		[]api.ValueType{api.ValueTypeExternref},
		[]api.ValueType{api.ValueTypeExternref},
	).Export("__wbg_node_02999533c4ea02e3")

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			stack[0] = uint64(r.put(nil))
		}),
		[]api.ValueType{},
		[]api.ValueType{api.ValueTypeExternref},
	).Export("__wbg_require_79b1e9274cde3c87")

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			stack[0] = uint64(r.put(nil))
		}),
		[]api.ValueType{api.ValueTypeExternref},
		[]api.ValueType{api.ValueTypeExternref},
	).Export("__wbg_msCrypto_0a36e2ec3a343d26")

	// getRandomValues(crypto, uint8arr)
	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			arrRef := uint32(stack[1])
			arrVal := r.get(arrRef)
			if obj, ok := arrVal.(*hostObject); ok && obj.kind == "uint8array" {
				if buf, ok := obj.data.([]byte); ok {
					rand.Read(buf)
				}
			}
		}),
		[]api.ValueType{api.ValueTypeExternref, api.ValueTypeExternref},
		[]api.ValueType{},
	).Export("__wbg_getRandomValues_bcb4912f16000dc4")

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {}),
		[]api.ValueType{api.ValueTypeExternref, api.ValueTypeExternref},
		[]api.ValueType{},
	).Export("__wbg_randomFillSync_ab2cfe79ebbf2740")

	// ==================== Date ====================

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			stack[0] = api.EncodeF64(float64(time.Now().UnixMilli()))
		}),
		[]api.ValueType{api.ValueTypeExternref},
		[]api.ValueType{api.ValueTypeF64},
	).Export("__wbg_getTime_46267b1c24877e30")

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			stack[0] = uint64(r.put(&hostObject{kind: "date", data: time.Now()}))
		}),
		[]api.ValueType{},
		[]api.ValueType{api.ValueTypeExternref},
	).Export("__wbg_new0_f788a2397c7ca929")

	// ==================== Global accessors ====================

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			stack[0] = uint64(r.put(&hostObject{kind: "globalThis", data: nil}))
		}),
		[]api.ValueType{},
		[]api.ValueType{api.ValueTypeI32},
	).Export("__wbg_static_accessor_GLOBAL_THIS_56578be7e9f832b0")

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			stack[0] = 0
		}),
		[]api.ValueType{},
		[]api.ValueType{api.ValueTypeI32},
	).Export("__wbg_static_accessor_SELF_37c5d418e4bf5819")

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			stack[0] = 0
		}),
		[]api.ValueType{},
		[]api.ValueType{api.ValueTypeI32},
	).Export("__wbg_static_accessor_WINDOW_5de37043a91a9c40")

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			stack[0] = 0
		}),
		[]api.ValueType{},
		[]api.ValueType{api.ValueTypeI32},
	).Export("__wbg_static_accessor_GLOBAL_88a902d13a557d07")

	// ==================== Constructors ====================

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			stack[0] = uint64(r.put(&hostObject{kind: "array", data: nil}))
		}),
		[]api.ValueType{},
		[]api.ValueType{api.ValueTypeExternref},
	).Export("__wbg_new_78feb108b6472713")

	// Function(code) constructor
	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			ptr := uint32(stack[0])
			length := uint32(stack[1])
			buf, _ := mod.Memory().Read(ptr, length)
			code := string(buf)
			stack[0] = uint64(r.put(&hostObject{kind: "function", data: code}))
		}),
		[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32},
		[]api.ValueType{api.ValueTypeExternref},
	).Export("__wbg_newnoargs_105ed471475aaf50")

	// new Map()
	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			stack[0] = uint64(r.put(&hostObject{kind: "map", data: nil, mapData: make(map[string]interface{})}))
		}),
		[]api.ValueType{},
		[]api.ValueType{api.ValueTypeExternref},
	).Export("__wbg_new_5e0be73521bc8c17")

	// fn.call(thisArg) — used for Function("return this")()
	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			fnRef := uint32(stack[0])
			fnVal := r.get(fnRef)
			if obj, ok := fnVal.(*hostObject); ok && obj.kind == "function" {
				if code, ok := obj.data.(string); ok && code == "return this" {
					stack[0] = uint64(r.put(&hostObject{kind: "globalThis", data: nil}))
					return
				}
			}
			stack[0] = uint64(r.put(nil))
		}),
		[]api.ValueType{api.ValueTypeExternref, api.ValueTypeExternref},
		[]api.ValueType{api.ValueTypeExternref},
	).Export("__wbg_call_672a4d21634d4a24")

	// new Object()
	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			stack[0] = uint64(r.put(&hostObject{kind: "object", data: nil}))
		}),
		[]api.ValueType{},
		[]api.ValueType{api.ValueTypeExternref},
	).Export("__wbg_new_405e22f390576ce2")

	// fn.call(this, arg)
	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			stack[0] = uint64(r.put(nil))
		}),
		[]api.ValueType{api.ValueTypeExternref, api.ValueTypeExternref, api.ValueTypeExternref},
		[]api.ValueType{api.ValueTypeExternref},
	).Export("__wbg_call_7cccdd69e0791ae2")

	// ==================== TypedArray / Buffer ====================

	// memory.buffer -> ArrayBuffer
	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			stack[0] = uint64(r.put(&hostObject{kind: "buffer", data: mod}))
		}),
		[]api.ValueType{api.ValueTypeExternref},
		[]api.ValueType{api.ValueTypeExternref},
	).Export("__wbg_buffer_609cc3eee51ed158")

	// new Uint8Array(buffer, offset, len)
	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			bufRef := uint32(stack[0])
			offset := uint32(stack[1])
			length := uint32(stack[2])

			bufVal := r.get(bufRef)
			if obj, ok := bufVal.(*hostObject); ok && obj.kind == "buffer" {
				if m, ok := obj.data.(api.Module); ok {
					data, ok := m.Memory().Read(offset, length)
					if ok {
						slice := make([]byte, length)
						copy(slice, data)
						stack[0] = uint64(r.put(&hostObject{kind: "uint8array", data: slice}))
						return
					}
				}
			}
			stack[0] = uint64(r.put(&hostObject{kind: "uint8array", data: []byte{}}))
		}),
		[]api.ValueType{api.ValueTypeExternref, api.ValueTypeI32, api.ValueTypeI32},
		[]api.ValueType{api.ValueTypeExternref},
	).Export("__wbg_newwithbyteoffsetandlength_d97e637ebe145a9a")

	// new Uint8Array(buffer) — memory view
	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			bufRef := uint32(stack[0])
			bufVal := r.get(bufRef)
			if obj, ok := bufVal.(*hostObject); ok && obj.kind == "buffer" {
				if m, ok := obj.data.(api.Module); ok {
					stack[0] = uint64(r.put(&hostObject{kind: "uint8array", data: nil, memMod: m}))
					return
				}
			}
			stack[0] = uint64(r.put(&hostObject{kind: "uint8array", data: []byte{}}))
		}),
		[]api.ValueType{api.ValueTypeExternref},
		[]api.ValueType{api.ValueTypeExternref},
	).Export("__wbg_new_a12002a7f91c75be")

	// new Uint8Array(length)
	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			length := int(uint32(stack[0]))
			buf := make([]byte, length)
			stack[0] = uint64(r.put(&hostObject{kind: "uint8array", data: buf}))
		}),
		[]api.ValueType{api.ValueTypeI32},
		[]api.ValueType{api.ValueTypeExternref},
	).Export("__wbg_newwithlength_a381634e90c276d4")

	// subarray(start, end)
	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			arrRef := uint32(stack[0])
			start := int(uint32(stack[1]))
			end := int(uint32(stack[2]))
			v := r.get(arrRef)
			if obj, ok := v.(*hostObject); ok && obj.kind == "uint8array" {
				if buf, ok := obj.data.([]byte); ok && start <= len(buf) && end <= len(buf) && start <= end {
					sub := make([]byte, end-start)
					copy(sub, buf[start:end])
					stack[0] = uint64(r.put(&hostObject{kind: "uint8array", data: sub}))
					return
				}
			}
			stack[0] = uint64(r.put(&hostObject{kind: "uint8array", data: []byte{}}))
		}),
		[]api.ValueType{api.ValueTypeExternref, api.ValueTypeI32, api.ValueTypeI32},
		[]api.ValueType{api.ValueTypeExternref},
	).Export("__wbg_subarray_aa9065fa9dc5df96")

	// ==================== wbindgen utilities ====================

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			retptr := uint32(stack[0])
			ref := uint32(stack[1])
			v := r.get(ref)
			debugStr := fmt.Sprintf("%v", v)
			malloc := mod.ExportedFunction("__wbindgen_malloc")
			if malloc != nil {
				results, err := malloc.Call(ctx, uint64(len(debugStr)), 1)
				if err == nil && len(results) > 0 {
					ptr := uint32(results[0])
					mod.Memory().Write(ptr, []byte(debugStr))
					mod.Memory().WriteUint32Le(retptr, ptr)
					mod.Memory().WriteUint32Le(retptr+4, uint32(len(debugStr)))
				}
			}
		}),
		[]api.ValueType{api.ValueTypeI32, api.ValueTypeExternref},
		[]api.ValueType{},
	).Export("__wbindgen_debug_string")

	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			ptr := uint32(stack[0])
			length := uint32(stack[1])
			buf, _ := mod.Memory().Read(ptr, length)
			msg := string(buf)
			log.Printf("[wasm-host] __wbindgen_throw: %s", msg)
			panic("wasm throw: " + msg)
		}),
		[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32},
		[]api.ValueType{},
	).Export("__wbindgen_throw")

	// __wbindgen_memory() -> externref
	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			r := refs(mod)
			stack[0] = uint64(r.put(&hostObject{kind: "memory", data: mod}))
		}),
		[]api.ValueType{},
		[]api.ValueType{api.ValueTypeExternref},
	).Export("__wbindgen_memory")

	// __wbindgen_init_externref_table() -> void
	b.NewFunctionBuilder().WithGoModuleFunction(
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// No-op: externrefs are managed in Go via externRefStore
		}),
		[]api.ValueType{},
		[]api.ValueType{},
	).Export("__wbindgen_init_externref_table")

	_, err := b.Instantiate(ctx)
	return err
}
