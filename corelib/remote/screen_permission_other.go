//go:build !darwin

package remote

// CheckScreenRecordingPermission always returns true on non-macOS platforms.
func CheckScreenRecordingPermission() bool {
	return true
}
