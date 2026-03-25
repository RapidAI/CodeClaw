package freeproxy

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
)

// rawHTTPPost performs a raw HTTP/1.1 POST over TLS, bypassing Go's net/http
// which causes v2/chat to silently drop connections (likely due to header
// canonicalization or chunked encoding behavior).
//
// Headers are sent in lowercase (like HTTP/2 / browser behavior) and the body
// is sent with an explicit Content-Length (no chunked encoding).
func rawHTTPPost(host string, path string, headers map[string]string, body []byte, timeout time.Duration) (*rawHTTPResponse, error) {
	conn, err := utlsDial(host)
	if err != nil {
		// Fallback to standard TLS if utls fails
		dialer := &net.Dialer{Timeout: 10 * time.Second}
		conn, err = tls.DialWithDialer(dialer, "tcp", host+":443", &tls.Config{
			NextProtos: []string{"http/1.1"},
		})
		if err != nil {
			return nil, fmt.Errorf("tls dial: %w", err)
		}
	}

	if timeout > 0 {
		conn.SetDeadline(time.Now().Add(timeout))
	}

	// Build raw HTTP/1.1 request with lowercase headers
	var req strings.Builder
	req.WriteString(fmt.Sprintf("POST %s HTTP/1.1\r\n", path))
	req.WriteString(fmt.Sprintf("host: %s\r\n", host))
	req.WriteString(fmt.Sprintf("content-length: %d\r\n", len(body)))

	for k, v := range headers {
		req.WriteString(fmt.Sprintf("%s: %s\r\n", strings.ToLower(k), v))
	}
	req.WriteString("\r\n")

	// Write headers then body separately to avoid large temp allocation
	headerBytes := []byte(req.String())
	if _, err := conn.Write(headerBytes); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write headers: %w", err)
	}
	if _, err := conn.Write(body); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write body: %w", err)
	}

	// Parse HTTP response
	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read status: %w", err)
	}

	resp := &rawHTTPResponse{
		conn:    conn,
		reader:  reader,
		Headers: make(map[string]string),
	}

	// Parse "HTTP/1.1 200 OK"
	parts := strings.SplitN(strings.TrimSpace(statusLine), " ", 3)
	if len(parts) < 2 {
		conn.Close()
		return nil, fmt.Errorf("malformed status line: %q", statusLine)
	}
	resp.StatusCode, _ = strconv.Atoi(parts[1])
	if len(parts) >= 3 {
		resp.StatusText = parts[2]
	}

	// Parse headers
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("read header: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break // end of headers
		}
		colonIdx := strings.IndexByte(line, ':')
		if colonIdx < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:colonIdx]))
		val := strings.TrimSpace(line[colonIdx+1:])
		resp.Headers[key] = val
	}

	// Determine body reading strategy
	if cl, ok := resp.Headers["content-length"]; ok {
		resp.contentLength, _ = strconv.ParseInt(cl, 10, 64)
	}
	resp.chunked = strings.Contains(resp.Headers["transfer-encoding"], "chunked")

	return resp, nil
}

// rawHTTPResponse wraps a raw HTTP response with streaming body support.
type rawHTTPResponse struct {
	StatusCode    int
	StatusText    string
	Headers       map[string]string
	conn          net.Conn
	reader        *bufio.Reader
	chunked       bool
	contentLength int64
	closed        bool
	bytesRead     int64
}

// Body returns an io.ReadCloser for the response body.
func (r *rawHTTPResponse) Body() io.ReadCloser {
	if r.chunked {
		return &chunkedReader{resp: r}
	}
	return &rawBodyReader{resp: r}
}

// Close closes the underlying connection.
func (r *rawHTTPResponse) Close() {
	if !r.closed {
		r.closed = true
		r.conn.Close()
	}
}

// rawBodyReader reads a fixed-length or connection-close body.
type rawBodyReader struct {
	resp *rawHTTPResponse
}

func (b *rawBodyReader) Read(p []byte) (int, error) {
	if b.resp.contentLength > 0 {
		remaining := b.resp.contentLength - b.resp.bytesRead
		if remaining <= 0 {
			return 0, io.EOF
		}
		if int64(len(p)) > remaining {
			p = p[:remaining]
		}
	}
	n, err := b.resp.reader.Read(p)
	b.resp.bytesRead += int64(n)
	return n, err
}

func (b *rawBodyReader) Close() error {
	b.resp.Close()
	return nil
}

// chunkedReader reads HTTP chunked transfer encoding.
type chunkedReader struct {
	resp      *rawHTTPResponse
	remaining int64
	done      bool
}

func (c *chunkedReader) Read(p []byte) (int, error) {
	if c.done {
		return 0, io.EOF
	}

	for c.remaining == 0 {
		// Read next chunk size line
		line, err := c.resp.reader.ReadString('\n')
		if err != nil {
			return 0, err
		}
		line = strings.TrimRight(line, "\r\n")
		// Chunk extensions after semicolon are ignored
		if idx := strings.IndexByte(line, ';'); idx >= 0 {
			line = line[:idx]
		}
		size, err := strconv.ParseInt(strings.TrimSpace(line), 16, 64)
		if err != nil {
			return 0, fmt.Errorf("bad chunk size %q: %w", line, err)
		}
		if size == 0 {
			c.done = true
			// Read trailing \r\n
			c.resp.reader.ReadString('\n')
			return 0, io.EOF
		}
		c.remaining = size
	}

	if int64(len(p)) > c.remaining {
		p = p[:c.remaining]
	}
	n, err := c.resp.reader.Read(p)
	c.remaining -= int64(n)

	// If chunk is fully read, consume trailing \r\n
	if c.remaining == 0 {
		c.resp.reader.ReadString('\n')
	}

	return n, err
}

func (c *chunkedReader) Close() error {
	c.resp.Close()
	return nil
}

// rawHTTPToResponse converts a rawHTTPResponse to an *http.Response for
// compatibility with existing code that expects *http.Response.
func rawHTTPToResponse(raw *rawHTTPResponse) *http.Response {
	return &http.Response{
		StatusCode: raw.StatusCode,
		Status:     fmt.Sprintf("%d %s", raw.StatusCode, raw.StatusText),
		Header:     rawHeadersToHTTP(raw.Headers),
		Body:       raw.Body(),
	}
}

func rawHeadersToHTTP(h map[string]string) http.Header {
	out := http.Header{}
	for k, v := range h {
		out.Set(k, v)
	}
	return out
}

// utlsDial establishes a TLS connection with Safari browser fingerprint,
// matching the approach used by ds2api's transport layer.
// This makes the TLS ClientHello look like Safari, reducing the chance
// of being fingerprinted as a Go HTTP client.
func utlsDial(host string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	plainConn, err := dialer.Dial("tcp", host+":443")
	if err != nil {
		return nil, fmt.Errorf("tcp dial: %w", err)
	}

	uCfg := &utls.Config{ServerName: host}
	uConn := utls.UClient(plainConn, uCfg, utls.HelloSafari_Auto)

	// Force HTTP/1.1 ALPN (we do raw HTTP/1.1, not h2)
	if err := uConn.BuildHandshakeState(); err != nil {
		plainConn.Close()
		return nil, fmt.Errorf("build handshake: %w", err)
	}
	for _, ext := range uConn.Extensions {
		if alpnExt, ok := ext.(*utls.ALPNExtension); ok {
			alpnExt.AlpnProtocols = []string{"http/1.1"}
			break
		}
	}

	if err := uConn.Handshake(); err != nil {
		plainConn.Close()
		return nil, fmt.Errorf("utls handshake: %w", err)
	}
	return uConn, nil
}
