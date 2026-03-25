// Verify WASM signing by running it in Node.js (the native environment)
// Usage: node verify_sign.mjs <body> <url>

import { readFileSync } from 'fs';
import { join } from 'path';
import { homedir } from 'os';

const wasmPath = join(homedir(), '.maclaw', 'freeproxy', 'sign_bg.wasm');

let wasm;

function getStringFromWasm(ptr, len) {
    const buf = new Uint8Array(wasm.memory.buffer, ptr, len);
    return new TextDecoder('utf-8', { ignoreBOM: true, fatal: true }).decode(buf);
}

let WASM_VECTOR_LEN = 0;
const encoder = new TextEncoder('utf-8');

function passStringToWasm(str, malloc, realloc) {
    if (realloc === undefined) {
        const buf = encoder.encode(str);
        const ptr = malloc(buf.length, 1) >>> 0;
        new Uint8Array(wasm.memory.buffer).subarray(ptr, ptr + buf.length).set(buf);
        WASM_VECTOR_LEN = buf.length;
        return ptr;
    }
    let len = str.length;
    let ptr = malloc(len, 1) >>> 0;
    const mem = new Uint8Array(wasm.memory.buffer);
    let offset = 0;
    for (; offset < len; offset++) {
        const code = str.charCodeAt(offset);
        if (code > 0x7F) break;
        mem[ptr + offset] = code;
    }
    if (offset !== len) {
        if (offset !== 0) str = str.slice(offset);
        ptr = realloc(ptr, len, len = offset + str.length * 3, 1) >>> 0;
        const view = new Uint8Array(wasm.memory.buffer).subarray(ptr + offset, ptr + len);
        const ret = encoder.encodeInto(str, view);
        offset += ret.written;
        ptr = realloc(ptr, len, offset, 1) >>> 0;
    }
    WASM_VECTOR_LEN = offset;
    return ptr;
}

function addToExternrefTable(obj) {
    const idx = wasm.__externref_table_alloc();
    wasm.__wbindgen_export_2.set(idx, obj);
    return idx;
}

function isNull(e) { return e == null; }

function handleError(f, args) {
    try {
        return f.apply(this, args);
    } catch (e) {
        const idx = addToExternrefTable(e);
        wasm.__wbindgen_exn_store(idx);
    }
}

const imports = {
    wbg: {
        __wbg_buffer_609cc3eee51ed158: (e) => e.buffer,
        __wbg_call_672a4d21634d4a24: function() { return handleError(function(e, t) { return e.call(t); }, arguments); },
        __wbg_call_7cccdd69e0791ae2: function() { return handleError(function(e, t, n) { return e.call(t, n); }, arguments); },
        __wbg_crypto_ed58b8e10a292839: (e) => e.crypto,
        __wbg_document_d249400bd7bd996d: (e) => { const t = e.document; return isNull(t) ? 0 : addToExternrefTable(t); },
        __wbg_getElementById_f827f0d6648718a8: (e, t, n) => { const r = e.getElementById(getStringFromWasm(t, n)); return isNull(r) ? 0 : addToExternrefTable(r); },
        __wbg_getRandomValues_bcb4912f16000dc4: function() { return handleError(function(e, t) { e.getRandomValues(t); }, arguments); },
        __wbg_getTime_46267b1c24877e30: (e) => e.getTime(),
        __wbg_instanceof_Window_def73ea0955fc569: (e) => { let t; try { t = e instanceof globalThis.Window; } catch { t = false; } return t; },
        __wbg_msCrypto_0a36e2ec3a343d26: (e) => e.msCrypto,
        __wbg_new0_f788a2397c7ca929: () => new Date(),
        __wbg_new_405e22f390576ce2: () => ({}),
        __wbg_new_5e0be73521bc8c17: () => new Map(),
        __wbg_new_78feb108b6472713: () => [],
        __wbg_new_a12002a7f91c75be: (e) => new Uint8Array(e),
        __wbg_newnoargs_105ed471475aaf50: (e, t) => new Function(getStringFromWasm(e, t)),
        __wbg_newwithbyteoffsetandlength_d97e637ebe145a9a: (e, t, n) => new Uint8Array(e, t >>> 0, n >>> 0),
        __wbg_newwithlength_a381634e90c276d4: (e) => new Uint8Array(e >>> 0),
        __wbg_node_02999533c4ea02e3: (e) => e.node,
        __wbg_process_5c1d670bc53614b8: (e) => e.process,
        __wbg_randomFillSync_ab2cfe79ebbf2740: function() { return handleError(function(e, t) { e.randomFillSync(t); }, arguments); },
        __wbg_require_79b1e9274cde3c87: function() { return handleError(function() { return module.require; }, arguments); },
        __wbg_set_37837023f3d740e8: (e, t, n) => { e[t >>> 0] = n; },
        __wbg_set_3f1d0b984ed272ed: (e, t, n) => { e[t] = n; },
        __wbg_set_65595bdd868b3009: (e, t, n) => { e.set(t, n >>> 0); },
        __wbg_set_8fc6bf8a5b1071d1: (e, t, n) => e.set(t, n),
        __wbg_static_accessor_GLOBAL_88a902d13a557d07: () => { const e = typeof global === 'undefined' ? null : global; return isNull(e) ? 0 : addToExternrefTable(e); },
        __wbg_static_accessor_GLOBAL_THIS_56578be7e9f832b0: () => { const e = typeof globalThis === 'undefined' ? null : globalThis; return isNull(e) ? 0 : addToExternrefTable(e); },
        __wbg_static_accessor_SELF_37c5d418e4bf5819: () => { const e = typeof self === 'undefined' ? null : self; return isNull(e) ? 0 : addToExternrefTable(e); },
        __wbg_static_accessor_WINDOW_5de37043a91a9c40: () => { const e = typeof window === 'undefined' ? null : window; return isNull(e) ? 0 : addToExternrefTable(e); },
        __wbg_subarray_aa9065fa9dc5df96: (e, t, n) => e.subarray(t >>> 0, n >>> 0),
        __wbg_versions_c71aa1626a93e0a1: (e) => e.versions,
        __wbindgen_bigint_from_i64: (e) => e,
        __wbindgen_bigint_from_u64: (e) => BigInt.asUintN(64, e),
        __wbindgen_debug_string: (e, t) => {
            // simplified
            const str = String(wasm.__wbindgen_export_2.get(t));
            const ptr = passStringToWasm(str, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
            const dv = new DataView(wasm.memory.buffer);
            dv.setInt32(e + 4, WASM_VECTOR_LEN, true);
            dv.setInt32(e + 0, ptr, true);
        },
        __wbindgen_error_new: (e, t) => new Error(getStringFromWasm(e, t)),
        __wbindgen_init_externref_table: () => {
            const table = wasm.__wbindgen_export_2;
            const offset = table.grow(4);
            table.set(0, undefined);
            table.set(offset + 0, undefined);
            table.set(offset + 1, null);
            table.set(offset + 2, true);
            table.set(offset + 3, false);
        },
        __wbindgen_is_function: (e) => typeof e === 'function',
        __wbindgen_is_object: (e) => typeof e === 'object' && e !== null,
        __wbindgen_is_string: (e) => typeof e === 'string',
        __wbindgen_is_undefined: (e) => e === undefined,
        __wbindgen_memory: () => wasm.memory,
        __wbindgen_number_new: (e) => e,
        __wbindgen_string_new: (e, t) => getStringFromWasm(e, t),
        __wbindgen_throw: (e, t) => { throw new Error(getStringFromWasm(e, t)); },
    }
};

async function init() {
    const wasmBytes = readFileSync(wasmPath);
    const { instance } = await WebAssembly.instantiate(wasmBytes, imports);
    wasm = instance.exports;
    wasm.__wbindgen_start();
}

function getSign(body, url) {
    const bodyPtr = passStringToWasm(body, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
    const bodyLen = WASM_VECTOR_LEN;
    const urlPtr = passStringToWasm(url, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
    const urlLen = WASM_VECTOR_LEN;
    const map = wasm.get_sign(bodyPtr, bodyLen, urlPtr, urlLen);
    // get_sign returns a Map directly (externref)
    return {
        sign: map.get('sign'),
        nonce: map.get('nonce'),
        timestamp: map.get('timestamp'),
    };
}

await init();

const body = process.argv[2] || '{"stream":true,"botCode":"AI_SEARCH","conversationId":"12345","question":"hi","agentId":""}';
const url = process.argv[3] || '/chatApi/v2/chat';

console.log('Body:', body);
console.log('URL:', url);

for (let i = 0; i < 3; i++) {
    const result = getSign(body, url);
    console.log(`Sign[${i}]:`, JSON.stringify(result));
}
