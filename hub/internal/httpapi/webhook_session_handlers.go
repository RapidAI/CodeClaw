package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/session"
)

// WebhookSessionRequest is the JSON body for POST /api/webhook/session.
type WebhookSessionRequest struct {
	Tool        string `json:"tool"`
	ProjectPath string `json:"project_path"`
	Prompt      string `json:"prompt"`
	CallbackURL string `json:"callback_url"`
}

// WebhookSessionResponse is returned on successful webhook session creation.
type WebhookSessionResponse struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
}

// WebhookCreateSessionHandler returns an http.HandlerFunc that creates a
// session via webhook. It validates a Bearer token from the Authorization
// header, decodes the JSON request, and returns a session_id.
func WebhookCreateSessionHandler(deviceSvc *device.Service, sessionSvc *session.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// --- Bearer token auth ---
		token := extractBearerToken(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing or invalid Bearer token")
			return
		}

		// --- Decode request body ---
		var req WebhookSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}

		req.Tool = strings.TrimSpace(req.Tool)
		req.ProjectPath = strings.TrimSpace(req.ProjectPath)
		req.Prompt = strings.TrimSpace(req.Prompt)
		req.CallbackURL = strings.TrimSpace(req.CallbackURL)

		// --- Validate required fields ---
		if req.Tool == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "tool is required")
			return
		}

		// Generate a unique session ID using tool name, timestamp, and random suffix.
		sessionID := fmt.Sprintf("webhook-%s-%d-%04x", req.Tool, time.Now().UnixNano(), rand.Intn(0xFFFF))

		resp := WebhookSessionResponse{
			SessionID: sessionID,
			Status:    "created",
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// extractBearerToken pulls the token from "Authorization: Bearer <token>".
func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return ""
	}
	return strings.TrimSpace(auth[len(prefix):])
}

// WebhookCallbackPayload is the JSON body sent to callback_url when a
// webhook-triggered session completes.
type WebhookCallbackPayload struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
	Summary   string `json:"summary"`
}

// SendWebhookCallback sends a POST request to callbackURL with the session
// result. It uses a 10-second timeout and returns an error if the request
// fails or the server responds with a non-2xx status code.
func SendWebhookCallback(callbackURL, sessionID, status, summary string) error {
	payload := WebhookCallbackPayload{
		SessionID: sessionID,
		Status:    status,
		Summary:   summary,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal webhook callback payload: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Post(callbackURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("send webhook callback to %s: %w", callbackURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook callback to %s returned status %d", callbackURL, resp.StatusCode)
	}

	return nil
}
