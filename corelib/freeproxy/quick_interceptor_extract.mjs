// Extract the full signing interceptor from the bundle
const bundle = await fetch('https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js').then(r => r.text());

// Find the signing interceptor - search for signatureHeader or the sign-related code
const patterns = [
    'signatureHeader',
    'signOpt',
    'sigInst',
    '_.No',
    '_.Ay',
    'O(e,t)',
];

for (const p of patterns) {
    let idx = 0;
    let count = 0;
    while (count < 3) {
        const i = bundle.indexOf(p, idx);
        if (i < 0) break;
        const start = Math.max(0, i - 300);
        const end = Math.min(bundle.length, i + 300);
        console.log(`\n=== "${p}" at ${i} ===`);
        console.log(bundle.substring(start, end));
        idx = i + p.length;
        count++;
    }
}

// Find the request interceptor that adds sign headers
// Look for the pattern where headers are set from the sign result
const signHeaderIdx = bundle.indexOf('e.headers.sign');
if (signHeaderIdx >= 0) {
    console.log('\n=== sign header setting ===');
    console.log(bundle.substring(Math.max(0, signHeaderIdx - 500), Math.min(bundle.length, signHeaderIdx + 500)));
}

// Look for where the sign Map entries are applied to headers
const headerPatterns = ['headers.sign', 'headers.nonce', 'headers.timestamp', 'headers["sign"]', "headers['sign']"];
for (const p of headerPatterns) {
    const i = bundle.indexOf(p);
    if (i >= 0) {
        console.log(`\n=== "${p}" at ${i} ===`);
        console.log(bundle.substring(Math.max(0, i - 300), Math.min(bundle.length, i + 300)));
    }
}

// Find the function that converts the sign Map to headers
// Look for forEach on the map
const forEachIdx = bundle.indexOf('.forEach((e,t)=>{o.headers[t]=e})');
if (forEachIdx >= 0) {
    console.log('\n=== forEach header copy ===');
    console.log(bundle.substring(Math.max(0, forEachIdx - 500), Math.min(bundle.length, forEachIdx + 500)));
}

// Also look for the main request interceptor that does signing
const mainInterceptorIdx = bundle.indexOf('interceptors.request.use', bundle.indexOf('interceptors.request.use') + 1);
if (mainInterceptorIdx >= 0) {
    console.log('\n=== Second request interceptor ===');
    console.log(bundle.substring(Math.max(0, mainInterceptorIdx - 200), Math.min(bundle.length, mainInterceptorIdx + 2000)));
}
