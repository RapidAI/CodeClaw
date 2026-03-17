package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/skill"
)

// SkillHandlers provides HTTP handlers for the Skill Catalog API.
type SkillHandlers struct {
	store *skill.SkillStore
}

// NewSkillHandlers creates a new SkillHandlers instance.
func NewSkillHandlers(store *skill.SkillStore) *SkillHandlers {
	return &SkillHandlers{store: store}
}

// skillError writes a JSON error response in {"error": "message"} format.
func skillError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// SearchSkills handles GET /api/v1/skills/search?q=xxx&tags=xxx&page=1
func (h *SkillHandlers) SearchSkills(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	tagsRaw := r.URL.Query().Get("tags")
	pageStr := r.URL.Query().Get("page")

	var tags []string
	if tagsRaw != "" {
		for _, t := range strings.Split(tagsRaw, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				tags = append(tags, t)
			}
		}
	}

	page := 1
	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	result := h.store.Search(q, tags, page)
	writeJSON(w, http.StatusOK, result)
}

// GetSkill handles GET /api/v1/skills/{id}
func (h *SkillHandlers) GetSkill(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		skillError(w, http.StatusBadRequest, "skill id is required")
		return
	}

	s, err := h.store.Get(id)
	if err != nil {
		skillError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// DownloadSkill handles GET /api/v1/skills/{id}/download
func (h *SkillHandlers) DownloadSkill(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		skillError(w, http.StatusBadRequest, "skill id is required")
		return
	}

	s, err := h.store.Get(id)
	if err != nil {
		skillError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// PopularSkills handles GET /api/v1/skills/popular
func (h *SkillHandlers) PopularSkills(w http.ResponseWriter, r *http.Request) {
	skills := h.store.TopByDownloads(20)
	if skills == nil {
		skills = []skill.HubSkillMeta{}
	}
	writeJSON(w, http.StatusOK, skills)
}

// PublishSkill handles POST /api/v1/skills
func (h *SkillHandlers) PublishSkill(w http.ResponseWriter, r *http.Request) {
	var s skill.HubSkillFull
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&s); err != nil {
		skillError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if s.ID == "" || s.Name == "" {
		skillError(w, http.StatusBadRequest, "id and name are required")
		return
	}

	// Community-published skills always get trust_level="community".
	s.TrustLevel = "community"

	if err := h.store.Publish(s); err != nil {
		skillError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, s)
}
