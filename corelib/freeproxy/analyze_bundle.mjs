// Download and analyze the _app bundle to find exact body construction and sign call
import https from 'https';

function fetch(url) {
    return new Promise((resolve, reject) => {
        https.get(url, res => {
            let data = '';
            res.on('data', chunk => data += chunk);
            res.on('end', () => resolve(data));
            res.on('error', reject);
        });
    });
}

console.log('Fetching _app bundle...');
const data = await fetch('https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js');
console.log(`Bundle size: ${data.length} bytes`);

// Find the sign call: (0,_.No)(body, url)
// The sign function is called with (body, url) and returns {sign, nonce, timestamp}
const signPatterns = [
    /\(0,\w+\.No\)\(/g,
    /No\)\([^)]*\)/g,
    /get_sign/g,
    /\.No\(/g,
];

for (const pat of signPatterns) {
    let m;
    while ((m = pat.exec(data)) !== null) {
        const ctx = data.substring(Math.max(0, m.index - 200), Math.min(data.length, m.index + 200));
        console.log(`\n=== Pattern: ${pat.source} at ${m.index} ===`);
        console.log(ctx);
    }
}

// Find the v2/chat endpoint reference
const chatPatterns = [
    /v2\/chat/g,
    /chatApi/g,
    /botCode/g,
    /agentId/g,
];

for (const pat of chatPatterns) {
    let m;
    const seen = new Set();
    while ((m = pat.exec(data)) !== null) {
        const ctx = data.substring(Math.max(0, m.index - 150), Math.min(data.length, m.index + 150));
        const key = `${m.index}`;
        if (!seen.has(key)) {
            seen.add(key);
            console.log(`\n=== ${pat.source} at ${m.index} ===`);
            console.log(ctx);
        }
    }
}

// Find the request interceptor that adds headers
const headerIdx = data.indexOf('e.headers.set("timestamp"');
if (headerIdx !== -1) {
    console.log('\n=== FULL INTERCEPTOR (timestamp header area) ===');
    // Go back to find the function start
    let start = headerIdx;
    let braceCount = 0;
    for (let i = headerIdx; i >= Math.max(0, headerIdx - 2000); i--) {
        if (data[i] === '}') braceCount++;
        if (data[i] === '{') {
            braceCount--;
            if (braceCount < 0) { start = i; break; }
        }
    }
    console.log(data.substring(start, Math.min(data.length, headerIdx + 500)));
}

// Find the O(e,t) function that extracts body for signing
// O = function that returns t.body for POST, "" for GET
const bodyExtractPatterns = [
    /function\s+\w+\(\w+,\w+\)\s*\{[^}]*\.body/g,
    /\.body\s*\?\?\s*""/g,
    /t\.body/g,
];

for (const pat of bodyExtractPatterns) {
    let m;
    while ((m = pat.exec(data)) !== null) {
        const ctx = data.substring(Math.max(0, m.index - 100), Math.min(data.length, m.index + 100));
        console.log(`\n=== body pattern: ${pat.source} at ${m.index} ===`);
        console.log(ctx);
    }
}
