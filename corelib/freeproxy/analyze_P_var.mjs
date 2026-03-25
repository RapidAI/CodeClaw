// Find what P="196610061" is used for in the bundle
import https from 'https';

function fetch(url) {
    return new Promise((resolve, reject) => {
        https.get(url, res => {
            let data = '';
            res.on('data', chunk => data += chunk);
            res.on('end', () => resolve(data));
            res.on('error', reject);
        });
    });
}

const data = await fetch('https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js');

// Find P="196610061" and its context
const pIdx = data.indexOf('P="196610061"');
if (pIdx !== -1) {
    console.log('=== P="196610061" context (wide) ===');
    console.log(data.substring(Math.max(0, pIdx - 500), Math.min(data.length, pIdx + 500)));
}

// Find all uses of P in the interceptor area (around 1865000-1870000)
const area = data.substring(1860000, 1875000);

// Find the full interceptor function
// Look for the sign call and surrounding code
const signCallIdx = data.indexOf('(0,_.No)(O(e,t),t.url)');
if (signCallIdx !== -1) {
    console.log('\n=== Sign call context (very wide) ===');
    // Go back to find function start
    let start = signCallIdx;
    for (let i = signCallIdx; i >= Math.max(0, signCallIdx - 3000); i--) {
        // Look for interceptors.request.use or similar
        if (data.substring(i, i + 20).includes('interceptors')) {
            start = i;
            break;
        }
    }
    console.log(data.substring(start, Math.min(data.length, signCallIdx + 1000)));
}

// Find where P is referenced after its declaration
const pDecl = data.indexOf('P="196610061"');
if (pDecl !== -1) {
    // Search for P being used in the next 5000 chars
    const after = data.substring(pDecl, pDecl + 5000);
    // Find all occurrences of P that look like variable usage
    const pUsages = [];
    const pPat = /[^a-zA-Z_]P[^a-zA-Z_0-9]/g;
    let m;
    while ((m = pPat.exec(after)) !== null) {
        pUsages.push({ idx: m.index, ctx: after.substring(Math.max(0, m.index - 30), m.index + 30) });
    }
    console.log('\n=== P usages after declaration ===');
    pUsages.forEach(u => console.log(`  at ${u.idx}: ${u.ctx}`));
}

// Also find the full request interceptor setup
const interceptorIdx = data.indexOf('interceptors.request.use');
if (interceptorIdx !== -1) {
    console.log('\n=== interceptors.request.use ===');
    console.log(data.substring(interceptorIdx, Math.min(data.length, interceptorIdx + 3000)));
}

// Find setConfig and baseUrl
const setConfigIdx = data.indexOf('setConfig');
if (setConfigIdx !== -1) {
    console.log('\n=== setConfig ===');
    console.log(data.substring(Math.max(0, setConfigIdx - 200), Math.min(data.length, setConfigIdx + 500)));
}
