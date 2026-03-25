package freeproxy

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/net/http/httpguts"
)

// TestSanitizeCookiePart verifies that sanitizeCookiePart strips all control
// characters that Go's net/http would reject in header values.
func TestSanitizeCookiePart(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"clean", "abc=123", "abc=123"},
		{"newline", "abc\n123", "abc123"},
		{"carriage_return", "abc\r123", "abc123"},
		{"null", "abc\x00123", "abc123"},
		{"tab_preserved", "abc\t123", "abc\t123"},
		{"bell_char", "abc\x07123", "abc123"},
		{"backspace", "abc\x08123", "abc123"},
		{"escape", "abc\x1b123", "abc123"},
		{"del_0x7f", "abc\x7f123", "abc123"},
		{"mixed_control", "a\x01b\x02c\x03d\x1fe", "abcde"},
		{"leading_trailing_space", "  abc  ", "abc"},
		{"all_control_chars", "\x00\x01\x02\x03\x04\x05\x06\x07\x08\x0b\x0c\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f\x7f", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeCookiePart(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeCookiePart(%q) = %q, want %q", tt.input, got, tt.want)
			}
			// The result must pass Go's header value validation
			if got != "" && !httpguts.ValidHeaderFieldValue(got) {
				t.Errorf("sanitizeCookiePart(%q) result %q is not a valid header value", tt.input, got)
			}
		})
	}
}

// TestSanitizeHeaderValue verifies that sanitizeHeaderValue strips all control
// characters that Go's net/http would reject.
func TestSanitizeHeaderValue(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"clean", "session_id=abc123; token=xyz", "session_id=abc123; token=xyz"},
		{"embedded_null", "session\x00id=abc", "sessionid=abc"},
		{"embedded_bel", "session\x07id=abc", "sessionid=abc"},
		{"del_char", "abc\x7fdef", "abcdef"},
		{"tab_ok", "abc\tdef", "abc\tdef"},
		{"crlf", "abc\r\ndef", "abcdef"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeHeaderValue(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeHeaderValue(%q) = %q, want %q", tt.input, got, tt.want)
			}
			if got != "" && !httpguts.ValidHeaderFieldValue(got) {
				t.Errorf("sanitizeHeaderValue(%q) result %q is not a valid header value", tt.input, got)
			}
		})
	}
}

// TestAuthStoreGetCookieSanitizes verifies that GetCookie() returns a sanitized
// value even when the stored cookie contains control characters (e.g. from a
// previously-persisted dirty dangbei_auth.json).
func TestAuthStoreGetCookieSanitizes(t *testing.T) {
	dir := t.TempDir()
	store := NewAuthStore(dir)

	// Simulate a dirty cookie with various control characters
	dirty := "session_id=abc\x01\x02\x03; token=xyz\x00\x7f\x1b"
	store.SetCookie(dirty)

	got := store.GetCookie()
	want := "session_id=abc; token=xyz"
	if got != want {
		t.Errorf("GetCookie() = %q, want %q", got, want)
	}
	if !httpguts.ValidHeaderFieldValue(got) {
		t.Errorf("GetCookie() result %q is not a valid header value", got)
	}
}

// TestAuthStoreLoadSanitizes verifies that loading a dirty cookie from disk
// still produces a clean value via GetCookie().
func TestAuthStoreLoadSanitizes(t *testing.T) {
	dir := t.TempDir()
	authFile := filepath.Join(dir, "dangbei_auth.json")

	// Write a dirty JSON file directly (simulating old persisted data)
	dirty := `{"cookie":"session_id=abc\u0001\u0002; token=xyz\u007f"}`
	if err := os.WriteFile(authFile, []byte(dirty), 0600); err != nil {
		t.Fatal(err)
	}

	store := NewAuthStore(dir)
	if err := store.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	got := store.GetCookie()
	want := "session_id=abc; token=xyz"
	if got != want {
		t.Errorf("GetCookie() after Load = %q, want %q", got, want)
	}
	if !httpguts.ValidHeaderFieldValue(got) {
		t.Errorf("GetCookie() result %q is not a valid header value", got)
	}
}
