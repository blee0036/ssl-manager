package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	content := `server_url: https://ssl.example.com
machine_id: machine-001
agent_token: secret-token-123
poll_interval_seconds: 30
log_level: debug
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.ServerURL != "https://ssl.example.com" {
		t.Errorf("ServerURL = %q, want %q", cfg.ServerURL, "https://ssl.example.com")
	}
	if cfg.MachineID != "machine-001" {
		t.Errorf("MachineID = %q, want %q", cfg.MachineID, "machine-001")
	}
	if cfg.AgentToken != "secret-token-123" {
		t.Errorf("AgentToken = %q, want %q", cfg.AgentToken, "secret-token-123")
	}
	if cfg.PollIntervalSeconds != 30 {
		t.Errorf("PollIntervalSeconds = %d, want %d", cfg.PollIntervalSeconds, 30)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
}

func TestLoadConfig_DefaultValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// Only required fields, poll_interval_seconds and log_level should use defaults
	content := `server_url: https://ssl.example.com
machine_id: machine-001
agent_token: secret-token-123
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.PollIntervalSeconds != 60 {
		t.Errorf("PollIntervalSeconds = %d, want default %d", cfg.PollIntervalSeconds, 60)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want default %q", cfg.LogLevel, "info")
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	content := `{invalid yaml: [[[`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestLoadConfig_MissingRequiredFields(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name    string
		content string
	}{
		{
			name: "missing server_url",
			content: `machine_id: machine-001
agent_token: secret-token-123
`,
		},
		{
			name: "missing machine_id",
			content: `server_url: https://ssl.example.com
agent_token: secret-token-123
`,
		},
		{
			name: "missing agent_token",
			content: `server_url: https://ssl.example.com
machine_id: machine-001
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, tt.name+".yaml")
			if err := os.WriteFile(path, []byte(tt.content), 0600); err != nil {
				t.Fatal(err)
			}

			_, err := LoadConfig(path)
			if err == nil {
				t.Fatalf("expected validation error for %s, got nil", tt.name)
			}
		})
	}
}

func TestLoadConfig_InvalidPollInterval(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	content := `server_url: https://ssl.example.com
machine_id: machine-001
agent_token: secret-token-123
poll_interval_seconds: -1
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for negative poll_interval_seconds, got nil")
	}
}

func TestSaveConfig_CreatesDirectoryAndFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "config.yaml")

	cfg := &AgentConfig{
		ServerURL:           "https://ssl.example.com",
		MachineID:           "machine-001",
		AgentToken:          "secret-token-123",
		PollIntervalSeconds: 45,
		LogLevel:            "warn",
	}

	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("config file was not created")
	}

	// Verify we can load it back
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig after SaveConfig failed: %v", err)
	}

	if loaded.ServerURL != cfg.ServerURL {
		t.Errorf("ServerURL = %q, want %q", loaded.ServerURL, cfg.ServerURL)
	}
	if loaded.MachineID != cfg.MachineID {
		t.Errorf("MachineID = %q, want %q", loaded.MachineID, cfg.MachineID)
	}
	if loaded.AgentToken != cfg.AgentToken {
		t.Errorf("AgentToken = %q, want %q", loaded.AgentToken, cfg.AgentToken)
	}
	if loaded.PollIntervalSeconds != cfg.PollIntervalSeconds {
		t.Errorf("PollIntervalSeconds = %d, want %d", loaded.PollIntervalSeconds, cfg.PollIntervalSeconds)
	}
	if loaded.LogLevel != cfg.LogLevel {
		t.Errorf("LogLevel = %q, want %q", loaded.LogLevel, cfg.LogLevel)
	}
}

func TestSaveConfig_ValidationError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// Missing required fields
	cfg := &AgentConfig{}
	if err := SaveConfig(path, cfg); err == nil {
		t.Fatal("expected validation error, got nil")
	}
}

func TestSaveConfig_NilConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := SaveConfig(path, nil); err == nil {
		t.Fatal("expected error for nil config, got nil")
	}
}

func TestConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	original := &AgentConfig{
		ServerURL:           "https://ssl-manager.internal:8443",
		MachineID:           "uuid-machine-abc-123",
		AgentToken:          "long-secret-token-value-xyz",
		PollIntervalSeconds: 120,
		LogLevel:            "debug",
	}

	if err := SaveConfig(path, original); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Compare fields individually because AgentConfig contains pointer fields
	if loaded.ServerURL != original.ServerURL {
		t.Errorf("ServerURL mismatch: got %q, want %q", loaded.ServerURL, original.ServerURL)
	}
	if loaded.MachineID != original.MachineID {
		t.Errorf("MachineID mismatch: got %q, want %q", loaded.MachineID, original.MachineID)
	}
	if loaded.AgentToken != original.AgentToken {
		t.Errorf("AgentToken mismatch: got %q, want %q", loaded.AgentToken, original.AgentToken)
	}
	if loaded.PollIntervalSeconds != original.PollIntervalSeconds {
		t.Errorf("PollIntervalSeconds mismatch: got %d, want %d", loaded.PollIntervalSeconds, original.PollIntervalSeconds)
	}
	if loaded.LogLevel != original.LogLevel {
		t.Errorf("LogLevel mismatch: got %q, want %q", loaded.LogLevel, original.LogLevel)
	}
	// Both should have nil AutoUpdate (not set)
	if loaded.AutoUpdate != nil {
		t.Errorf("AutoUpdate mismatch: got %v, want nil", loaded.AutoUpdate)
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *AgentConfig
		wantErr bool
	}{
		{
			name:    "nil config",
			cfg:     nil,
			wantErr: true,
		},
		{
			name: "valid config",
			cfg: &AgentConfig{
				ServerURL:           "https://example.com",
				MachineID:           "m1",
				AgentToken:          "token",
				PollIntervalSeconds: 60,
			},
			wantErr: false,
		},
		{
			name: "zero poll interval",
			cfg: &AgentConfig{
				ServerURL:           "https://example.com",
				MachineID:           "m1",
				AgentToken:          "token",
				PollIntervalSeconds: 0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIsAutoUpdateEnabled_NilDefaultsToTrue(t *testing.T) {
	cfg := &AgentConfig{
		ServerURL:           "https://example.com",
		MachineID:           "m1",
		AgentToken:          "token",
		PollIntervalSeconds: 60,
		AutoUpdate:          nil,
	}

	if !cfg.IsAutoUpdateEnabled() {
		t.Error("IsAutoUpdateEnabled() = false, want true when AutoUpdate is nil")
	}
}

func TestIsAutoUpdateEnabled_ExplicitTrue(t *testing.T) {
	enabled := true
	cfg := &AgentConfig{
		ServerURL:           "https://example.com",
		MachineID:           "m1",
		AgentToken:          "token",
		PollIntervalSeconds: 60,
		AutoUpdate:          &enabled,
	}

	if !cfg.IsAutoUpdateEnabled() {
		t.Error("IsAutoUpdateEnabled() = false, want true when AutoUpdate is explicitly true")
	}
}

func TestIsAutoUpdateEnabled_ExplicitFalse(t *testing.T) {
	disabled := false
	cfg := &AgentConfig{
		ServerURL:           "https://example.com",
		MachineID:           "m1",
		AgentToken:          "token",
		PollIntervalSeconds: 60,
		AutoUpdate:          &disabled,
	}

	if cfg.IsAutoUpdateEnabled() {
		t.Error("IsAutoUpdateEnabled() = true, want false when AutoUpdate is explicitly false")
	}
}

func TestAutoUpdate_SerializationRoundTrip(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name       string
		autoUpdate bool
	}{
		{"auto_update_true", true},
		{"auto_update_false", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, tt.name+".yaml")
			val := tt.autoUpdate
			original := &AgentConfig{
				ServerURL:           "https://example.com",
				MachineID:           "m1",
				AgentToken:          "token",
				PollIntervalSeconds: 60,
				LogLevel:            "info",
				AutoUpdate:          &val,
			}

			if err := SaveConfig(path, original); err != nil {
				t.Fatalf("SaveConfig failed: %v", err)
			}

			loaded, err := LoadConfig(path)
			if err != nil {
				t.Fatalf("LoadConfig failed: %v", err)
			}

			if loaded.AutoUpdate == nil {
				t.Fatal("loaded AutoUpdate is nil, expected non-nil")
			}
			if *loaded.AutoUpdate != tt.autoUpdate {
				t.Errorf("AutoUpdate = %v, want %v", *loaded.AutoUpdate, tt.autoUpdate)
			}
			if loaded.IsAutoUpdateEnabled() != tt.autoUpdate {
				t.Errorf("IsAutoUpdateEnabled() = %v, want %v", loaded.IsAutoUpdateEnabled(), tt.autoUpdate)
			}
		})
	}
}

func TestLoadConfig_WithoutAutoUpdate_DefaultsToEnabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// Config file without auto_update field
	content := `server_url: https://ssl.example.com
machine_id: machine-001
agent_token: secret-token-123
poll_interval_seconds: 30
log_level: info
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.AutoUpdate != nil {
		t.Errorf("AutoUpdate = %v, want nil for config without auto_update field", cfg.AutoUpdate)
	}
	if !cfg.IsAutoUpdateEnabled() {
		t.Error("IsAutoUpdateEnabled() = false, want true when auto_update field is absent (nil defaults to true)")
	}
}
