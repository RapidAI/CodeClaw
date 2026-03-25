// Check what token the cookie contains
const fs = require('fs');
const path = require('path');
const os = require('os');

const authPath = path.join(os.homedir(), '.maclaw', 'freeproxy', 'dangbei_auth.json');
try {
    const data = JSON.parse(fs.readFileSync(authPath, 'utf8'));
    const cookie = data.cookie || '';
    
    // Parse cookie string
    const cookies = {};
    cookie.split(';').forEach(c => {
        const [k, ...v] = c.trim().split('=');
        if (k) cookies[k.trim()] = v.join('=');
    });
    
    console.log('Cookie keys:', Object.keys(cookies));
    
    // Look for token-like values
    for (const [k, v] of Object.entries(cookies)) {
        if (k.toLowerCase().includes('token') || k.toLowerCase().includes('user') || k.toLowerCase().includes('auth')) {
            console.log(`${k} = ${v.substring(0, 50)}...`);
        }
    }
    
    // Also check if there's a 'token' cookie specifically
    if (cookies.token) {
        console.log('\ntoken cookie:', cookies.token.substring(0, 80));
    }
    
    // Check for deviceId
    if (cookies.deviceId) {
        console.log('deviceId cookie:', cookies.deviceId);
    }
    
    // Print all cookie names
    console.log('\nAll cookies:');
    for (const k of Object.keys(cookies).sort()) {
        console.log(`  ${k}: ${(cookies[k] || '').substring(0, 60)}`);
    }
} catch (e) {
    console.error('Error:', e.message);
}
