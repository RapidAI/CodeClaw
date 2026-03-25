// Test various endpoints and connection methods to isolate the v2/chat timeout issue
import { readFileSync } from 'fs';
import { join } from 'path';
import { homedir } from 'os';

const authPath = join(homedir(), '.maclaw', 'freeproxy', 'dangbei_auth.json');
const auth = JSON.parse(readFileSync(authPath, 'utf-8'));
const cookie = auth.cookie;

async function testEndpoint(label, url, method, body, extraHeaders = {}) {
    const start = Date.now();
    try {
        const resp = await fetch(url, {
            method,
            headers: {
                'content-type': 'application/json',
                'cookie': cookie,
                'origin': 'https://ai.dangbei.com',
                'referer': 'https://ai.dangbei.com/',
                'user-agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36',
                ...extraHeaders,
            },
            body: body,
            signal: AbortSignal.timeout(12000),
        });
        const elapsed = Date.now() - start;
        const text = await resp.text();
        console.log(`[${label}] ${resp.status} in ${elapsed}ms - ${text.substring(0, 200)}`);
    } catch (e) {
        console.log(`[${label}] FAILED after ${Date.now() - start}ms: ${e.message}`);
    }
}

// Test 1: v1/chat (old endpoint, uses MD5 signing)
console.log('=== Testing various endpoints ===\n');

await testEndpoint('v1/create', 'https://ai-api.dangbei.net/ai-search/conversationApi/v1/create', 'POST', '{}');

// Create a real conversation for testing
const createResp = await fetch('https://ai-api.dangbei.net/ai-search/conversationApi/v1/create', {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'cookie': cookie, 'origin': 'https://ai.dangbei.com' },
    body: '{}',
});
const createData = await createResp.json();
const convId = createData.data?.conversationId;
console.log('Created conversation:', convId);

if (convId) {
    const chatBody = `{"stream":true,"botCode":"AI_SEARCH","conversationId":"${convId}","question":"hi","agentId":""}`;

    // Test v2/chat with stream:false
    await testEndpoint('v2/chat stream:false',
        'https://ai-api.dangbei.net/ai-search/chatApi/v2/chat', 'POST',
        `{"stream":false,"botCode":"AI_SEARCH","conversationId":"${convId}","question":"hi","agentId":""}`);

    // Test v2/chat with accept: application/json instead of text/event-stream
    await testEndpoint('v2/chat accept:json',
        'https://ai-api.dangbei.net/ai-search/chatApi/v2/chat', 'POST', chatBody,
        { 'accept': 'application/json' });

    // Test v2/chat with accept: text/event-stream
    await testEndpoint('v2/chat accept:sse',
        'https://ai-api.dangbei.net/ai-search/chatApi/v2/chat', 'POST', chatBody,
        { 'accept': 'text/event-stream' });

    // Test v1/chat endpoint if it exists
    await testEndpoint('v1/chat',
        'https://ai-api.dangbei.net/ai-search/chatApi/v1/chat', 'POST', chatBody);

    // Cleanup
    await fetch(`https://ai-api.dangbei.net/ai-search/conversationApi/v1/delete?conversationId=${convId}`, {
        method: 'DELETE', headers: { 'cookie': cookie, 'origin': 'https://ai.dangbei.com' },
    }).catch(() => {});
}
