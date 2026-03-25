package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// TestPreservation_InvitationCodeResponseJSON verifies that invitationCodeResponse
// serializes to lowercase JSON field names. This baseline must hold before and after
// the invite-list fix.
//
// **Validates: Requirements 3.1, 3.2, 3.3, 3.4**
func TestPreservation_InvitationCodeResponseJSON(t *testing.T) {
	resp := invitationCodeResponse{
		ID:           "ic-001",
		Code:         "ABCD-1234",
		Status:       "unused",
		UsedByEmail:  "",
		UsedAt:       nil,
		ValidityDays: 30,
		BoundAt:      nil,
		Exported:     false,
		VIP:          true,
		CreatedAt:    "2024-01-15T10:00:00Z",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("json.Marshal(invitationCodeResponse): %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	expectedFields := []string{
		"id", "code", "status", "used_by_email",
		"used_at", "validity_days", "bound_at",
		"exported", "vip", "created_at",
	}

	for _, field := range expectedFields {
		if _, ok := raw[field]; !ok {
			t.Errorf("missing expected lowercase JSON field %q; got keys: %v", field, jsonKeys(raw))
		}
	}

	// Verify no uppercase/PascalCase keys leaked through
	for k := range raw {
		if k != toLowerSnake(k) {
			t.Errorf("unexpected non-lowercase field %q in JSON output", k)
		}
	}
}

// TestPreservation_ToInvitationCodeResponse verifies the toInvitationCodeResponse
// conversion function produces correct output from a store.InvitationCode.
//
// **Validates: Requirements 3.4**
func TestPreservation_ToInvitationCodeResponse(t *testing.T) {
	now := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	usedAt := time.Date(2024, 6, 16, 8, 30, 0, 0, time.UTC)

	t.Run("unused_code", func(t *testing.T) {
		ic := &store.InvitationCode{
			ID:           "ic-100",
			Code:         "TEST-UNUSED",
			Status:       "unused",
			UsedByEmail:  "",
			UsedAt:       nil,
			ValidityDays: 7,
			Exported:     false,
			VIP:          false,
			CreatedAt:    now,
		}

		resp := toInvitationCodeResponse(ic)

		if resp.ID != "ic-100" {
			t.Errorf("ID: got %q, want %q", resp.ID, "ic-100")
		}
		if resp.Code != "TEST-UNUSED" {
			t.Errorf("Code: got %q, want %q", resp.Code, "TEST-UNUSED")
		}
		if resp.Status != "unused" {
			t.Errorf("Status: got %q, want %q", resp.Status, "unused")
		}
		if resp.UsedByEmail != "" {
			t.Errorf("UsedByEmail: got %q, want empty", resp.UsedByEmail)
		}
		if resp.UsedAt != nil {
			t.Errorf("UsedAt: got %v, want nil", resp.UsedAt)
		}
		if resp.ValidityDays != 7 {
			t.Errorf("ValidityDays: got %d, want 7", resp.ValidityDays)
		}
		if resp.BoundAt != nil {
			t.Errorf("BoundAt: got %v, want nil", resp.BoundAt)
		}
		if resp.Exported != false {
			t.Errorf("Exported: got %v, want false", resp.Exported)
		}
		if resp.VIP != false {
			t.Errorf("VIP: got %v, want false", resp.VIP)
		}
		if resp.CreatedAt != "2024-06-15T12:00:00Z" {
			t.Errorf("CreatedAt: got %q, want %q", resp.CreatedAt, "2024-06-15T12:00:00Z")
		}
	})

	t.Run("used_code", func(t *testing.T) {
		ic := &store.InvitationCode{
			ID:           "ic-200",
			Code:         "TEST-USED",
			Status:       "used",
			UsedByEmail:  "user@example.com",
			UsedAt:       &usedAt,
			ValidityDays: 30,
			Exported:     true,
			VIP:          true,
			CreatedAt:    now,
		}

		resp := toInvitationCodeResponse(ic)

		if resp.ID != "ic-200" {
			t.Errorf("ID: got %q, want %q", resp.ID, "ic-200")
		}
		if resp.Status != "used" {
			t.Errorf("Status: got %q, want %q", resp.Status, "used")
		}
		if resp.UsedByEmail != "user@example.com" {
			t.Errorf("UsedByEmail: got %q, want %q", resp.UsedByEmail, "user@example.com")
		}
		if resp.UsedAt == nil {
			t.Fatal("UsedAt: got nil, want non-nil")
		}
		if *resp.UsedAt != "2024-06-16T08:30:00Z" {
			t.Errorf("UsedAt: got %q, want %q", *resp.UsedAt, "2024-06-16T08:30:00Z")
		}
		if resp.BoundAt == nil {
			t.Fatal("BoundAt: got nil, want non-nil (should mirror UsedAt)")
		}
		if *resp.BoundAt != *resp.UsedAt {
			t.Errorf("BoundAt: got %q, want same as UsedAt %q", *resp.BoundAt, *resp.UsedAt)
		}
		if resp.Exported != true {
			t.Errorf("Exported: got %v, want true", resp.Exported)
		}
		if resp.VIP != true {
			t.Errorf("VIP: got %v, want true", resp.VIP)
		}
	})
}

// TestPreservation_DeprecatedEmailInviteHandlerExists verifies that the
// DeprecatedEmailInviteHandler function still exists and returns 410 Gone.
// The fix must NOT delete this function — it may be referenced elsewhere.
//
// **Validates: Requirements 3.2**
func TestPreservation_DeprecatedEmailInviteHandlerExists(t *testing.T) {
	handler := DeprecatedEmailInviteHandler()
	if handler == nil {
		t.Fatal("DeprecatedEmailInviteHandler() returned nil; function must not be removed")
	}

	req := httptest.NewRequest(http.MethodGet, "/deprecated-test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusGone {
		t.Errorf("DeprecatedEmailInviteHandler: expected status 410, got %d", rr.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if code, _ := body["code"].(string); code != "FEATURE_REMOVED" {
		t.Errorf("expected error code %q, got %q", "FEATURE_REMOVED", code)
	}
}

// jsonKeys returns the keys of a JSON object map.
func jsonKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// toLowerSnake is a simple check: returns the input unchanged if it's already
// lowercase with underscores. For our purposes, any byte that is uppercase
// means the field name is not properly tagged.
func toLowerSnake(s string) string {
	for _, c := range s {
		if c >= 'A' && c <= 'Z' {
			return "" // force mismatch
		}
	}
	return s
}
