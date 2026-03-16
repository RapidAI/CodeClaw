package discovery

import (
	"strings"
	"sync"
	"time"
)

// ToolCategory 类别层索引
type ToolCategory struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	ToolCount   int      `json:"tool_count"`
}

// ToolIndex 工具层索引
type ToolIndex struct {
	Name        string   `json:"name"`
	Category    string   `json:"category"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Source      string   `json:"source"` // "builtin", "mcp", "skill"
	Available   bool     `json:"available"`
}

// ToolDetail 参数层完整定义
type ToolDetail struct {
	ToolIndex
	Parameters []ToolParameter `json:"parameters"`
	Examples   []string        `json:"examples"`
}

// ToolParameter 工具参数定义
type ToolParameter struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// lruEntry tracks a cached ToolDetail with its last access time.
type lruEntry struct {
	detail     *ToolDetail
	accessedAt time.Time
}

// lruCache is a simple LRU cache using a map + oldest-eviction strategy.
type lruCache struct {
	items   map[string]*lruEntry
	maxSize int
}

func newLRUCache(maxSize int) *lruCache {
	return &lruCache{
		items:   make(map[string]*lruEntry),
		maxSize: maxSize,
	}
}

func (c *lruCache) get(key string) (*ToolDetail, bool) {
	entry, ok := c.items[key]
	if !ok {
		return nil, false
	}
	entry.accessedAt = time.Now()
	return entry.detail, true
}

func (c *lruCache) put(key string, detail *ToolDetail) {
	if _, exists := c.items[key]; exists {
		c.items[key] = &lruEntry{detail: detail, accessedAt: time.Now()}
		return
	}
	if len(c.items) >= c.maxSize {
		c.evictOldest()
	}
	c.items[key] = &lruEntry{detail: detail, accessedAt: time.Now()}
}

func (c *lruCache) remove(key string) {
	delete(c.items, key)
}

func (c *lruCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time
	first := true
	for k, v := range c.items {
		if first || v.accessedAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = v.accessedAt
			first = false
		}
	}
	if oldestKey != "" {
		delete(c.items, oldestKey)
	}
}

// Protocol implements the three-layer tool discovery protocol.
type Protocol struct {
	categories []ToolCategory
	tools      map[string][]ToolIndex // category → tools
	details    map[string]*ToolDetail // toolName → detail
	tagIndex   map[string][]string    // tag → toolNames
	cache      *lruCache
	mu         sync.RWMutex
}

// NewProtocol creates a new Protocol and registers built-in tools.
func NewProtocol() *Protocol {
	p := &Protocol{
		categories: make([]ToolCategory, 0),
		tools:      make(map[string][]ToolIndex),
		details:    make(map[string]*ToolDetail),
		tagIndex:   make(map[string][]string),
		cache:      newLRUCache(50),
	}
	p.registerBuiltins()
	return p
}

// ListCategories returns category-level summaries.
func (p *Protocol) ListCategories() []ToolCategory {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]ToolCategory, len(p.categories))
	copy(out, p.categories)
	return out
}

// ListToolsByCategory returns tool-level indexes for a given category.
func (p *Protocol) ListToolsByCategory(category string) []ToolIndex {
	p.mu.RLock()
	defer p.mu.RUnlock()
	tools, ok := p.tools[category]
	if !ok {
		return nil
	}
	out := make([]ToolIndex, len(tools))
	copy(out, tools)
	return out
}

// GetToolDetail returns the full parameter-level definition for a tool.
// It checks the LRU cache first, then falls back to the details map.
func (p *Protocol) GetToolDetail(toolName string) (*ToolDetail, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check LRU cache first.
	if detail, ok := p.cache.get(toolName); ok {
		return detail, true
	}

	// Fall back to details map.
	detail, ok := p.details[toolName]
	if !ok {
		return nil, false
	}

	// Populate cache for future access.
	p.cache.put(toolName, detail)
	return detail, true
}

// MatchByTags returns tool indexes that match any of the given tags (case-insensitive).
func (p *Protocol) MatchByTags(tags []string) []ToolIndex {
	p.mu.RLock()
	defer p.mu.RUnlock()

	seen := make(map[string]bool)
	var results []ToolIndex

	for _, tag := range tags {
		normalizedTag := strings.ToLower(tag)
		toolNames, ok := p.tagIndex[normalizedTag]
		if !ok {
			continue
		}
		for _, name := range toolNames {
			if seen[name] {
				continue
			}
			seen[name] = true
			// Find the tool index.
			for _, catTools := range p.tools {
				for _, t := range catTools {
					if t.Name == name {
						results = append(results, t)
					}
				}
			}
		}
	}
	return results
}

// UpdateIndex adds or updates a tool in the index.
// It updates the category, tool list, tag index, and detail (if provided via details map).
func (p *Protocol) UpdateIndex(tool ToolIndex) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Remove old entry if it exists (handles category changes).
	p.removeToolLocked(tool.Name)

	// Add to category tools.
	p.tools[tool.Category] = append(p.tools[tool.Category], tool)

	// Update tag index.
	for _, tag := range tool.Tags {
		normalizedTag := strings.ToLower(tag)
		p.tagIndex[normalizedTag] = append(p.tagIndex[normalizedTag], tool.Name)
	}

	// Update category counts.
	p.refreshCategoriesLocked()
}

// UpdateDetail adds or updates a tool's full detail definition.
func (p *Protocol) UpdateDetail(detail *ToolDetail) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.details[detail.Name] = detail
	// Also update cache if it was cached.
	p.cache.put(detail.Name, detail)
}

// RemoveIndex removes a tool from all indexes.
func (p *Protocol) RemoveIndex(toolName string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.removeToolLocked(toolName)
	delete(p.details, toolName)
	p.cache.remove(toolName)
	p.refreshCategoriesLocked()
}

// removeToolLocked removes a tool from tools map and tag index. Caller must hold mu.
func (p *Protocol) removeToolLocked(toolName string) {
	for cat, catTools := range p.tools {
		for i, t := range catTools {
			if t.Name == toolName {
				// Remove tags for this tool.
				for _, tag := range t.Tags {
					normalizedTag := strings.ToLower(tag)
					p.removeFromSlice(normalizedTag, toolName)
				}
				// Remove from category tools.
				p.tools[cat] = append(catTools[:i], catTools[i+1:]...)
				return
			}
		}
	}
}

// removeFromSlice removes a value from the tag index slice.
func (p *Protocol) removeFromSlice(tag, toolName string) {
	names := p.tagIndex[tag]
	for i, n := range names {
		if n == toolName {
			p.tagIndex[tag] = append(names[:i], names[i+1:]...)
			if len(p.tagIndex[tag]) == 0 {
				delete(p.tagIndex, tag)
			}
			return
		}
	}
}

// refreshCategoriesLocked rebuilds the categories slice from the tools map. Caller must hold mu.
func (p *Protocol) refreshCategoriesLocked() {
	catMap := make(map[string]*ToolCategory)
	for _, cat := range p.categories {
		c := cat
		c.ToolCount = 0
		catMap[c.Name] = &c
	}

	for catName, catTools := range p.tools {
		if _, ok := catMap[catName]; !ok {
			catMap[catName] = &ToolCategory{
				Name: catName,
			}
		}
		catMap[catName].ToolCount = len(catTools)
	}

	// Remove empty categories that aren't built-in.
	cats := make([]ToolCategory, 0, len(catMap))
	for _, c := range catMap {
		if c.ToolCount > 0 || isBuiltinCategory(c.Name) {
			cats = append(cats, *c)
		}
	}
	p.categories = cats
}

func isBuiltinCategory(name string) bool {
	switch name {
	case "会话管理", "设备管理", "系统":
		return true
	}
	return false
}

// registerBuiltins registers built-in tool categories and tools.
func (p *Protocol) registerBuiltins() {
	// Define built-in categories.
	sessionMgmt := ToolCategory{
		Name:        "会话管理",
		Description: "远程会话的创建、切换、控制和终止",
		Tags:        []string{"session", "会话", "remote"},
	}
	deviceMgmt := ToolCategory{
		Name:        "设备管理",
		Description: "远程设备的查看和截屏",
		Tags:        []string{"device", "设备", "machine"},
	}
	systemCat := ToolCategory{
		Name:        "系统",
		Description: "帮助、记忆管理和技能沉淀",
		Tags:        []string{"system", "系统", "help"},
	}

	p.categories = []ToolCategory{sessionMgmt, deviceMgmt, systemCat}

	// Register session management tools.
	sessionTools := []struct {
		name string
		desc string
		tags []string
	}{
		{"launch_session", "启动新的远程工具会话", []string{"session", "launch", "启动", "会话"}},
		{"use_session", "切换到指定会话上下文", []string{"session", "use", "切换", "会话"}},
		{"exit_session", "退出当前会话上下文", []string{"session", "exit", "退出", "会话"}},
		{"send_input", "向当前会话发送输入", []string{"session", "send", "发送", "输入"}},
		{"interrupt_session", "中断当前会话", []string{"session", "interrupt", "中断", "会话"}},
		{"kill_session", "终止指定会话", []string{"session", "kill", "终止", "会话"}},
		{"session_detail", "查看会话详情", []string{"session", "detail", "详情", "会话"}},
	}
	for _, st := range sessionTools {
		p.addBuiltinTool(st.name, "会话管理", st.desc, st.tags)
	}

	// Register device management tools.
	deviceTools := []struct {
		name string
		desc string
		tags []string
	}{
		{"list_machines", "查看所有在线设备", []string{"device", "list", "设备", "查看"}},
		{"screenshot", "对设备进行截屏", []string{"device", "screenshot", "截屏", "截图"}},
	}
	for _, dt := range deviceTools {
		p.addBuiltinTool(dt.name, "设备管理", dt.desc, dt.tags)
	}

	// Register system tools.
	systemTools := []struct {
		name string
		desc string
		tags []string
	}{
		{"help", "显示帮助信息", []string{"system", "help", "帮助"}},
		{"list_sessions", "查看所有会话列表", []string{"system", "session", "list", "会话", "列表"}},
		{"view_memory", "查看用户记忆数据", []string{"system", "memory", "记忆", "查看"}},
		{"clear_memory", "清除用户记忆数据", []string{"system", "memory", "记忆", "清除"}},
		{"crystallize_skill", "将操作模式沉淀为技能", []string{"system", "skill", "技能", "沉淀"}},
	}
	for _, st := range systemTools {
		p.addBuiltinTool(st.name, "系统", st.desc, st.tags)
	}

	// Refresh category counts.
	p.refreshCategoriesLocked()
}

// addBuiltinTool adds a single built-in tool to the index.
func (p *Protocol) addBuiltinTool(name, category, description string, tags []string) {
	tool := ToolIndex{
		Name:        name,
		Category:    category,
		Description: description,
		Tags:        tags,
		Source:      "builtin",
		Available:   true,
	}
	p.tools[category] = append(p.tools[category], tool)

	// Build tag index.
	for _, tag := range tags {
		normalizedTag := strings.ToLower(tag)
		p.tagIndex[normalizedTag] = append(p.tagIndex[normalizedTag], name)
	}

	// Register a basic detail entry.
	p.details[name] = &ToolDetail{
		ToolIndex:  tool,
		Parameters: nil,
		Examples:   nil,
	}
}
