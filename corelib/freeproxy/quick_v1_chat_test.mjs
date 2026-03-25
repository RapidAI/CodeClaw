// Test v1 endpoints with proper v1 signing to find a working chat path
import { readFileSync } from 'fs';
import { join } from 'path';
import { homedir } from 'os';
import { createHash } from 'crypto';

const authPath = join(homedir(), '.maclaw', 'freeproxy', 'dangbei_auth.json');
const auth = JSON.parse(readFileSync(authPath, 'utf-8'));
const cookie = auth.cookie;
const tokenMatch = cookie.match(/token=([^;]+)/);
const token = tokenMatch ? tokenMatch[1] : '';

function nonce(len) {
    const c = 'useandom-26T198340PX75pxJACKVERYMINDBUSHWOLF_GQZbfghjklqvwyzrict';
    let r = ''; for (let i = 0; i < len; i++) r += c[Math.floor(Math.random() * c.length)]; return r;
}
function v1Sign(ts, body, n) {
    return createHash('md5').update(`${ts}${body}${n}`).digest('hex').toUpperCase();
}
function makeHeaders(body) {
    const ts = Math.floor(Date.now() / 1000);
    const n = nonce(21);
    const s = v1Sign(ts, body, n);
    return {
        'content-type': 'application/json',
        'accept': 'text/event-stream',
        'cookie': cookie,
        'origin': 'https://ai.dangbei.com',
        'referer': 'https://ai.dangbei.com/',
        'user-agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36',
        'sign': s, 'nonce': n, 'timestamp': String(ts),
        'token': token, 'apptype': '6', 'deviceid': '',
        'lang': 'zh', 'client-ver': '1.0.2', 'appversion': '1.3.9', 'version': 'v1',
    };
}

// Create conversation
const cr = await fetch('https://ai-api.dangbei.net/ai-search/conversationApi/v1/create', {
    method: 'POST', headers: { 'content-type': 'application/json', 'cookie': cookie, 'origin': 'https://ai.dangbei.com' }, body: '{}',
}).then(r => r.json());
const convId = cr.data?.conversationId;
console.log('ConversationID:', convId);

// Test 1: chatApi/v1/chat with v1 signing
console.log('\n=== Test 1: chatApi/v1/chat ===');
let body = JSON.stringify({stream:true,botCode:"AI_SEARCH",conversationId:convId,question:"hello",agentId:""});
let start = Date.now();
try {
    const r = await fetch('https://ai-api.dangbei.net/ai-search/chatApi/v1/chat', {
        method: 'POST', headers: makeHeaders(body), body, signal: AbortSignal.timeout(15000),
    });
    console.log(`Status: ${r.status} (${Date.now()-start}ms)`);
    const t = await r.text();
    console.log(`Response (${t.length}):`, t.substring(0, 1000));
} catch(e) { console.log(`FAIL ${Date.now()-start}ms:`, e.message); }

// Test 2: agentApi/v1/agentChat - try different body formats
console.log('\n=== Test 2a: agentApi/v1/agentChat (same body) ===');
body = JSON.stringify({stream:true,botCode:"AI_SEARCH",conversationId:convId,question:"hello",agentId:""});
start = Date.now();
try {
    const r = await fetch('https://ai-api.dangbei.net/ai-search/agentApi/v1/agentChat', {
        method: 'POST', headers: makeHeaders(body), body, signal: AbortSignal.timeout(15000),
    });
    console.log(`Status: ${r.status} (${Date.now()-start}ms)`);
    const t = await r.text();
    console.log(`Response (${t.length}):`, t.substring(0, 1000));
} catch(e) { console.log(`FAIL ${Date.now()-start}ms:`, e.message); }

// Test 2b: agentApi with different body format
console.log('\n=== Test 2b: agentApi/v1/agentChat (with model field) ===');
body = JSON.stringify({stream:true,botCode:"AI_SEARCH",conversationId:convId,question:"hello",agentId:"",modelCode:"deepseek_r1"});
start = Date.now();
try {
    const r = await fetch('https://ai-api.dangbei.net/ai-search/agentApi/v1/agentChat', {
        method: 'POST', headers: makeHeaders(body), body, signal: AbortSignal.timeout(15000),
    });
    console.log(`Status: ${r.status} (${Date.now()-start}ms)`);
    const t = await r.text();
    console.log(`Response (${t.length}):`, t.substring(0, 1000));
} catch(e) { console.log(`FAIL ${Date.now()-start}ms:`, e.message); }

// Test 3: chatApi/v1/chat with stream:false
console.log('\n=== Test 3: chatApi/v1/chat stream:false ===');
body = JSON.stringify({stream:false,botCode:"AI_SEARCH",conversationId:convId,question:"hello",agentId:""});
start = Date.now();
try {
    const r = await fetch('https://ai-api.dangbei.net/ai-search/chatApi/v1/chat', {
        method: 'POST', headers: {...makeHeaders(body), 'accept': 'application/json'}, body, signal: AbortSignal.timeout(15000),
    });
    console.log(`Status: ${r.status} (${Date.now()-start}ms)`);
    const t = await r.text();
    console.log(`Response (${t.length}):`, t.substring(0, 1000));
} catch(e) { console.log(`FAIL ${Date.now()-start}ms:`, e.message); }

// Test 4: chatApi/v1/chat with accept */*
console.log('\n=== Test 4: chatApi/v1/chat accept:*/* ===');
body = JSON.stringify({stream:true,botCode:"AI_SEARCH",conversationId:convId,question:"hi",agentId:""});
start = Date.now();
try {
    const r = await fetch('https://ai-api.dangbei.net/ai-search/chatApi/v1/chat', {
        method: 'POST', headers: {...makeHeaders(body), 'accept': '*/*'}, body, signal: AbortSignal.timeout(15000),
    });
    console.log(`Status: ${r.status} (${Date.now()-start}ms)`);
    const t = await r.text();
    console.log(`Response (${t.length}):`, t.substring(0, 1000));
} catch(e) { console.log(`FAIL ${Date.now()-start}ms:`, e.message); }

// Cleanup
await fetch(`https://ai-api.dangbei.net/ai-search/conversationApi/v1/delete?conversationId=${convId}`, {
    method: 'DELETE', headers: { 'cookie': cookie, 'origin': 'https://ai.dangbei.com' },
}).catch(() => {});
