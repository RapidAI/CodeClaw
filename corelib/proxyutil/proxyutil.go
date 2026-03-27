// Package proxyutil provides shared proxy URL construction and http.Transport helpers.
package proxyutil

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/proxy"
)

// Config holds the proxy configuration fields needed to build a proxy URL or Transport.
type Config struct {
	Enabled  bool
	Protocol string // "http", "https", "socks5"; empty defaults to "http"
	Host     string
	Port     string
	Username string
	Password string
	Bypass   string // semicolon-separated bypass list
}

// ProxyURL builds the full proxy URL string, e.g. "socks5://user:pass@host:port".
// Returns "" if Host or Port is empty.
func (c Config) ProxyURL() string {
	if c.Host == "" || c.Port == "" {
		return ""
	}
	scheme := c.Protocol
	if scheme == "" {
		scheme = "http"
	}
	if c.Username != "" && c.Password != "" {
		return fmt.Sprintf("%s://%s:%s@%s:%s", scheme,
			url.QueryEscape(c.Username), url.QueryEscape(c.Password),
			c.Host, c.Port)
	}
	return fmt.Sprintf("%s://%s:%s", scheme, c.Host, c.Port)
}

// ShouldBypass returns true if the given host matches the bypass list.
func (c Config) ShouldBypass(host string) bool {
	if c.Bypass == "" {
		return false
	}
	h := strings.TrimSpace(strings.ToLower(host))
	// strip port
	if hp, _, err := net.SplitHostPort(h); err == nil {
		h = hp
	}
	for _, pattern := range strings.Split(c.Bypass, ";") {
		pattern = strings.TrimSpace(strings.ToLower(pattern))
		if pattern == "" {
			continue
		}
		if matchBypass(h, pattern) {
			return true
		}
	}
	return false
}

// matchBypass checks if host matches a bypass pattern (supports * wildcards).
func matchBypass(host, pattern string) bool {
	if pattern == "*" {
		return true
	}
	// Simple wildcard: "*.example.com" or "10.*"
	if strings.Contains(pattern, "*") {
		// Convert to prefix/suffix match
		parts := strings.SplitN(pattern, "*", 2)
		if len(parts) == 2 {
			return strings.HasPrefix(host, parts[0]) && strings.HasSuffix(host, parts[1])
		}
	}
	return host == pattern
}

// ProxyFunc returns an http.Transport-compatible proxy function.
// For HTTP/HTTPS proxies it returns a standard URL-based proxy.
// For SOCKS5 it also returns a URL-based proxy (Go 1.16+ supports socks5:// in http.Transport.Proxy).
func (c Config) ProxyFunc() func(*http.Request) (*url.URL, error) {
	rawURL := c.ProxyURL()
	if rawURL == "" {
		return nil
	}
	proxyURL, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	return func(req *http.Request) (*url.URL, error) {
		if c.ShouldBypass(req.URL.Host) {
			return nil, nil
		}
		return proxyURL, nil
	}
}

// WrapTransport sets the Proxy field on an existing *http.Transport.
// For SOCKS5, it also sets DialContext via golang.org/x/net/proxy for full compatibility.
func WrapTransport(t *http.Transport, c Config) {
	if !c.Enabled {
		return
	}
	rawURL := c.ProxyURL()
	if rawURL == "" {
		return
	}

	scheme := c.Protocol
	if scheme == "" {
		scheme = "http"
	}

	if scheme == "socks5" {
		// Use golang.org/x/net/proxy for SOCKS5 dial
		var auth *proxy.Auth
		if c.Username != "" {
			auth = &proxy.Auth{User: c.Username, Password: c.Password}
		}
		dialer, err := proxy.SOCKS5("tcp", net.JoinHostPort(c.Host, c.Port), auth, proxy.Direct)
		if err == nil {
			// proxy.Dialer doesn't implement DialContext, so wrap with Dial.
			t.Dial = func(network, addr string) (net.Conn, error) {
				if c.ShouldBypass(addr) {
					return net.Dial(network, addr)
				}
				return dialer.Dial(network, addr)
			}
			t.Proxy = nil // ensure no double-proxy
		}
		return
	}

	// HTTP / HTTPS proxy
	t.Proxy = c.ProxyFunc()
}
