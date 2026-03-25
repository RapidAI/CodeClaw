// Find the actual axios instance and how chat streaming works
const bundle = await fetch('https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js').then(r => r.text());

// The interceptor code is in the default export module (rj)
// b = n(4677) but that's CSS. Let me search differently.
// The code shows: b.S.setConfig and b.S.interceptors
// So S is a property of module b's exports

// Find where S is exported with setConfig method
const setConfigIdx = bundle.indexOf('.setConfig({baseUrl:');
console.log('setConfig context:');
const beforeSetConfig = bundle.substring(Math.max(0, setConfigIdx - 100), setConfigIdx);
console.log(beforeSetConfig);

// The variable before .setConfig is the axios instance
// Let's find the actual chat page code that calls the API and handles streaming
// Search in page-specific chunks

// Actually, let me look at the _app page's chat handling
// The chat page likely uses a different chunk. Let me check the page routes
const pageChunks = [...bundle.matchAll(/_next\/static\/chunks\/pages\/([^"]+)/g)];
console.log('\nPage chunks referenced:', pageChunks.map(m => m[1]));

// Search for the actual streaming response handling
// In the browser, axios with responseType can handle streams
// Or they might use fetch API directly for streaming
for (const p of [
    'adapter:"fetch"',
    'responseType:"stream"', 
    'onDownloadProgress',
    'getReader',
    'TextDecoder',
]) {
    let idx = 1700000; // near the chat/sign code area
    while (true) {
        const i = bundle.indexOf(p, idx);
        if (i < 0 || i > 1950000) break;
        console.log(`\n"${p}" at ${i}:`);
        console.log(bundle.substring(Math.max(0, i - 150), Math.min(bundle.length, i + 200)));
        idx = i + p.length;
    }
}

// The key insight: maybe the chat page is in a SEPARATE chunk file
// Let me check what page chunks exist
const htmlResp = await fetch('https://ai.dangbei.com/chat');
const html = await htmlResp.text();
const scripts = [...html.matchAll(/src="([^"]*\.js)"/g)].map(m => m[1]);
console.log('\n=== Scripts loaded on /chat page ===');
scripts.forEach(s => console.log(s));
