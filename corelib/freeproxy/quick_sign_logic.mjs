// Extract the complete signing logic from the bundle
const bundle = await fetch('https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js').then(r => r.text());

// The key code is around offset 1866414 where _.No and _.Ay are used
// Let's get a much larger chunk around the signing interceptor
const idx = bundle.indexOf('(0,_.Ay)()');
if (idx < 0) { console.log('_.Ay not found'); process.exit(1); }

// Go back further to find the start of the interceptor function
const start = Math.max(0, idx - 2000);
const end = Math.min(bundle.length, idx + 2000);
console.log('=== Signing interceptor context (4000 chars) ===');
console.log(bundle.substring(start, end));

// Also find where the sign Map is applied to headers
// Search for where the Map entries become HTTP headers
console.log('\n\n=== Looking for Map -> headers conversion ===');
// The pattern should be something like: map.forEach((v,k) => headers[k] = v)
// or: e.headers.sign = map.get('sign')
const mapToHeaderPatterns = [
    'forEach(function(e,t){',
    '.get("sign")',
    '.get("nonce")',
    'headers.sign',
    'e.headers[t]',
    'headers.set(',
];
for (const p of mapToHeaderPatterns) {
    let searchIdx = 1860000; // near the interceptor
    const i = bundle.indexOf(p, searchIdx);
    if (i >= 0 && i < 1880000) {
        console.log(`\n"${p}" at ${i}:`);
        console.log(bundle.substring(Math.max(0, i - 200), Math.min(bundle.length, i + 200)));
    }
}

// Find the function that takes the sign Map and sets headers
// Look for the code after get_sign returns
const getSignIdx = bundle.indexOf('get_sign');
if (getSignIdx >= 0) {
    console.log(`\n\n=== get_sign at ${getSignIdx} ===`);
    console.log(bundle.substring(Math.max(0, getSignIdx - 500), Math.min(bundle.length, getSignIdx + 500)));
}
