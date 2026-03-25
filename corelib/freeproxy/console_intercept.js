// Paste this into browser DevTools console on ai.dangbei.com
// It intercepts the next fetch to v2/chat and logs all headers

(function() {
    const origFetch = window.fetch;
    window.fetch = async function(input, init) {
        const req = input instanceof Request ? input : new Request(input, init);
        const url = req.url || (typeof input === 'string' ? input : '');
        
        if (url.includes('/chatApi/v2/chat') || url.includes('/v2/chat')) {
            console.log('=== INTERCEPTED v2/chat ===');
            console.log('URL:', url);
            console.log('Method:', req.method);
            
            // Log all headers
            console.log('Headers:');
            const headers = {};
            for (const [k, v] of req.headers.entries()) {
                headers[k] = v;
                console.log(`  ${k}: ${v}`);
            }
            
            // Log body
            const body = await req.clone().text();
            console.log('Body:', body);
            
            // Output as JSON for easy copy-paste
            console.log('\n=== COPY THIS JSON ===');
            console.log(JSON.stringify({
                sign: headers.sign,
                nonce: headers.nonce,
                timestamp: headers.timestamp,
                token: headers.token,
                apptype: headers.apptype,
                deviceid: headers.deviceid,
                version: headers.version,
                body: body,
                url: url,
            }, null, 2));
        }
        
        return origFetch.apply(this, arguments);
    };
    console.log('Interceptor installed. Send a chat message now.');
})();
