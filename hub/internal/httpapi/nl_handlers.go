package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/mcp"
	"github.com/RapidAI/CodeClaw/hub/internal/skill"
)

// ──────────────────────────────────────────────────────────────────────────────
// NL Skill handlers
// ──────────────────────────────────────────────────────────────────────────────

func ListNLSkillsHandler(exec *skill.Executor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if exec == nil {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		writeJSON(w, http.StatusOK, exec.List(r.Context()))
	}
}

func CreateNLSkillHandler(exec *skill.Executor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if exec == nil {
			writeError(w, http.StatusServiceUnavailable, "SKILL_NOT_CONFIGURED", "Skill executor is not configured")
			return
		}
		var def skill.SkillDefinition
		if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if err := exec.Register(r.Context(), def); err != nil {
			writeError(w, http.StatusInternalServerError, "SKILL_CREATE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func UpdateNLSkillHandler(exec *skill.Executor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if exec == nil {
			writeError(w, http.StatusServiceUnavailable, "SKILL_NOT_CONFIGURED", "Skill executor is not configured")
			return
		}
		var def skill.SkillDefinition
		if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		// Snapshot the old skill for rollback.
		old := exec.Get(r.Context(), def.Name)
		_ = exec.Delete(r.Context(), def.Name)
		if err := exec.Register(r.Context(), def); err != nil {
			// Rollback: restore the old skill if it existed.
			if old != nil {
				_ = exec.Register(r.Context(), *old)
			}
			writeError(w, http.StatusInternalServerError, "SKILL_UPDATE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func DeleteNLSkillHandler(exec *skill.Executor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if exec == nil {
			writeError(w, http.StatusServiceUnavailable, "SKILL_NOT_CONFIGURED", "Skill executor is not configured")
			return
		}
		name := r.PathValue("name")
		if name == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "name is required")
			return
		}
		if err := exec.Delete(r.Context(), name); err != nil {
			writeError(w, http.StatusInternalServerError, "SKILL_DELETE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func ListCandidateSkillsHandler(cryst *skill.Crystallizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cryst == nil {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		candidates, err := cryst.ListCandidates(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CANDIDATES_LIST_FAILED", err.Error())
			return
		}
		if candidates == nil {
			candidates = []skill.SkillDefinition{}
		}
		writeJSON(w, http.StatusOK, candidates)
	}
}

func ConfirmCandidateSkillHandler(cryst *skill.Crystallizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cryst == nil {
			writeError(w, http.StatusServiceUnavailable, "CRYSTALLIZER_NOT_CONFIGURED", "Skill crystallizer is not configured")
			return
		}
		var def skill.SkillDefinition
		if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if err := cryst.Confirm(r.Context(), def); err != nil {
			writeError(w, http.StatusInternalServerError, "CANDIDATE_CONFIRM_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func IgnoreCandidateSkillHandler(cryst *skill.Crystallizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cryst == nil {
			writeError(w, http.StatusServiceUnavailable, "CRYSTALLIZER_NOT_CONFIGURED", "Skill crystallizer is not configured")
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if body.Name == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "name is required")
			return
		}
		if err := cryst.Ignore(r.Context(), body.Name); err != nil {
			writeError(w, http.StatusInternalServerError, "CANDIDATE_IGNORE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// MCP Server handlers
// ──────────────────────────────────────────────────────────────────────────────

func ListMCPServersHandler(reg *mcp.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		writeJSON(w, http.StatusOK, reg.ListServers(r.Context()))
	}
}

func RegisterMCPServerHandler(reg *mcp.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil {
			writeError(w, http.StatusServiceUnavailable, "MCP_NOT_CONFIGURED", "MCP registry is not configured")
			return
		}
		var srv mcp.MCPServer
		if err := json.NewDecoder(r.Body).Decode(&srv); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if err := reg.Register(r.Context(), srv); err != nil {
			writeError(w, http.StatusInternalServerError, "MCP_REGISTER_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func UpdateMCPServerHandler(reg *mcp.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil {
			writeError(w, http.StatusServiceUnavailable, "MCP_NOT_CONFIGURED", "MCP registry is not configured")
			return
		}
		var srv mcp.MCPServer
		if err := json.NewDecoder(r.Body).Decode(&srv); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		// Snapshot the old server for rollback.
		var old *mcp.MCPServer
		for _, s := range reg.ListServers(r.Context()) {
			if s.ID == srv.ID {
				copy := s
				old = &copy
				break
			}
		}
		_ = reg.Unregister(r.Context(), srv.ID)
		if err := reg.Register(r.Context(), srv); err != nil {
			if old != nil {
				_ = reg.Register(r.Context(), *old)
			}
			writeError(w, http.StatusInternalServerError, "MCP_UPDATE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func UnregisterMCPServerHandler(reg *mcp.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil {
			writeError(w, http.StatusServiceUnavailable, "MCP_NOT_CONFIGURED", "MCP registry is not configured")
			return
		}
		id := r.PathValue("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "id is required")
			return
		}
		if err := reg.Unregister(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, "MCP_UNREGISTER_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func GetMCPServerToolsHandler(reg *mcp.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		id := r.PathValue("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "id is required")
			return
		}
		servers := reg.ListServers(r.Context())
		for _, srv := range servers {
			if srv.ID == id {
				writeJSON(w, http.StatusOK, srv.Tools)
				return
			}
		}
		writeError(w, http.StatusNotFound, "MCP_SERVER_NOT_FOUND", "server not found")
	}
}

func CheckMCPServerHealthHandler(reg *mcp.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil {
			writeError(w, http.StatusServiceUnavailable, "MCP_NOT_CONFIGURED", "MCP registry is not configured")
			return
		}
		id := r.PathValue("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "id is required")
			return
		}
		if err := reg.HealthCheck(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, "MCP_HEALTH_CHECK_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}
