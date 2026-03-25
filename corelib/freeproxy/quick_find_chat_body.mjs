// Find the actual chat body construction and stream handling
const url = 'https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js';
const text = await fetch(url).then(r => r.text());

// Search for botCode usage (this is in the body construction)
console.log('=== botCode occurrences ===');
let idx = 0;
let count = 0;
while ((idx = text.indexOf('botCode', idx)) !== -1 && count < 20) {
    const ctx = text.substring(Math.max(0, idx - 150), Math.min(text.length, idx + 200));
    console.log(`\n  #${++count} at ${idx}:`);
    console.log(`  ...${ctx}...`);
    idx += 7;
}

// Search for "question" field near "conversationId" (body construction)
console.log('\n\n=== question + conversationId patterns ===');
idx = 0;
count = 0;
while ((idx = text.indexOf('conversationId', idx)) !== -1 && count < 20) {
    // Check if "question" is nearby
    const nearby = text.substring(Math.max(0, idx - 300), Math.min(text.length, idx + 300));
    if (nearby.includes('question')) {
        console.log(`\n  #${++count} at ${idx}:`);
        console.log(`  ...${nearby}...`);
    }
    idx += 14;
}

// Search for stream response handling - look for patterns like parsing SSE data
console.log('\n\n=== SSE data parsing (data: prefix handling) ===');
const dataPatterns = [
    /data:\s*\[DONE\]/g,
    /split\s*\(\s*["']\\n["']\s*\)/g,
    /split\s*\(\s*["']data:["']\s*\)/g,
    /\.replace\s*\(\s*["']data:["']/g,
    /startsWith\s*\(\s*["']data:["']\s*\)/g,
];
for (const pat of dataPatterns) {
    const matches = [...text.matchAll(pat)];
    console.log(`  ${pat.source}: ${matches.length} matches`);
    for (const m of matches.slice(0, 3)) {
        const ctx = text.substring(Math.max(0, m.index - 100), Math.min(text.length, m.index + 200));
        console.log(`    at ${m.index}: ...${ctx}...`);
    }
}

// Search for adapter:"fetch" which is used for streaming
console.log('\n\n=== adapter:"fetch" or adapter:fetch ===');
for (const search of ['adapter:"fetch"', "adapter:'fetch'", 'adapter: "fetch"', 'adapter:"fetch"']) {
    idx = text.indexOf(search);
    if (idx >= 0) {
        console.log(`  Found "${search}" at ${idx}:`);
        console.log(`  ...${text.substring(Math.max(0, idx - 200), Math.min(text.length, idx + 300))}...`);
    }
}

// Search for responseType:"stream" or similar
console.log('\n\n=== responseType patterns ===');
for (const search of ['responseType:"stream"', 'responseType:"text"', 'adapter:"fetch"', 'fetchOptions']) {
    idx = text.indexOf(search);
    if (idx >= 0) {
        console.log(`  Found "${search}" at ${idx}:`);
        console.log(`  ...${text.substring(Math.max(0, idx - 100), Math.min(text.length, idx + 200))}...`);
    }
}
