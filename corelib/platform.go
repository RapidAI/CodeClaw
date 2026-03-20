package corelib

import (
	"os"
	"runtime"
)

// PlatformCapabilities 描述当前运行环境的能力。
// 在 Kernel 初始化时自动检测，也可通过 KernelOptions.PlatformOverride 覆盖。
type PlatformCapabilities struct {
	hasDisplay   bool
	hasClipboard bool
	hasNotify    bool
	displayInfo  string // 检测失败时的诊断信息
	osName       string // "linux", "darwin", "windows"
	arch         string // "amd64", "arm64", ...
}

// HasDisplay 返回是否有显示服务器（X11/Wayland/macOS/Windows）。
func (p *PlatformCapabilities) HasDisplay() bool { return p.hasDisplay }

// HasClipboard 返回是否支持剪贴板。
func (p *PlatformCapabilities) HasClipboard() bool { return p.hasClipboard }

// HasNotify 返回是否支持系统通知。
func (p *PlatformCapabilities) HasNotify() bool { return p.hasNotify }

// DisplayInfo 返回显示环境的诊断信息。
func (p *PlatformCapabilities) DisplayInfo() string { return p.displayInfo }

// OSName 返回操作系统名称。
func (p *PlatformCapabilities) OSName() string { return p.osName }

// Arch 返回 CPU 架构。
func (p *PlatformCapabilities) Arch() string { return p.arch }

// DetectPlatform 自动检测当前环境能力。
// Linux 下检查 DISPLAY 和 WAYLAND_DISPLAY 环境变量。
// Windows 和 macOS 默认认为有完整 GUI 能力。
func DetectPlatform() PlatformCapabilities {
	p := PlatformCapabilities{
		osName: runtime.GOOS,
		arch:   runtime.GOARCH,
	}

	switch runtime.GOOS {
	case "linux":
		display := os.Getenv("DISPLAY")
		wayland := os.Getenv("WAYLAND_DISPLAY")
		if display != "" || wayland != "" {
			p.hasDisplay = true
			p.hasClipboard = true
			p.hasNotify = true
			p.displayInfo = "display=" + display + " wayland=" + wayland
		} else {
			p.hasDisplay = false
			p.hasClipboard = false
			p.hasNotify = false
			p.displayInfo = "no DISPLAY or WAYLAND_DISPLAY set (headless)"
		}
	case "darwin", "windows":
		p.hasDisplay = true
		p.hasClipboard = true
		p.hasNotify = true
		p.displayInfo = runtime.GOOS + " desktop"
	default:
		p.displayInfo = "unknown platform: " + runtime.GOOS
	}

	return p
}
