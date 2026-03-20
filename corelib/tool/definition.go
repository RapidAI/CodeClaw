package tool

import (
	"encoding/json"
	"fmt"
	"log"
)

// MCPToolView is a tool exposed by an MCP Server.
type MCPToolView struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// MCPServerView is the runtime view of an MCP Server.
type MCPServerView struct {
	ID           string `json:"id"`
	HealthStatus string `json:"health_status"`
}

// MCPToolSet groups tools from a single MCP server.
type MCPToolSet struct {
	ServerID   string
	ServerName string
	Tools      []MCPToolView
}

// MCPServerProvider abstracts access to remote MCP servers (decouples from MCPRegistry).
type MCPServerProvider interface {
	ListServers() []MCPServerView
	GetServerTools(serverID string) []MCPToolView
}

// LocalMCPToolProvider abstracts access to local (stdio) MCP servers (decouples from LocalMCPManager).
type LocalMCPToolProvider interface {
	GetAllTools() []MCPToolSet
}

// DefinitionGenerator dynamically generates the Agent's tool definition
// list by merging builtin tool definitions with tools from healthy MCP Servers
// and running local (stdio) MCP Servers.
type DefinitionGenerator struct {
	mcpProvider      MCPServerProvider
	localMCPProvider LocalMCPToolProvider
	builtinDefs      []map[string]interface{}
}

// NewDefinitionGenerator creates a new generator.
// builtinDefs are the static tool definitions (e.g. from buildToolDefinitions).
func NewDefinitionGenerator(mcpProvider MCPServerProvider, builtinDefs []map[string]interface{}) *DefinitionGenerator {
	return &DefinitionGenerator{
		mcpProvider: mcpProvider,
		builtinDefs: builtinDefs,
	}
}

// SetLocalMCPProvider sets the local MCP provider for stdio-based tool discovery.
func (g *DefinitionGenerator) SetLocalMCPProvider(provider LocalMCPToolProvider) {
	g.localMCPProvider = provider
}

// Generate produces the complete tool definition list: builtin + dynamic MCP tools.
// Dynamic tool names that conflict with builtin names get a server_id prefix.
// Only tools from healthy remote MCP Servers and running local MCP Servers are included.
func (g *DefinitionGenerator) Generate() []map[string]interface{} {
	result := make([]map[string]interface{}, len(g.builtinDefs))
	copy(result, g.builtinDefs)

	builtinNames := make(map[string]bool, len(g.builtinDefs))
	for _, def := range g.builtinDefs {
		if name := ExtractToolName(def); name != "" {
			builtinNames[name] = true
		}
	}

	dynamicNames := make(map[string]string)
	type pendingTool struct {
		serverID string
		tool     MCPToolView
	}
	var pending []pendingTool

	if g.mcpProvider != nil {
		servers := g.mcpProvider.ListServers()
		for _, srv := range servers {
			if srv.HealthStatus != "healthy" {
				continue
			}
			tools := g.mcpProvider.GetServerTools(srv.ID)
			for _, t := range tools {
				pending = append(pending, pendingTool{serverID: srv.ID, tool: t})
				if _, exists := dynamicNames[t.Name]; !exists {
					dynamicNames[t.Name] = srv.ID
				} else {
					dynamicNames[t.Name] = ""
				}
			}
		}
	}

	if g.localMCPProvider != nil {
		for _, ts := range g.localMCPProvider.GetAllTools() {
			for _, t := range ts.Tools {
				pending = append(pending, pendingTool{serverID: ts.ServerID, tool: t})
				if _, exists := dynamicNames[t.Name]; !exists {
					dynamicNames[t.Name] = ts.ServerID
				} else {
					dynamicNames[t.Name] = ""
				}
			}
		}
	}

	for _, p := range pending {
		name := p.tool.Name
		needsPrefix := builtinNames[name]
		if !needsPrefix {
			if ownerID := dynamicNames[name]; ownerID == "" {
				needsPrefix = true
			}
		}
		finalName := name
		if needsPrefix {
			finalName = fmt.Sprintf("%s_%s", p.serverID, name)
		}
		def := MCPToolToDefinition(finalName, p.tool)
		result = append(result, def)
	}

	return result
}

// ExtractToolName extracts the tool name from an OpenAI function calling definition.
func ExtractToolName(def map[string]interface{}) string {
	fn, ok := def["function"]
	if !ok {
		return ""
	}
	fnMap, ok := fn.(map[string]interface{})
	if !ok {
		return ""
	}
	name, _ := fnMap["name"].(string)
	return name
}

// MCPToolToDefinition converts an MCPToolView into an OpenAI function calling
// tool definition (map format matching toolDef output).
func MCPToolToDefinition(name string, tool MCPToolView) map[string]interface{} {
	params := BuildParametersFromSchema(tool.InputSchema)
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": tool.Description,
			"parameters":  params,
		},
	}
}

// BuildParametersFromSchema converts an MCP tool's InputSchema into the
// OpenAI function calling parameters format.
func BuildParametersFromSchema(schema map[string]interface{}) map[string]interface{} {
	if schema == nil || len(schema) == 0 {
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}

	if t, ok := schema["type"]; ok {
		if ts, ok := t.(string); ok && ts == "object" {
			result := make(map[string]interface{}, len(schema))
			for k, v := range schema {
				result[k] = v
			}
			if _, hasProp := result["properties"]; !hasProp {
				result["properties"] = map[string]interface{}{}
			}
			return result
		}
	}

	if LooksLikePropertiesMap(schema) {
		return map[string]interface{}{
			"type":       "object",
			"properties": schema,
		}
	}

	data, err := json.Marshal(schema)
	if err != nil {
		log.Printf("[ToolDefGen] failed to marshal schema: %v", err)
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}
	return result
}

// LooksLikePropertiesMap heuristically checks if a map looks like a JSON Schema
// properties map (each value is a map with a "type" key).
func LooksLikePropertiesMap(m map[string]interface{}) bool {
	if len(m) == 0 {
		return false
	}
	for _, v := range m {
		vm, ok := v.(map[string]interface{})
		if !ok {
			return false
		}
		if _, hasType := vm["type"]; !hasType {
			return false
		}
	}
	return true
}
