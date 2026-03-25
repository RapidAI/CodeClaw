// Search ALL chunks for SSE parsing and chat streaming code
const chunks = [
    'pages/_app-3da91045335ded21.js',
    '6527.9f92f59eeb60fcbc.js',
    '96f49dd3-b9ec81b4944f95c0.js',
    '4062-35991c397ba9845f.js',
    '6603-a3cd13bd6e4e6652.js',
    '7586-31963b10538dd6ba.js',
    '8856-c10b609ebf8048fc.js',
    '4979-adf286ee00e1f343.js',
    '6609-a74d03d0a83f8e45.js',
    '2437-b4518419850b0a9e.js',
    '9974-ddab145ae3871df6.js',
    '2216-57cd310cb3cf2441.js',
    '2806-e5e80b5c63bc655d.js',
    '8100-eaaee8a2289aea87.js',
    '8428-588717c658a18a9d.js',
    'pages/chat-56b75a527ca55cdb.js',
];

for (const chunk of chunks) {
    const url = `https://ai.dangbei.com/_next/static/chunks/${chunk}`;
    const text = await fetch(url).then(r => r.text());
    
    // Search for SSE/streaming patterns
    const patterns = ['DONE', 'content_type', 'conversation.message', 'answer', 'thinking', 'onDownloadProgress', 'adapter:"fetch"'];
    const found = [];
    for (const p of patterns) {
        if (text.includes(p)) found.push(p);
    }
    if (found.length > 0) {
        console.log(`${chunk} (${text.length}): ${found.join(', ')}`);
        // Show context for interesting patterns
        for (const p of found) {
            if (p === 'answer' || p === 'DONE') continue; // too generic
            const i = text.indexOf(p);
            console.log(`  "${p}" at ${i}: ${text.substring(Math.max(0, i - 80), Math.min(text.length, i + 120))}`);
        }
    }
}
