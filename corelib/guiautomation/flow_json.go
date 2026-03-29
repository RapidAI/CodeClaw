package guiautomation

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SaveFlow saves a GUIRecordedFlow as indented JSON to the given directory.
// Base64 snapshot data in each step is extracted to external PNG files under
// a snapshots/ subdirectory, and the step's SnapshotRef is replaced with the
// relative file path.
func SaveFlow(flow *GUIRecordedFlow, dir string) error {
	safeName := sanitizeFlowName(flow.Name)
	flowDir := filepath.Join(dir, safeName)
	snapshotDir := filepath.Join(flowDir, "snapshots")

	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		return fmt.Errorf("create flow directory: %w", err)
	}

	// Extract inline base64 snapshots to external PNG files.
	for i := range flow.Steps {
		ref := flow.Steps[i].SnapshotRef
		if ref == "" || !looksLikeBase64PNG(ref) {
			continue
		}

		data, err := base64.StdEncoding.DecodeString(ref)
		if err != nil {
			continue // skip invalid base64, keep the ref as-is
		}

		relPath := fmt.Sprintf("snapshots/step_%03d.png", i+1)
		absPath := filepath.Join(flowDir, relPath)
		if err := os.WriteFile(absPath, data, 0o644); err != nil {
			return fmt.Errorf("write snapshot %s: %w", relPath, err)
		}
		flow.Steps[i].SnapshotRef = relPath
	}

	data, err := json.MarshalIndent(flow, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal flow: %w", err)
	}

	jsonPath := filepath.Join(flowDir, "flow.json")
	if err := os.WriteFile(jsonPath, data, 0o644); err != nil {
		return fmt.Errorf("write flow.json: %w", err)
	}

	return nil
}

// LoadFlow loads a GUIRecordedFlow from the given directory and flow name.
// It verifies that referenced snapshot files exist.
func LoadFlow(dir string, name string) (*GUIRecordedFlow, error) {
	safeName := sanitizeFlowName(name)
	flowDir := filepath.Join(dir, safeName)
	jsonPath := filepath.Join(flowDir, "flow.json")

	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, fmt.Errorf("read flow.json: %w", err)
	}

	var flow GUIRecordedFlow
	if err := json.Unmarshal(data, &flow); err != nil {
		return nil, fmt.Errorf("unmarshal flow: %w", err)
	}

	// Verify snapshot files exist.
	for i, step := range flow.Steps {
		if step.SnapshotRef == "" {
			continue
		}
		absPath := filepath.Join(flowDir, step.SnapshotRef)
		if _, err := os.Stat(absPath); err != nil {
			return nil, fmt.Errorf("step %d: snapshot file missing: %s", i, step.SnapshotRef)
		}
	}

	return &flow, nil
}

// looksLikeBase64PNG returns true if s appears to be inline base64 PNG data
// rather than a file path reference.
func looksLikeBase64PNG(s string) bool {
	// PNG files start with the magic bytes 0x89 0x50 0x4E 0x47 which in
	// base64 encode to "iVBOR". A file path would not start this way.
	if len(s) > 100 && strings.HasPrefix(s, "iVBOR") {
		return true
	}
	return false
}

// sanitizeFlowName replaces unsafe filesystem characters with underscores.
// Reuses the same pattern as browser/recorder.go.
func sanitizeFlowName(name string) string {
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_",
		"?", "_", "\"", "_", "<", "_", ">", "_", "|", "_",
		" ", "_",
	)
	s := replacer.Replace(name)
	if s == "" {
		s = "unnamed"
	}
	return s
}
