package websearch

import (
	"crypto/tls"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/proxyutil"
)

var (
	sharedClient *http.Client
	clientOnce   sync.Once
	clientMu     sync.Mutex
)

// httpClient returns a shared HTTP client with sensible defaults.
func httpClient() *http.Client {
	clientOnce.Do(func() {
		sharedClient = &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
				},
				MaxIdleConns:        10,
				IdleConnTimeout:     30 * time.Second,
				DisableCompression:  true,
				MaxConnsPerHost:     5,
				TLSHandshakeTimeout: 10 * time.Second,
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return http.ErrUseLastResponse
				}
				if len(via) > 0 {
					req.Header.Set("User-Agent", via[0].Header.Get("User-Agent"))
				}
				return nil
			},
		}
	})
	return sharedClient
}

// SetProxy reconfigures the shared HTTP client's transport to use the given proxy.
// Safe to call multiple times; rebuilds the transport each time.
func SetProxy(cfg proxyutil.Config) {
	clientMu.Lock()
	defer clientMu.Unlock()

	// Ensure client exists
	_ = httpClient()

	t := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
		MaxIdleConns:        10,
		IdleConnTimeout:     30 * time.Second,
		DisableCompression:  true,
		MaxConnsPerHost:     5,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	if cfg.Enabled {
		proxyutil.WrapTransport(t, cfg)
	}
	sharedClient.Transport = t
}

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:133.0) Gecko/20100101 Firefox/133.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:133.0) Gecko/20100101 Firefox/133.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0",
}

func pickUserAgent() string {
	return userAgents[rand.Intn(len(userAgents))]
}
