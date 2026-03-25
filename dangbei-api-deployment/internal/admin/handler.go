package admin

import (
	"github.com/go-chi/chi/v5"

	"ds2api/internal/account"
	"ds2api/internal/config"
	"ds2api/internal/deepseek"
)

type Handler struct {
	Store *config.Store
	Pool  *account.Pool
	DS    *deepseek.Client
}

func RegisterRoutes(r chi.Router, h *Handler) {
	r.Post("/login", h.login)
	r.Get("/verify", h.verify)
	r.Group(func(pr chi.Router) {
		pr.Use(h.requireAdmin)
		pr.Get("/vercel/config", h.getVercelConfig)
		pr.Get("/config", h.getConfig)
		pr.Post("/config", h.updateConfig)
		pr.Post("/keys", h.addKey)
		pr.Delete("/keys/{key}", h.deleteKey)
		pr.Get("/accounts", h.listAccounts)
		pr.Post("/accounts", h.addAccount)
		pr.Delete("/accounts/{identifier}", h.deleteAccount)
		pr.Get("/queue/status", h.queueStatus)
		pr.Post("/accounts/test", h.testSingleAccount)
		pr.Post("/accounts/test-all", h.testAllAccounts)
		pr.Post("/import", h.batchImport)
		pr.Post("/test", h.testAPI)
		pr.Post("/vercel/sync", h.syncVercel)
		pr.Get("/vercel/status", h.vercelStatus)
		pr.Get("/export", h.exportConfig)
	})
}
