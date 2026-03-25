// Use puppeteer to make a REAL browser request and capture the result
// This definitively proves whether the issue is signing or transport
// Usage: node puppeteer_test.mjs

import puppeteer from 'puppeteer';
import { readFileSync } from 'fs';
import { join } from 'path';
import { homedir } from 'os';

// Load cookies
const cookiePath = join(homedir(), '.maclaw', 'freeproxy', 'dangbei_cookies.json');
let cookies;
try {
    cookies = JSON.parse(readFileSync(cookiePath, 'utf8'));
} catch (e) {
    console.error('Cannot read cookies from', cookiePath);
    process.exit(1);
}

console.log('Loaded', cookies.length, 'cookies');

const browser = await puppeteer.launch({
    headless: true,
    args: ['--no-sandbox'],
});

const page = await browser.newPage();

// Set cookies
for (const c of cookies) {
    try {
        await page.setCookie({
            name: c.Name || c.name,
            value: c.Value || c.value,
            domain: c.Domain || c.domain || '.dangbei.com',
            path: c.Path || c.path || '/',
        });
    } catch (e) {
        // skip invalid cookies
    }
}

// Navigate to the page to load JS/WASM
console.log('Navigating to ai.dangbei.com...');
await page.goto('https://ai.dangbei.com', { waitUntil: 'networkidle2', timeout: 30000 });
console.log('Page loaded');

// Intercept the next v2/chat request
let interceptedRequest = null;
let interceptedResponse = null;

page.on('request', req => {
    if (req.url().includes('/chatApi/v2/chat')) {
        interceptedRequest = {
            url: req.url(),
            method: req.method(),
            headers: req.headers(),
            postData: req.postData(),
        };
        console.log('\n=== INTERCEPTED REQUEST ===');
        console.log('URL:', interceptedRequest.url);
        console.log('Headers:', JSON.stringify(interceptedRequest.headers, null, 2));
        console.log('Body:', interceptedRequest.postData);
    }
});

page.on('response', async resp => {
    if (resp.url().includes('/chatApi/v2/chat')) {
        interceptedResponse = {
            status: resp.status(),
            headers: resp.headers(),
        };
        console.log('\n=== INTERCEPTED RESPONSE ===');
        console.log('Status:', interceptedResponse.status);
        console.log('Headers:', JSON.stringify(interceptedResponse.headers, null, 2));
        try {
            const text = await resp.text();
            console.log('Body (first 500):', text.substring(0, 500));
        } catch (e) {
            console.log('Could not read body:', e.message);
        }
    }
});

// Type a message in the chat
console.log('\nTrying to send a chat message via the page...');

// Wait for the chat input to appear
try {
    await page.waitForSelector('textarea, input[type="text"], [contenteditable]', { timeout: 10000 });
    console.log('Found input element');
    
    // Try to type in the first textarea
    const textarea = await page.$('textarea');
    if (textarea) {
        await textarea.type('hi', { delay: 50 });
        // Press Enter to send
        await textarea.press('Enter');
        console.log('Sent message via textarea');
    }
} catch (e) {
    console.log('Could not find/use input:', e.message);
    console.log('Trying direct API call via page.evaluate...');
}

// Wait for the response
await new Promise(r => setTimeout(r, 10000));

if (!interceptedRequest) {
    console.log('\nNo v2/chat request intercepted. Trying direct evaluate...');
    
    // Execute the sign + fetch directly in the browser context
    const result = await page.evaluate(async () => {
        // The sign function should be available through webpack modules
        // Let's just make a direct fetch and see what happens
        const body = JSON.stringify({
            stream: true,
            botCode: "AI_SEARCH", 
            conversationId: "test123",
            question: "hi",
            agentId: "",
        });
        
        try {
            const resp = await fetch('https://ai-api.dangbei.net/ai-search/chatApi/v2/chat', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: body,
            });
            return { status: resp.status, text: await resp.text().catch(() => 'read error') };
        } catch (e) {
            return { error: e.message };
        }
    });
    console.log('Direct fetch result:', result);
}

await browser.close();
console.log('\nDone');
