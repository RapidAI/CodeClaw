// Search for a generic/universal agent in the agent list
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

// Get all pages of agents
let allAgents = [];
for (let page = 1; page <= 5; page++) {
    const body = JSON.stringify({pageNo: page, pageSize: 20});
    const resp = await fetch('https://ai-api.dangbei.net/ai-search/agentApi/v1/pageQuery', {
        method: 'POST', headers: makeHeaders(body), body,
    }).then(r => r.json());
    
    if (!resp.success || !resp.data?.records?.length) break;
    allAgents.push(...resp.data.records);
    console.log(`Page ${page}: ${resp.data.records.length} agents (total: ${resp.data.total})`);
}

console.log(`\nTotal agents found: ${allAgents.length}`);
console.log('\nAll agents:');
for (const a of allAgents) {
    console.log(`  ${a.agentId}: ${a.name} - ${a.intro?.substring(0, 50)}`);
}

// Look for anything that sounds like a generic assistant
const genericKeywords = ['助手', '通用', 'AI', '搜索', '问答', '对话', '智能', '百科'];
console.log('\n=== Potentially generic agents ===');
for (const a of allAgents) {
    const text = `${a.name} ${a.intro || ''}`;
    for (const kw of genericKeywords) {
        if (text.includes(kw)) {
            console.log(`  ${a.agentId}: ${a.name} - ${a.intro}`);
            break;
        }
    }
}

// Also check: what does the main chat page use?
// The v2/chat endpoint uses botCode:"AI_SEARCH" with empty agentId
// Let's look at the bundle for how the chat page decides between v2/chat and agentChat
const chatUrl = 'https://ai.dangbei.com/_next/static/chunks/pages/chat-56b75a527ca55cdb.js';
const chatText = await fetch(chatUrl).then(r => r.text());

// Search for v2/chat or agentChat references
console.log('\n=== Chat page: API endpoint references ===');
for (const search of ['v2/chat', 'v1/chat', 'agentChat', 'chatApi', 'agentApi']) {
    const idx = chatText.indexOf(search);
    if (idx >= 0) {
        console.log(`  "${search}" at ${idx}: ${chatText.substring(Math.max(0, idx-100), idx+100)}`);
    } else {
        console.log(`  "${search}": NOT FOUND`);
    }
}
