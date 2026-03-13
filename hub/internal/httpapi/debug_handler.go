package httpapi

import (
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/session"
)

func DebugListMachinesHandler(devices *device.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
		if userID != "" {
			items, err := devices.ListMachines(r.Context(), userID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
				return
			}

			writeJSON(w, http.StatusOK, map[string]any{
				"machines": items,
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"machines": devices.ListOnlineMachines(),
		})
	}
}

func DebugListMachineEventsHandler(devices *device.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"events": devices.ListEvents(100),
		})
	}
}

func DebugListSessionsHandler(svc *session.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		machineID := strings.TrimSpace(r.URL.Query().Get("machine_id"))
		userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
		if machineID == "" || userID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "machine_id and user_id are required")
			return
		}

		items, err := svc.ListByMachine(r.Context(), userID, machineID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"sessions": items,
		})
	}
}

func DebugGetSessionHandler(svc *session.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		machineID := strings.TrimSpace(r.URL.Query().Get("machine_id"))
		userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
		sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
		if machineID == "" || userID == "" || sessionID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "machine_id, user_id and session_id are required")
			return
		}

		item, ok := svc.GetSnapshot(userID, machineID, sessionID)
		if !ok || item == nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "session not found")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"session_id":    item.SessionID,
			"machine_id":    item.MachineID,
			"user_id":       item.UserID,
			"summary":       item.Summary,
			"preview":       item.Preview,
			"recent_events": item.RecentEvents,
			"host_online":   item.HostOnline,
			"updated_at":    item.UpdatedAt.Unix(),
		})
	}
}
