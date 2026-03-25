// Extract chat endpoint usage from the bundle
const bundle = await fetch('https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js').then(r => r.text());

// Find all chat-related endpoint references
const endpoints = [
    'chatApi/v1/chat',
    'chatApi/v2/chat', 
    'agentApi/v1/agentChat',
    'agentApi/v1/create',
    'agentApi/v1/page',
];

for (const ep of endpoints) {
    let idx = 0;
    let count = 0;
    while (count < 3) {
        const i = bundle.indexOf(ep, idx);
        if (i < 0) break;
        const start = Math.max(0, i - 400);
        const end = Math.min(bundle.length, i + 400);
        console.log(`\n=== "${ep}" at ${i} ===`);
        console.log(bundle.substring(start, end));
        idx = i + ep.length;
        count++;
    }
}

// Also search for the actual fetch/axios call that sends to chat
const chatCallPatterns = ['/v2/chat', '/v1/chat', '/agentChat'];
for (const p of chatCallPatterns) {
    const i = bundle.indexOf(`"${p}"`);
    if (i >= 0) {
        console.log(`\n=== Quoted "${p}" at ${i} ===`);
        console.log(bundle.substring(Math.max(0, i - 300), Math.min(bundle.length, i + 300)));
    }
    const i2 = bundle.indexOf(`'${p}'`);
    if (i2 >= 0) {
        console.log(`\n=== Single-quoted '${p}' at ${i2} ===`);
        console.log(bundle.substring(Math.max(0, i2 - 300), Math.min(bundle.length, i2 + 300)));
    }
}
