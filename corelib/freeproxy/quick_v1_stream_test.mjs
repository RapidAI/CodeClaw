// Test v1/chat with proper SSE streaming consumption
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

const cr = await fetch('https://ai-api.dangbei.net/ai-search/conversationApi/v1/create', {
    method: 'POST', headers: { 'content-type': 'application/json', 'cookie': cookie, 'origin': 'https://ai.dangbei.com' }, body: '{}',
}).then(r => r.json());
const convId = cr.data?.conversationId;
console.log('ConversationID:', convId);

const body = JSON.stringify({stream:true,botCode:"AI_SEARCH",conversationId:convId,question:"1+1等于几",agentId:""});
const ts = Math.floor(Date.now() / 1000);
const n = nonce(21);
const s = v1Sign(ts, body, n);

console.log('Sending v1/chat with streaming...');
const start = Date.now();
const resp = await fetch('https://ai-api.dangbei.net/ai-search/chatApi/v1/chat', {
    method: 'POST',
    headers: {
        'content-type': 'application/json',
        'accept': 'text/event-stream',
        'cookie': cookie,
        'origin': 'https://ai.dangbei.com',
        'referer': 'https://ai.dangbei.com/',
        'user-agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36',
        'sign': s, 'nonce': n, 'timestamp': String(ts),
        'token': token, 'apptype': '6', 'deviceid': '',
        'lang': 'zh', 'client-ver': '1.0.2', 'appversion': '1.3.9', 'version': 'v1',
    },
    body,
    signal: AbortSignal.timeout(30000),
});

console.log(`Status: ${resp.status} (${Date.now()-start}ms)`);
console.log('Content-Type:', resp.headers.get('content-type'));
console.log('Transfer-Encoding:', resp.headers.get('transfer-encoding'));

// Try to read as stream
const reader = resp.body.getReader();
const decoder = new TextDecoder();
let totalText = '';
let chunks = 0;

while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    const text = decoder.decode(value, { stream: true });
    totalText += text;
    chunks++;
    if (chunks <= 5) console.log(`Chunk ${chunks}: ${text.substring(0, 200)}`);
    if (totalText.length > 10000) break;
}

console.log(`\nTotal: ${chunks} chunks, ${totalText.length} chars`);
console.log('Full response:', totalText.substring(0, 2000));

// Cleanup
await fetch(`https://ai-api.dangbei.net/ai-search/conversationApi/v1/delete?conversationId=${convId}`, {
    method: 'DELETE', headers: { 'cookie': cookie, 'origin': 'https://ai.dangbei.com' },
}).catch(() => {});
