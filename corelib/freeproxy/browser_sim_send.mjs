// Browser-simulated WASM signing + send real request
// Usage: node browser_sim_send.mjs <cookie> <conversationId>

import { readFileSync } from 'fs';
import { join } from 'path';
import { homedir } from 'os';

const wasmPath = join(homedir(), '.maclaw', 'freeproxy', 'sign_bg.wasm');
const cookie = process.argv[2];
const convId = process.argv[3];

if (!cookie || !convId) {
    console.error('Usage: node browser_sim_send.mjs <cookie> <conversationId>');
    process.exit(1);
}

let wasm, WASM_VECTOR_LEN = 0;
const enc = new TextEncoder('utf-8');

function gs(p, l) {
    return new TextDecoder('utf-8', { ignoreBOM: true, fatal: true })
        .decode(new Uint8Array(wasm.memory.buffer, p, l));
}

function ps(s, m, r) {
    if (r === undefined) {
        const b = enc.encode(s);
        const p = m(b.length, 1) >>> 0;
        new Uint8Array(wasm.memory.buffer).subarray(p, p + b.length).set(b);
        WASM_VECTOR_LEN = b.length;
        return p;
    }
    let l = s.length, p = m(l, 1) >>> 0;
    const mem = new Uint8Array(wasm.memory.buffer);
    let o = 0;
    for (; o < l; o++) {
        const c = s.charCodeAt(o);
        if (c > 127) break;
        mem[p + o] = c;
    }
    if (o !== l) {
        if (o !== 0) s = s.slice(o);
        p = r(p, l, l = o + s.length * 3, 1) >>> 0;
        const v = new Uint8Array(wasm.memory.buffer).subarray(p + o, p + l);
        const ret = enc.encodeInto(s, v);
        o += ret.written;
        p = r(p, l, o, 1) >>> 0;
    }
    WASM_VECTOR_LEN = o;
    return p;
}

function at(o) {
    const i = wasm.__externref_table_alloc();
    wasm.__wbindgen_export_2.set(i, o);
    return i;
}
function c(e) { return e == null; }
function he(f, a) { try { return f.apply(this, a); } catch (e) { wasm.__wbindgen_exn_store(at(e)); } }

// Fake browser env
class Window {}
const fakeDoc = { getElementById: () => ({ tagName: 'DIV' }) };
const fakeWin = new Window();
fakeWin.document = fakeDoc;
fakeWin.crypto = globalThis.crypto;
globalThis.Window = Window;
globalThis.self = fakeWin;
globalThis.window = fakeWin;
globalThis.document = fakeDoc;

const imports = { wbg: {
    __wbg_buffer_609cc3eee51ed158: e => e.buffer,
    __wbg_call_672a4d21634d4a24: function() { return he(function(e, t) { return e.call(t); }, arguments); },
    __wbg_call_7cccdd69e0791ae2: function() { return he(function(e, t, n) { return e.call(t, n); }, arguments); },
    __wbg_crypto_ed58b8e10a292839: e => e.crypto,
    __wbg_document_d249400bd7bd996d: e => { const t = e.document; return c(t) ? 0 : at(t); },
    __wbg_getElementById_f827f0d6648718a8: (e, t, n) => { const r = e.getElementById(gs(t, n)); return c(r) ? 0 : at(r); },
    __wbg_getRandomValues_bcb4912f16000dc4: function() { return he(function(e, t) { e.getRandomValues(t); }, arguments); },
    __wbg_getTime_46267b1c24877e30: e => e.getTime(),
    __wbg_instanceof_Window_def73ea0955fc569: e => { try { return e instanceof Window; } catch { return false; } },
    __wbg_msCrypto_0a36e2ec3a343d26: e => e.msCrypto,
    __wbg_new0_f788a2397c7ca929: () => new Date(),
    __wbg_new_405e22f390576ce2: () => ({}),
    __wbg_new_5e0be73521bc8c17: () => new Map(),
    __wbg_new_78feb108b6472713: () => [],
    __wbg_new_a12002a7f91c75be: e => new Uint8Array(e),
    __wbg_newnoargs_105ed471475aaf50: (e, t) => new Function(gs(e, t)),
    __wbg_newwithbyteoffsetandlength_d97e637ebe145a9a: (e, t, n) => new Uint8Array(e, t >>> 0, n >>> 0),
    __wbg_newwithlength_a381634e90c276d4: e => new Uint8Array(e >>> 0),
    __wbg_node_02999533c4ea02e3: e => e.node,
    __wbg_process_5c1d670bc53614b8: e => e.process,
    __wbg_randomFillSync_ab2cfe79ebbf2740: function() { return he(function(e, t) { e.randomFillSync(t); }, arguments); },
    __wbg_require_79b1e9274cde3c87: function() { return he(function() { return module.require; }, arguments); },
    __wbg_set_37837023f3d740e8: (e, t, n) => { e[t >>> 0] = n; },
    __wbg_set_3f1d0b984ed272ed: (e, t, n) => { e[t] = n; },
    __wbg_set_65595bdd868b3009: (e, t, n) => { e.set(t, n >>> 0); },
    __wbg_set_8fc6bf8a5b1071d1: (e, t, n) => e.set(t, n),
    __wbg_static_accessor_GLOBAL_88a902d13a557d07: () => { const e = typeof global === 'undefined' ? null : global; return c(e) ? 0 : at(e); },
    __wbg_static_accessor_GLOBAL_THIS_56578be7e9f832b0: () => { const e = typeof globalThis === 'undefined' ? null : globalThis; return c(e) ? 0 : at(e); },
    __wbg_static_accessor_SELF_37c5d418e4bf5819: () => { const e = typeof self === 'undefined' ? null : self; return c(e) ? 0 : at(e); },
    __wbg_static_accessor_WINDOW_5de37043a91a9c40: () => { const e = typeof window === 'undefined' ? null : window; return c(e) ? 0 : at(e); },
    __wbg_subarray_aa9065fa9dc5df96: (e, t, n) => e.subarray(t >>> 0, n >>> 0),
    __wbg_versions_c71aa1626a93e0a1: e => e.versions,
    __wbindgen_bigint_from_i64: e => e,
    __wbindgen_bigint_from_u64: e => BigInt.asUintN(64, e),
    __wbindgen_debug_string: (e, t) => { const s = String(wasm.__wbindgen_export_2.get(t)); const p = ps(s, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc); const d = new DataView(wasm.memory.buffer); d.setInt32(e + 4, WASM_VECTOR_LEN, true); d.setInt32(e + 0, p, true); },
    __wbindgen_error_new: (e, t) => new Error(gs(e, t)),
    __wbindgen_init_externref_table: () => { const t = wasm.__wbindgen_export_2; const o = t.grow(4); t.set(0, undefined); t.set(o + 0, undefined); t.set(o + 1, null); t.set(o + 2, true); t.set(o + 3, false); },
    __wbindgen_is_function: e => typeof e === 'function',
    __wbindgen_is_object: e => typeof e === 'object' && e !== null,
    __wbindgen_is_string: e => typeof e === 'string',
    __wbindgen_is_undefined: e => e === undefined,
    __wbindgen_memory: () => wasm.memory,
    __wbindgen_number_new: e => e,
    __wbindgen_string_new: (e, t) => gs(e, t),
    __wbindgen_throw: (e, t) => { throw new Error(gs(e, t)); },
}};

const wb = readFileSync(wasmPath);
const { instance } = await WebAssembly.instantiate(wb, imports);
wasm = instance.exports;
wasm.__wbindgen_start();

const body = `{"stream":true,"botCode":"AI_SEARCH","conversationId":"${convId}","question":"hi","agentId":""}`;
const url = '/chatApi/v2/chat';

const bp = ps(body, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
const bl = WASM_VECTOR_LEN;
const up = ps(url, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
const ul = WASM_VECTOR_LEN;
const m = wasm.get_sign(bp, bl, up, ul);

const sign = m.get('sign');
const nonce = m.get('nonce');
const timestamp = String(m.get('timestamp'));

console.log('sign:', sign);
console.log('nonce:', nonce);
console.log('timestamp:', timestamp);

// Send the request
console.log('\n=== Sending request ===');
const resp = await fetch(`https://ai-api.dangbei.net/ai-search/chatApi/v2/chat`, {
    method: 'POST',
    headers: {
        'content-type': 'application/json',
        'accept': 'text/event-stream',
        'cookie': cookie,
        'origin': 'https://ai.dangbei.com',
        'referer': 'https://ai.dangbei.com/',
        'user-agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36',
        'sign': sign,
        'nonce': nonce,
        'timestamp': timestamp,
        'apptype': '5',
        'lang': 'zh',
        'client-ver': '1.0.2',
        'appversion': '1.3.9',
        'version': 'v2',
        'token': '',
    },
    body: body,
    signal: AbortSignal.timeout(15000),
});

console.log('Status:', resp.status, resp.statusText);
const text = await resp.text();
console.log('Response (' + text.length + ' chars):');
console.log(text.substring(0, 2000));
