package browser

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ── managed browser process ──

var (
	managedBrowserMu   sync.Mutex
	managedBrowserProc *os.Process
)

// DiscoverCDPAddr tries to auto-discover a Chrome/Edge CDP endpoint.
// Priority: 1) DevToolsActivePort file  2) common ports (9222, 9229, 9333)
// Returns an HTTP address like "http://127.0.0.1:9222" or error.
func DiscoverCDPAddr() (string, error) {
	// 1. Try DevToolsActivePort file (works with chrome://inspect remote debugging).
	if port, _ := readDevToolsActivePort(); port > 0 {
		if probePort(port) {
			return fmt.Sprintf("http://127.0.0.1:%d", port), nil
		}
	}

	// 2. Scan common debug ports.
	for _, port := range []int{9222, 9229, 9333} {
		if probePort(port) {
			return fmt.Sprintf("http://127.0.0.1:%d", port), nil
		}
	}

	return "", fmt.Errorf("未发现 Chrome/Edge 调试端口")
}

// DiscoverOrLaunch tries DiscoverCDPAddr first. If that fails, it launches
// the user's default Chrome/Edge with --remote-debugging-port=0 using the
// user's default profile, waits for the DevToolsActivePort file, and returns
// the CDP address. This preserves login state because it reuses the real profile.
func DiscoverOrLaunch() (string, error) {
	// Fast path: already available.
	if addr, err := DiscoverCDPAddr(); err == nil {
		// Verify the port is actually serving CDP (not just TCP-open).
		if _, err2 := DiscoverTargets(addr); err2 == nil {
			return addr, nil
		}
		log.Printf("[browser] 端口可达但 CDP 无响应，将重新启动浏览器")
	}

	// Detect browser.
	bi := detectBrowser()
	if bi == nil {
		return "", fmt.Errorf("未找到 Chrome 或 Edge 浏览器，请安装后重试")
	}

	// Determine the user's default profile directory so we keep login state.
	userDataDir := defaultUserDataDir(bi.name)
	if userDataDir == "" {
		return "", fmt.Errorf("无法确定浏览器 profile 目录")
	}

	// Check if browser is already running (profile locked).
	if isBrowserRunning(bi.name) {
		// Browser is running but no debug port — we need to restart it.
		log.Printf("[browser] %s 正在运行但未开启调试端口，正在重启...", bi.name)
		killBrowserByName(bi.name)
		// Wait for processes to fully exit and release profile lock.
		waitForProfileUnlock(userDataDir, 8*time.Second)
	}

	// Clean stale DevToolsActivePort file before launch.
	dtapPath := filepath.Join(userDataDir, "DevToolsActivePort")
	os.Remove(dtapPath)

	// Launch with --remote-debugging-port=0 (auto-pick free port).
	args := []string{
		fmt.Sprintf("--user-data-dir=%s", userDataDir),
		"--remote-debugging-port=0",
		"--no-first-run",
		"--no-default-browser-check",
	}
	cmd := exec.Command(bi.path, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("启动浏览器失败: %w", err)
	}

	// Track the process so we can clean up later.
	managedBrowserMu.Lock()
	managedBrowserProc = cmd.Process
	managedBrowserMu.Unlock()

	// Detach — don't block on browser exit.
	go func() {
		cmd.Wait()
		managedBrowserMu.Lock()
		managedBrowserProc = nil
		managedBrowserMu.Unlock()
	}()

	// Wait for DevToolsActivePort file to appear (Chrome writes it after startup).
	port, err := waitForDevToolsActivePort(dtapPath, 15*time.Second)
	if err != nil {
		return "", fmt.Errorf("浏览器已启动但未能获取调试端口: %w", err)
	}

	addr := fmt.Sprintf("http://127.0.0.1:%d", port)
	log.Printf("[browser] 已自动启动 %s，调试端口: %d", bi.name, port)
	return addr, nil
}

// waitForDevToolsActivePort polls the DevToolsActivePort file until it appears
// and contains a valid port number, then verifies the port is actually listening.
func waitForDevToolsActivePort(path string, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			lines := strings.SplitN(strings.TrimSpace(string(data)), "\n", 2)
			if port, err := strconv.Atoi(strings.TrimSpace(lines[0])); err == nil && port > 0 {
				// File exists with a port — now verify the CDP server is actually ready.
				if probePort(port) {
					return port, nil
				}
				// Port not ready yet, keep polling.
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return 0, fmt.Errorf("等待 DevToolsActivePort 超时 (%v)", timeout)
}

// ── browser detection (mirrors freeproxy/browser.go logic) ──

type browserInfo struct {
	path string // absolute path to executable
	name string // "chrome" or "edge"
}

func detectBrowser() *browserInfo {
	if p := findChromeExe(); p != "" {
		return &browserInfo{path: p, name: "chrome"}
	}
	if p := findEdgeExe(); p != "" {
		return &browserInfo{path: p, name: "edge"}
	}
	return nil
}

func findChromeExe() string {
	switch runtime.GOOS {
	case "windows":
		for _, base := range []string{
			os.Getenv("ProgramFiles"),
			os.Getenv("ProgramFiles(x86)"),
			os.Getenv("LocalAppData"),
		} {
			if base == "" {
				continue
			}
			p := filepath.Join(base, `Google\Chrome\Application\chrome.exe`)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	case "darwin":
		p := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
		if _, err := os.Stat(p); err == nil {
			return p
		}
	default:
		if p, err := exec.LookPath("google-chrome"); err == nil {
			return p
		}
		if p, err := exec.LookPath("google-chrome-stable"); err == nil {
			return p
		}
	}
	return ""
}

func findEdgeExe() string {
	switch runtime.GOOS {
	case "windows":
		for _, base := range []string{
			os.Getenv("ProgramFiles(x86)"),
			os.Getenv("ProgramFiles"),
		} {
			if base == "" {
				continue
			}
			p := filepath.Join(base, `Microsoft\Edge\Application\msedge.exe`)
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

// defaultUserDataDir returns the default user-data-dir for the given browser.
func defaultUserDataDir(browserName string) string {
	switch runtime.GOOS {
	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			return ""
		}
		if browserName == "chrome" {
			return filepath.Join(localAppData, "Google", "Chrome", "User Data")
		}
		return filepath.Join(localAppData, "Microsoft", "Edge", "User Data")
	case "darwin":
		home, _ := os.UserHomeDir()
		if home == "" {
			return ""
		}
		if browserName == "chrome" {
			return filepath.Join(home, "Library/Application Support/Google/Chrome")
		}
		return filepath.Join(home, "Library/Application Support/Microsoft Edge")
	default: // linux
		home, _ := os.UserHomeDir()
		if home == "" {
			return ""
		}
		if browserName == "chrome" {
			return filepath.Join(home, ".config/google-chrome")
		}
		return filepath.Join(home, ".config/microsoft-edge")
	}
}

// isBrowserRunning checks if Chrome/Edge processes are running.
func isBrowserRunning(browserName string) bool {
	switch runtime.GOOS {
	case "windows":
		var procName string
		if browserName == "chrome" {
			procName = "chrome.exe"
		} else {
			procName = "msedge.exe"
		}
		out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("IMAGENAME eq %s", procName), "/NH").Output()
		if err != nil {
			return false
		}
		return strings.Contains(string(out), procName)
	default:
		var procName string
		if browserName == "chrome" {
			procName = "chrome"
		} else {
			procName = "msedge"
		}
		out, _ := exec.Command("pgrep", "-f", procName).Output()
		return len(strings.TrimSpace(string(out))) > 0
	}
}

// killBrowserByName terminates all instances of the given browser.
func killBrowserByName(browserName string) {
	switch runtime.GOOS {
	case "windows":
		var procName string
		if browserName == "chrome" {
			procName = "chrome.exe"
		} else {
			procName = "msedge.exe"
		}
		exec.Command("taskkill", "/F", "/IM", procName).Run()
	default:
		var procName string
		if browserName == "chrome" {
			procName = "chrome"
		} else {
			procName = "msedge"
		}
		exec.Command("pkill", "-f", procName).Run()
	}
}

// waitForProfileUnlock waits until the browser processes have fully exited.
// More reliable than checking lock files, which Chrome may not always clean up.
func waitForProfileUnlock(userDataDir string, timeout time.Duration) {
	_ = userDataDir // reserved for future lock-file checks
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		// Check if any Chrome/Edge processes are still running.
		// Once they're all gone, the profile is unlocked.
		chromeGone := !isBrowserRunning("chrome")
		edgeGone := !isBrowserRunning("edge")
		if chromeGone && edgeGone {
			// Extra grace period for file handles to be released.
			time.Sleep(500 * time.Millisecond)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	// Timeout — proceed anyway; Chrome child processes may linger harmlessly.
}

// readDevToolsActivePort reads the DevToolsActivePort file from known Chrome profile locations.
// Returns (port, wsPath) where wsPath may be empty.
func readDevToolsActivePort() (int, string) {
	var candidates []string

	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		if home != "" {
			candidates = append(candidates,
				filepath.Join(home, "Library/Application Support/Google/Chrome/DevToolsActivePort"),
				filepath.Join(home, "Library/Application Support/Google/Chrome Canary/DevToolsActivePort"),
				filepath.Join(home, "Library/Application Support/Chromium/DevToolsActivePort"),
				filepath.Join(home, "Library/Application Support/Microsoft Edge/DevToolsActivePort"),
			)
		}
	case "linux":
		home, _ := os.UserHomeDir()
		if home != "" {
			candidates = append(candidates,
				filepath.Join(home, ".config/google-chrome/DevToolsActivePort"),
				filepath.Join(home, ".config/chromium/DevToolsActivePort"),
				filepath.Join(home, ".config/microsoft-edge/DevToolsActivePort"),
			)
		}
	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData != "" {
			candidates = append(candidates,
				filepath.Join(localAppData, "Google", "Chrome", "User Data", "DevToolsActivePort"),
				filepath.Join(localAppData, "Chromium", "User Data", "DevToolsActivePort"),
				filepath.Join(localAppData, "Microsoft", "Edge", "User Data", "DevToolsActivePort"),
			)
		}
	}

	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		lines := strings.SplitN(strings.TrimSpace(string(data)), "\n", 2)
		port, err := strconv.Atoi(strings.TrimSpace(lines[0]))
		if err != nil || port <= 0 || port > 65535 {
			continue
		}
		wsPath := ""
		if len(lines) > 1 {
			wsPath = strings.TrimSpace(lines[1])
		}
		return port, wsPath
	}
	return 0, ""
}

// probePort checks if a TCP port is listening on localhost (2s timeout).
func probePort(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
