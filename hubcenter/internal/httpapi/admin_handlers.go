package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/auth"
)

type AdminSetupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
}

type AdminLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type AdminChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func AdminStatusHandler(admins *auth.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		initialized, err := admins.IsInitialized(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"initialized": initialized,
		})
	}
}

func SetupAdminHandler(admins *auth.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req AdminSetupRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.Username == "" || req.Password == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Username and password are required")
			return
		}

		initialized, err := admins.IsInitialized(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		if initialized {
			writeError(w, http.StatusConflict, "ADMIN_ALREADY_INITIALIZED", "Admin has already been initialized")
			return
		}

		if err := admins.SetupInitialAdmin(r.Context(), req.Username, req.Password, req.Email); err != nil {
			writeError(w, http.StatusInternalServerError, "SETUP_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"message": "CodeClaw Hub Center admin initialized",
		})
	}
}

func AdminLoginHandler(admins *auth.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req AdminLoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.Username == "" || req.Password == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Username and password are required")
			return
		}

		token, admin, err := admins.Login(r.Context(), req.Username, req.Password)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "LOGIN_FAILED", "Invalid username or password")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"access_token": token,
			"expires_in":   7200,
			"admin": map[string]any{
				"username": admin.Username,
				"email":    admin.Email,
			},
		})
	}
}

func AdminChangePasswordHandler(admins *auth.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin := AdminFromContext(r.Context())
		if admin == nil {
			writeError(w, http.StatusUnauthorized, "ADMIN_UNAUTHORIZED", "Admin authorization required")
			return
		}

		var req AdminChangePasswordRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.CurrentPassword == "" || req.NewPassword == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Current password and new password are required")
			return
		}

		token, updatedAdmin, err := admins.ChangePassword(r.Context(), admin.Username, req.CurrentPassword, req.NewPassword)
		if err != nil {
			if err == auth.ErrInvalidAdminPassword {
				writeError(w, http.StatusUnauthorized, "INVALID_PASSWORD", "Current password is incorrect")
				return
			}
			writeError(w, http.StatusInternalServerError, "CHANGE_PASSWORD_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":           true,
			"access_token": token,
			"admin": map[string]any{
				"username": updatedAdmin.Username,
				"email":    updatedAdmin.Email,
			},
		})
	}
}
