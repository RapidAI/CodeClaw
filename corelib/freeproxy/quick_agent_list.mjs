// Find available agents and try agentChat with a real agentId
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

// Query agent list
console.log('=== agentApi/v1/pageQuery ===');
const queryBody = JSON.stringify({pageNo: 1, pageSize: 20});
const r1 = await fetch('https://ai-api.dangbei.net/ai-search/agentApi/v1/pageQuery', {
    method: 'POST', headers: makeHeaders(queryBody), body: queryBody,
}).then(r => r.text());
console.log(r1.substring(0, 2000));

// Also try to find the bundle code that constructs the agentChat body
// Search for agentId in the _app bundle
const appUrl = 'https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js';
const appText = await fetch(appUrl).then(r => r.text());

// Find agentId usage patterns
console.log('\n=== agentId in _app bundle ===');
let idx = 0;
let count = 0;
while ((idx = appText.indexOf('agentId', idx)) !== -1 && count < 15) {
    const ctx = appText.substring(Math.max(0, idx - 100), Math.min(appText.length, idx + 150));
    console.log(`\n  #${++count} at ${idx}: ...${ctx}...`);
    idx += 7;
}

// Search chat page for agentId
const chatUrl = 'https://ai.dangbei.com/_next/static/chunks/pages/chat-56b75a527ca55cdb.js';
const chatText = await fetch(chatUrl).then(r => r.text());
console.log('\n=== agentId in chat page ===');
idx = 0;
count = 0;
while ((idx = chatText.indexOf('agentId', idx)) !== -1 && count < 15) {
    const ctx = chatText.substring(Math.max(0, idx - 100), Math.min(chatText.length, idx + 150));
    console.log(`\n  #${++count} at ${idx}: ...${ctx}...`);
    idx += 7;
}
