package freeproxy

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// BrowserInfo holds the detected browser executable and its type.
type BrowserInfo struct {
	Path string // absolute path to the browser executable
	Name string // "chrome" or "edge"
}

// dedicatedBrowserProc tracks the browser process launched by LaunchDedicatedBrowser.
var (
	dedicatedBrowserMu   sync.Mutex
	dedicatedBrowserProc *os.Process
)

// DetectBrowser finds Chrome or Edge on the system.
// It prefers Chrome over Edge.
func DetectBrowser() *BrowserInfo {
	if p := findChrome(); p != "" {
		return &BrowserInfo{Path: p, Name: "chrome"}
	}
	if p := findEdge(); p != "" {
		return &BrowserInfo{Path: p, Name: "edge"}
	}
	return nil
}

// maclawUserDataDir returns the dedicated user-data-dir for maclaw's browser instance.
func maclawUserDataDir() string {
	base := os.TempDir()
	return filepath.Join(base, "maclaw-browser-profile")
}

// LaunchDedicatedBrowser launches a browser with a dedicated profile directory.
// No debug port, no CDP — just a plain browser window for the user to log in.
func LaunchDedicatedBrowser(openURL string) error {
	// Kill any previously launched browser to avoid orphan processes
	KillDedicatedBrowser()

	bi := DetectBrowser()
	if bi == nil {
		return fmt.Errorf("未找到 Chrome 或 Edge 浏览器")
	}

	userDataDir := maclawUserDataDir()
	args := []string{
		fmt.Sprintf("--user-data-dir=%s", userDataDir),
		"--no-first-run",
		"--no-default-browser-check",
		openURL,
	}

	cmd := exec.Command(bi.Path, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch browser: %w", err)
	}

	// Save process reference so we can kill it later in FinishLogin
	dedicatedBrowserMu.Lock()
	dedicatedBrowserProc = cmd.Process
	dedicatedBrowserMu.Unlock()

	// Detach — don't wait for browser to exit
	go func() {
		cmd.Wait()
		dedicatedBrowserMu.Lock()
		dedicatedBrowserProc = nil
		dedicatedBrowserMu.Unlock()
	}()
	return nil
}

// KillDedicatedBrowser kills the browser process launched by LaunchDedicatedBrowser.
// It's safe to call even if the browser was already closed by the user.
// After killing, it waits until the cookie DB file is unlocked (up to 8 seconds).
func KillDedicatedBrowser() {
	dedicatedBrowserMu.Lock()
	proc := dedicatedBrowserProc
	dedicatedBrowserProc = nil // clear immediately so re-launch doesn't see stale ref
	dedicatedBrowserMu.Unlock()

	if proc == nil {
		return
	}
	// Kill the process tree. On Windows, use taskkill /T to kill child processes too
	// (Chrome spawns multiple child processes).
	if runtime.GOOS == "windows" {
		exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", proc.Pid)).Run()
	} else {
		proc.Kill()
	}

	// Wait for the cookie DB file to become readable (unlocked).
	// Chrome holds an exclusive lock while running; after kill it takes
	// a few seconds for all child processes to exit and release the lock.
	cookieDB := findCookieDB()
	if cookieDB == "" {
		time.Sleep(2 * time.Second) // fallback: blind wait
		return
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		f, err := os.Open(cookieDB)
		if err == nil {
			f.Close()
			// File is readable — give a tiny extra moment for full flush
			time.Sleep(500 * time.Millisecond)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// findCookieDB returns the path to the cookie DB in the dedicated profile, or "".
func findCookieDB() string {
	profileDir := maclawUserDataDir()
	for _, sub := range []string{"Default", "Profile 1"} {
		for _, rel := range []string{
			filepath.Join(sub, "Network", "Cookies"),
			filepath.Join(sub, "Cookies"),
		} {
			p := filepath.Join(profileDir, rel)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

// ExtractCookiesFromProfile reads cookies for the given domain directly from
// the browser profile's SQLite cookie database. No CDP or debug port needed.
func ExtractCookiesFromProfile(domain string) (string, error) {
	profileDir := maclawUserDataDir()

	// Try common cookie database locations.
	// Chrome 118+ moved Cookies into Default/Network/Cookies.
	var cookieDBPath string
	for _, sub := range []string{"Default", "Profile 1"} {
		for _, rel := range []string{
			filepath.Join(sub, "Network", "Cookies"), // Chrome 118+
			filepath.Join(sub, "Cookies"),             // Chrome < 118
		} {
			candidate := filepath.Join(profileDir, rel)
			if _, err := os.Stat(candidate); err == nil {
				cookieDBPath = candidate
				break
			}
		}
		if cookieDBPath != "" {
			break
		}
	}
	if cookieDBPath == "" {
		return "", fmt.Errorf("cookie 数据库不存在 (profileDir=%s)，请先在浏览器中打开当贝 AI 页面", profileDir)
	}

	// Chrome locks the Cookies file while running.
	// On Windows, use the "copy" command which can read share-locked files.
	// On other platforms, try direct file read.
	tmpFile, err := os.CreateTemp("", "maclaw-cookies-*.db")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	if runtime.GOOS == "windows" {
		// Windows copy command handles share-locked files
		cmd := exec.Command("cmd", "/c", "copy", "/y", cookieDBPath, tmpPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("copy cookie db: %w (%s)", err, string(out))
		}
	} else {
		src, err := os.ReadFile(cookieDBPath)
		if err != nil {
			return "", fmt.Errorf("read cookie db: %w", err)
		}
		if err := os.WriteFile(tmpPath, src, 0600); err != nil {
			return "", fmt.Errorf("write temp cookie db: %w", err)
		}
	}

	db, err := sql.Open("sqlite", tmpPath)
	if err != nil {
		return "", fmt.Errorf("open cookie db: %w", err)
	}
	defer db.Close()

	// Chrome 80+ stores cookies in the "cookies" table.
	// host_key is ".dangbei.com" or "ai.dangbei.com".
	rows, err := db.Query(
		`SELECT name, value, encrypted_value FROM cookies WHERE host_key LIKE ?`,
		"%"+domain,
	)
	if err != nil {
		return "", fmt.Errorf("query cookies: %w", err)
	}
	defer rows.Close()

	var parts []string
	var decryptErrs []string
	for rows.Next() {
		var name, value string
		var encValue []byte
		if err := rows.Scan(&name, &value, &encValue); err != nil {
			continue
		}
		// Sanitize: strip control characters that break HTTP headers
		name = sanitizeCookiePart(name)
		if name == "" {
			continue
		}
		if value != "" {
			parts = append(parts, name+"="+sanitizeCookiePart(value))
			continue
		}
		// Chrome 80+ encrypts cookie values. Try platform-specific decryption.
		if len(encValue) > 0 {
			if dec, err := decryptCookieValue(encValue, profileDir); err == nil && dec != "" {
				parts = append(parts, name+"="+sanitizeCookiePart(dec))
			} else if err != nil {
				decryptErrs = append(decryptErrs, fmt.Sprintf("%s: %v", name, err))
			}
		}
	}

	if len(parts) == 0 {
		detail := fmt.Sprintf("profileDir=%s, cookieDB=%s", profileDir, cookieDBPath)
		if len(decryptErrs) > 0 {
			detail += ", 解密错误: " + strings.Join(decryptErrs, "; ")
		}
		return "", fmt.Errorf("未获取到 cookie (%s)。请确认已在浏览器中登录当贝 AI，如刚登录请稍等几秒再试", detail)
	}
	return strings.Join(parts, "; "), nil
}

// ── Browser detection helpers ──

// sanitizeCookiePart strips control characters from a cookie name or value.
// Delegates to sanitizeHeaderValue (same rules apply).
func sanitizeCookiePart(s string) string {
	return sanitizeHeaderValue(s)
}

func findChrome() string {
	switch runtime.GOOS {
	case "windows":
		candidates := []string{
			filepath.Join(os.Getenv("ProgramFiles"), `Google\Chrome\Application\chrome.exe`),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), `Google\Chrome\Application\chrome.exe`),
			filepath.Join(os.Getenv("LocalAppData"), `Google\Chrome\Application\chrome.exe`),
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	case "darwin":
		p := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
		if _, err := os.Stat(p); err == nil {
			return p
		}
	default: // linux
		if p, err := exec.LookPath("google-chrome"); err == nil {
			return p
		}
		if p, err := exec.LookPath("google-chrome-stable"); err == nil {
			return p
		}
	}
	return ""
}

func findEdge() string {
	switch runtime.GOOS {
	case "windows":
		candidates := []string{
			filepath.Join(os.Getenv("ProgramFiles(x86)"), `Microsoft\Edge\Application\msedge.exe`),
			filepath.Join(os.Getenv("ProgramFiles"), `Microsoft\Edge\Application\msedge.exe`),
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	case "darwin":
		p := "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"
		if _, err := os.Stat(p); err == nil {
			return p
		}
	default:
		if p, err := exec.LookPath("microsoft-edge"); err == nil {
			return p
		}
	}
	return ""
}
