package skill

// HubSkillMeta 是 SkillHub 中 Skill 的元数据。
type HubSkillMeta struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	TrustLevel  string   `json:"trust_level"` // "official", "community", "unknown"
	Downloads   int      `json:"downloads"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

// HubSkillStep represents a single action within a Hub Skill.
type HubSkillStep struct {
	Action  string                 `json:"action"`
	Params  map[string]interface{} `json:"params"`
	OnError string                 `json:"on_error"`
}

// HubSkillFull 包含完整的 Skill 定义，用于下载。
type HubSkillFull struct {
	HubSkillMeta
	Triggers []string      `json:"triggers"`
	Steps    []HubSkillStep `json:"steps"`
	Manifest SkillManifest `json:"manifest"`
}

// SkillManifest 描述 Skill 的依赖和兼容性。
type SkillManifest struct {
	MinMaclawVersion string   `json:"min_maclaw_version,omitempty"`
	RequiredMCP      []string `json:"required_mcp,omitempty"`
	Permissions      []string `json:"permissions,omitempty"`
}

// SkillSearchResult 搜索结果的分页包装。
type SkillSearchResult struct {
	Skills []HubSkillMeta `json:"skills"`
	Total  int            `json:"total"`
	Page   int            `json:"page"`
}
