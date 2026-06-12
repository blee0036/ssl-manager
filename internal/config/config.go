package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
)

// DefaultConfigPath is the default path for the config file.
const DefaultConfigPath = "./data/config.json"

// Config represents the global system configuration stored in config.json.
type Config struct {
	Server        ServerConfig        `json:"server"`
	Agent         AgentConfig         `json:"agent"`
	Alert         AlertConfig         `json:"alert"`
	Certbot       CertbotConfig       `json:"certbot"`
	Readonly      ReadonlyConfig      `json:"readonly"`
	DomainMonitor DomainMonitorConfig `json:"domain_monitor"`
	Turnstile     TurnstileConfig     `json:"turnstile"`
	ThirdpartDNS  ThirdpartDNSConfig  `json:"thirdpart_dns"`
	Cleanup       CleanupConfig       `json:"cleanup"`
}

// ServerConfig holds settings for the Web Backend server.
type ServerConfig struct {
	ExternalURL string `json:"external_url"` // Web 外部访问地址
	ListenAddr  string `json:"listen_addr"`  // 监听地址，默认 :8080
}

// AgentConfig holds settings for Agent communication.
type AgentConfig struct {
	HeartbeatTimeoutSeconds int `json:"heartbeat_timeout_seconds"` // 心跳超时秒数，默认 120
	PollIntervalSeconds     int `json:"poll_interval_seconds"`     // Agent 轮询间隔，默认 60
}

// AlertConfig holds settings for alert notifications.
type AlertConfig struct {
	DefaultBeforeDays int `json:"default_before_days"` // 证书过期提前告警天数，默认 15
}

// CertbotConfig holds settings for Certbot integration.
type CertbotConfig struct {
	BinaryPath string `json:"binary_path"` // certbot 二进制路径，默认 "certbot"
	DataDir    string `json:"data_dir"`    // certbot 工作目录
	Email      string `json:"email"`       // certbot 注册邮箱
}

// ReadonlyConfig holds settings for the read-only access mode.
type ReadonlyConfig struct {
	Enabled      bool   `json:"enabled"`
	ViewPassword string `json:"view_password"`
}

// DomainMonitorConfig holds settings for domain SSL monitoring.
type DomainMonitorConfig struct {
	DefaultPort     int `json:"default_port"`      // 默认监控端口，默认 443
	IntervalMinutes int `json:"interval_minutes"`  // 监控间隔分钟数，默认 60
}

// TurnstileConfig holds settings for Cloudflare Turnstile human verification.
type TurnstileConfig struct {
	Enabled   bool   `json:"enabled"`    // 是否启用 Turnstile 验证，默认 false
	SiteKey   string `json:"site_key"`   // Turnstile site key（前端使用）
	SecretKey string `json:"secret_key"` // Turnstile secret key（仅后端使用，绝不下发前端）
}

// ThirdpartDNSConfig holds settings for third-party DNS sync scheduling.
type ThirdpartDNSConfig struct {
	SyncIntervalMinutes int `json:"sync_interval_minutes"` // 定时同步间隔分钟数，默认 360；<=0 禁用定时同步
}

// CleanupConfig holds settings for periodic data cleanup.
type CleanupConfig struct {
	RetentionDays int `json:"retention_days"` // 保留天数，超过此天数的旧记录会被清理，默认 7；<=0 禁用清理
	MinKeepCount  int `json:"min_keep_count"` // 每个表最少保留的记录数，默认 1000
}

// DefaultConfig returns a Config with sensible default values.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			ExternalURL: "http://localhost:8080",
			ListenAddr:  ":8080",
		},
		Agent: AgentConfig{
			HeartbeatTimeoutSeconds: 120,
			PollIntervalSeconds:     60,
		},
		Alert: AlertConfig{
			DefaultBeforeDays: 15,
		},
		Certbot: CertbotConfig{
			BinaryPath: "certbot",
			DataDir:    "./data/certbot",
			Email:      "",
		},
		Readonly: ReadonlyConfig{
			Enabled:      false,
			ViewPassword: "",
		},
		DomainMonitor: DomainMonitorConfig{
			DefaultPort:     443,
			IntervalMinutes: 60,
		},
		Turnstile: TurnstileConfig{
			Enabled:   false,
			SiteKey:   "",
			SecretKey: "",
		},
		ThirdpartDNS: ThirdpartDNSConfig{
			SyncIntervalMinutes: 360,
		},
		Cleanup: CleanupConfig{
			RetentionDays: 7,
			MinKeepCount:  1000,
		},
	}
}

// LoadConfig reads and parses a config.json file from the given path.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Note: LoadConfig unmarshals into DefaultConfig() which has SyncIntervalMinutes=360.
	// If the JSON file has "thirdpart_dns": {"sync_interval_minutes": 0}, that 0 is an
	// explicit "disabled" value and must be preserved. Old config files without the field
	// will keep the default 360 from DefaultConfig(). No normalization needed here.

	if err := ValidateConfig(cfg); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// SaveConfig writes the config to the given path with proper JSON formatting.
// It creates parent directories if they don't exist and sets file permissions to 0600.
func SaveConfig(path string, cfg *Config) error {
	if err := ValidateConfig(cfg); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Append newline for POSIX compliance
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// ValidateConfig checks that required fields are set and values are within valid ranges.
func ValidateConfig(cfg *Config) error {
	if cfg == nil {
		return errors.New("config is nil")
	}

	if cfg.Server.ExternalURL == "" {
		return errors.New("server.external_url is required")
	}

	if cfg.Server.ListenAddr == "" {
		return errors.New("server.listen_addr is required")
	}

	if cfg.Agent.HeartbeatTimeoutSeconds <= 0 {
		return errors.New("agent.heartbeat_timeout_seconds must be positive")
	}

	if cfg.Agent.PollIntervalSeconds <= 0 {
		return errors.New("agent.poll_interval_seconds must be positive")
	}

	if cfg.Alert.DefaultBeforeDays <= 0 {
		return errors.New("alert.default_before_days must be positive")
	}

	if cfg.Readonly.Enabled && cfg.Readonly.ViewPassword == "" {
		return errors.New("readonly.view_password is required when readonly mode is enabled")
	}

	if cfg.DomainMonitor.DefaultPort <= 0 || cfg.DomainMonitor.DefaultPort > 65535 {
		return errors.New("domain_monitor.default_port must be between 1 and 65535")
	}

	if cfg.DomainMonitor.IntervalMinutes <= 0 {
		return errors.New("domain_monitor.interval_minutes must be positive")
	}

	if cfg.Turnstile.Enabled {
		if cfg.Turnstile.SiteKey == "" {
			return errors.New("turnstile.site_key is required when turnstile is enabled")
		}
		if cfg.Turnstile.SecretKey == "" {
			return errors.New("turnstile.secret_key is required when turnstile is enabled")
		}
	}

	return nil
}

// CheckFilePermissions checks if the config file has secure permissions (0600).
// On non-Unix systems (Windows), this check is skipped with an info log.
// Returns nil if permissions are correct or if running on a non-Unix system.
// Logs a security warning if permissions are too open.
func CheckFilePermissions(path string) error {
	if runtime.GOOS == "windows" {
		log.Printf("[INFO] Skipping file permission check on Windows for: %s", path)
		return nil
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist yet, no warning needed
		}
		return fmt.Errorf("failed to stat config file: %w", err)
	}

	mode := info.Mode().Perm()
	if mode != 0600 {
		log.Printf("[SECURITY WARNING] Config file %s has permissions %04o, expected 0600. "+
			"This file may contain sensitive data (passwords, tokens). "+
			"Please run: chmod 600 %s", path, mode, path)
	}

	return nil
}
