package auth

import (
	"net/http"
	"testing"
)

func TestJWTCreateVerify(t *testing.T) {
	token, err := CreateJWT(1)
	if err != nil {
		t.Fatalf("create jwt failed: %v", err)
	}
	payload, err := VerifyJWT(token)
	if err != nil {
		t.Fatalf("verify jwt failed: %v", err)
	}
	if payload["role"] != "admin" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestVerifyAdminRequest(t *testing.T) {
	token, _ := CreateJWT(1)
	req, _ := http.NewRequest(http.MethodGet, "/admin/config", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if err := VerifyAdminRequest(req); err != nil {
		t.Fatalf("expected token accepted: %v", err)
	}
}
