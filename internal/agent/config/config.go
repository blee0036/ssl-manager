package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

// Platform-specific default paths for the Agent config and state files.
var (
	DefaultConfigPath string
	DefaultStatePath  string
	DefaultConfigDir  string
)

func init() {
	switch runtime.GOOS {
	case "darwin":
		DefaultConfigDir = "/Library/Application Support/ssl-manager-agent"
	default: // linux and others
		DefaultConfigDir = "/etc/ssl-manager-agent"
	}
	DefaultConfigPath = filepath.Join(DefaultConfigDir, "config.yaml")
	DefaultStatePath = filepath.Join(DefaultConfigDir, "state.json")
}

// AgentConfig represents the Agent's YAML configuration file.
type AgentConfig struct {
	ServerURL           string `yaml:"server_url"`
	MachineID           string `yaml:"machine_id"`
	AgentToken          string `yaml:"agent_token"`
	PollIntervalSeconds int    `yaml:"poll_interval_seconds"`
	LogLevel            string `yaml:"log_level"`
	AutoUpdate          *bool  `yaml:"auto_update,omitempty"`
}

// IsAutoUpdateEnabled returns whether auto-update is enabled.
// If AutoUpdate is nil (not set in config), it defaults to true (enabled).
func (c *AgentConfig) IsAutoUpdateEnabled() bool {
	if c.AutoUpdate == nil {
		return true
	}
	return *c.AutoUpdate
}

// DefaultAgentConfig returns an AgentConfig with sensible default values.
func DefaultAgentConfig() *AgentConfig {
	return &AgentConfig{
		PollIntervalSeconds: 60,
		LogLevel:            "info",
	}
}

// LoadConfig reads and parses a YAML config file from the given path.
func LoadConfig(path string) (*AgentConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read agent config file: %w", err)
	}

	cfg := DefaultAgentConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse agent config file: %w", err)
	}

	if err := ValidateConfig(cfg); err != nil {
		return nil, fmt.Errorf("agent config validation failed: %w", err)
	}

	return cfg, nil
}

// SaveConfig writes the AgentConfig to the given path in YAML format.
// It creates parent directories if they don't exist and sets file permissions to 0600.
func SaveConfig(path string, cfg *AgentConfig) error {
	if err := ValidateConfig(cfg); err != nil {
		return fmt.Errorf("agent config validation failed: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create agent config directory: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal agent config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write agent config file: %w", err)
	}

	return nil
}

// ValidateConfig checks that required fields are set and values are within valid ranges.
func ValidateConfig(cfg *AgentConfig) error {
	if cfg == nil {
		return errors.New("agent config is nil")
	}

	if cfg.ServerURL == "" {
		return errors.New("server_url is required")
	}

	if cfg.MachineID == "" {
		return errors.New("machine_id is required")
	}

	if cfg.AgentToken == "" {
		return errors.New("agent_token is required")
	}

	if cfg.PollIntervalSeconds <= 0 {
		return errors.New("poll_interval_seconds must be positive")
	}

	return nil
}
