package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// pngMagicBytes is the 8-byte PNG file header signature.
var pngMagicBytes = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}

// ParseScreenshotOutput extracts and validates base64-encoded PNG data from
// the screenshot command's stdout output. It strips whitespace, BOM markers,
// null bytes, and other non-base64 characters, then validates the encoding
// and confirms the decoded data starts with PNG magic bytes.
//
// The function tries standard base64 first, then falls back to raw (no-padding)
// base64 to handle platform differences in the `base64` command output.
func ParseScreenshotOutput(stdout string) (string, error) {
	// Strip UTF-8 BOM if present.
	cleaned := strings.TrimPrefix(stdout, "\xEF\xBB\xBF")
	cleaned = strings.TrimSpace(cleaned)

	// Remove all whitespace (newlines, spaces, tabs, carriage returns).
	cleaned = strings.Join(strings.Fields(cleaned), "")

	// Strip any remaining non-base64 characters (null bytes, zero-width
	// spaces, control characters, etc.) that shells or terminal emulators
	// may inject.
	cleaned = stripNonBase64(cleaned)

	if cleaned == "" {
		return "", fmt.Errorf("screenshot command produced no output")
	}

	// Try standard base64 (with padding) first.
	decoded, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		// Fallback: try raw base64 (no padding) — some `base64` implementations
		// omit trailing '=' characters.
		decoded, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(cleaned, "="))
		if err != nil {
			// Provide diagnostic info: show the first 80 chars of the cleaned
			// output so the log reveals what went wrong.
			preview := cleaned
			if len(preview) > 80 {
				preview = preview[:80] + "..."
			}
			return "", fmt.Errorf("invalid base64 encoding (len=%d, preview=%s)", len(cleaned), preview)
		}
	}

	if len(decoded) < len(pngMagicBytes) || !bytes.Equal(decoded[:len(pngMagicBytes)], pngMagicBytes) {
		return "", fmt.Errorf("output is not PNG (decoded %d bytes, header=%x)", len(decoded), safeHeader(decoded, 8))
	}

	// Re-encode to canonical standard base64 so downstream consumers always
	// receive a well-formed string regardless of the original encoding.
	canonical := base64.StdEncoding.EncodeToString(decoded)
	return canonical, nil
}

// stripNonBase64 removes any character that is not part of the standard base64
// alphabet (A-Z, a-z, 0-9, +, /, =). This handles BOM remnants, null bytes,
// zero-width spaces, and other invisible characters that may be injected by
// shells, terminal emulators, or PowerShell.
func stripNonBase64(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '+' || c == '/' || c == '=' {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// safeHeader returns up to n bytes from data for diagnostic logging.
func safeHeader(data []byte, n int) []byte {
	if len(data) < n {
		return data
	}
	return data[:n]
}

// BuildScreenshotCommand returns a platform-specific shell command string that
// captures a screenshot and outputs the result as raw base64-encoded PNG data
// to stdout. Temporary files are cleaned up on macOS and Linux regardless of
// success or failure.
func BuildScreenshotCommand() string {
	switch runtime.GOOS {
	case "windows":
		return buildWindowsScreenshotCommand()
	case "darwin":
		return buildDarwinScreenshotCommand()
	case "linux":
		return buildLinuxScreenshotCommand()
	default:
		return ""
	}
}

func buildWindowsScreenshotCommand() string {
	// Returns a pure PowerShell script block (without the powershell.exe prefix).
	// The caller (captureAndSend) invokes this via powershell -Command directly,
	// avoiding cmd.exe quote mangling that corrupts base64 output.
	// SetProcessDPIAware ensures correct coordinates on high-DPI displays.
	return `Add-Type -AssemblyName System.Drawing; ` +
		`Add-Type -AssemblyName System.Windows.Forms; ` +
		`Add-Type -TypeDefinition 'using System.Runtime.InteropServices; public class DPI { [DllImport("user32.dll")] public static extern bool SetProcessDPIAware(); }'; ` +
		`[DPI]::SetProcessDPIAware() | Out-Null; ` +
		`$bounds = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds; ` +
		`$bmp = New-Object System.Drawing.Bitmap($bounds.Width, $bounds.Height); ` +
		`$g = [System.Drawing.Graphics]::FromImage($bmp); ` +
		`$g.CopyFromScreen($bounds.Location, [System.Drawing.Point]::Empty, $bounds.Size); ` +
		`$g.Dispose(); ` +
		`$ms = New-Object System.IO.MemoryStream; ` +
		`$bmp.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png); ` +
		`$bmp.Dispose(); ` +
		`$b64 = [Convert]::ToBase64String($ms.ToArray()); ` +
		`$ms.Dispose(); ` +
		`[Console]::Out.Write($b64)`
}

func buildDarwinScreenshotCommand() string {
	return `tmpfile=$(mktemp /tmp/screenshot_XXXXXX.png); ` +
		`trap "rm -f \"$tmpfile\"" EXIT; ` +
		`screencapture -x "$tmpfile" && ` +
		`base64 -i "$tmpfile"`
}

func buildLinuxScreenshotCommand() string {
	return `tmpfile=$(mktemp /tmp/screenshot_XXXXXX.png); ` +
		`trap "rm -f \"$tmpfile\"" EXIT; ` +
		`if command -v scrot >/dev/null 2>&1; then ` +
		`scrot "$tmpfile"; ` +
		`elif command -v gnome-screenshot >/dev/null 2>&1; then ` +
		`gnome-screenshot -f "$tmpfile"; ` +
		`elif command -v import >/dev/null 2>&1; then ` +
		`import -window root "$tmpfile"; ` +
		`elif command -v grim >/dev/null 2>&1; then ` +
		`grim "$tmpfile"; ` +
		`else ` +
		`echo "no screenshot tool found (scrot, gnome-screenshot, import, or grim required)" >&2; exit 1; ` +
		`fi && ` +
		`base64 -w 0 < "$tmpfile" 2>/dev/null || base64 < "$tmpfile"`
}

// sanitizeWindowTitle strips characters that could be used for shell injection
// in the window title parameter. Only alphanumeric, spaces, hyphens, underscores,
// dots, and common CJK characters are allowed.
func sanitizeWindowTitle(title string) string {
	var b strings.Builder
	for _, r := range title {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_' || r == '.' || r == '(' || r == ')':
			b.WriteRune(r)
		case r >= 0x4E00 && r <= 0x9FFF: // CJK Unified Ideographs
			b.WriteRune(r)
		case r >= 0x3040 && r <= 0x30FF: // Hiragana + Katakana
			b.WriteRune(r)
		case r >= 0xAC00 && r <= 0xD7AF: // Hangul
			b.WriteRune(r)
		default:
			// Skip potentially dangerous characters
		}
	}
	return b.String()
}

// BuildWindowScreenshotCommand returns a platform-specific shell command that
// captures a screenshot of a specific window by title and outputs base64 PNG
// to stdout. If the window is not found, the command should fail with a
// non-zero exit code.
func BuildWindowScreenshotCommand(windowTitle string) string {
	// Sanitize the title to prevent shell injection.
	windowTitle = sanitizeWindowTitle(windowTitle)
	if windowTitle == "" {
		return ""
	}
	switch runtime.GOOS {
	case "windows":
		return buildWindowsWindowScreenshotCommand(windowTitle)
	case "darwin":
		return buildDarwinWindowScreenshotCommand(windowTitle)
	case "linux":
		return buildLinuxWindowScreenshotCommand(windowTitle)
	default:
		return ""
	}
}

func buildWindowsWindowScreenshotCommand(windowTitle string) string {
	// Returns a pure PowerShell script block (without the powershell.exe prefix).
	// The caller (captureAndSend) invokes this via powershell -Command directly.
	// SetProcessDPIAware ensures correct coordinates on high-DPI displays.
	// Escape single quotes in the title for PowerShell.
	escaped := strings.ReplaceAll(windowTitle, "'", "''")
	return fmt.Sprintf(
		`Add-Type -AssemblyName System.Drawing; `+
			`Add-Type -AssemblyName System.Windows.Forms; `+
			`Add-Type @'`+"\n"+
			`using System; using System.Runtime.InteropServices; using System.Text;`+"\n"+
			`public class WinAPI {`+"\n"+
			`  public struct RECT { public int Left, Top, Right, Bottom; }`+"\n"+
			`  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr hWnd, out RECT rect);`+"\n"+
			`  [DllImport("user32.dll")] public static extern IntPtr FindWindow(string cls, string title);`+"\n"+
			`  public delegate bool EnumWindowsProc(IntPtr hWnd, IntPtr lParam);`+"\n"+
			`  [DllImport("user32.dll")] public static extern bool EnumWindows(EnumWindowsProc proc, IntPtr lParam);`+"\n"+
			`  [DllImport("user32.dll", CharSet=CharSet.Auto)] public static extern int GetWindowText(IntPtr hWnd, StringBuilder sb, int count);`+"\n"+
			`  [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr hWnd);`+"\n"+
			`  [DllImport("user32.dll")] public static extern bool SetProcessDPIAware();`+"\n"+
			`}`+"\n"+
			`'@;`+
			`[WinAPI]::SetProcessDPIAware() | Out-Null; `+
			`$target = '%s'; `+
			`$found = $null; `+
			`[WinAPI]::EnumWindows({ param($h,$l); `+
			`if ([WinAPI]::IsWindowVisible($h)) { `+
			`$sb = New-Object Text.StringBuilder 256; `+
			`[WinAPI]::GetWindowText($h, $sb, 256) | Out-Null; `+
			`$t = $sb.ToString(); `+
			`if ($t -like ('*' + $target + '*')) { $script:found = $h } `+
			`} return $true }, [IntPtr]::Zero) | Out-Null; `+
			`if (-not $found) { Write-Error 'Window not found'; exit 1 }; `+
			`$r = New-Object WinAPI+RECT; `+
			`[WinAPI]::GetWindowRect($found, [ref]$r) | Out-Null; `+
			`$w = $r.Right - $r.Left; $h = $r.Bottom - $r.Top; `+
			`if ($w -le 0 -or $h -le 0) { Write-Error 'Invalid window size'; exit 1 }; `+
			`$bmp = New-Object Drawing.Bitmap($w, $h); `+
			`$g = [Drawing.Graphics]::FromImage($bmp); `+
			`$g.CopyFromScreen($r.Left, $r.Top, 0, 0, (New-Object Drawing.Size($w,$h))); `+
			`$g.Dispose(); `+
			`$ms = New-Object IO.MemoryStream; `+
			`$bmp.Save($ms, [Drawing.Imaging.ImageFormat]::Png); `+
			`$bmp.Dispose(); `+
			`$b64 = [Convert]::ToBase64String($ms.ToArray()); `+
			`$ms.Dispose(); `+
			`[Console]::Out.Write($b64)`, escaped)
}

func buildDarwinWindowScreenshotCommand(windowTitle string) string {
	// Use osascript to find the window ID, then screencapture -l <windowID>
	escaped := strings.ReplaceAll(windowTitle, `"`, `\"`)
	return fmt.Sprintf(`tmpfile=$(mktemp /tmp/screenshot_XXXXXX.png); `+
		`trap "rm -f \"$tmpfile\"" EXIT; `+
		`wid=$(osascript -e 'tell application "System Events" to set wlist to every window of every process whose name of every window contains "%s"' -e 'if (count of wlist) > 0 then return id of item 1 of wlist' 2>/dev/null); `+
		`if [ -z "$wid" ]; then echo "Window not found" >&2; exit 1; fi; `+
		`screencapture -x -l "$wid" "$tmpfile" && `+
		`base64 -i "$tmpfile"`, escaped)
}

func buildLinuxWindowScreenshotCommand(windowTitle string) string {
	escaped := strings.ReplaceAll(windowTitle, `"`, `\"`)
	return fmt.Sprintf(`tmpfile=$(mktemp /tmp/screenshot_XXXXXX.png); `+
		`trap "rm -f \"$tmpfile\"" EXIT; `+
		`wid=$(xdotool search --name "%s" 2>/dev/null | head -1); `+
		`if [ -z "$wid" ]; then echo "Window not found" >&2; exit 1; fi; `+
		`if command -v import >/dev/null 2>&1; then `+
		`import -window "$wid" "$tmpfile"; `+
		`elif command -v scrot >/dev/null 2>&1; then `+
		`scrot -u "$tmpfile"; `+
		`else echo "no screenshot tool found" >&2; exit 1; fi && `+
		`base64 -w 0 < "$tmpfile" 2>/dev/null || base64 < "$tmpfile"`, escaped)
}

// DetectDisplayServer checks whether a graphical display environment is
// available on the current platform.
// Returns (available, reason) where reason is non-empty when available is false.
//   - Windows: always returns true (desktop app necessarily has display)
//   - macOS: always returns true (Quartz display server is available for desktop apps)
//   - Linux: checks DISPLAY or WAYLAND_DISPLAY environment variables
func DetectDisplayServer() (bool, string) {
	switch runtime.GOOS {
	case "windows":
		return true, ""
	case "darwin":
		return true, ""
	case "linux":
		if display := os.Getenv("DISPLAY"); display != "" {
			return true, ""
		}
		if waylandDisplay := os.Getenv("WAYLAND_DISPLAY"); waylandDisplay != "" {
			return true, ""
		}
		return false, "no display server detected: neither DISPLAY nor WAYLAND_DISPLAY environment variable is set"
	default:
		return false, fmt.Sprintf("unsupported platform for display detection: %s", runtime.GOOS)
	}
}

// CaptureScreenshot executes the full screenshot capture flow for the given
// session: detect display → build command → execute → parse output → send image.
// Only SDK-mode sessions are supported; PTY sessions return an error.
func (m *RemoteSessionManager) CaptureScreenshot(sessionID string) error {
	cmdStr := BuildScreenshotCommand()
	if cmdStr == "" {
		return fmt.Errorf("screenshot capture is not supported on %s", runtime.GOOS)
	}
	return m.captureAndSend(sessionID, "", cmdStr)
}

// CaptureWindowScreenshot captures a screenshot of a specific window by title
// and sends it through the image transfer pipeline. The windowTitle is matched
// as a substring against visible window titles.
func (m *RemoteSessionManager) CaptureWindowScreenshot(sessionID, windowTitle string) error {
	if strings.TrimSpace(windowTitle) == "" {
		return fmt.Errorf("window title must not be empty")
	}
	cmdStr := BuildWindowScreenshotCommand(windowTitle)
	if cmdStr == "" {
		return fmt.Errorf("window screenshot is not supported on %s", runtime.GOOS)
	}
	return m.captureAndSend(sessionID, windowTitle, cmdStr)
}

// captureAndSend is the shared implementation for CaptureScreenshot and
// CaptureWindowScreenshot. It validates the session, executes the shell
// command, parses the base64 output, and sends the image via the hub.
func (m *RemoteSessionManager) captureAndSend(sessionID, label, cmdStr string) error {
	s, ok := m.Get(sessionID)
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	if _, isSDK := s.Exec.(*SDKExecutionHandle); !isSDK {
		return fmt.Errorf("screenshot capture is only supported in SDK mode sessions")
	}

	available, reason := DetectDisplayServer()
	if !available {
		return fmt.Errorf("screenshot requires a graphical display environment: %s", reason)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var shellName string
	var shellArgs []string
	if runtime.GOOS == "windows" {
		// Use PowerShell directly to avoid cmd.exe quote mangling that
		// corrupts base64 output. The Windows screenshot commands return
		// pure PowerShell script blocks (no powershell.exe prefix).
		shellName = "powershell"
		shellArgs = []string{"-NoProfile", "-NonInteractive", "-Command", cmdStr}
	} else {
		shellName = "bash"
		shellArgs = []string{"-c", cmdStr}
	}

	cmd := exec.CommandContext(ctx, shellName, shellArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	logLabel := "fullscreen"
	if label != "" {
		logLabel = fmt.Sprintf("window %q", label)
	}
	m.app.log(fmt.Sprintf("[screenshot] capturing %s for session=%s", logLabel, sessionID))

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("screenshot command timed out after 30s")
		}
		m.app.log(fmt.Sprintf("[screenshot] capture failed for session=%s: %v, stderr: %s", sessionID, err, stderr.String()))
		return fmt.Errorf("screenshot command failed: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}

	rawOut := stdout.String()
	base64Data, err := ParseScreenshotOutput(rawOut)
	if err != nil {
		// Log diagnostic info: raw output length and first 120 chars to help
		// identify what the screenshot command actually produced.
		preview := rawOut
		if len(preview) > 120 {
			preview = preview[:120] + "..."
		}
		m.app.log(fmt.Sprintf("[screenshot] failed to parse output for session=%s: %v (stdout_len=%d, stderr=%q, preview=%q)",
			sessionID, err, len(rawOut), strings.TrimSpace(stderr.String()), preview))
		return fmt.Errorf("screenshot output parse error: %w", err)
	}

	msg := NewImageTransferMessage(sessionID, "image/png", base64Data)
	if err := ValidateImageTransferMessage(msg, ImageOutputSizeLimit); err != nil {
		m.app.log(fmt.Sprintf("[screenshot] image exceeds size limit for session=%s: %v", sessionID, err))
		return err
	}

	if m.hubClient != nil {
		if err := m.hubClient.SendSessionImage(msg); err != nil {
			m.app.log(fmt.Sprintf("[screenshot] failed to send image for session=%s: %v", sessionID, err))
			return err
		}
	}

	m.app.log(fmt.Sprintf("[screenshot] successfully captured %s for session=%s", logLabel, sessionID))
	return nil
}
