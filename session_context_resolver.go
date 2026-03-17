package main

import (
	"os"
	"path/filepath"
	"strings"
)

// SessionContextResolver 自动推断会话启动参数
type SessionContextResolver struct {
	app *App
}

// NewSessionContextResolver creates a new SessionContextResolver.
func NewSessionContextResolver(app *App) *SessionContextResolver {
	return &SessionContextResolver{app: app}
}

// ResolveProject 按优先级推断项目路径:
// (a) 当前桌面端打开的项目
// (b) 最近使用的项目
// (c) 默认项目
// 无法推断时返回空字符串，由调用方展示项目列表。
func (r *SessionContextResolver) ResolveProject() (projectPath string, reason string) {
	// Priority (a): currently open project in the desktop app
	current := r.app.GetCurrentProjectPath()
	if current != "" {
		return current, "当前打开的项目"
	}

	// Priority (b) & (c): load config for project list
	cfg, err := r.app.LoadConfig()
	if err != nil {
		return "", ""
	}

	// Priority (b): most recently used project (first in the list)
	if len(cfg.Projects) > 0 {
		return cfg.Projects[0].Path, "最近使用的项目"
	}

	// Priority (c): default project via CurrentProject ID
	if cfg.CurrentProject != "" {
		for _, p := range cfg.Projects {
			if p.Id == cfg.CurrentProject {
				return p.Path, "默认项目"
			}
		}
	}

	return "", ""
}

// ResolveTool 根据项目语言/框架推荐工具。
// 检查项目目录中的语言指示文件，结合工具目录的安装和健康状态进行推荐。
// 无法推荐时返回空字符串。
func (r *SessionContextResolver) ResolveTool(projectPath, taskDescription string) (toolName string, reason string) {
	type recommendation struct {
		tool   string
		reason string
	}

	var candidates []recommendation

	if projectPath != "" {
		switch {
		case fileExists(filepath.Join(projectPath, "go.mod")):
			candidates = []recommendation{
				{"opencode", "Go 项目推荐使用 OpenCode"},
				{"claude", "Go 项目推荐使用 Claude"},
			}
		case fileExists(filepath.Join(projectPath, "package.json")):
			candidates = []recommendation{
				{"claude", "Node.js 项目推荐使用 Claude"},
				{"cursor", "Node.js 项目推荐使用 Cursor"},
			}
		case fileExists(filepath.Join(projectPath, "requirements.txt")) || fileExists(filepath.Join(projectPath, "setup.py")):
			candidates = []recommendation{
				{"claude", "Python 项目推荐使用 Claude"},
			}
		}
	}

	// Default: claude as the most versatile tool
	if len(candidates) == 0 {
		candidates = []recommendation{
			{"claude", "默认推荐使用 Claude"},
		}
	}

	// Check if the recommended tool is installed and healthy
	toolManager := NewToolManager(r.app)
	for _, c := range candidates {
		status := toolManager.GetToolStatus(c.tool)
		if status.Installed && strings.TrimSpace(status.Path) != "" {
			return c.tool, c.reason
		}
	}

	// Fallback: try any installed tool from the catalog
	fallbackOrder := []string{"claude", "gemini", "codex", "opencode", "cursor", "codebuddy", "iflow", "kilo"}
	for _, name := range fallbackOrder {
		status := toolManager.GetToolStatus(name)
		if status.Installed && strings.TrimSpace(status.Path) != "" {
			return name, "已安装的可用工具"
		}
	}

	return "", ""
}

// fileExists returns true if the path exists and is not a directory.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
