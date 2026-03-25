// Search through all chat page chunks for the streaming logic
const chunks = [
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
    try {
        const text = await fetch(url).then(r => r.text());
        // Check if this chunk has chat-related code
        const hasChat = text.includes('v2/chat') || text.includes('agentChat') || 
                        text.includes('botCode') || text.includes('stream:!0') ||
                        text.includes('onDownloadProgress') || text.includes('adapter:"fetch"');
        if (hasChat) {
            console.log(`\n=== ${chunk} (${text.length} chars) - HAS CHAT CODE ===`);
            for (const p of ['v2/chat', 'agentChat', 'stream:!0', 'botCode', 'adapter:"fetch"', 'adapter:"xhr"', 'onDownloadProgress', 'responseType', '[DONE]', 'data:']) {
                const i = text.indexOf(p);
                if (i >= 0) {
                    console.log(`  "${p}" at ${i}:`);
                    console.log('  ' + text.substring(Math.max(0, i - 100), Math.min(text.length, i + 200)).replace(/\n/g, ' '));
                }
            }
        }
    } catch (e) {
        console.log(`${chunk}: fetch error`);
    }
}
