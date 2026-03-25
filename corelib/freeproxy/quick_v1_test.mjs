// Test v1 signing with agentApi endpoint (known working path)
import { readFileSync } from 'fs';
import { join } from 'path';
import { homedir } from 'os';
import { createHash } from 'crypto';

const authPath = join(homedir(), '.maclaw', 'freeproxy', 'dangbei_auth.json');
const auth = JSON.parse(readFileSync(authPath, 'utf-8'));
const cookie = auth.cookie;
const tokenMatch = cookie.match(/token=([^;]+)/);
const token = tokenMatch ? tokenMatch[1] : '';

function generateNonce(len) {
    const chars = 'useandom-26T198340PX75pxJACKVERYMINDBUSHWOLF_GQZbfghjklqvwyzrict';
    let r = '';
    for (let i = 0; i < len; i++) r += chars[Math.floor(Math.random() * chars.length)];
    return r;
}

function v1Sign(timestamp, body, nonce) {
    return createHash('md5').update(`${timestamp}${body}${nonce}`).digest('hex').toUpperCase();
}

// Create conversation
const createResp = await fetch('https://ai-api.dangbei.net/ai-search/conversationApi/v1/create', {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'cookie': cookie, 'origin': 'https://ai.dangbei.com' },
    body: '{}',
});
const createData = await createResp.json();
const convId = createData.data?.conversationId;
console.log('ConversationID:', convId);

const body = JSON.stringify({stream:true,botCode:"AI_SEARCH",conversationId:convId,question:"hi",agentId:""});
const timestamp = Math.floor(Date.now() / 1000);
const nonce = generateNonce(21);
const sign = v1Sign(timestamp, body, nonce);
console.log(`v1 sign=${sign} nonce=${nonce} ts=${timestamp}`);

// Test 1: agentApi/v1/agentChat with v1 signing
console.log('\n=== Test 1: agentApi/v1/agentChat (v1 signing) ===');
let start = Date.now();
try {
    const resp = await fetch('https://ai-api.dangbei.net/ai-search/agentApi/v1/agentChat', {
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
            'timestamp': String(timestamp),
            'version': 'v1',
            'apptype': '5',
            'lang': 'zh',
            'client-ver': '1.0.2',
            'appversion': '1.3.9',
            'token': token,
        },
        body: body,
        signal: AbortSignal.timeout(15000),
    });
    console.log(`Status: ${resp.status} (${Date.now() - start}ms)`);
    const text = await resp.text();
    console.log(`Response (${text.length} chars):`, text.substring(0, 800));
} catch (e) {
    console.log(`FAILED after ${Date.now() - start}ms:`, e.message);
}

// Test 2: chatApi/v2/chat with v1 signing
console.log('\n=== Test 2: chatApi/v2/chat (v1 signing) ===');
const nonce2 = generateNonce(21);
const ts2 = Math.floor(Date.now() / 1000);
const sign2 = v1Sign(ts2, body, nonce2);
start = Date.now();
try {
    const resp = await fetch('https://ai-api.dangbei.net/ai-search/chatApi/v2/chat', {
        method: 'POST',
        headers: {
            'content-type': 'application/json',
            'accept': 'text/event-stream',
            'cookie': cookie,
            'origin': 'https://ai.dangbei.com',
            'referer': 'https://ai.dangbei.com/',
            'user-agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36',
            'sign': sign2,
            'nonce': nonce2,
            'timestamp': String(ts2),
            'version': 'v1',
            'apptype': '5',
            'lang': 'zh',
            'client-ver': '1.0.2',
            'appversion': '1.3.9',
            'token': token,
        },
        body: body,
        signal: AbortSignal.timeout(15000),
    });
    console.log(`Status: ${resp.status} (${Date.now() - start}ms)`);
    const text = await resp.text();
    console.log(`Response (${text.length} chars):`, text.substring(0, 800));
} catch (e) {
    console.log(`FAILED after ${Date.now() - start}ms:`, e.message);
}

// Cleanup
await fetch(`https://ai-api.dangbei.net/ai-search/conversationApi/v1/delete?conversationId=${convId}`, {
    method: 'DELETE', headers: { 'cookie': cookie, 'origin': 'https://ai.dangbei.com' },
}).catch(() => {});
