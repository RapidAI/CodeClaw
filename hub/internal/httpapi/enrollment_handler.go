package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

type enrollmentResponse struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Status    string `json:"status"`
	Note      string `json:"note"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type approveEnrollmentRequest struct {
	ID string `json:"id"`
}

type rejectEnrollmentRequest struct {
	ID string `json:"id"`
}

func ListPendingEnrollmentsHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := identity.ListPendingEnrollments(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		resp := make([]enrollmentResponse, len(items))
		for i, item := range items {
			resp[i] = enrollmentResponse{
				ID:        item.ID,
				Email:     item.Email,
				Status:    item.Status,
				Note:      item.Note,
				CreatedAt: item.CreatedAt.Format(time.RFC3339),
				UpdatedAt: item.UpdatedAt.Format(time.RFC3339),
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"enrollments": resp})
	}
}

func ApproveEnrollmentHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req approveEnrollmentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.ID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Enrollment ID is required")
			return
		}
		user, err := identity.ApproveEnrollment(r.Context(), req.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "APPROVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":   true,
			"user": map[string]any{"email": user.Email, "sn": user.SN},
		})
	}
}

func RejectEnrollmentHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req rejectEnrollmentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.ID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Enrollment ID is required")
			return
		}
		if err := identity.RejectEnrollment(r.Context(), req.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "REJECT_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}
