// Test: check TLS fingerprint hypothesis
// Try connecting to v2/chat with different approaches
import { readFileSync } from 'fs';
import { join } from 'path';
import { homedir } from 'os';
import https from 'https';

const authPath = join(homedir(), '.maclaw', 'freeproxy', 'dangbei_auth.json');
const auth = JSON.parse(readFileSync(authPath, 'utf-8'));
const cookie = auth.cookie;

// Test 1: Check response headers from a simple OPTIONS/HEAD request
console.log('=== Test 1: HEAD request to v2/chat ===');
await new Promise((resolve) => {
    const req = https.request({
        hostname: 'ai-api.dangbei.net',
        path: '/ai-search/chatApi/v2/chat',
        method: 'OPTIONS',
        headers: {
            'Origin': 'https://ai.dangbei.com',
            'Access-Control-Request-Method': 'POST',
            'Access-Control-Request-Headers': 'content-type,sign,nonce,timestamp',
        },
        timeout: 10000,
    }, (res) => {
        console.log(`Status: ${res.statusCode}`);
        console.log('Headers:', JSON.stringify(res.headers, null, 2));
        res.on('data', () => {});
        res.on('end', resolve);
    });
    req.on('timeout', () => { console.log('OPTIONS timeout'); req.destroy(); resolve(); });
    req.on('error', (e) => { console.log('OPTIONS error:', e.message); resolve(); });
    req.end();
});

// Test 2: Check if the domain uses Cloudflare or other WAF
console.log('\n=== Test 2: Check WAF/CDN headers ===');
await new Promise((resolve) => {
    const req = https.request({
        hostname: 'ai-api.dangbei.net',
        path: '/ai-search/userInfoApi/v1/getUserInfo',
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'Cookie': cookie,
            'Origin': 'https://ai.dangbei.com',
        },
        timeout: 10000,
    }, (res) => {
        console.log(`Status: ${res.statusCode}`);
        // Look for WAF/CDN indicators
        const wafHeaders = ['server', 'x-powered-by', 'cf-ray', 'x-cache', 'via', 'x-cdn', 'x-waf', 'x-request-id'];
        for (const h of wafHeaders) {
            if (res.headers[h]) console.log(`  ${h}: ${res.headers[h]}`);
        }
        console.log('All headers:', JSON.stringify(res.headers, null, 2));
        let body = '';
        res.on('data', (d) => body += d);
        res.on('end', () => { console.log('Body:', body.substring(0, 200)); resolve(); });
    });
    req.on('error', (e) => { console.log('Error:', e.message); resolve(); });
    req.write('{}');
    req.end();
});

// Test 3: Try v2/chat with Node.js https module (not fetch) to see if it behaves differently
console.log('\n=== Test 3: v2/chat via https module ===');
await new Promise((resolve) => {
    const body = '{"stream":true,"botCode":"AI_SEARCH","conversationId":"fake","question":"hi","agentId":""}';
    const req = https.request({
        hostname: 'ai-api.dangbei.net',
        path: '/ai-search/chatApi/v2/chat',
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'Cookie': cookie,
            'Origin': 'https://ai.dangbei.com',
            'Content-Length': Buffer.byteLength(body),
        },
        timeout: 10000,
    }, (res) => {
        console.log(`Status: ${res.statusCode}`);
        console.log('Headers:', JSON.stringify(res.headers, null, 2));
        let data = '';
        res.on('data', (d) => data += d);
        res.on('end', () => { console.log('Body:', data.substring(0, 300)); resolve(); });
    });
    req.on('timeout', () => { console.log('v2/chat https timeout'); req.destroy(); resolve(); });
    req.on('error', (e) => { console.log('v2/chat https error:', e.message); resolve(); });
    req.write(body);
    req.end();
});

// Test 4: Try v2/chat with HTTP/2
console.log('\n=== Test 4: v2/chat via HTTP/2 ===');
import http2 from 'http2';
await new Promise((resolve) => {
    const client = http2.connect('https://ai-api.dangbei.net');
    const body = '{"stream":true,"botCode":"AI_SEARCH","conversationId":"fake","question":"hi","agentId":""}';
    const req = client.request({
        ':method': 'POST',
        ':path': '/ai-search/chatApi/v2/chat',
        'content-type': 'application/json',
        'cookie': cookie,
        'origin': 'https://ai.dangbei.com',
        'content-length': Buffer.byteLength(body),
    });
    
    let headers = null;
    req.on('response', (h) => {
        headers = h;
        console.log(`Status: ${h[':status']}`);
        console.log('Headers:', JSON.stringify(h, null, 2));
    });
    
    let data = '';
    req.on('data', (d) => data += d);
    req.on('end', () => {
        console.log('Body:', data.substring(0, 300));
        client.close();
        resolve();
    });
    
    setTimeout(() => {
        if (!headers) {
            console.log('HTTP/2 timeout - no response headers received');
        } else {
            console.log('HTTP/2 timeout - headers received but body incomplete');
        }
        client.close();
        resolve();
    }, 10000);
    
    req.write(body);
    req.end();
});
