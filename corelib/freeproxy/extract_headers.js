const https = require('https');
https.get('https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js', (res) => {
    let data = '';
    res.on('data', (chunk) => data += chunk);
    res.on('end', () => {
        // Find the full interceptor with appType, token, deviceId
        const idx = data.indexOf('e.headers.set("appType"');
        if (idx !== -1) {
            const start = Math.max(0, idx - 100);
            const end = Math.min(data.length, idx + 600);
            console.log('=== appType + headers ===');
            console.log(data.substring(start, end));
        }
        
        // Find all header set calls in the interceptor
        const pattern = /e\.headers\.set\("[^"]+"/g;
        let match;
        const seen = new Set();
        while ((match = pattern.exec(data)) !== null) {
            if (!seen.has(match[0])) {
                seen.add(match[0]);
                const ctx = data.substring(Math.max(0, match.index - 50), Math.min(data.length, match.index + 150));
                console.log('\n--- ' + match[0] + ' ---');
                console.log(ctx);
            }
        }
    });
});
