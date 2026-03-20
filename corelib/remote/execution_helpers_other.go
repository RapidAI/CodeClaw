//go:build !windows

package remote

import "os/exec"

// HideCommandWindow is a no-op on non-Windows platforms.
func HideCommandWindow(_ *exec.Cmd) {}
