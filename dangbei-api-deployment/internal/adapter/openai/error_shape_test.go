package openai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteOpenAIErrorIncludesUnifiedFields(t *testing.T) {
	rec := httptest.NewRecorder()
	writeOpenAIError(rec, http.StatusBadRequest, "invalid input")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	errObj, _ := body["error"].(map[string]any)
	if errObj["message"] != "invalid input" {
		t.Fatalf("unexpected message: %v", errObj["message"])
	}
	if errObj["type"] != "invalid_request_error" {
		t.Fatalf("unexpected type: %v", errObj["type"])
	}
	if errObj["code"] != "invalid_request" {
		t.Fatalf("unexpected code: %v", errObj["code"])
	}
	if _, ok := errObj["param"]; !ok {
		t.Fatal("expected param field")
	}
}

