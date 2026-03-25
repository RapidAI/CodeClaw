package util

import (
	"encoding/json"
	"net/http"
)

// WriteJSON writes a JSON response with the given status code.
// This is a shared helper to avoid duplicate writeJSON functions
// in openai, claude, and admin packages.
func WriteJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// ToBool loosely converts an interface value to bool.
func ToBool(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

// IntFrom converts a JSON-decoded numeric value (float64, int, int64) to int.
func IntFrom(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}
