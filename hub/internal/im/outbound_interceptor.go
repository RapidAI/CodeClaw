package im

import (
	"context"
	"log"

	"github.com/RapidAI/CodeClaw/hub/internal/security"
)

// OutboundInterceptor checks file/image outbound permissions before
// sending responses through IM channels.
type OutboundInterceptor struct {
	securitySvc security.SecurityPolicyProvider
	auditLog    func(email, platform, fileType string)
}

// NewOutboundInterceptor creates a new OutboundInterceptor.
// svc may be nil (interceptor becomes a no-op).
func NewOutboundInterceptor(svc security.SecurityPolicyProvider, auditLog func(email, platform, fileType string)) *OutboundInterceptor {
	return &OutboundInterceptor{
		securitySvc: svc,
		auditLog:    auditLog,
	}
}

// CheckOutbound inspects the outgoing response and blocks file/image
// delivery when the user's effective policy forbids it.
//
// platform identifies the IM channel (e.g. "feishu", "qqbot") for audit logging.
// Returns (possibly replaced response, wasIntercepted).
// On any error querying the policy the interceptor fails open (allows).
func (i *OutboundInterceptor) CheckOutbound(ctx context.Context, userID string, resp *GenericResponse, platform ...string) (*GenericResponse, bool) {
	if i.securitySvc == nil {
		return resp, false
	}

	// Check centralized security switch
	enabled, err := i.securitySvc.IsCentralizedEnabled(ctx)
	if err != nil {
		log.Printf("[OutboundInterceptor] IsCentralizedEnabled error, fail-open: %v", err)
		return resp, false
	}
	if !enabled {
		return resp, false
	}

	// Get effective policy for the user
	policy, err := i.securitySvc.GetEffectivePolicyByUserID(ctx, userID)
	if err != nil {
		log.Printf("[OutboundInterceptor] GetEffectivePolicyByUserID error for %s, fail-open: %v", userID, err)
		return resp, false
	}

	plat := ""
	if len(platform) > 0 {
		plat = platform[0]
	}

	// Check file outbound
	if resp.FileData != "" && !policy.FileOutboundEnabled {
		if i.auditLog != nil {
			i.auditLog(userID, plat, "file")
		}
		return &GenericResponse{StatusCode: 403, Body: "文件外发已被管理员禁止"}, true
	}

	// Check image outbound
	if resp.ImageKey != "" && !policy.ImageOutboundEnabled {
		if i.auditLog != nil {
			i.auditLog(userID, plat, "image")
		}
		return &GenericResponse{StatusCode: 403, Body: "图片外发已被管理员禁止"}, true
	}

	return resp, false
}
