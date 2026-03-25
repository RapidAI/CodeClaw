// Test agentChat with a real agentId to see if it streams
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
        'user-agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36',
        'sign': s, 'nonce': n, 'timestamp': String(ts),
        'token': token, 'apptype': '6', 'deviceid': '',
        'lang': 'zh', 'client-ver': '1.0.2', 'appversion': '1.3.9', 'version': 'v1',
    };
}

const agentId = '3886112611'; // 哪吒

// Step 1: Create agent conversation
console.log('=== Creating agent conversation ===');
const createBody = JSON.stringify({agentId});
const crResp = await fetch('https://ai-api.dangbei.net/ai-search/agentApi/v1/create', {
    method: 'POST', headers: makeHeaders(createBody), body: createBody,
});
console.log('Create status:', crResp.status);
const crText = await crResp.text();
console.log('Create response:', crText.substring(0, 500));

let convId;
try {
    const cr = JSON.parse(crText);
    convId = cr.data?.conversationId;
} catch(e) {
    console.log('Parse error, trying regular conversation instead');
    // Fallback: use regular conversation
    const regBody = '{}';
    const regResp = await fetch('https://ai-api.dangbei.net/ai-search/conversationApi/v1/create', {
        method: 'POST', headers: {...makeHeaders(regBody)}, body: regBody,
    });
    const regText = await regResp.text();
    console.log('Regular create:', regText.substring(0, 300));
    const reg = JSON.parse(regText);
    convId = reg.data?.conversationId;
}
console.log('ConversationID:', convId);

if (!convId) {
    console.log('Failed to create conversation, exiting');
    process.exit(1);
}

// Step 2: agentChat with real agentId
console.log('\n=== agentApi/v1/agentChat with real agentId ===');
const chatBody = JSON.stringify({
    stream: true,
    botCode: "AI_SEARCH",
    conversationId: convId,
    question: "你好",
    agentId: agentId,
});

const start = Date.now();
try {
    const r = await fetch('https://ai-api.dangbei.net/ai-search/agentApi/v1/agentChat', {
        method: 'POST', headers: {...makeHeaders(chatBody), 'accept': 'text/event-stream'}, body: chatBody,
        signal: AbortSignal.timeout(30000),
    });
    console.log(`Status: ${r.status} (${Date.now()-start}ms)`);
    console.log('Content-Type:', r.headers.get('content-type'));

    const reader = r.body.getReader();
    const decoder = new TextDecoder();
    let totalText = '';
    let chunks = 0;
    while (true) {
        const {done, value} = await Promise.race([
            reader.read(),
            new Promise((_, reject) => setTimeout(() => reject(new Error('read timeout')), 20000))
        ]);
        if (done) break;
        chunks++;
        const chunk = decoder.decode(value, {stream: true});
        totalText += chunk;
        if (chunks <= 5) console.log(`  Chunk ${chunks}: ${chunk.substring(0, 300)}`);
        if (totalText.length > 5000) { reader.cancel(); break; }
    }
    console.log(`\nTotal: ${chunks} chunks, ${totalText.length} chars`);
    if (totalText.length > 0) console.log('Full:', totalText.substring(0, 3000));
} catch(e) {
    console.log(`FAIL ${Date.now()-start}ms:`, e.message);
}

// Step 3: Also try chatApi/v1/chat but with the agent conversation
console.log('\n=== chatApi/v1/chat with agentId in body ===');
const chat2Body = JSON.stringify({
    stream: true,
    botCode: "AI_SEARCH",
    conversationId: convId,
    question: "你好啊",
    agentId: agentId,
});
const start2 = Date.now();
try {
    const r = await fetch('https://ai-api.dangbei.net/ai-search/chatApi/v1/chat', {
        method: 'POST', headers: {...makeHeaders(chat2Body), 'accept': 'text/event-stream'}, body: chat2Body,
        signal: AbortSignal.timeout(15000),
    });
    console.log(`Status: ${r.status} (${Date.now()-start2}ms)`);
    const t = await r.text();
    console.log(`Response (${t.length}):`, t.substring(0, 500));
} catch(e) {
    console.log(`FAIL ${Date.now()-start2}ms:`, e.message);
}

// Cleanup
await fetch(`https://ai-api.dangbei.net/ai-search/conversationApi/v1/delete?conversationId=${convId}`, {
    method: 'DELETE', headers: { 'cookie': cookie, 'origin': 'https://ai.dangbei.com' },
}).catch(() => {});
