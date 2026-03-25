// Find the actual chat call site - search for the body format used with v2/chat
const bundle = await fetch('https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js').then(r => r.text());

// Search for the body fields used in chat: botCode, conversationId, question, agentId, stream
// These are the fields we know the chat body contains
const bodyPatterns = [
    'botCode',
    'agentId',
    'conversationId.*question',
    'stream.*botCode',
];

for (const p of ['botCode:"AI_SEARCH"', 'botCode:"', "botCode:'", 'botCode:', '"AI_SEARCH"']) {
    let idx = 0, count = 0;
    while (count < 5) {
        const i = bundle.indexOf(p, idx);
        if (i < 0) break;
        console.log(`\n=== "${p}" at ${i} ===`);
        console.log(bundle.substring(Math.max(0, i - 300), Math.min(bundle.length, i + 500)));
        idx = i + p.length;
        count++;
    }
}

// Also search for where the chat request body is constructed
// Look for patterns like {stream:true, ...} or {question: ...}
for (const p of ['stream:!0,botCode', 'stream:!0,', 'question:e', 'agentId:']) {
    let idx = 0, count = 0;
    while (count < 3) {
        const i = bundle.indexOf(p, idx);
        if (i < 0) break;
        console.log(`\n=== "${p}" at ${i} ===`);
        console.log(bundle.substring(Math.max(0, i - 200), Math.min(bundle.length, i + 300)));
        idx = i + p.length;
        count++;
    }
}
