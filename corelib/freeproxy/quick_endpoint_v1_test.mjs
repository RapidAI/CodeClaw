// Comprehensive test of ALL possible chat endpoints with v1 signing
// Goal: find one that actually returns streaming chat content
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

// Create conversation
const cr = await fetch('https://ai-api.dangbei.net/ai-search/conversationApi/v1/create', {
    method: 'POST', headers: { 'content-type': 'application/json', 'cookie': cookie, 'origin': 'https://ai.dangbei.com' }, body: '{}',
}).then(r => r.json());
const convId = cr.data?.conversationId;
console.log('ConversationID:', convId);

// Also create an agent conversation
let agentConvId;
try {
    const acr = await fetch('https://ai-api.dangbei.net/ai-search/agentApi/v1/create', {
        method: 'POST', 
        headers: { 'content-type': 'application/json', 'cookie': cookie, 'origin': 'https://ai.dangbei.com' }, 
        body: JSON.stringify({agentId: "", name: "test"}),
    }).then(r => r.json());
    agentConvId = acr.data?.conversationId;
    console.log('Agent ConversationID:', agentConvId);
} catch(e) { console.log('Agent create failed:', e.message); }

async function testEndpoint(name, url, bodyObj, extraHeaders = {}) {
    const body = JSON.stringify(bodyObj);
    const ts = Math.floor(Date.now() / 1000);
    const n = nonce(21);
    const s = v1Sign(ts, body, n);
    const headers = {
        'content-type': 'application/json',
        'accept': 'text/event-stream',
        'cookie': cookie,
        'origin': 'https://ai.dangbei.com',
        'referer': 'https://ai.dangbei.com/',
        'user-agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36',
        'sign': s, 'nonce': n, 'timestamp': String(ts),
        'token': token, 'apptype': '6', 'deviceid': '',
        'lang': 'zh', 'client-ver': '1.0.2', 'appversion': '1.3.9',
        ...extraHeaders,
    };
    
    console.log(`\n=== ${name} ===`);
    const start = Date.now();
    try {
        const r = await fetch(url, {
            method: 'POST', headers, body, signal: AbortSignal.timeout(15000),
        });
        const elapsed = Date.now() - start;
        console.log(`Status: ${r.status} (${elapsed}ms), Content-Type: ${r.headers.get('content-type')}`);
        
        // Read response as stream to see if it's SSE
        const reader = r.body.getReader();
        const decoder = new TextDecoder();
        let totalText = '';
        let chunks = 0;
        const readStart = Date.now();
        while (Date.now() - readStart < 10000) {
            const {done, value} = await Promise.race([
                reader.read(),
                new Promise((_, reject) => setTimeout(() => reject(new Error('read timeout')), 10000))
            ]);
            if (done) break;
            chunks++;
            const chunk = decoder.decode(value, {stream: true});
            totalText += chunk;
            if (totalText.length > 3000) break;
        }
        reader.cancel();
        console.log(`Got ${chunks} chunks, ${totalText.length} chars in ${Date.now()-readStart}ms`);
        console.log(`Response: ${totalText.substring(0, 1500)}`);
    } catch(e) { 
        console.log(`FAIL ${Date.now()-start}ms: ${e.message}`); 
    }
}

// Test 1: chatApi/v1/chat with appType=6 (web)
await testEndpoint('chatApi/v1/chat (web, v1 sign)', 
    'https://ai-api.dangbei.net/ai-search/chatApi/v1/chat',
    {stream:true, botCode:"AI_SEARCH", conversationId:convId, question:"说hello", agentId:""},
    {version: 'v1'}
);

// Test 2: agentApi/v1/agentChat with regular convId
await testEndpoint('agentApi/v1/agentChat (regular conv)',
    'https://ai-api.dangbei.net/ai-search/agentApi/v1/agentChat',
    {stream:true, botCode:"AI_SEARCH", conversationId:convId, question:"说hello", agentId:""},
    {version: 'v1'}
);

// Test 3: agentApi/v1/agentChat with agent convId
if (agentConvId) {
    await testEndpoint('agentApi/v1/agentChat (agent conv)',
        'https://ai-api.dangbei.net/ai-search/agentApi/v1/agentChat',
        {stream:true, botCode:"AI_SEARCH", conversationId:agentConvId, question:"说hello", agentId:""},
        {version: 'v1'}
    );
}

// Test 4: chatApi/v1/chat with appType=5 (windows)
await testEndpoint('chatApi/v1/chat (win, appType=5)',
    'https://ai-api.dangbei.net/ai-search/chatApi/v1/chat',
    {stream:true, botCode:"AI_SEARCH", conversationId:convId, question:"说hi", agentId:""},
    {version: 'v1', apptype: '5'}
);

// Test 5: chatApi/v2/chat with v1 signing (known to timeout for non-browser, but try anyway with short timeout)
await testEndpoint('chatApi/v2/chat (v1 sign, expect timeout)',
    'https://ai-api.dangbei.net/ai-search/chatApi/v2/chat',
    {stream:true, botCode:"AI_SEARCH", conversationId:convId, question:"hi", agentId:""},
    {version: 'v1'}
);

// Test 6: Try without version header
await testEndpoint('chatApi/v1/chat (no version header)',
    'https://ai-api.dangbei.net/ai-search/chatApi/v1/chat',
    {stream:true, botCode:"AI_SEARCH", conversationId:convId, question:"说hey", agentId:""},
    {} // no version header
);

// Test 7: agentApi/v1/agentChat with modelCode
await testEndpoint('agentApi/v1/agentChat (with modelCode)',
    'https://ai-api.dangbei.net/ai-search/agentApi/v1/agentChat',
    {stream:true, botCode:"AI_SEARCH", conversationId:convId, question:"说hello", agentId:"", modelCode:"deepseek_r1"},
    {version: 'v1'}
);

// Cleanup
await fetch(`https://ai-api.dangbei.net/ai-search/conversationApi/v1/delete?conversationId=${convId}`, {
    method: 'DELETE', headers: { 'cookie': cookie, 'origin': 'https://ai.dangbei.com' },
}).catch(() => {});
if (agentConvId) {
    await fetch(`https://ai-api.dangbei.net/ai-search/conversationApi/v1/delete?conversationId=${agentConvId}`, {
        method: 'DELETE', headers: { 'cookie': cookie, 'origin': 'https://ai.dangbei.com' },
    }).catch(() => {});
}
