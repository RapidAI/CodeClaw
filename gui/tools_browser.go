package main

import (
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// registerBrowserTools registers browser automation tools (CDP-based) into the gui ToolRegistry.
// The tool definitions live in corelib/browser/tools.go (single source of truth).
// This function bridges them into the gui-local ToolRegistry.
func registerBrowserTools(registry *ToolRegistry) {
	// Register into a temporary corelib registry.
	coreReg := tool.NewRegistry()
	browser.RegisterTools(coreReg)

	// Bridge each corelib tool into the gui registry.
	for _, ct := range coreReg.ListAvailable() {
		if ct.Source != "builtin:browser" {
			continue
		}
		gt := RegisteredTool{
			Name:        ct.Name,
			Description: ct.Description,
			Category:    ToolCategory(ct.Category),
			Tags:        ct.Tags,
			Priority:    ct.Priority,
			Status:      RegToolStatus(ct.Status),
			InputSchema: ct.InputSchema,
			Required:    ct.Required,
			Source:      ct.Source,
		}
		// Bridge the handler: corelib Handler -> gui ToolHandler (same signature).
		if ct.Handler != nil {
			h := ct.Handler // capture
			gt.Handler = func(args map[string]interface{}) string {
				return h(args)
			}
		}
		registry.Register(gt)
	}
}
