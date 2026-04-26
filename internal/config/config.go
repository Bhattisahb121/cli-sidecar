package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ToolConfig holds settings for a single CLI tool.
type ToolConfig struct {
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
	Enabled bool              `json:"enabled"`
}

// Config is the top-level configuration.
type Config struct {
	Host  string       `json:"host"`
	Port  int          `json:"port"`
	Tools []ToolConfig `json:"tools"`
}

func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "cli-sidecar.json"
	}
	return filepath.Join(home, ".config", "cli-sidecar", "config.json")
}

func DefaultConfig() *Config {
	return &Config{
		Host: "127.0.0.1",
		Port: 8830,
		Tools: []ToolConfig{
			{
				Name:    "claude",
				Command: "claude",
				Args:    []string{"-p"},
				Enabled: true,
			},
			{
				Name:    "codex",
				Command: "codex",
				Args:    []string{"exec"},
				Enabled: true,
			},
			{
				Name:    "gemini",
				Command: "gemini",
				Args:    []string{"-p"},
				Enabled: true,
			},
		},
	}
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DefaultConfig(), nil
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	if cfg.Port == 0 {
		cfg.Port = 8830
	}
	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}

	return &cfg, nil
}

func Save(path string, cfg *Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
