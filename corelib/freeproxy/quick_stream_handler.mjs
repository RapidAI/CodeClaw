// Find the streaming response handler in _app bundle
const bundle = await fetch('https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js').then(r => r.text());

// The axios fetch adapter at offset ~1924958 handles streaming
// Let's get a large chunk of the fetch adapter code
const fetchAdapterIdx = bundle.indexOf('fetch:nV&&(async e=>{');
if (fetchAdapterIdx < 0) {
    // Try alternative
    const idx = bundle.indexOf('n0={http:null,xhr:nz,fetch:nV');
    if (idx >= 0) {
        console.log('=== Fetch adapter definition (2000 chars) ===');
        console.log(bundle.substring(idx, Math.min(bundle.length, idx + 2000)));
    }
}

// Search for where the chat response is actually consumed
// The chat page must have code that reads the SSE stream
// Look for data: prefix parsing (SSE format)
for (const p of ['"data:"', "'data:'", 'startsWith("data:")', 'indexOf("data:")', 'split("\\n")', '"[DONE]"', 'event:']) {
    let idx = 0, count = 0;
    while (count < 5) {
        const i = bundle.indexOf(p, idx);
        if (i < 0) break;
        const ctx = bundle.substring(Math.max(0, i - 200), Math.min(bundle.length, i + 300));
        console.log(`\n=== "${p}" at ${i} ===`);
        console.log(ctx);
        idx = i + p.length;
        count++;
    }
}

// Also search for the chat message handling - where tokens are accumulated
for (const p of ['content_type', 'content:', 'type:"answer"', 'type:"text"', 'delta', 'chunk']) {
    let idx = 1700000, count = 0;
    while (count < 3) {
        const i = bundle.indexOf(p, idx);
        if (i < 0 || i > 1990000) break;
        console.log(`\n=== "${p}" at ${i} ===`);
        console.log(bundle.substring(Math.max(0, i - 100), Math.min(bundle.length, i + 200)));
        idx = i + p.length;
        count++;
    }
}
