// Test: can we use agentChat without a specific character agent?
// Try with botCode as agentId, or find a generic agent
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

async function testChat(name, agentId, convId, question) {
    const chatBody = JSON.stringify({
        stream: true,
        botCode: "AI_SEARCH",
        conversationId: convId,
        question: question,
        agentId: agentId,
    });
    console.log(`\n=== ${name} ===`);
    const start = Date.now();
    try {
        const r = await fetch('https://ai-api.dangbei.net/ai-search/agentApi/v1/agentChat', {
            method: 'POST', headers: makeHeaders(chatBody), body: chatBody,
            signal: AbortSignal.timeout(20000),
        });
        console.log(`Status: ${r.status} (${Date.now()-start}ms), CT: ${r.headers.get('content-type')}`);
        const t = await r.text();
        // Extract content from SSE
        const contents = [];
        for (const line of t.split('\n')) {
            if (line.startsWith('data:')) {
                try {
                    const d = JSON.parse(line.substring(5));
                    if (d.content && d.content_type === 'text') contents.push(d.content);
                } catch(e) {}
            }
        }
        if (contents.length > 0) {
            console.log(`Content: ${contents.join('')}`);
        } else {
            console.log(`Raw (${t.length}): ${t.substring(0, 500)}`);
        }
    } catch(e) {
        console.log(`FAIL ${Date.now()-start}ms: ${e.message}`);
    }
}

// Test 1: Use a regular conversation (from conversationApi/v1/create) with agentChat
// This is what the main chat page would do
const regBody = '{}';
const regResp = await fetch('https://ai-api.dangbei.net/ai-search/conversationApi/v1/create', {
    method: 'POST', headers: {...makeHeaders(regBody), 'accept': 'application/json'}, body: regBody,
}).then(r => r.json());
const regConvId = regResp.data?.conversationId;
console.log('Regular ConversationID:', regConvId);

// Test with regular conv + empty agentId
await testChat('Regular conv + empty agentId', '', regConvId, '1+1等于几');

// Test with regular conv + agentId="AI_SEARCH"
await testChat('Regular conv + agentId=AI_SEARCH', 'AI_SEARCH', regConvId, '1+1等于几');

// Test 2: Use agent conversation with 哪吒 but ask a normal question
const agentBody = JSON.stringify({agentId: '3886112611'});
const agentResp = await fetch('https://ai-api.dangbei.net/ai-search/agentApi/v1/create', {
    method: 'POST', headers: {...makeHeaders(agentBody), 'accept': 'application/json'}, body: agentBody,
}).then(r => r.json());
const agentConvId = agentResp.data?.conversationId;
console.log('\nAgent ConversationID:', agentConvId);

await testChat('Agent conv (哪吒) + normal question', '3886112611', agentConvId, '1+1等于几');

// Test 3: Try agentChat with regular conv but a real agentId
await testChat('Regular conv + real agentId (哪吒)', '3886112611', regConvId, '1+1等于几');

// Test 4: Try with modelCode in body
console.log('\n=== agentChat with modelCode ===');
const mcBody = JSON.stringify({
    stream: true,
    botCode: "AI_SEARCH",
    conversationId: regConvId,
    question: "1+1等于几",
    agentId: "",
    modelCode: "deepseek_r1",
});
const start4 = Date.now();
try {
    const r = await fetch('https://ai-api.dangbei.net/ai-search/agentApi/v1/agentChat', {
        method: 'POST', headers: makeHeaders(mcBody), body: mcBody,
        signal: AbortSignal.timeout(15000),
    });
    console.log(`Status: ${r.status} (${Date.now()-start4}ms)`);
    const t = await r.text();
    console.log(`Response (${t.length}): ${t.substring(0, 500)}`);
} catch(e) {
    console.log(`FAIL: ${e.message}`);
}

// Cleanup
for (const id of [regConvId, agentConvId].filter(Boolean)) {
    await fetch(`https://ai-api.dangbei.net/ai-search/conversationApi/v1/delete?conversationId=${id}`, {
        method: 'DELETE', headers: { 'cookie': cookie, 'origin': 'https://ai.dangbei.com' },
    }).catch(() => {});
}
