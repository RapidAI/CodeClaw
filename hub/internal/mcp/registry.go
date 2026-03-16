package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/discovery"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/google/uuid"
)

const (
	// storeKey is the SystemSettingsRepository key for persisting MCP servers.
	storeKey = "mcp_servers"
	// callTimeout is the HTTP timeout for MCP tool calls.
	callTimeout = 30 * time.Second
	// healthFailThreshold is the number of consecutive health check failures
	// before a server is marked unavailable.
	healthFailThreshold = 3
)

// MCPServer represents a registered MCP Server.
type MCPServer struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	EndpointURL  string    `json:"endpoint_url"`
	AuthType     string    `json:"auth_type"`     // "none", "api_key", "bearer"
	AuthSecret   string    `json:"auth_secret"`
	Tools        []MCPTool `json:"tools"`
	HealthStatus string    `json:"health_status"` // "healthy", "slow", "unavailable"
	FailCount    int       `json:"fail_count"`
	LastCheckAt  time.Time `json:"last_check_at"`
	CreatedAt    time.Time `json:"created_at"`
}

// MCPTool represents a tool provided by an MCP Server.
type MCPTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// mcpCallRequest is the JSON body sent to an MCP Server for tool invocation.
type mcpCallRequest struct {
	Tool  string                 `json:"tool"`
	Input map[string]interface{} `json:"input"`
}

// mcpCallResponse is the JSON body returned by an MCP Server.
type mcpCallResponse struct {
	Result interface{} `json:"result"`
	Error  string      `json:"error,omitempty"`
}

// Registry manages MCP Server registration, discovery, and tool invocation.
type Registry struct {
	store     store.SystemSettingsRepository
	servers   map[string]*MCPServer
	mu        sync.RWMutex
	client    *http.Client
	discovery *discovery.Protocol
}

// NewRegistry creates a new Registry and loads persisted servers from the DB.
func NewRegistry(system store.SystemSettingsRepository, disc *discovery.Protocol) (*Registry, error) {
	r := &Registry{
		store:     system,
		servers:   make(map[string]*MCPServer),
		client:    &http.Client{Timeout: callTimeout},
		discovery: disc,
	}
	if err := r.load(context.Background()); err != nil {
		return nil, fmt.Errorf("mcp: load servers: %w", err)
	}
	// Re-register tools in discovery for all loaded servers.
	for _, srv := range r.servers {
		r.registerToolsInDiscovery(srv)
	}
	return r, nil
}

// load reads persisted MCP servers from the SystemSettingsRepository.
func (r *Registry) load(ctx context.Context) error {
	raw, err := r.store.Get(ctx, storeKey)
	if err != nil || raw == "" {
		return nil
	}
	var servers []MCPServer
	if err := json.Unmarshal([]byte(raw), &servers); err != nil {
		return fmt.Errorf("mcp: unmarshal servers: %w", err)
	}
	for i := range servers {
		srv := servers[i]
		r.servers[srv.ID] = &srv
	}
	return nil
}

// persist saves all servers to the SystemSettingsRepository. Caller must hold r.mu.
func (r *Registry) persist(ctx context.Context) error {
	servers := make([]MCPServer, 0, len(r.servers))
	for _, srv := range r.servers {
		servers = append(servers, *srv)
	}
	data, err := json.Marshal(servers)
	if err != nil {
		return fmt.Errorf("mcp: marshal servers: %w", err)
	}
	return r.store.Set(ctx, storeKey, string(data))
}

// toolIndexName returns the discovery index name for an MCP tool,
// prefixed with the server name to avoid collisions.
func toolIndexName(serverName, toolName string) string {
	return fmt.Sprintf("mcp:%s:%s", serverName, toolName)
}

// registerToolsInDiscovery registers all tools of a server in the discovery Protocol.
func (r *Registry) registerToolsInDiscovery(srv *MCPServer) {
	for _, tool := range srv.Tools {
		idx := discovery.ToolIndex{
			Name:        toolIndexName(srv.Name, tool.Name),
			Category:    "MCP 工具",
			Description: tool.Description,
			Tags:        []string{"mcp", srv.Name, tool.Name},
			Source:      "mcp",
			Available:   srv.HealthStatus != "unavailable",
		}
		r.discovery.UpdateIndex(idx)
	}
}

// removeToolsFromDiscovery removes all tools of a server from the discovery Protocol.
func (r *Registry) removeToolsFromDiscovery(srv *MCPServer) {
	for _, tool := range srv.Tools {
		r.discovery.RemoveIndex(toolIndexName(srv.Name, tool.Name))
	}
}

// updateToolsAvailability updates the Available flag for all tools of a server
// in the discovery Protocol.
func (r *Registry) updateToolsAvailability(srv *MCPServer, available bool) {
	for _, tool := range srv.Tools {
		idx := discovery.ToolIndex{
			Name:        toolIndexName(srv.Name, tool.Name),
			Category:    "MCP 工具",
			Description: tool.Description,
			Tags:        []string{"mcp", srv.Name, tool.Name},
			Source:      "mcp",
			Available:   available,
		}
		r.discovery.UpdateIndex(idx)
	}
}

// Register adds a new MCP Server to the registry, persists it, and registers
// its tools in the discovery Protocol. If the server ID is empty, a UUID is generated.
func (r *Registry) Register(ctx context.Context, server MCPServer) error {
	if server.ID == "" {
		server.ID = uuid.New().String()
	}
	if server.HealthStatus == "" {
		server.HealthStatus = "healthy"
	}
	if server.CreatedAt.IsZero() {
		server.CreatedAt = time.Now()
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.servers[server.ID] = &server
	if err := r.persist(ctx); err != nil {
		delete(r.servers, server.ID)
		return err
	}
	r.registerToolsInDiscovery(&server)
	return nil
}

// Unregister removes an MCP Server from the registry, persists the change,
// and removes its tools from the discovery Protocol.
func (r *Registry) Unregister(ctx context.Context, serverID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	srv, ok := r.servers[serverID]
	if !ok {
		return fmt.Errorf("mcp: server %s not found", serverID)
	}

	r.removeToolsFromDiscovery(srv)
	delete(r.servers, serverID)
	return r.persist(ctx)
}

// CallTool invokes a tool on the specified MCP Server via HTTP POST.
// The request has a 30-second timeout.
func (r *Registry) CallTool(ctx context.Context, serverID, toolName string, input map[string]interface{}) (interface{}, error) {
	r.mu.RLock()
	srv, ok := r.servers[serverID]
	if !ok {
		r.mu.RUnlock()
		return nil, fmt.Errorf("mcp: server %s not found", serverID)
	}
	// Copy endpoint and auth info under read lock.
	endpoint := srv.EndpointURL
	authType := srv.AuthType
	authSecret := srv.AuthSecret
	r.mu.RUnlock()

	reqBody := mcpCallRequest{
		Tool:  toolName,
		Input: input,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("mcp: marshal call request: %w", err)
	}

	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("mcp: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	applyAuth(req, authType, authSecret)

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mcp: call tool %s on %s: %w", toolName, serverID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mcp: server %s returned status %d", serverID, resp.StatusCode)
	}

	var callResp mcpCallResponse
	if err := json.NewDecoder(resp.Body).Decode(&callResp); err != nil {
		return nil, fmt.Errorf("mcp: decode response from %s: %w", serverID, err)
	}
	if callResp.Error != "" {
		return nil, fmt.Errorf("mcp: tool error: %s", callResp.Error)
	}
	return callResp.Result, nil
}

// applyAuth sets authentication headers on the HTTP request.
func applyAuth(req *http.Request, authType, authSecret string) {
	switch authType {
	case "api_key":
		req.Header.Set("X-API-Key", authSecret)
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+authSecret)
	}
}

// ListServers returns a copy of all registered MCP Servers.
func (r *Registry) ListServers(ctx context.Context) []MCPServer {
	r.mu.RLock()
	defer r.mu.RUnlock()

	servers := make([]MCPServer, 0, len(r.servers))
	for _, srv := range r.servers {
		servers = append(servers, *srv)
	}
	return servers
}

// HealthCheck performs a health check on the specified MCP Server by sending
// an HTTP GET to its endpoint. On failure, FailCount is incremented; if it
// reaches healthFailThreshold the server is marked "unavailable" and the
// discovery Protocol is updated. On success, FailCount is reset and the
// server is marked "healthy".
func (r *Registry) HealthCheck(ctx context.Context, serverID string) error {
	r.mu.RLock()
	srv, ok := r.servers[serverID]
	if !ok {
		r.mu.RUnlock()
		return fmt.Errorf("mcp: server %s not found", serverID)
	}
	endpoint := srv.EndpointURL
	authType := srv.AuthType
	authSecret := srv.AuthSecret
	r.mu.RUnlock()

	checkCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return r.recordHealthFailure(ctx, serverID, err)
	}
	applyAuth(req, authType, authSecret)

	resp, err := r.client.Do(req)
	if err != nil {
		return r.recordHealthFailure(ctx, serverID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return r.recordHealthFailure(ctx, serverID, fmt.Errorf("status %d", resp.StatusCode))
	}

	// Success — reset fail count.
	r.mu.Lock()
	defer r.mu.Unlock()

	srv, ok = r.servers[serverID]
	if !ok {
		return nil
	}

	previousStatus := srv.HealthStatus
	srv.FailCount = 0
	srv.HealthStatus = "healthy"
	srv.LastCheckAt = time.Now()

	if previousStatus != "healthy" {
		r.updateToolsAvailability(srv, true)
	}

	return r.persist(ctx)
}

// recordHealthFailure increments the fail count for a server and marks it
// unavailable if the threshold is reached.
func (r *Registry) recordHealthFailure(ctx context.Context, serverID string, checkErr error) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	srv, ok := r.servers[serverID]
	if !ok {
		return fmt.Errorf("mcp: server %s not found", serverID)
	}

	srv.FailCount++
	srv.LastCheckAt = time.Now()

	if srv.FailCount >= healthFailThreshold && srv.HealthStatus != "unavailable" {
		srv.HealthStatus = "unavailable"
		r.updateToolsAvailability(srv, false)
	}

	_ = r.persist(ctx)
	return fmt.Errorf("mcp: health check failed for %s: %w", serverID, checkErr)
}
