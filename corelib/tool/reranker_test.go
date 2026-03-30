package tool

import (
	"strings"
	"testing"
)

func TestBuildMCPToolBody_EmptySchema(t *testing.T) {
	if got := BuildMCPToolBody(nil); got != "" {
		t.Errorf("nil schema: expected empty, got %q", got)
	}
	if got := BuildMCPToolBody(map[string]interface{}{}); got != "" {
		t.Errorf("empty map: expected empty, got %q", got)
	}
	// No "properties" key.
	if got := BuildMCPToolBody(map[string]interface{}{"type": "object"}); got != "" {
		t.Errorf("no properties: expected empty, got %q", got)
	}
	// Empty properties.
	schema := map[string]interface{}{
		"properties": map[string]interface{}{},
	}
	if got := BuildMCPToolBody(schema); got != "" {
		t.Errorf("empty properties: expected empty, got %q", got)
	}
}

func TestBuildMCPToolBody_MultipleParams(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query":   map[string]interface{}{"type": "string", "description": "SQL query to run"},
			"timeout": map[string]interface{}{"type": "integer", "description": "Timeout in seconds"},
		},
	}
	got := BuildMCPToolBody(schema)
	if !strings.HasPrefix(got, "Parameters:") {
		t.Errorf("should start with 'Parameters:', got %q", got)
	}
	if !strings.Contains(got, "query (string): SQL query to run") {
		t.Errorf("should contain query param, got %q", got)
	}
	if !strings.Contains(got, "timeout (integer): Timeout in seconds") {
		t.Errorf("should contain timeout param, got %q", got)
	}
}

func TestBuildMCPToolBody_NestedSchema(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"config": map[string]interface{}{
				"type":        "object",
				"description": "Configuration object",
			},
		},
	}
	got := BuildMCPToolBody(schema)
	if !strings.Contains(got, "config (object): Configuration object") {
		t.Errorf("should contain nested object param, got %q", got)
	}
}

func TestBuildMCPToolBody_NoTypeNoDesc(t *testing.T) {
	schema := map[string]interface{}{
		"properties": map[string]interface{}{
			"data": map[string]interface{}{},
		},
	}
	got := BuildMCPToolBody(schema)
	if !strings.Contains(got, "data (any)") {
		t.Errorf("missing type should default to 'any', got %q", got)
	}
}
