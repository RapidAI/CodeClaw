package httpapi

import "net/http"

func HealthHandler(serviceName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"service": serviceName,
		})
	}
}
