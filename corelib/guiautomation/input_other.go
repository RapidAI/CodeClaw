//go:build !windows && !darwin && !linux

package guiautomation

// NewInputSimulator returns a no-op simulator on unsupported platforms.
func NewInputSimulator() InputSimulator {
	return &noopInputSimulator{}
}
