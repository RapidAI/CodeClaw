// Debug agentApi endpoints - find correct parameters
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
        'cookie': cookie,
        'origin': 'https://ai.dangbei.com',
        'referer': 'https://ai.dangbei.com/',
        'user-agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36',
        'sign': s, 'nonce': n, 'timestamp': String(ts),
        'token': token, 'apptype': '6', 'deviceid': '',
        'lang': 'zh', 'client-ver': '1.0.2', 'appversion': '1.3.9', 'version': 'v1',
    };
}

// Test 1: agentApi/v1/create with various body formats
console.log('=== agentApi/v1/create tests ===');
const createBodies = [
    {},
    {agentId: ""},
    {agentId: "", botCode: "AI_SEARCH"},
    {botCode: "AI_SEARCH"},
    {name: "test"},
    {agentId: "", name: "test", botCode: "AI_SEARCH"},
];
for (const b of createBodies) {
    const body = JSON.stringify(b);
    const r = await fetch('https://ai-api.dangbei.net/ai-search/agentApi/v1/create', {
        method: 'POST', headers: makeHeaders(body), body,
    }).then(r => r.text());
    console.log(`  ${body} => ${r.substring(0, 200)}`);
}

// Create a regular conversation for agentChat tests
const cr = await fetch('https://ai-api.dangbei.net/ai-search/conversationApi/v1/create', {
    method: 'POST', headers: { 'content-type': 'application/json', 'cookie': cookie, 'origin': 'https://ai.dangbei.com' }, body: '{}',
}).then(r => r.json());
const convId = cr.data?.conversationId;
console.log('\nConversationID:', convId);

// Test 2: agentApi/v1/agentChat with various body formats
console.log('\n=== agentApi/v1/agentChat body format tests ===');
const chatBodies = [
    // Original format
    {stream:true, botCode:"AI_SEARCH", conversationId:convId, question:"hi", agentId:""},
    // Without botCode
    {stream:true, conversationId:convId, question:"hi", agentId:""},
    // Without agentId
    {stream:true, botCode:"AI_SEARCH", conversationId:convId, question:"hi"},
    // With content instead of question
    {stream:true, botCode:"AI_SEARCH", conversationId:convId, content:"hi", agentId:""},
    // With message instead of question
    {stream:true, botCode:"AI_SEARCH", conversationId:convId, message:"hi", agentId:""},
    // With query instead of question
    {stream:true, botCode:"AI_SEARCH", conversationId:convId, query:"hi", agentId:""},
    // Minimal
    {conversationId:convId, question:"hi"},
    // With model
    {stream:true, botCode:"AI_SEARCH", conversationId:convId, question:"hi", agentId:"", model:"deepseek_r1"},
];

for (const b of chatBodies) {
    const body = JSON.stringify(b);
    const headers = {...makeHeaders(body), 'accept': 'text/event-stream'};
    try {
        const r = await fetch('https://ai-api.dangbei.net/ai-search/agentApi/v1/agentChat', {
            method: 'POST', headers, body, signal: AbortSignal.timeout(10000),
        });
        const t = await r.text();
        console.log(`  ${body.substring(0, 80)}... => ${r.status}: ${t.substring(0, 200)}`);
    } catch(e) {
        console.log(`  ${body.substring(0, 80)}... => FAIL: ${e.message}`);
    }
}

// Test 3: Try chatApi/v1/chat with different accept headers and body formats
console.log('\n=== chatApi/v1/chat variations ===');
const v1Bodies = [
    // With model field
    {stream:true, botCode:"AI_SEARCH", conversationId:convId, question:"说hello world", agentId:"", modelCode:"deepseek_r1"},
    // With model instead of modelCode
    {stream:true, botCode:"AI_SEARCH", conversationId:convId, question:"说hello world", agentId:"", model:"deepseek_r1"},
    // Without stream
    {botCode:"AI_SEARCH", conversationId:convId, question:"说hello world", agentId:""},
];

for (const b of v1Bodies) {
    const body = JSON.stringify(b);
    const headers = {...makeHeaders(body), 'accept': '*/*'};
    try {
        const r = await fetch('https://ai-api.dangbei.net/ai-search/chatApi/v1/chat', {
            method: 'POST', headers, body, signal: AbortSignal.timeout(10000),
        });
        const ct = r.headers.get('content-type');
        const t = await r.text();
        console.log(`  ${body.substring(0, 80)}... => ${r.status} (${ct}): ${t.substring(0, 300)}`);
    } catch(e) {
        console.log(`  ${body.substring(0, 80)}... => FAIL: ${e.message}`);
    }
}

// Cleanup
await fetch(`https://ai-api.dangbei.net/ai-search/conversationApi/v1/delete?conversationId=${convId}`, {
    method: 'DELETE', headers: { 'cookie': cookie, 'origin': 'https://ai.dangbei.com' },
}).catch(() => {});
