// Find where eY (chatApi/v2/chat) and e3 (agentChat) are actually called
const bundle = await fetch('https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js').then(r => r.text());

// eY is the v2/chat function, e3 is agentChat
// Find where they're called with actual parameters
// The function names are minified, so let's search for the actual call patterns

// Search for chatApi/v2/chat usage in context
const v2Idx = bundle.indexOf('"/chatApi/v2/chat"');
if (v2Idx >= 0) {
    // The function is defined as eY = function(e){...post({url:"/chatApi/v2/chat"}, e)...}
    // Find where eY is called - look for the variable name
    // Go back to find the variable assignment
    const defStart = bundle.lastIndexOf('=function(e)', v2Idx);
    const varName = bundle.substring(defStart - 3, defStart).trim();
    console.log(`v2/chat function variable: "${varName}"`);
    
    // Search for calls to this function
    const callPattern = varName + '(';
    let idx = 0, count = 0;
    while (count < 5) {
        const i = bundle.indexOf(callPattern, idx);
        if (i < 0) break;
        // Skip the definition itself
        if (Math.abs(i - v2Idx) < 200) { idx = i + 5; continue; }
        const ctx = bundle.substring(Math.max(0, i - 200), Math.min(bundle.length, i + 500));
        console.log(`\n=== Call to ${varName}() at ${i} ===`);
        console.log(ctx);
        idx = i + 5;
        count++;
    }
}

// Also find the streaming/SSE handling code
// The browser uses axios with responseType: 'stream' or onDownloadProgress
const streamPatterns = ['responseType', 'onDownloadProgress', 'text/event-stream', 'EventSource', 'getReader'];
for (const p of streamPatterns) {
    let idx = 980000; // near the API definitions
    while (true) {
        const i = bundle.indexOf(p, idx);
        if (i < 0 || i > 1000000) break;
        console.log(`\n=== "${p}" at ${i} ===`);
        console.log(bundle.substring(Math.max(0, i - 150), Math.min(bundle.length, i + 150)));
        idx = i + p.length;
    }
}

// Search more broadly for stream handling
const streamIdx = bundle.indexOf('fetchEventSource');
if (streamIdx >= 0) {
    console.log(`\n=== fetchEventSource at ${streamIdx} ===`);
    console.log(bundle.substring(Math.max(0, streamIdx - 300), Math.min(bundle.length, streamIdx + 500)));
}

// Search for SSE/stream patterns near the chat code
for (const p of ['onmessage', 'EventSource', 'ReadableStream', 'getReader', 'fetchEventSource', 'sse', 'SSE']) {
    const i = bundle.indexOf(p);
    if (i >= 0) {
        console.log(`\n=== "${p}" at ${i} ===`);
        console.log(bundle.substring(Math.max(0, i - 100), Math.min(bundle.length, i + 200)));
    }
}
