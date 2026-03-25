//go:build !windows

package freeproxy

import "fmt"

// decryptCookieValue is a no-op on non-Windows platforms.
// macOS uses Keychain and Linux uses libsecret/kwallet for Chrome cookie encryption.
// For now, only plaintext cookies are supported on these platforms.
func decryptCookieValue(encrypted []byte, profileDir string) (string, error) {
	return "", fmt.Errorf("cookie decryption not supported on this platform")
}
