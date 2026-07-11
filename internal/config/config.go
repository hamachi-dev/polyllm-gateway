package config

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

type ProviderConfig struct {
	Endpoint string `yaml:"endpoint"`
	APIKey   string `yaml:"api_key"`
}

type RouteConfig struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	API      string `yaml:"api"`
}

type ListenConfig struct {
	OpenAI     string `yaml:"openai"`
	Anthropic  string `yaml:"anthropic"`
}

type Config struct {
	Listen    ListenConfig              `yaml:"listen"`
	Providers map[string]ProviderConfig `yaml:"providers"`
	Routes    map[string]RouteConfig    `yaml:"routes"`
}

var envPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

func expandEnv(s string) string {
	return envPattern.ReplaceAllStringFunc(s, func(match string) string {
		key := match[2 : len(match)-1]
		if val, ok := os.LookupEnv(key); ok {
			return val
		}
		return match
	})
}

func expandEnvRecursive(v interface{}) {
	switch val := v.(type) {
	case string:
		return
	case map[string]interface{}:
		for k, vv := range val {
			switch w := vv.(type) {
			case string:
				val[k] = expandEnv(w)
			case map[string]interface{}:
				expandEnvRecursive(w)
			}
		}
	case []interface{}:
		for _, vv := range val {
			expandEnvRecursive(vv)
		}
	}
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	expandEnvRecursive(raw)

	expanded, err := yaml.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal expanded config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(expanded, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	return &cfg, nil
}
