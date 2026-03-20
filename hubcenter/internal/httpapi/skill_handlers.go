package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/skill"
)

type SkillHandlers struct {
	store *skill.SkillStore
}

func NewSkillHandlers(store *skill.SkillStore) *SkillHandlers {
	return &SkillHandlers{store: store}
}

func skillError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

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

func (h *SkillHandlers) PopularSkills(w http.ResponseWriter, r *http.Request) {
	skills := h.store.TopByDownloads(20)
	if skills == nil {
		skills = []skill.HubSkillMeta{}
	}
	writeJSON(w, http.StatusOK, skills)
}

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
	s.TrustLevel = "community"
	if err := h.store.Publish(s); err != nil {
		skillError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, s)
}

func (h *SkillHandlers) RateSkill(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		skillError(w, http.StatusBadRequest, "skill id is required")
		return
	}
	var req struct {
		MaclawID string `json:"maclaw_id"`
		Score    int    `json:"score"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		skillError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.MaclawID == "" {
		skillError(w, http.StatusBadRequest, "maclaw_id is required")
		return
	}
	if req.Score < 1 || req.Score > 5 {
		skillError(w, http.StatusBadRequest, "score must be between 1 and 5")
		return
	}
	if err := h.store.Rate(id, req.MaclawID, req.Score); err != nil {
		skillError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *SkillHandlers) AdminSetVisibility(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      string `json:"id"`
		Visible bool   `json:"visible"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		skillError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.ID == "" {
		skillError(w, http.StatusBadRequest, "id is required")
		return
	}
	if err := h.store.SetVisibility(req.ID, req.Visible); err != nil {
		skillError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *SkillHandlers) AdminDeleteSkill(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		skillError(w, http.StatusBadRequest, "skill id is required")
		return
	}
	if err := h.store.DeleteSkill(id); err != nil {
		skillError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *SkillHandlers) AdminListSkills(w http.ResponseWriter, r *http.Request) {
	pageStr := r.URL.Query().Get("page")
	page := 1
	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}
	result := h.store.ListAll(page)
	writeJSON(w, http.StatusOK, result)
}
