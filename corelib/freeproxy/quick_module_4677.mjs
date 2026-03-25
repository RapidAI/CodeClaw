// Extract module 4677 which exports the S (axios instance) used for API calls
const bundle = await fetch('https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js').then(r => r.text());

// Find module 4677
const moduleIdx = bundle.indexOf('4677:');
if (moduleIdx < 0) { console.log('Module 4677 not found'); process.exit(1); }

// Extract the module content (find the next module boundary)
let depth = 0, start = moduleIdx;
// Find the function start
const funcStart = bundle.indexOf('function', moduleIdx);
const openBrace = bundle.indexOf('{', funcStart);
let i = openBrace;
depth = 1;
while (depth > 0 && i < bundle.length - 1) {
    i++;
    if (bundle[i] === '{') depth++;
    else if (bundle[i] === '}') depth--;
}
const moduleContent = bundle.substring(moduleIdx, Math.min(i + 1, moduleIdx + 5000));
console.log('=== Module 4677 (first 5000 chars) ===');
console.log(moduleContent);

// Also look for how the chat page actually sends the request
// Search for the page component that handles chat
// Look for patterns like: useSendMessage, handleSend, submitQuestion
for (const p of ['sendMessage', 'handleSend', 'submitQuestion', 'handleSubmit', 'askQuestion', 'sendChat']) {
    const idx = bundle.indexOf(p);
    if (idx >= 0) {
        console.log(`\n=== "${p}" at ${idx} ===`);
        console.log(bundle.substring(Math.max(0, idx - 200), Math.min(bundle.length, idx + 500)));
    }
}
