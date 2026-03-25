// Deep DOM trace: track EVERY property access during WASM init and sign
// This will reveal if __wbindgen_start() reads __NEXT_DATA__ or other page state

import { readFileSync } from 'fs';
import { join } from 'path';
import { homedir } from 'os';

const wasmPath = join(homedir(), '.maclaw', 'freeproxy', 'sign_bg.wasm');

let wasm, V = 0;
const enc = new TextEncoder('utf-8');
const accessLog = [];

function log(phase, msg) {
    accessLog.push({ phase, msg });
    console.log(`[${phase}] ${msg}`);
}

function gs(p, l) {
    return new TextDecoder('utf-8', { ignoreBOM: true, fatal: true })
        .decode(new Uint8Array(wasm.memory.buffer, p, l));
}

function ps(s, m, r) {
    if (!r) { const b = enc.encode(s); const p = m(b.length, 1) >>> 0; new Uint8Array(wasm.memory.buffer).subarray(p, p + b.length).set(b); V = b.length; return p; }
    let l = s.length, p = m(l, 1) >>> 0;
    const mem = new Uint8Array(wasm.memory.buffer);
    let o = 0;
    for (; o < l; o++) { const c = s.charCodeAt(o); if (c > 127) break; mem[p + o] = c; }
    if (o !== l) { if (o !== 0) s = s.slice(o); p = r(p, l, l = o + s.length * 3, 1) >>> 0; const v = new Uint8Array(wasm.memory.buffer).subarray(p + o, p + l); const ret = enc.encodeInto(s, v); o += ret.written; p = r(p, l, o, 1) >>> 0; }
    V = o; return p;
}

function at(o) { const i = wasm.__externref_table_alloc(); wasm.__wbindgen_export_2.set(i, o); return i; }
function c(e) { return e == null; }
function he(f, a) { try { return f.apply(this, a); } catch (e) { wasm.__wbindgen_exn_store(at(e)); } }

let currentPhase = 'setup';

// Deep-tracked fake element
function createDeepElement(id) {
    const handler = {
        get(target, prop) {
            if (typeof prop === 'symbol') return target[prop];
            log(currentPhase, `element#${id}.${prop}`);
            if (prop === 'tagName') return 'DIV';
            if (prop === 'id') return id;
            if (prop === 'innerHTML' || prop === 'innerText' || prop === 'textContent') return '';
            if (prop === 'nodeType') return 1;
            if (prop === 'children' || prop === 'childNodes') return [];
            if (prop === 'getAttribute') return (name) => { log(currentPhase, `element#${id}.getAttribute("${name}")`); return null; };
            if (prop === 'querySelector') return (sel) => { log(currentPhase, `element#${id}.querySelector("${sel}")`); return null; };
            if (prop === 'querySelectorAll') return (sel) => { log(currentPhase, `element#${id}.querySelectorAll("${sel}")`); return []; };
            if (prop === 'getElementById') return (eid) => { log(currentPhase, `element#${id}.getElementById("${eid}")`); return createDeepElement(eid); };
            if (prop === 'dataset') return new Proxy({}, { get(t, p) { if (typeof p === 'symbol') return undefined; log(currentPhase, `element#${id}.dataset.${p}`); return undefined; } });
            if (prop === 'style') return new Proxy({}, { get(t, p) { if (typeof p === 'symbol') return undefined; log(currentPhase, `element#${id}.style.${p}`); return ''; } });
            return undefined;
        }
    };
    return new Proxy({}, handler);
}

// Deep-tracked document
const fakeDocument = new Proxy({}, {
    get(target, prop) {
        if (typeof prop === 'symbol') return undefined;
        log(currentPhase, `document.${prop}`);
        if (prop === 'getElementById') return (id) => {
            log(currentPhase, `document.getElementById("${id}")`);
            return createDeepElement(id);
        };
        if (prop === 'querySelector') return (sel) => {
            log(currentPhase, `document.querySelector("${sel}")`);
            // If looking for __NEXT_DATA__ script
            if (sel.includes('__NEXT_DATA__')) {
                log(currentPhase, `  -> returning fake __NEXT_DATA__ script element`);
                return createDeepElement('__NEXT_DATA__');
            }
            return null;
        };
        if (prop === 'querySelectorAll') return (sel) => {
            log(currentPhase, `document.querySelectorAll("${sel}")`);
            return [];
        };
        if (prop === 'createElement') return (tag) => {
            log(currentPhase, `document.createElement("${tag}")`);
            return createDeepElement(tag);
        };
        if (prop === 'cookie') return '';
        if (prop === 'location') return { href: 'https://ai.dangbei.com/', hostname: 'ai.dangbei.com' };
        return undefined;
    }
});

class Window {}
const fakeWin = new Proxy(new Window(), {
    get(target, prop) {
        if (typeof prop === 'symbol') return target[prop];
        if (prop === 'constructor') return Window;
        if (prop === '__proto__') return Window.prototype;
        log(currentPhase, `window.${prop}`);
        if (prop === 'document') return fakeDocument;
        if (prop === 'crypto') return globalThis.crypto;
        if (prop === 'location') return { href: 'https://ai.dangbei.com/', hostname: 'ai.dangbei.com', protocol: 'https:' };
        if (prop === 'navigator') return { userAgent: 'Mozilla/5.0' };
        if (prop === 'localStorage') return {
            getItem: (key) => { log(currentPhase, `localStorage.getItem("${key}")`); return null; },
            setItem: (key, val) => { log(currentPhase, `localStorage.setItem("${key}", "${val}")`); },
        };
        return undefined;
    }
});

// Ensure instanceof Window works
Object.setPrototypeOf(fakeWin, Window.prototype);
globalThis.Window = Window;
globalThis.self = fakeWin;
globalThis.window = fakeWin;
globalThis.document = fakeDocument;


const imports = { wbg: {
    __wbg_buffer_609cc3eee51ed158: e => e.buffer,
    __wbg_call_672a4d21634d4a24: function() { return he(function(e, t) { return e.call(t); }, arguments); },
    __wbg_call_7cccdd69e0791ae2: function() { return he(function(e, t, n) { return e.call(t, n); }, arguments); },
    __wbg_crypto_ed58b8e10a292839: e => { log(currentPhase, `get .crypto from ${typeof e}`); return e.crypto; },
    __wbg_document_d249400bd7bd996d: e => { log(currentPhase, `get .document`); const t = e.document; return c(t) ? 0 : at(t); },
    __wbg_getElementById_f827f0d6648718a8: (e, t, n) => { const id = gs(t, n); log(currentPhase, `getElementById("${id}")`); const r = e.getElementById(id); return c(r) ? 0 : at(r); },
    __wbg_getRandomValues_bcb4912f16000dc4: function() { return he(function(e, t) { e.getRandomValues(t); }, arguments); },
    __wbg_getTime_46267b1c24877e30: e => { const t = e.getTime(); log(currentPhase, `getTime() = ${t}`); return t; },
    __wbg_instanceof_Window_def73ea0955fc569: e => { let t; try { t = e instanceof Window; } catch { t = false; } log(currentPhase, `instanceof Window = ${t}`); return t; },
    __wbg_msCrypto_0a36e2ec3a343d26: e => e.msCrypto,
    __wbg_new0_f788a2397c7ca929: () => { log(currentPhase, 'new Date()'); return new Date(); },
    __wbg_new_405e22f390576ce2: () => ({}),
    __wbg_new_5e0be73521bc8c17: () => { log(currentPhase, 'new Map()'); return new Map(); },
    __wbg_new_78feb108b6472713: () => [],
    __wbg_new_a12002a7f91c75be: e => new Uint8Array(e),
    __wbg_newnoargs_105ed471475aaf50: (e, t) => { const code = gs(e, t); log(currentPhase, `new Function("${code}")`); return new Function(code); },
    __wbg_newwithbyteoffsetandlength_d97e637ebe145a9a: (e, t, n) => new Uint8Array(e, t >>> 0, n >>> 0),
    __wbg_newwithlength_a381634e90c276d4: e => new Uint8Array(e >>> 0),
    __wbg_node_02999533c4ea02e3: e => e.node,
    __wbg_process_5c1d670bc53614b8: e => e.process,
    __wbg_randomFillSync_ab2cfe79ebbf2740: function() { return he(function(e, t) { e.randomFillSync(t); }, arguments); },
    __wbg_require_79b1e9274cde3c87: function() { return he(function() { return module.require; }, arguments); },
    __wbg_set_37837023f3d740e8: (e, t, n) => { e[t >>> 0] = n; },
    __wbg_set_3f1d0b984ed272ed: (e, t, n) => { e[t] = n; },
    __wbg_set_65595bdd868b3009: (e, t, n) => { e.set(t, n >>> 0); },
    __wbg_set_8fc6bf8a5b1071d1: (e, t, n) => { log(currentPhase, `map.set("${t}", ${typeof n === 'string' ? `"${n}"` : n})`); return e.set(t, n); },
    __wbg_static_accessor_GLOBAL_88a902d13a557d07: () => { const e = typeof global === 'undefined' ? null : global; return c(e) ? 0 : at(e); },
    __wbg_static_accessor_GLOBAL_THIS_56578be7e9f832b0: () => { const e = typeof globalThis === 'undefined' ? null : globalThis; return c(e) ? 0 : at(e); },
    __wbg_static_accessor_SELF_37c5d418e4bf5819: () => { const e = typeof self === 'undefined' ? null : self; return c(e) ? 0 : at(e); },
    __wbg_static_accessor_WINDOW_5de37043a91a9c40: () => { const e = typeof window === 'undefined' ? null : window; return c(e) ? 0 : at(e); },
    __wbg_subarray_aa9065fa9dc5df96: (e, t, n) => e.subarray(t >>> 0, n >>> 0),
    __wbg_versions_c71aa1626a93e0a1: e => e.versions,
    __wbindgen_bigint_from_i64: e => e,
    __wbindgen_bigint_from_u64: e => BigInt.asUintN(64, e),
    __wbindgen_debug_string: (e, t) => { const s = String(wasm.__wbindgen_export_2.get(t)); const p = ps(s, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc); const d = new DataView(wasm.memory.buffer); d.setInt32(e + 4, V, true); d.setInt32(e + 0, p, true); },
    __wbindgen_error_new: (e, t) => new Error(gs(e, t)),
    __wbindgen_init_externref_table: () => { const t = wasm.__wbindgen_export_2; const o = t.grow(4); t.set(0, undefined); t.set(o + 0, undefined); t.set(o + 1, null); t.set(o + 2, true); t.set(o + 3, false); },
    __wbindgen_is_function: e => typeof e === 'function',
    __wbindgen_is_object: e => typeof e === 'object' && e !== null,
    __wbindgen_is_string: e => typeof e === 'string',
    __wbindgen_is_undefined: e => e === undefined,
    __wbindgen_memory: () => wasm.memory,
    __wbindgen_number_new: e => e,
    __wbindgen_string_new: (e, t) => { const s = gs(e, t); log(currentPhase, `string_new: "${s.substring(0, 100)}"`); return s; },
    __wbindgen_throw: (e, t) => { throw new Error(gs(e, t)); },
}};

console.log('=== Loading WASM ===');
const wb = readFileSync(wasmPath);
const { instance } = await WebAssembly.instantiate(wb, imports);
wasm = instance.exports;

console.log('\n=== Phase: __wbindgen_start() (INIT) ===');
currentPhase = 'INIT';
wasm.__wbindgen_start();

console.log('\n=== Phase: get_sign() ===');
currentPhase = 'SIGN';
const body = '{"stream":true,"botCode":"AI_SEARCH","conversationId":"12345","question":"hi","agentId":""}';
const url = '/chatApi/v2/chat';

const bp = ps(body, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc); const bl = V;
const up = ps(url, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc); const ul = V;
const m = wasm.get_sign(bp, bl, up, ul);

console.log('\n=== Result ===');
console.log('sign:', m.get('sign'));
console.log('nonce:', m.get('nonce'));
console.log('timestamp:', m.get('timestamp'));

console.log('\n=== Access Summary ===');
const initAccess = accessLog.filter(a => a.phase === 'INIT');
const signAccess = accessLog.filter(a => a.phase === 'SIGN');
console.log(`INIT phase: ${initAccess.length} accesses`);
initAccess.forEach(a => console.log(`  ${a.msg}`));
console.log(`SIGN phase: ${signAccess.length} accesses`);
signAccess.forEach(a => console.log(`  ${a.msg}`));
