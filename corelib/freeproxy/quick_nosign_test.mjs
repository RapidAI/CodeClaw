// Test: send chat request WITHOUT signing to see if server returns error vs timeout
import { readFileSync } from 'fs';
import { join } from 'path';
import { homedir } from 'os';

const authPath = join(homedir(), '.maclaw', 'freeproxy', 'dangbei_auth.json');
const auth = JSON.parse(readFileSync(authPath, 'utf-8'));
const cookie = auth.cookie;
const tokenMatch = cookie.match(/token=([^;]+)/);
const token = tokenMatch ? tokenMatch[1] : '';

// Test 1: No signing at all
console.log('=== Test 1: No signing headers ===');
let start = Date.now();
try {
    const resp = await fetch('https://ai-api.dangbei.net/ai-search/chatApi/v2/chat', {
        method: 'POST',
        headers: {
            'content-type': 'application/json',
            'cookie': cookie,
            'origin': 'https://ai.dangbei.com',
        },
        body: '{"stream":true,"botCode":"AI_SEARCH","conversationId":"fake","question":"hi","agentId":""}',
        signal: AbortSignal.timeout(15000),
    });
    console.log(`Status: ${resp.status} (${Date.now() - start}ms)`);
    const text = await resp.text();
    console.log('Response:', text.substring(0, 500));
} catch (e) {
    console.log(`FAILED after ${Date.now() - start}ms:`, e.message);
}

// Test 2: Wrong sign value
console.log('\n=== Test 2: Fake sign headers ===');
start = Date.now();
try {
    const resp = await fetch('https://ai-api.dangbei.net/ai-search/chatApi/v2/chat', {
        method: 'POST',
        headers: {
            'content-type': 'application/json',
            'cookie': cookie,
            'origin': 'https://ai.dangbei.com',
            'sign': 'AAAABBBBCCCCDDDDEEEEFFFFGGGGHHH',
            'nonce': 'fake_nonce_12345678901',
            'timestamp': String(Math.floor(Date.now() / 1000)),
            'apptype': '5',
            'lang': 'zh',
            'client-ver': '1.0.2',
            'appversion': '1.3.9',
            'token': token,
        },
        body: '{"stream":true,"botCode":"AI_SEARCH","conversationId":"fake","question":"hi","agentId":""}',
        signal: AbortSignal.timeout(15000),
    });
    console.log(`Status: ${resp.status} (${Date.now() - start}ms)`);
    const text = await resp.text();
    console.log('Response:', text.substring(0, 500));
} catch (e) {
    console.log(`FAILED after ${Date.now() - start}ms:`, e.message);
}

// Test 3: Simple non-chat API call to verify connectivity
console.log('\n=== Test 3: getUserInfo (no signing needed) ===');
start = Date.now();
try {
    const resp = await fetch('https://ai-api.dangbei.net/ai-search/userInfoApi/v1/getUserInfo', {
        method: 'POST',
        headers: { 'content-type': 'application/json', 'cookie': cookie, 'origin': 'https://ai.dangbei.com' },
        body: '{}',
        signal: AbortSignal.timeout(10000),
    });
    console.log(`Status: ${resp.status} (${Date.now() - start}ms)`);
    const text = await resp.text();
    console.log('Response:', text.substring(0, 300));
} catch (e) {
    console.log(`FAILED after ${Date.now() - start}ms:`, e.message);
}
