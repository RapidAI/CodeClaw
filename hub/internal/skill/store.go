package skill

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const pageSize = 20

// SkillStore 管理 Hub 侧的 Skill 存储。
// MVP 阶段使用 JSON 文件存储，每个 Skill 一个 JSON 文件。
type SkillStore struct {
	mu     sync.RWMutex
	dir    string
	index  []HubSkillMeta
	skills map[string]*HubSkillFull
}

// NewSkillStore 创建 SkillStore 并加载目录中所有 Skill 到内存索引。
func NewSkillStore(dir string) *SkillStore {
	s := &SkillStore{
		dir:    dir,
		skills: make(map[string]*HubSkillFull),
	}
	_ = os.MkdirAll(dir, 0o755)
	_ = s.RebuildIndex()
	return s
}

// Search 对 name、description、tags 进行关键词匹配，支持中文，返回分页结果。
func (s *SkillStore) Search(query string, tags []string, page int) SkillSearchResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if page < 1 {
		page = 1
	}

	queryLower := strings.ToLower(query)
	queryTerms := strings.Fields(queryLower)

	var matched []HubSkillMeta
	for _, meta := range s.index {
		if matchesSkill(meta, queryTerms, tags) {
			matched = append(matched, meta)
		}
	}

	total := len(matched)
	start := (page - 1) * pageSize
	if start >= total {
		return SkillSearchResult{Skills: []HubSkillMeta{}, Total: total, Page: page}
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	return SkillSearchResult{
		Skills: matched[start:end],
		Total:  total,
		Page:   page,
	}
}

// Get 返回完整 Skill 定义。
func (s *SkillStore) Get(id string) (*HubSkillFull, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	skill, ok := s.skills[id]
	if !ok {
		return nil, fmt.Errorf("skill not found: %s", id)
	}
	return skill, nil
}

// Publish 写入 JSON 文件并更新内存索引。
func (s *SkillStore) Publish(skill HubSkillFull) error {
	data, err := json.MarshalIndent(skill, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal skill: %w", err)
	}

	path := filepath.Join(s.dir, skill.ID+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write skill file: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.skills[skill.ID] = &skill
	s.rebuildIndexFromSkills()
	return nil
}

// RebuildIndex 重新扫描目录构建内存索引。
func (s *SkillStore) RebuildIndex() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read skill dir: %w", err)
	}

	skills := make(map[string]*HubSkillFull)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			continue
		}
		var skill HubSkillFull
		if err := json.Unmarshal(data, &skill); err != nil {
			continue
		}
		if skill.ID == "" {
			continue
		}
		skills[skill.ID] = &skill
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.skills = skills
	s.rebuildIndexFromSkills()
	return nil
}

// rebuildIndexFromSkills rebuilds the index slice from the skills map.
// Must be called with s.mu held (write lock).
func (s *SkillStore) rebuildIndexFromSkills() {
	index := make([]HubSkillMeta, 0, len(s.skills))
	for _, skill := range s.skills {
		index = append(index, skill.HubSkillMeta)
	}
	s.index = index
}

// TopByDownloads returns the top N skills sorted by download count descending.
func (s *SkillStore) TopByDownloads(n int) []HubSkillMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if n <= 0 || len(s.index) == 0 {
		return nil
	}

	sorted := make([]HubSkillMeta, len(s.index))
	copy(sorted, s.index)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Downloads > sorted[j].Downloads
	})

	if n > len(sorted) {
		n = len(sorted)
	}
	return sorted[:n]
}

// matchesSkill checks if a skill matches the given query terms and tags.
func matchesSkill(meta HubSkillMeta, queryTerms []string, tags []string) bool {
	// If both query and tags are empty, match everything.
	if len(queryTerms) == 0 && len(tags) == 0 {
		return true
	}

	// Check tag filter: all requested tags must be present.
	if len(tags) > 0 {
		tagSet := make(map[string]struct{}, len(meta.Tags))
		for _, t := range meta.Tags {
			tagSet[strings.ToLower(t)] = struct{}{}
		}
		for _, t := range tags {
			if _, ok := tagSet[strings.ToLower(t)]; !ok {
				return false
			}
		}
	}

	// If no query terms, tag match is sufficient.
	if len(queryTerms) == 0 {
		return true
	}

	// Build searchable text from name, description, and tags.
	searchText := strings.ToLower(meta.Name + " " + meta.Description + " " + strings.Join(meta.Tags, " "))

	// All query terms must appear in the searchable text.
	for _, term := range queryTerms {
		if !strings.Contains(searchText, term) {
			return false
		}
	}
	return true
}
