package im

import (
	"context"
	"errors"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/security"
)

// --- mock SecurityPolicyProvider ---

type mockSecurityProvider struct {
	centralizedEnabled bool
	centralizedErr     error
	policy             *security.EffectivePolicy
	policyErr          error
}

func (m *mockSecurityProvider) IsCentralizedEnabled(_ context.Context) (bool, error) {
	return m.centralizedEnabled, m.centralizedErr
}

func (m *mockSecurityProvider) GetEffectivePolicyByUserID(_ context.Context, _ string) (*security.EffectivePolicy, error) {
	return m.policy, m.policyErr
}

func (m *mockSecurityProvider) GetHeartbeatPolicy(_ context.Context, _ string) (*security.HeartbeatSecurityPayload, error) {
	return nil, nil
}

// --- helpers ---

func collectAudit(calls *[]auditCall) func(string, string, string) {
	return func(email, platform, fileType string) {
		*calls = append(*calls, auditCall{email, platform, fileType})
	}
}

type auditCall struct {
	email, platform, fileType string
}

// --- tests ---

func TestCheckOutbound_NilSecuritySvc(t *testing.T) {
	interceptor := NewOutboundInterceptor(nil, nil)
	resp := &GenericResponse{StatusCode: 200, Body: "hello", FileData: "abc"}
	got, intercepted := interceptor.CheckOutbound(context.Background(), "user1", resp)
	if intercepted {
		t.Fatal("expected no interception when securitySvc is nil")
	}
	if got != resp {
		t.Fatal("expected original response returned")
	}
}

func TestCheckOutbound_CentralizedDisabled(t *testing.T) {
	provider := &mockSecurityProvider{centralizedEnabled: false}
	interceptor := NewOutboundInterceptor(provider, nil)
	resp := &GenericResponse{StatusCode: 200, FileData: "data"}
	got, intercepted := interceptor.CheckOutbound(context.Background(), "user1", resp)
	if intercepted {
		t.Fatal("expected no interception when centralized is disabled")
	}
	if got != resp {
		t.Fatal("expected original response")
	}
}

func TestCheckOutbound_CentralizedCheckError_FailOpen(t *testing.T) {
	provider := &mockSecurityProvider{centralizedErr: errors.New("db error")}
	interceptor := NewOutboundInterceptor(provider, nil)
	resp := &GenericResponse{StatusCode: 200, FileData: "data"}
	got, intercepted := interceptor.CheckOutbound(context.Background(), "user1", resp)
	if intercepted {
		t.Fatal("expected fail-open on centralized check error")
	}
	if got != resp {
		t.Fatal("expected original response on error")
	}
}

func TestCheckOutbound_PolicyQueryError_FailOpen(t *testing.T) {
	provider := &mockSecurityProvider{
		centralizedEnabled: true,
		policyErr:          errors.New("db error"),
	}
	interceptor := NewOutboundInterceptor(provider, nil)
	resp := &GenericResponse{StatusCode: 200, FileData: "data"}
	got, intercepted := interceptor.CheckOutbound(context.Background(), "user1", resp)
	if intercepted {
		t.Fatal("expected fail-open on policy query error")
	}
	if got != resp {
		t.Fatal("expected original response on error")
	}
}

func TestCheckOutbound_FileBlocked(t *testing.T) {
	var audits []auditCall
	provider := &mockSecurityProvider{
		centralizedEnabled: true,
		policy:             &security.EffectivePolicy{FileOutboundEnabled: false, ImageOutboundEnabled: true},
	}
	interceptor := NewOutboundInterceptor(provider, collectAudit(&audits))
	resp := &GenericResponse{StatusCode: 200, Body: "result", FileData: "base64data", FileName: "report.pdf"}
	got, intercepted := interceptor.CheckOutbound(context.Background(), "user@example.com", resp)
	if !intercepted {
		t.Fatal("expected file to be intercepted")
	}
	if got.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", got.StatusCode)
	}
	if got.Body != "文件外发已被管理员禁止" {
		t.Fatalf("unexpected body: %s", got.Body)
	}
	if len(audits) != 1 || audits[0].fileType != "file" {
		t.Fatalf("expected one audit call for file, got %v", audits)
	}
}

func TestCheckOutbound_ImageBlocked(t *testing.T) {
	var audits []auditCall
	provider := &mockSecurityProvider{
		centralizedEnabled: true,
		policy:             &security.EffectivePolicy{FileOutboundEnabled: true, ImageOutboundEnabled: false},
	}
	interceptor := NewOutboundInterceptor(provider, collectAudit(&audits))
	resp := &GenericResponse{StatusCode: 200, Body: "screenshot", ImageKey: "img_key_123"}
	got, intercepted := interceptor.CheckOutbound(context.Background(), "user@example.com", resp)
	if !intercepted {
		t.Fatal("expected image to be intercepted")
	}
	if got.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", got.StatusCode)
	}
	if got.Body != "图片外发已被管理员禁止" {
		t.Fatalf("unexpected body: %s", got.Body)
	}
	if len(audits) != 1 || audits[0].fileType != "image" {
		t.Fatalf("expected one audit call for image, got %v", audits)
	}
}

func TestCheckOutbound_AllAllowed(t *testing.T) {
	provider := &mockSecurityProvider{
		centralizedEnabled: true,
		policy:             &security.EffectivePolicy{FileOutboundEnabled: true, ImageOutboundEnabled: true},
	}
	interceptor := NewOutboundInterceptor(provider, nil)
	resp := &GenericResponse{StatusCode: 200, FileData: "data", ImageKey: "img"}
	got, intercepted := interceptor.CheckOutbound(context.Background(), "user1", resp)
	if intercepted {
		t.Fatal("expected no interception when both are allowed")
	}
	if got != resp {
		t.Fatal("expected original response")
	}
}

func TestCheckOutbound_NoFileNoImage_NoInterception(t *testing.T) {
	provider := &mockSecurityProvider{
		centralizedEnabled: true,
		policy:             &security.EffectivePolicy{FileOutboundEnabled: false, ImageOutboundEnabled: false},
	}
	interceptor := NewOutboundInterceptor(provider, nil)
	resp := &GenericResponse{StatusCode: 200, Body: "plain text only"}
	got, intercepted := interceptor.CheckOutbound(context.Background(), "user1", resp)
	if intercepted {
		t.Fatal("expected no interception for text-only response")
	}
	if got != resp {
		t.Fatal("expected original response")
	}
}

func TestCheckOutbound_FileBlockedPriority(t *testing.T) {
	// When response has both file and image, and file is blocked,
	// file interception should trigger first.
	provider := &mockSecurityProvider{
		centralizedEnabled: true,
		policy:             &security.EffectivePolicy{FileOutboundEnabled: false, ImageOutboundEnabled: false},
	}
	var audits []auditCall
	interceptor := NewOutboundInterceptor(provider, collectAudit(&audits))
	resp := &GenericResponse{StatusCode: 200, FileData: "data", ImageKey: "img"}
	got, intercepted := interceptor.CheckOutbound(context.Background(), "user1", resp)
	if !intercepted {
		t.Fatal("expected interception")
	}
	if got.Body != "文件外发已被管理员禁止" {
		t.Fatalf("expected file block message, got: %s", got.Body)
	}
	if len(audits) != 1 || audits[0].fileType != "file" {
		t.Fatalf("expected file audit, got %v", audits)
	}
}

func TestCheckOutbound_AuditLogNil_NoError(t *testing.T) {
	provider := &mockSecurityProvider{
		centralizedEnabled: true,
		policy:             &security.EffectivePolicy{FileOutboundEnabled: false},
	}
	interceptor := NewOutboundInterceptor(provider, nil) // nil auditLog
	resp := &GenericResponse{StatusCode: 200, FileData: "data"}
	got, intercepted := interceptor.CheckOutbound(context.Background(), "user1", resp)
	if !intercepted {
		t.Fatal("expected interception even without audit logger")
	}
	if got.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", got.StatusCode)
	}
}
