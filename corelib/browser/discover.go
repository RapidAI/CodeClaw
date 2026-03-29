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

// debugProfileDir returns a dedicated user-data-dir for debug-mode Chrome/Edge.
// Using a separate profile avoids conflicts with the user's running browser
// (the root cause of "browser exits immediately" failures).
func debugProfileDir() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = os.TempDir()
	}
	return filepath.Join(home, ".maclaw-chrome-debug-profile")
}

// debugPort is the fixed CDP port used for the isolated debug browser.
const debugPort = 9222

// DiscoverOrLaunch tries DiscoverCDPAddr first. If that fails, it launches
// Chrome/Edge with an isolated debug profile and a fixed port (9222).
// This avoids user-data-dir conflicts with any running browser instance —
// the key insight from maclaw's stable three-step approach:
//  1. Kill stale debug-port browsers
//  2. Launch with --user-data-dir=<isolated> --remote-debugging-port=9222
//  3. Connect to http://127.0.0.1:9222
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

	// Use an isolated debug profile to avoid conflicts with the user's browser.
	debugDir := debugProfileDir()

	// Step 1: If port 9222 is occupied but not serving CDP, or if a previous
	// debug instance is stuck, kill it. We try to only kill our managed
	// process first; falling back to killBrowserByName only as last resort.
	if probePort(debugPort) {
		addr := fmt.Sprintf("http://127.0.0.1:%d", debugPort)
		if _, err := DiscoverTargets(addr); err != nil {
			log.Printf("[browser] 端口 %d 被占用但非 CDP，尝试清理...", debugPort)
			if !killManagedBrowser() {
				// No managed process — something else holds the port.
				// As a last resort, kill browser processes.
				killBrowserByName(bi.name)
			}
			waitForPortRelease(debugPort, 8*time.Second)
		}
	}

	// Clean stale lock files in the debug profile.
	for _, lockName := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie", "lockfile"} {
		os.Remove(filepath.Join(debugDir, lockName))
	}
	os.Remove(filepath.Join(debugDir, "DevToolsActivePort"))

	// Step 2: Launch with isolated profile + fixed port.
	addr, err := launchDebugBrowser(bi, debugDir, debugPort)
	if err != nil {
		// Retry once: force-kill our managed process and try again.
		log.Printf("[browser] 首次启动失败 (%v)，强制清理后重试...", err)
		killManagedBrowser()
		killBrowserByName(bi.name) // fallback: ensure nothing holds the port
		waitForPortRelease(debugPort, 10*time.Second)
		for _, lockName := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie", "lockfile"} {
			os.Remove(filepath.Join(debugDir, lockName))
		}
		addr, err = launchDebugBrowser(bi, debugDir, debugPort)
		if err != nil {
			return "", fmt.Errorf("浏览器两次启动均失败: %w", err)
		}
	}

	return addr, nil
}

// launchDebugBrowser starts Chrome/Edge with the given user-data-dir and
// remote-debugging-port, waits for the port to become available, and returns
// the CDP HTTP address.
func launchDebugBrowser(bi *browserInfo, userDataDir string, port int) (string, error) {
	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--no-first-run",
		"--no-default-browser-check",
		"--user-data-dir=" + userDataDir,
	}
	cmd := exec.Command(bi.path, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	log.Printf("[browser] 启动命令: %s %v", bi.path, args)
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("启动浏览器失败: %w", err)
	}

	// Track the process for cleanup.
	managedBrowserMu.Lock()
	managedBrowserProc = cmd.Process
	managedBrowserMu.Unlock()

	procExited := make(chan struct{})
	go func() {
		cmd.Wait()
		close(procExited)
		managedBrowserMu.Lock()
		managedBrowserProc = nil
		managedBrowserMu.Unlock()
	}()

	// Check if the process exits immediately (profile conflict or bad args).
	select {
	case <-procExited:
		return "", fmt.Errorf("浏览器进程立即退出，可能存在 profile 冲突")
	case <-time.After(2 * time.Second):
		// Good — process is alive.
	}

	// Wait for the fixed port to start serving CDP.
	addr := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-procExited:
			return "", fmt.Errorf("浏览器进程在等待 CDP 端口期间退出")
		default:
		}
		if _, err := DiscoverTargets(addr); err == nil {
			log.Printf("[browser] 已启动 %s，调试端口: %d (独立 profile)", bi.name, port)
			return addr, nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	return "", fmt.Errorf("浏览器已启动但端口 %d 未响应 CDP", port)
}

// waitForPortRelease waits until the given TCP port is no longer listening.
func waitForPortRelease(port int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !probePort(port) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	log.Printf("[browser] waitForPortRelease: 端口 %d 超时仍被占用", port)
}

// killManagedBrowser kills the browser process that we started (if any).
// Returns true if a managed process was found and killed.
func killManagedBrowser() bool {
	managedBrowserMu.Lock()
	proc := managedBrowserProc
	managedBrowserMu.Unlock()
	if proc == nil {
		return false
	}
	log.Printf("[browser] 终止托管浏览器进程 (PID %d)...", proc.Pid)
	proc.Kill()
	// Give the OS a moment to release resources.
	time.Sleep(1 * time.Second)
	return true
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
	case "darwin":
		var appName string
		if browserName == "chrome" {
			appName = "Google Chrome"
		} else {
			appName = "Microsoft Edge"
		}
		out, _ := exec.Command("pgrep", "-f", appName).Output()
		return len(strings.TrimSpace(string(out))) > 0
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
		// /T kills the entire process tree (GPU helper, crashpad, etc.)
		exec.Command("taskkill", "/F", "/T", "/IM", procName).Run()
		// Fallback: enumerate remaining PIDs via tasklist and kill individually.
		// tasklist is available on all Windows versions (unlike wmic which is deprecated).
		out, err := exec.Command("tasklist", "/FI",
			fmt.Sprintf("IMAGENAME eq %s", procName), "/FO", "CSV", "/NH").Output()
		if err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				line = strings.TrimSpace(line)
				// CSV format: "chrome.exe","12345","Console","1","123,456 K"
				fields := strings.Split(line, ",")
				if len(fields) >= 2 {
					pid := strings.Trim(fields[1], "\" ")
					if pid != "" && pid != "0" {
						exec.Command("taskkill", "/F", "/T", "/PID", pid).Run()
					}
				}
			}
		}
	case "darwin":
		// On macOS, use killall which is more reliable than pkill for app bundles.
		var appName string
		if browserName == "chrome" {
			appName = "Google Chrome"
		} else {
			appName = "Microsoft Edge"
		}
		exec.Command("killall", appName).Run()
		// Also pkill helper processes.
		var helperName string
		if browserName == "chrome" {
			helperName = "Google Chrome Helper"
		} else {
			helperName = "Microsoft Edge Helper"
		}
		exec.Command("killall", helperName).Run()
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

	// Also check our isolated debug profile directory.
	debugDir := debugProfileDir()
	candidates = append(candidates, filepath.Join(debugDir, "DevToolsActivePort"))

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
