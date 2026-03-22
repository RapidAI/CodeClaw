package main

import (
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
)

const (
	// maxToolBudget is the maximum number of tools to send to the LLM.
	// Core tools are always included; remaining budget goes to the highest-
	// scoring candidates ranked by BM25 similarity to the user message.
	maxToolBudget = 28

	// maxDynamicRouted caps how many MCP/non-code dynamic tools can be included.
	maxDynamicRouted = 18
)

// ---------------------------------------------------------------------------
// Core tool whitelist — these are always included regardless of the user
// message because they cover the fundamental interaction loop.
// ---------------------------------------------------------------------------

var coreToolNames = map[string]bool{
	// Session lifecycle — essential for the primary interaction loop
	"list_sessions": true, "create_session": true,
	"send_and_observe": true, "get_session_output": true, "get_session_events": true,
	"control_session": true,
	// Local operations — high frequency
	"bash": true, "read_file": true, "write_file": true, "list_directory": true,
	// MCP & Skill essentials
	"call_mcp_tool": true, "run_skill": true,
	// Screenshot & file delivery (high frequency)
	"screenshot": true, "send_file": true,
	// Long-term memory — essential for evolving intelligence
	"memory": true,
}

// builtinToolNames is the complete set of all builtin tool names (core + non-core).
// When a ToolRouter has a registry, it uses ToolRouter.isBuiltin() instead.
// This static set is kept as a fallback for tests that create a ToolRouter
// without a registry.
var builtinToolNames = map[string]bool{
	// Core (duplicated here for the isBuiltinToolName check)
	"list_sessions": true, "create_session": true, "list_providers": true,
	"send_input": true, "get_session_output": true, "get_session_events": true,
	"interrupt_session": true, "kill_session": true, "screenshot": true,
	"list_mcp_tools": true, "call_mcp_tool": true,
	"list_skills": true, "search_skill_hub": true, "install_skill_hub": true, "run_skill": true,
	"parallel_execute": true, "recommend_tool": true, "craft_tool": true,
	"bash": true, "read_file": true, "write_file": true, "list_directory": true,
	"send_file": true, "open": true,
	"memory": true,
	"create_template": true, "list_templates": true, "launch_template": true,
	"get_config": true, "update_config": true, "batch_update_config": true,
	"list_config_schema": true, "export_config": true, "import_config": true,
	"set_max_iterations": true,
	"create_scheduled_task": true, "list_scheduled_tasks": true,
	"delete_scheduled_task": true, "update_scheduled_task": true,
	"search_and_install_skill": true,
	"switch_llm_provider": true,
	// Merged tools (optimized)
	"send_and_observe": true, "control_session": true, "manage_config": true,
	"query_audit_log": true,
}

// isBuiltinToolName returns true if the tool name is a known builtin tool.
// This is the static fallback used when no registry is available.
func isBuiltinToolName(name string) bool {
	return builtinToolNames[name]
}

// ToolRouter selects the most relevant tools for a given user message.
//
// Strategy:
//  1. Core tools (whitelist) are always included — they cover the basic
//     interaction loop and cost ~13 tool slots.
//  2. All remaining tools (non-core builtins + MCP dynamic tools) compete
//     for the remaining budget via BM25 scoring against the user message.
//  3. MCP dynamic tools are additionally capped at maxDynamicRouted.
type ToolRouter struct {
	generator *ToolDefinitionGenerator
	hubClient *SkillHubClient
	registry  *ToolRegistry
}

// NewToolRouter creates a new ToolRouter.
func NewToolRouter(generator *ToolDefinitionGenerator) *ToolRouter {
	return &ToolRouter{generator: generator}
}

// SetRegistry sets the ToolRegistry used for dynamic builtin detection and
// tag-based scoring. When set, isBuiltinToolName lookups use the registry
// instead of the static builtinToolNames map.
func (r *ToolRouter) SetRegistry(reg *ToolRegistry) {
	r.registry = reg
}

// SetHubClient sets the SkillHubClient used for recommendation matching.
func (r *ToolRouter) SetHubClient(client *SkillHubClient) {
	r.hubClient = client
}

// isBuiltin checks whether a tool name is a builtin tool. If the router has
// a registry, it queries the registry (category == builtin or non_code);
// otherwise it falls back to the static builtinToolNames map.
func (r *ToolRouter) isBuiltin(name string) bool {
	if r.registry != nil {
		if t, ok := r.registry.Get(name); ok {
			return t.Category == ToolCategoryBuiltin || t.Category == ToolCategoryNonCode
		}
		return false
	}
	return isBuiltinToolName(name)
}

// tagsForTool returns the tags for a tool from the registry, or nil if
// the registry is not set or the tool is not found.
func (r *ToolRouter) tagsForTool(name string) []string {
	if r.registry == nil {
		return nil
	}
	if t, ok := r.registry.Get(name); ok {
		return t.Tags
	}
	return nil
}

// Route selects the most relevant tools for a given user message.
// When len(allTools) <= maxToolBudget, all tools are returned unchanged.
// Otherwise, core tools are kept unconditionally and the remaining budget
// is filled by ranking all other tools (builtin + dynamic) via BM25
// scoring against the user message.
func (r *ToolRouter) Route(userMessage string, allTools []map[string]interface{}) []map[string]interface{} {
	if len(allTools) <= maxToolBudget {
		return allTools
	}

	// Partition into core (always kept) and candidates (compete for budget).
	var core, candidates []map[string]interface{}
	for _, tool := range allTools {
		if coreToolNames[extractToolName(tool)] {
			core = append(core, tool)
		} else {
			candidates = append(candidates, tool)
		}
	}

	remaining := maxToolBudget - len(core)
	if remaining <= 0 || len(candidates) == 0 {
		return core
	}

	// Build a BM25 index over candidate tool descriptions.
	docs := make([]bm25.Doc, len(candidates))
	for i, t := range candidates {
		name := extractToolName(t)
		desc := extractToolDescription(t)
		text := name + " " + desc
		if tags := r.tagsForTool(name); len(tags) > 0 {
			text += " " + strings.Join(tags, " ")
		}
		docs[i] = bm25.Doc{ID: name, Text: text}
	}
	idx := bm25.New()
	idx.Rebuild(docs)
	scores := idx.Score(userMessage)

	type scored struct {
		index int
		score float64
	}
	scoredList := make([]scored, len(candidates))
	for i, t := range candidates {
		name := extractToolName(t)
		scoredList[i] = scored{index: i, score: scores[name]}
	}
	sort.SliceStable(scoredList, func(i, j int) bool {
		return scoredList[i].score > scoredList[j].score
	})

	// Fill remaining budget, respecting the MCP dynamic tool cap.
	dynamicCount := 0
	result := make([]map[string]interface{}, len(core), maxToolBudget+2)
	copy(result, core)
	for _, s := range scoredList {
		if len(result) >= maxToolBudget {
			break
		}
		name := extractToolName(candidates[s.index])
		if !r.isBuiltin(name) {
			dynamicCount++
			if dynamicCount > maxDynamicRouted {
				continue
			}
		}
		result = append(result, candidates[s.index])
	}

	// Check recommended Skills from Hub for keyword overlap with user message.
	if r.hubClient != nil {
		if hint := r.matchRecommendations(bm25.Tokenize(userMessage)); hint != nil {
			result = append(result, hint)
		}
	}

	return result
}

// matchRecommendations checks if any recommended Skill from the Hub matches
// the user message tokens via simple keyword overlap. Returns a tool hint
// map if a match is found, nil otherwise.
func (r *ToolRouter) matchRecommendations(msgTokens []string) map[string]interface{} {
	if len(msgTokens) == 0 {
		return nil
	}

	recommendations := r.hubClient.GetRecommendations()
	if len(recommendations) == 0 {
		return nil
	}

	msgSet := make(map[string]struct{}, len(msgTokens))
	for _, t := range msgTokens {
		msgSet[t] = struct{}{}
	}

	for _, rec := range recommendations {
		recTokens := bm25.Tokenize(rec.Name + " " + rec.Description)
		matchCount := 0
		for _, rt := range recTokens {
			if _, ok := msgSet[rt]; ok {
				matchCount++
				if len([]rune(rt)) > 1 {
					// A multi-char token match is strong enough on its own.
					return searchAndInstallSkillHint()
				}
			}
		}
		// Require at least 2 single-char matches to avoid false positives
		// from single CJK character overlap.
		if matchCount >= 2 {
			return searchAndInstallSkillHint()
		}
	}

	return nil
}

// searchAndInstallSkillHint returns a tool definition map for the
// search_and_install_skill hint that the LLM can invoke.
func searchAndInstallSkillHint() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "search_and_install_skill",
			"description": "Search SkillHub for a matching Skill and install it. Use this when the user's request might be handled by a Skill available on the Hub.",
			"parameters": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}

// extractToolDescription extracts the description from an OpenAI function
// calling tool definition.
func extractToolDescription(def map[string]interface{}) string {
	fn, ok := def["function"]
	if !ok {
		return ""
	}
	fnMap, ok := fn.(map[string]interface{})
	if !ok {
		return ""
	}
	desc, _ := fnMap["description"].(string)
	return desc
}
