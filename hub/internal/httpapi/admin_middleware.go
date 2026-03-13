package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type adminContextKey string

const adminUserContextKey adminContextKey = "admin_user"

func RequireAdmin(admins *auth.AdminService, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authz := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(strings.ToLower(authz), "bearer ") {
			writeError(w, http.StatusUnauthorized, "ADMIN_UNAUTHORIZED", "Admin authorization required")
			return
		}

		token := strings.TrimSpace(authz[len("Bearer "):])
		admin, err := admins.Authenticate(r.Context(), token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "ADMIN_UNAUTHORIZED", "Invalid admin token")
			return
		}

		ctx := context.WithValue(r.Context(), adminUserContextKey, admin)
		next(w, r.WithContext(ctx))
	}
}

func AdminFromContext(ctx context.Context) *store.AdminUser {
	admin, _ := ctx.Value(adminUserContextKey).(*store.AdminUser)
	return admin
}
