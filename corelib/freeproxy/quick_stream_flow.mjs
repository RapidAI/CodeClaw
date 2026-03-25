// Find the actual streaming flow - onDownloadProgress handler and chat page logic
const url = 'https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js';
const text = await fetch(url).then(r => r.text());

// The chat page chunk likely has the actual chat logic
const chatUrl = 'https://ai.dangbei.com/_next/static/chunks/pages/chat-56b75a527ca55cdb.js';
const chatText = await fetch(chatUrl).then(r => r.text());

console.log('=== Chat page chunk size:', chatText.length, '===');

// Search chat page for key patterns
const chatPatterns = ['agentChat', 'v2/chat', 'v1/chat', 'botCode', 'AI_SEARCH', 
    'question', 'conversationId', 'stream', 'onDownloadProgress', 'getReader',
    'modelCode', 'deepseek', 'content_type', 'thinking'];
for (const p of chatPatterns) {
    const idx = chatText.indexOf(p);
    if (idx >= 0) {
        console.log(`\n"${p}" at ${idx}:`);
        console.log(chatText.substring(Math.max(0, idx - 150), Math.min(chatText.length, idx + 200)));
    }
}

// Now look at the _app bundle's onDownloadProgress context more carefully
console.log('\n\n=== _app onDownloadProgress contexts ===');
let idx = 0;
let count = 0;
while ((idx = text.indexOf('onDownloadProgress', idx)) !== -1 && count < 10) {
    const ctx = text.substring(Math.max(0, idx - 300), Math.min(text.length, idx + 400));
    // Only show if it's near chat-related code
    if (ctx.includes('chat') || ctx.includes('stream') || ctx.includes('question') || ctx.includes('agent') || ctx.includes('response')) {
        console.log(`\n  #${++count} at ${idx}:`);
        console.log(`  ...${ctx}...`);
    }
    idx += 18;
}

// Search for the actual fetch/axios streaming setup near chat endpoints
// The key is: the code that calls eY (v2/chat) or e3 (agentChat) with streaming config
console.log('\n\n=== Searching for streaming config near API calls ===');
// Look for patterns like: {data: {...}, onDownloadProgress: ...}
const streamConfigPattern = /\{[^}]*onDownloadProgress[^}]*\}/g;
let match;
count = 0;
while ((match = streamConfigPattern.exec(text)) !== null && count < 5) {
    if (match[0].length < 500) {
        console.log(`\n  #${++count} at ${match.index} (${match[0].length} chars):`);
        // Show more context
        const ctx = text.substring(Math.max(0, match.index - 200), Math.min(text.length, match.index + match[0].length + 200));
        console.log(`  ...${ctx}...`);
    }
}
