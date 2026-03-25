// Extract the API function definitions and their callers from _app bundle
const url = 'https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js';
const text = await fetch(url).then(r => r.text());

// Find all API endpoint definitions (eY, e3, eQ, etc.)
// Pattern: eX=function(e){...url:"/..."}
const apiDefs = [...text.matchAll(/\b(e[A-Z0-9_]+)\s*=\s*function\s*\(e\)\s*\{[^}]*url:\s*"([^"]+)"/g)];
console.log('=== API endpoint definitions ===');
for (const m of apiDefs) {
    console.log(`  ${m[1]} -> ${m[2]}`);
}

// Find the agentChat function and its surrounding context
const agentIdx = text.indexOf('"/agentApi/v1/agentChat"');
if (agentIdx > 0) {
    console.log('\n=== agentChat context (500 chars before, 500 after) ===');
    console.log(text.substring(Math.max(0, agentIdx - 500), agentIdx + 500));
}

// Find where agentChat (e3) is called
// First find the function name
const agentFnMatch = text.match(/\b(\w+)\s*=\s*function\s*\(e\)\s*\{[^}]*url:\s*"\/agentApi\/v1\/agentChat"/);
if (agentFnMatch) {
    const fnName = agentFnMatch[1];
    console.log(`\n=== agentChat function name: ${fnName} ===`);
    
    // Find all calls to this function
    const callPattern = new RegExp(`\\b${fnName}\\s*\\(`, 'g');
    let match;
    let count = 0;
    while ((match = callPattern.exec(text)) !== null && count < 10) {
        const ctx = text.substring(Math.max(0, match.index - 200), Math.min(text.length, match.index + 300));
        console.log(`\n  Call ${++count} at ${match.index}:`);
        console.log(`  ...${ctx}...`);
    }
}

// Find v2/chat function and callers
const v2FnMatch = text.match(/\b(\w+)\s*=\s*function\s*\(e\)\s*\{[^}]*url:\s*"\/chatApi\/v2\/chat"/);
if (v2FnMatch) {
    const fnName = v2FnMatch[1];
    console.log(`\n=== v2/chat function name: ${fnName} ===`);
    
    const callPattern = new RegExp(`\\b${fnName}\\s*\\(`, 'g');
    let match;
    let count = 0;
    while ((match = callPattern.exec(text)) !== null && count < 10) {
        const ctx = text.substring(Math.max(0, match.index - 200), Math.min(text.length, match.index + 300));
        console.log(`\n  Call ${++count} at ${match.index}:`);
        console.log(`  ...${ctx}...`);
    }
}

// Search for stream reading / SSE parsing logic
console.log('\n=== Stream/SSE parsing patterns ===');
const ssePatterns = [
    /getReader\(\)[\s\S]{0,500}read\(\)/g,
    /onDownloadProgress[\s\S]{0,300}/g,
    /data:\s*\[DONE\]/g,
    /content_type/g,
];

for (const pat of ssePatterns) {
    const matches = [...text.matchAll(pat)];
    if (matches.length > 0) {
        console.log(`\n  Pattern: ${pat.source} (${matches.length} matches)`);
        for (const m of matches.slice(0, 3)) {
            console.log(`    at ${m.index}: ...${text.substring(m.index, Math.min(text.length, m.index + 200))}...`);
        }
    }
}
