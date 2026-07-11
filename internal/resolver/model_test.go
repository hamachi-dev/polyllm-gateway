package resolver

import (
	"testing"
)

func TestResolve(t *testing.T) {
	routes := map[string]Route{
		"gpt-5": {
			Provider: "opencode",
			Model:    "qwen3.7-plus",
			API:      "openai",
		},
		"claude-sonnet-4": {
			Provider: "opencode",
			Model:    "deepseek-v4-flash",
			API:      "anthropic",
		},
	}

	res := New(routes)

	tests := []struct {
		name      string
		model     string
		wantOK    bool
		wantProvider string
		wantModel string
		wantAPI   string
	}{
		{"known model gpt-5", "gpt-5", true, "opencode", "qwen3.7-plus", "openai"},
		{"known model claude", "claude-sonnet-4", true, "opencode", "deepseek-v4-flash", "anthropic"},
		{"unknown model", "nonexistent", false, "", "", ""},
		{"empty string", "", false, "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route, ok := res.Resolve(tt.model)
			if ok != tt.wantOK {
				t.Errorf("Resolve() ok = %v, want %v", ok, tt.wantOK)
			}
			if route.Provider != tt.wantProvider {
				t.Errorf("Resolve() Provider = %v, want %v", route.Provider, tt.wantProvider)
			}
			if route.Model != tt.wantModel {
				t.Errorf("Resolve() Model = %v, want %v", route.Model, tt.wantModel)
			}
			if route.API != tt.wantAPI {
				t.Errorf("Resolve() API = %v, want %v", route.API, tt.wantAPI)
			}
		})
	}
}

func TestKeys(t *testing.T) {
	routes := map[string]Route{
		"gpt-5": {Provider: "p1", Model: "m1", API: "openai"},
		"gpt-3": {Provider: "p2", Model: "m2", API: "openai"},
	}

	res := New(routes)
	keys := res.Keys()

	if len(keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(keys))
	}

	seen := make(map[string]bool)
	for _, k := range keys {
		seen[k] = true
	}
	if !seen["gpt-5"] || !seen["gpt-3"] {
		t.Error("Keys() missing expected entries")
	}
}

func TestAddRoute(t *testing.T) {
	res := New(map[string]Route{})

	err := res.AddRoute("new-model", Route{
		Provider: "test",
		Model:    "test-model",
		API:      "openai",
	})
	if err != nil {
		t.Fatalf("AddRoute failed: %v", err)
	}

	route, ok := res.Resolve("new-model")
	if !ok {
		t.Fatal("expected route after AddRoute")
	}
	if route.Model != "test-model" {
		t.Errorf("expected test-model, got %s", route.Model)
	}

	err = res.AddRoute("new-model", Route{})
	if err == nil {
		t.Error("expected error for duplicate route")
	}
}
