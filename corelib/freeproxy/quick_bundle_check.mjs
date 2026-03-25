// Check what chat endpoint the current dangbei frontend actually uses
import https from 'https';

// Fetch the main page to find the current _app bundle URL
const html = await fetch('https://ai.dangbei.com/').then(r => r.text());

// Find _app bundle
const appMatch = html.match(/_next\/static\/chunks\/pages\/_app-[a-f0-9]+\.js/);
console.log('_app bundle:', appMatch?.[0] || 'NOT FOUND');

if (appMatch) {
    const bundleUrl = `https://ai.dangbei.com/${appMatch[0]}`;
    console.log('Fetching bundle...');
    const bundle = await fetch(bundleUrl).then(r => r.text());
    console.log('Bundle size:', bundle.length);

    // Search for chat API endpoints
    const patterns = [
        /chatApi\/v\d+\/chat/g,
        /agentApi\/v\d+\/\w+/g,
        /\/ai-search\/[^"'\s]+chat[^"'\s]*/g,
    ];
    
    for (const p of patterns) {
        const matches = [...new Set(bundle.match(p) || [])];
        if (matches.length) console.log(`Pattern ${p}: ${JSON.stringify(matches)}`);
    }

    // Search for the interceptor to see current signing logic
    const interceptorIdx = bundle.indexOf('interceptors.request.use');
    if (interceptorIdx >= 0) {
        const chunk = bundle.substring(interceptorIdx - 200, interceptorIdx + 1500);
        console.log('\n=== Request interceptor ===');
        console.log(chunk);
    }

    // Check for WASM file reference
    const wasmMatch = bundle.match(/sign_bg\.[a-f0-9]+\.wasm/);
    console.log('\nWASM file:', wasmMatch?.[0] || 'NOT FOUND');

    // Check for version header setting
    const versionIdx = bundle.indexOf('"version"');
    if (versionIdx >= 0) {
        // Find all occurrences
        let idx = 0;
        let count = 0;
        while (count < 5) {
            const i = bundle.indexOf('"version"', idx);
            if (i < 0) break;
            const ctx = bundle.substring(Math.max(0, i - 100), Math.min(bundle.length, i + 100));
            if (ctx.includes('header') || ctx.includes('Header') || ctx.includes('.No') || ctx.includes('config')) {
                console.log(`\nversion header context at ${i}:`, ctx);
            }
            idx = i + 10;
            count++;
        }
    }
}
