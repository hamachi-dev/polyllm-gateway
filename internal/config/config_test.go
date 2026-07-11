package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	yaml := `
listen:
  openai: ":8000"
  anthropic: ":8001"

providers:
  opencode:
    endpoint: https://api.example.com/v1
    api_key: test-key-123
    models:
      qwen3.7-plus:
        api: anthropic
      deepseek-v4-flash:
        api: openai

routes:
  gpt-5:
    provider: opencode
    model: qwen3.7-plus

  gpt-5-mini:
    provider: opencode
    model: deepseek-v4-flash
`

	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Listen.OpenAI != ":8000" {
		t.Errorf("expected :8000, got %s", cfg.Listen.OpenAI)
	}
	if cfg.Listen.Anthropic != ":8001" {
		t.Errorf("expected :8001, got %s", cfg.Listen.Anthropic)
	}

	oc, ok := cfg.Providers["opencode"]
	if !ok {
		t.Fatal("expected opencode provider")
	}
	if oc.Endpoint != "https://api.example.com/v1" {
		t.Errorf("expected https://api.example.com/v1, got %s", oc.Endpoint)
	}
	if oc.APIKey != "test-key-123" {
		t.Errorf("expected test-key-123, got %s", oc.APIKey)
	}
	if oc.Models == nil {
		t.Fatal("expected models")
	}
	if oc.Models["qwen3.7-plus"].API != "anthropic" {
		t.Errorf("expected anthropic, got %s", oc.Models["qwen3.7-plus"].API)
	}

	route, ok := cfg.Routes["gpt-5"]
	if !ok {
		t.Fatal("expected gpt-5 route")
	}
	if route.Provider != "opencode" {
		t.Errorf("expected opencode, got %s", route.Provider)
	}
	if route.Model != "qwen3.7-plus" {
		t.Errorf("expected qwen3.7-plus, got %s", route.Model)
	}
	if route.API != "anthropic" {
		t.Errorf("expected anthropic (resolved), got %s", route.API)
	}
}

func TestLoadEnvExpansion(t *testing.T) {
	os.Setenv("TEST_API_KEY", "expanded-key-456")
	defer os.Unsetenv("TEST_API_KEY")

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	yaml := `
providers:
  opencode:
    api_key: ${TEST_API_KEY}
`

	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	oc, ok := cfg.Providers["opencode"]
	if !ok {
		t.Fatal("expected opencode provider")
	}
	if oc.APIKey != "expanded-key-456" {
		t.Errorf("expected expanded-key-456, got %s", oc.APIKey)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadEmptyRoutes(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "empty.yaml")

	yaml := `listen:
  openai: ":8000"
  anthropic: ":8001"
providers: {}
routes: {}
`

	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(cfg.Routes) != 0 {
		t.Error("expected empty routes")
	}
}

func TestLoadRouteAPIDefault(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	yaml := `
providers:
  opencode:
    endpoint: https://example.com/v1
    api_key: test

routes:
  gpt-5:
    provider: opencode
    model: deepseek-v4-flash
`

	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	route, ok := cfg.Routes["gpt-5"]
	if !ok {
		t.Fatal("expected gpt-5 route")
	}
	if route.API != "openai" {
		t.Errorf("expected openai (default), got %s", route.API)
	}
}

func TestLoadRouteAPIOverride(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	yaml := `
providers:
  opencode:
    endpoint: https://example.com/v1
    api_key: test
    models:
      deepseek-v4-flash:
        api: anthropic

routes:
  my-model:
    provider: opencode
    model: deepseek-v4-flash
    api: openai
`

	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	route, ok := cfg.Routes["my-model"]
	if !ok {
		t.Fatal("expected route")
	}
	if route.API != "openai" {
		t.Errorf("expected openai (override), got %s", route.API)
	}
}
