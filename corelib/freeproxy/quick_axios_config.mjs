// Find how the axios instance (b.S) is configured for chat requests
const bundle = await fetch('https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js').then(r => r.text());

// b.S is the axios instance. Find its creation
// The interceptor code shows: b.S.setConfig({baseUrl:"https://ai-api.dangbei.net/ai-search"})
const setConfigIdx = bundle.indexOf('setConfig({baseUrl:');
if (setConfigIdx >= 0) {
    console.log('=== setConfig ===');
    console.log(bundle.substring(Math.max(0, setConfigIdx - 500), Math.min(bundle.length, setConfigIdx + 200)));
}

// Find the b.S definition - it's likely an axios.create() or custom wrapper
// Search for the module that exports S
const bSIdx = bundle.indexOf('b.S.interceptors');
if (bSIdx >= 0) {
    // Go back to find where b is imported
    const moduleStart = bundle.lastIndexOf('n.d(t,', bSIdx);
    if (moduleStart >= 0) {
        console.log('\n=== Module exports near b.S ===');
        console.log(bundle.substring(moduleStart, Math.min(bundle.length, moduleStart + 500)));
    }
}

// Find the actual chat page component that calls the API
// Search for patterns like: chat, send, submit with question
for (const p of ['fetchEventSource', '@microsoft/fetch-event-source', 'EventSourcePolyfill', 'sse-', 'text/event-stream']) {
    const i = bundle.indexOf(p);
    if (i >= 0) {
        console.log(`\n=== "${p}" at ${i} ===`);
        console.log(bundle.substring(Math.max(0, i - 200), Math.min(bundle.length, i + 300)));
    }
}

// The key question: does the browser use fetch API or XMLHttpRequest for the chat endpoint?
// Search for adapter:"fetch" near chat-related code
for (const p of ['adapter:"fetch"', "adapter:'fetch'", 'adapter:["fetch"', 'responseType:"stream"']) {
    let idx = 0, count = 0;
    while (count < 5) {
        const i = bundle.indexOf(p, idx);
        if (i < 0) break;
        console.log(`\n=== "${p}" at ${i} ===`);
        console.log(bundle.substring(Math.max(0, i - 200), Math.min(bundle.length, i + 200)));
        idx = i + p.length;
        count++;
    }
}

// Search for where the chat response is consumed as a stream
for (const p of ['onDownloadProgress', 'onChunk', 'onData', 'decoder.decode']) {
    let idx = 0, count = 0;
    while (count < 3) {
        const i = bundle.indexOf(p, idx);
        if (i < 0) break;
        const ctx = bundle.substring(Math.max(0, i - 200), Math.min(bundle.length, i + 300));
        if (ctx.includes('chat') || ctx.includes('stream') || ctx.includes('event') || ctx.includes('data:') || ctx.includes('SSE') || ctx.includes('text')) {
            console.log(`\n=== "${p}" at ${i} ===`);
            console.log(ctx);
        }
        idx = i + p.length;
        count++;
    }
}
