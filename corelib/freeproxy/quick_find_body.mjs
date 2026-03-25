// Find where the chat request body is built
const bundle = await fetch('https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js').then(r => r.text());

// Search for body construction patterns
for (const p of [
    'stream:!0', 'stream:!1',
    'question:', 'conversationId:',
    'body:{stream', 'data:{stream',
    'body:{question', 'data:{question',
    '{stream:!0,botCode',
    'responseType:"stream"',
    'responseType:"text"',
    'adapter:"fetch"',
    'adapter:',
]) {
    let idx = 0, count = 0;
    while (count < 3) {
        const i = bundle.indexOf(p, idx);
        if (i < 0) break;
        const ctx = bundle.substring(Math.max(0, i - 150), Math.min(bundle.length, i + 300));
        // Filter: only show if it looks chat-related
        if (ctx.includes('chat') || ctx.includes('question') || ctx.includes('botCode') || ctx.includes('stream') || ctx.includes('conversation') || ctx.includes('agent') || ctx.includes('fetch') || ctx.includes('adapter')) {
            console.log(`\n=== "${p}" at ${i} ===`);
            console.log(ctx);
        }
        idx = i + p.length;
        count++;
    }
}
