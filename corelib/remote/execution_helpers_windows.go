//go:build windows

package remote

import (
	"os/exec"
	"syscall"
)

// HideCommandWindow sets SysProcAttr to prevent a visible console window
// from appearing when the process is started on Windows.
func HideCommandWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
