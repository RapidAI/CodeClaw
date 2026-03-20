package remote

import (
	"crypto/sha256"
	"fmt"
)

// LaunchFingerprint computes a deterministic hash of the LaunchSpec fields
// that affect session behavior.
func LaunchFingerprint(spec LaunchSpec) string {
	h := sha256.New()
	fmt.Fprintf(h, "tool=%s\n", spec.Tool)
	fmt.Fprintf(h, "model=%s\n", spec.ModelID)
	fmt.Fprintf(h, "yolo=%v\n", spec.YoloMode)
	fmt.Fprintf(h, "admin=%v\n", spec.AdminMode)
	fmt.Fprintf(h, "proxy=%v\n", spec.UseProxy)
	fmt.Fprintf(h, "team=%v\n", spec.TeamMode)
	fmt.Fprintf(h, "python=%s\n", spec.PythonEnv)
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

// ModeChanged returns true if two LaunchSpecs differ in any field that
// affects session behavior.
func ModeChanged(a, b LaunchSpec) bool {
	return LaunchFingerprint(a) != LaunchFingerprint(b)
}
