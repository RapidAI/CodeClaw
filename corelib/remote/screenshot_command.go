package remote

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

// BuildScreenshotCommand returns a platform-specific shell command string that
// captures a screenshot and outputs the result as raw base64-encoded PNG data
// to stdout.
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

// SanitizeWindowTitle strips characters that could be used for shell injection.
func SanitizeWindowTitle(title string) string {
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
// captures a screenshot of a specific window by title.
func BuildWindowScreenshotCommand(windowTitle string) string {
	windowTitle = SanitizeWindowTitle(windowTitle)
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

// DetectDisplayServer checks whether a graphical display environment is available.
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
