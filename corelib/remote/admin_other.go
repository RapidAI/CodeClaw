//go:build !windows

package remote

import (
	"os"
	"sync"
)

var (
	processElevatedOnce sync.Once
	processElevated     bool
)

// IsProcessElevated returns true if the current process is running as
// root (uid 0) on Unix-like systems. The result is cached because
// effective UID does not change during normal operation.
func IsProcessElevated() bool {
	processElevatedOnce.Do(func() {
		processElevated = os.Getuid() == 0
	})
	return processElevated
}
