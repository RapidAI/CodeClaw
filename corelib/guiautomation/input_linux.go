//go:build linux

package guiautomation

// NewInputSimulator creates a Linux InputSimulator.
// TODO: implement via XTest extension.
func NewInputSimulator() InputSimulator {
	return &noopInputSimulator{}
}
