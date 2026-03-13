package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/mail"
)

type AdminSendTestMailRequest struct {
	Email string `json:"email"`
}

func AdminSendTestMailHandler(mailer mail.Mailer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mailer == nil {
			writeError(w, http.StatusBadRequest, "MAIL_NOT_CONFIGURED", "Mail delivery is not configured")
			return
		}

		var req AdminSendTestMailRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}

		email := strings.TrimSpace(req.Email)
		if email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email is required")
			return
		}

		body := "This is a CodeClaw Hub Center test email.\r\n\r\nYour mail configuration is working."
		if err := mailer.Send(r.Context(), []string{email}, "CodeClaw Hub Center test email", body); err != nil {
			writeError(w, http.StatusInternalServerError, "MAIL_SEND_FAILED", fmt.Sprintf("Failed to send test email: %v", err))
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"message": "Test email sent",
		})
	}
}
