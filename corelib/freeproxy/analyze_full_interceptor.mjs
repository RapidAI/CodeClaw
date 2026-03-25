import https from 'https';
function fetch(url) {
    return new Promise((resolve, reject) => {
        https.get(url, res => { let d=''; res.on('data',c=>d+=c); res.on('end',()=>resolve(d)); res.on('error',reject); });
    });
}
const data = await fetch('https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js');

// Get the full interceptor from "interceptors.request.use(function(e,t)" to the response interceptor
const start = data.indexOf('b.S.interceptors.request.use(function(e,t)');
const end = data.indexOf('b.S.interceptors.response.use', start + 10);
const interceptor = data.substring(start, end);

console.log('=== FULL REQUEST INTERCEPTOR ===');
console.log(interceptor);

// Also find all headers.set calls in the full interceptor
const pat = /e\.headers\.set\("([^"]+)",\s*([^)]+)\)/g;
let m;
console.log('\n=== All headers.set calls ===');
while ((m = pat.exec(interceptor)) !== null) {
    console.log(`  ${m[1]} = ${m[2]}`);
}
