package remote

import (
	"fmt"
	"runtime"
)

// DisplayInfo describes a single display/monitor in the virtual desktop.
type DisplayInfo struct {
	Index   int     `json:"index"`
	Name    string  `json:"name"`
	X       int     `json:"x"`
	Y       int     `json:"y"`
	Width   int     `json:"width"`
	Height  int     `json:"height"`
	Scale   float64 `json:"scale"`
	Primary bool    `json:"primary"`
}

// EnumDisplays returns all connected displays.
// Platform-specific implementations are in screenshot_multimon_windows.go,
// screenshot_multimon_darwin.go, and screenshot_multimon_linux.go.
func EnumDisplays() ([]DisplayInfo, error) {
	switch runtime.GOOS {
	case "windows":
		return enumDisplaysWindows()
	case "darwin":
		return enumDisplaysDarwin()
	case "linux":
		return enumDisplaysLinux()
	default:
		return nil, fmt.Errorf("EnumDisplays: unsupported platform %s", runtime.GOOS)
	}
}

// BuildMultiMonitorScreenshotCommand returns a platform-specific shell command
// that captures all monitors into a single stitched PNG and outputs base64.
func BuildMultiMonitorScreenshotCommand() string {
	switch runtime.GOOS {
	case "windows":
		return buildMultiMonitorScreenshotWindows()
	case "darwin":
		return buildMultiMonitorScreenshotDarwin()
	case "linux":
		return buildMultiMonitorScreenshotLinux()
	default:
		return ""
	}
}

// BuildSingleMonitorScreenshotCommand returns a platform-specific shell command
// that captures only the specified monitor by index and outputs base64.
func BuildSingleMonitorScreenshotCommand(screenIndex int) string {
	switch runtime.GOOS {
	case "windows":
		return buildSingleMonitorScreenshotWindows(screenIndex)
	case "darwin":
		return buildSingleMonitorScreenshotDarwin(screenIndex)
	case "linux":
		return buildSingleMonitorScreenshotLinux(screenIndex)
	default:
		return ""
	}
}

// ValidateScreenIndex checks whether screenIndex is within the range of
// connected displays. Returns an error containing the actual display count
// when the index is out of range.
func ValidateScreenIndex(screenIndex int) error {
	displays, err := EnumDisplays()
	if err != nil {
		return fmt.Errorf("ValidateScreenIndex: cannot enumerate displays: %w", err)
	}
	if screenIndex < 0 || screenIndex >= len(displays) {
		return fmt.Errorf("screen index %d out of range: %d display(s) available", screenIndex, len(displays))
	}
	return nil
}

// DegradedScreenshotResult holds the screenshot command along with degradation
// metadata. When multi-monitor capture is unavailable, the engine falls back
// to primary-monitor-only capture and sets Degraded=true with a reason.
type DegradedScreenshotResult struct {
	Command  string // the screenshot command to execute
	Degraded bool   // true if degraded from multi-monitor to primary
	Reason   string // reason for degradation (empty if not degraded)
}

// BuildScreenshotCommandWithFallback tries BuildMultiMonitorScreenshotCommand
// first. If the command is empty (unsupported platform), it falls back to
// BuildScreenshotCommand with degraded=true.
func BuildScreenshotCommandWithFallback() DegradedScreenshotResult {
	cmd := BuildMultiMonitorScreenshotCommand()
	if cmd != "" {
		return DegradedScreenshotResult{
			Command:  cmd,
			Degraded: false,
		}
	}
	return DegradedScreenshotResult{
		Command:  BuildScreenshotCommand(),
		Degraded: true,
		Reason:   fmt.Sprintf("multi-monitor screenshot not supported on %s, falling back to primary monitor", runtime.GOOS),
	}
}

// BuildSingleMonitorScreenshotCommandSafe validates screenIndex via
// ValidateScreenIndex before building the command. If the index is valid it
// returns the single-monitor command with degraded=false. If the index is out
// of range it returns an empty command with degraded=true and the validation
// error as the reason.
func BuildSingleMonitorScreenshotCommandSafe(screenIndex int) (DegradedScreenshotResult, error) {
	if err := ValidateScreenIndex(screenIndex); err != nil {
		return DegradedScreenshotResult{
			Degraded: true,
			Reason:   err.Error(),
		}, err
	}
	return DegradedScreenshotResult{
		Command:  BuildSingleMonitorScreenshotCommand(screenIndex),
		Degraded: false,
	}, nil
}
