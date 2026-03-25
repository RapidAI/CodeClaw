// Check if "version" header is set in the interceptor
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

// Find the exact interceptor code that sets headers
const idx = data.indexOf('e.headers.set("timestamp"');
if (idx === -1) { console.log('Not found'); process.exit(1); }

// Get the full case 5 block
const caseStart = data.lastIndexOf('case 5:', idx);
const caseEnd = data.indexOf('[2,e]', idx);
const caseBlock = data.substring(caseStart, caseEnd + 10);

console.log('=== Case 5 (header setting) ===');
console.log(caseBlock);

// Check all headers.set calls
const headerSetPattern = /e\.headers\.set\("([^"]+)"/g;
let m;
console.log('\n=== All headers set in interceptor ===');
while ((m = headerSetPattern.exec(caseBlock)) !== null) {
    console.log(`  ${m[1]}`);
}

// Check if "version" is set anywhere in headers
const versionInHeaders = caseBlock.includes('headers.set("version"');
console.log(`\n"version" header set in case 5: ${versionInHeaders}`);

// Also check if there's another interceptor that sets version
const allVersionSets = [];
const vPat = /headers\.set\("version"/g;
while ((m = vPat.exec(data)) !== null) {
    const ctx = data.substring(Math.max(0, m.index - 100), Math.min(data.length, m.index + 100));
    allVersionSets.push({ idx: m.index, ctx });
}
console.log(`\nTotal "version" header sets in bundle: ${allVersionSets.length}`);
allVersionSets.forEach(v => console.log(`  at ${v.idx}: ${v.ctx}`));
