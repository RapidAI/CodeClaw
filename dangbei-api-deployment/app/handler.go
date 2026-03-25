package app

import (
	"net/http"

	"ds2api/internal/server"
)

func NewHandler() http.Handler {
	return server.NewApp().Router
}
