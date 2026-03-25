package freeproxy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	dangbeiDomain = "ai.dangbei.com"
	dangbeiBase   = "https://ai.dangbei.com"
)

// AuthStore manages the 当贝 authentication token (cookie-based).
type AuthStore struct {
	mu       sync.RWMutex
	cookie   string // raw cookie string from browser
	filePath string // persistence path
}

// NewAuthStore creates an AuthStore that persists to the given directory.
func NewAuthStore(configDir string) *AuthStore {
	return &AuthStore{
		filePath: filepath.Join(configDir, "dangbei_auth.json"),
	}
}

type authData struct {
	Cookie string `json:"cookie"`
}

// Load reads the persisted cookie from disk.
func (s *AuthStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var ad authData
	if err := json.Unmarshal(data, &ad); err != nil {
		return err
	}
	s.cookie = ad.Cookie
	return nil
}

// Save persists the cookie to disk.
func (s *AuthStore) Save() error {
	s.mu.RLock()
	cookie := s.cookie
	s.mu.RUnlock()

	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, _ := json.Marshal(authData{Cookie: cookie})
	return os.WriteFile(s.filePath, data, 0600)
}

// SetCookie updates the stored cookie.
func (s *AuthStore) SetCookie(cookie string) {
	s.mu.Lock()
	s.cookie = cookie
	s.mu.Unlock()
}

// GetCookie returns the current cookie, sanitized for use in HTTP headers.
// This ensures even previously-persisted dirty cookies are safe.
func (s *AuthStore) GetCookie() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sanitizeHeaderValue(s.cookie)
}

// HasAuth returns true if a cookie is available.
func (s *AuthStore) HasAuth() bool {
	return strings.TrimSpace(s.GetCookie()) != ""
}

// LoginViaBrowser launches a dedicated browser for the user to log in to 当贝 AI.
func LoginViaBrowser() error {
	return LaunchDedicatedBrowser(dangbeiBase + "/chat")
}

// FinishLogin kills the dedicated browser (to flush cookies to disk),
// then reads cookies from the browser profile's SQLite database.
func FinishLogin() (string, error) {
	KillDedicatedBrowser()
	return ExtractCookiesFromProfile(dangbeiDomain)
}
