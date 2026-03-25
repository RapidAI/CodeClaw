package claude

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteClaudeErrorIncludesUnifiedFields(t *testing.T) {
	rec := httptest.NewRecorder()
	writeClaudeError(rec, http.StatusUnauthorized, "bad token")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	errObj, _ := body["error"].(map[string]any)
	if errObj["message"] != "bad token" {
		t.Fatalf("unexpected message: %v", errObj["message"])
	}
	if errObj["type"] != "invalid_request_error" {
		t.Fatalf("unexpected type: %v", errObj["type"])
	}
	if errObj["code"] != "authentication_failed" {
		t.Fatalf("unexpected code: %v", errObj["code"])
	}
	if _, ok := errObj["param"]; !ok {
		t.Fatal("expected param field")
	}
}

