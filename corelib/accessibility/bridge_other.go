//go:build !windows && !darwin && !linux

package accessibility

// NewBridge returns a no-op bridge on unsupported platforms.
func NewBridge() Bridge {
	return &noopBridge{}
}
