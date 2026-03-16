package main

import (
	"time"
)

// MCPToolView mirrors hub/internal/mcp.MCPTool for Wails bindings.
type MCPToolView struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// MCPServerView mirrors hub/internal/mcp.MCPServer for Wails bindings.
type MCPServerView struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	EndpointURL  string        `json:"endpoint_url"`
	AuthType     string        `json:"auth_type"`     // "none", "api_key", "bearer"
	AuthSecret   string        `json:"auth_secret"`
	Tools        []MCPToolView `json:"tools"`
	HealthStatus string        `json:"health_status"` // "healthy", "slow", "unavailable"
	FailCount    int           `json:"fail_count"`
	LastCheckAt  time.Time     `json:"last_check_at"`
	CreatedAt    time.Time     `json:"created_at"`
}

// ListMCPServers returns all registered MCP Servers from the hub.
func (a *App) ListMCPServers() []MCPServerView {
	var servers []MCPServerView
	if err := a.nlSkillGet("/api/admin/mcp-servers", &servers); err != nil {
		a.log("ListMCPServers error: " + err.Error())
		return nil
	}
	return servers
}

// RegisterMCPServer registers a new MCP Server on the hub.
func (a *App) RegisterMCPServer(server MCPServerView) error {
	return a.nlSkillPost("/api/admin/mcp-servers", server)
}

// UpdateMCPServer updates an existing MCP Server on the hub.
func (a *App) UpdateMCPServer(server MCPServerView) error {
	return a.nlSkillPost("/api/admin/mcp-servers/update", server)
}

// UnregisterMCPServer removes an MCP Server by ID from the hub.
func (a *App) UnregisterMCPServer(serverID string) error {
	return a.nlSkillDelete("/api/admin/mcp-servers/" + serverID)
}

// GetMCPServerTools returns the tool list for a specific MCP Server.
func (a *App) GetMCPServerTools(serverID string) []MCPToolView {
	var tools []MCPToolView
	if err := a.nlSkillGet("/api/admin/mcp-servers/"+serverID+"/tools", &tools); err != nil {
		a.log("GetMCPServerTools error: " + err.Error())
		return nil
	}
	return tools
}

// CheckMCPServerHealth triggers a health check for the specified MCP Server.
func (a *App) CheckMCPServerHealth(serverID string) error {
	return a.nlSkillPost("/api/admin/mcp-servers/"+serverID+"/health", nil)
}
