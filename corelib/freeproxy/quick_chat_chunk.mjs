// Download and analyze the chat page chunk to find the actual streaming request code
const chatChunk = await fetch('https://ai.dangbei.com/_next/static/chunks/pages/chat-56b75a527ca55cdb.js').then(r => r.text());
console.log('Chat chunk size:', chatChunk.length);

// Search for chat API call, streaming, and body construction
for (const p of [
    'v2/chat', 'v1/chat', 'agentChat',
    'stream:', 'botCode', 'question',
    'adapter', 'responseType', 'onDownloadProgress',
    'getReader', 'TextDecoder',
    'fetchEventSource', 'EventSource',
    'data:', 'event:', '[DONE]',
]) {
    let idx = 0, count = 0;
    while (count < 3) {
        const i = chatChunk.indexOf(p, idx);
        if (i < 0) break;
        console.log(`\n=== "${p}" at ${i} ===`);
        console.log(chatChunk.substring(Math.max(0, i - 200), Math.min(chatChunk.length, i + 300)));
        idx = i + p.length;
        count++;
    }
}
