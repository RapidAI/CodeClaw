//go:build windows

package main

import (
	"syscall"
	"time"
	"unsafe"
)

var (
	screenDimUser32      = syscall.NewLazyDLL("user32.dll")
	screenDimKernel32    = syscall.NewLazyDLL("kernel32.dll")
	procGetLastInputInfo = screenDimUser32.NewProc("GetLastInputInfo")
	procSendMessageW     = screenDimUser32.NewProc("SendMessageW")
	procGetTickCount64   = screenDimKernel32.NewProc("GetTickCount64")
)

func init() {
	platformGetIdleDuration = getIdleDurationWindows
	platformDimDisplay = dimDisplayWindows
}

// LASTINPUTINFO structure for GetLastInputInfo.
type lastInputInfo struct {
	CbSize uint32
	DwTime uint32
}

func getIdleDurationWindows() time.Duration {
	var lii lastInputInfo
	lii.CbSize = uint32(unsafe.Sizeof(lii))
	r, _, _ := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&lii)))
	if r == 0 {
		return 0
	}
	// GetTickCount64 returns milliseconds since boot as uint64,
	// avoids the 49-day wrap-around of 32-bit GetTickCount.
	tick, _, _ := procGetTickCount64.Call()
	idleMs := uint64(tick) - uint64(lii.DwTime)
	return time.Duration(idleMs) * time.Millisecond
}

func dimDisplayWindows() {
	const (
		hwndBroadcast  = 0xFFFF
		wmSyscommand   = 0x0112
		scMonitorpower = 0xF170
		monitorOff     = 2
	)
	procSendMessageW.Call(
		uintptr(hwndBroadcast),
		uintptr(wmSyscommand),
		uintptr(scMonitorpower),
		uintptr(monitorOff),
	)
}
