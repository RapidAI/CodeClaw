package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/auth"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/entry"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/hubs"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/mail"
)

type EntryResolveRequest struct {
	Email string `json:"email"`
}

type HubHeartbeatRequest struct {
	HubSecret string `json:"hub_secret"`
}

func RegisterHubHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req hubs.RegisterHubRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		resp, err := service.RegisterHubFromIP(r.Context(), req, clientIPFromRequest(r))
		if err != nil {
			if errors.Is(err, hubs.ErrEmailBlocked) {
				writeError(w, http.StatusForbidden, "EMAIL_BLOCKED", err.Error())
				return
			}
			if errors.Is(err, hubs.ErrIPBlocked) {
				writeError(w, http.StatusForbidden, "IP_BLOCKED", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "REGISTER_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func HubHeartbeatHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hubID := r.PathValue("id")
		if hubID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_HUB_ID", "Hub id is required")
			return
		}

		var req HubHeartbeatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}

		if err := service.HeartbeatHubWithSecret(r.Context(), hubID, req.HubSecret); err != nil {
			if errors.Is(err, hubs.ErrHubUnauthorized) {
				writeError(w, http.StatusUnauthorized, "HUB_UNREGISTERED", "Hub is not registered")
				return
			}
			writeError(w, http.StatusInternalServerError, "HEARTBEAT_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "online"})
	}
}

func EntryResolveHandler(service *entry.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req EntryResolveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		resp, err := service.ResolveByEmailFromIP(r.Context(), req.Email, clientIPFromRequest(r))
		if err != nil {
			if errors.Is(err, entry.ErrIPBlocked) {
				writeError(w, http.StatusForbidden, "IP_BLOCKED", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "ENTRY_RESOLVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func NewRouter(adminService *auth.AdminService, hubService *hubs.Service, entryService *entry.Service, mailer mail.Mailer) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", HealthHandler("codeclaw-hubcenter"))
	mux.HandleFunc("GET /api/admin/status", AdminStatusHandler(adminService))
	mux.HandleFunc("POST /api/admin/setup", SetupAdminHandler(adminService))
	mux.HandleFunc("POST /api/admin/login", AdminLoginHandler(adminService))
	mux.HandleFunc("POST /api/admin/password", RequireAdmin(adminService, AdminChangePasswordHandler(adminService)))
	mux.HandleFunc("GET /api/admin/hubs", RequireAdmin(adminService, ListHubsHandler(hubService)))
	mux.HandleFunc("POST /api/admin/hubs/{id}/visibility", RequireAdmin(adminService, UpdateHubVisibilityHandler(hubService)))
	mux.HandleFunc("POST /api/admin/hubs/{id}/disable", RequireAdmin(adminService, DisableHubHandler(hubService)))
	mux.HandleFunc("POST /api/admin/hubs/{id}/enable", RequireAdmin(adminService, EnableHubHandler(hubService)))
	mux.HandleFunc("DELETE /api/admin/hubs/{id}", RequireAdmin(adminService, DeleteHubHandler(hubService)))
	mux.HandleFunc("GET /api/admin/blocked-emails", RequireAdmin(adminService, ListBlockedEmailsHandler(hubService)))
	mux.HandleFunc("POST /api/admin/blocked-emails", RequireAdmin(adminService, AddBlockedEmailHandler(hubService)))
	mux.HandleFunc("DELETE /api/admin/blocked-emails/{email}", RequireAdmin(adminService, RemoveBlockedEmailHandler(hubService)))
	mux.HandleFunc("GET /api/admin/blocked-ips", RequireAdmin(adminService, ListBlockedIPsHandler(hubService)))
	mux.HandleFunc("POST /api/admin/blocked-ips", RequireAdmin(adminService, AddBlockedIPHandler(hubService)))
	mux.HandleFunc("DELETE /api/admin/blocked-ips/{ip}", RequireAdmin(adminService, RemoveBlockedIPHandler(hubService)))
	mux.HandleFunc("POST /api/admin/mail/test", RequireAdmin(adminService, AdminSendTestMailHandler(mailer)))
	mux.HandleFunc("POST /api/hubs/register", RegisterHubHandler(hubService))
	mux.HandleFunc("POST /api/hubs/{id}/heartbeat", HubHeartbeatHandler(hubService))
	mux.HandleFunc("POST /api/entry/resolve", EntryResolveHandler(entryService))
	registerAdminStaticRoutes(mux, "./web/admin", "/admin")
	return mux
}
