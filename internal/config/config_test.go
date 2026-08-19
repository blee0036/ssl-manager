package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg == nil {
		t.Fatal("DefaultConfig() returned nil")
	}

	if cfg.Server.ExternalURL != "http://localhost:8080" {
		t.Errorf("expected Server.ExternalURL http://localhost:8080, got %s", cfg.Server.ExternalURL)
	}
	if cfg.Server.ListenAddr != ":8080" {
		t.Errorf("expected Server.ListenAddr :8080, got %s", cfg.Server.ListenAddr)
	}
	if cfg.Agent.HeartbeatTimeoutSeconds != 120 {
		t.Errorf("expected Agent.HeartbeatTimeoutSeconds 120, got %d", cfg.Agent.HeartbeatTimeoutSeconds)
	}
	if cfg.Agent.PollIntervalSeconds != 60 {
		t.Errorf("expected Agent.PollIntervalSeconds 60, got %d", cfg.Agent.PollIntervalSeconds)
	}
	if cfg.Alert.DefaultBeforeDays != 15 {
		t.Errorf("expected Alert.DefaultBeforeDays 15, got %d", cfg.Alert.DefaultBeforeDays)
	}
	if cfg.Readonly.Enabled != false {
		t.Error("expected Readonly.Enabled false")
	}
	if cfg.Certbot.BinaryPath != "certbot" {
		t.Errorf("expected Certbot.BinaryPath certbot, got %s", cfg.Certbot.BinaryPath)
	}
	if cfg.DomainMonitor.DefaultPort != 443 {
		t.Errorf("expected DomainMonitor.DefaultPort 443, got %d", cfg.DomainMonitor.DefaultPort)
	}
	if cfg.DomainMonitor.IntervalMinutes != 60 {
		t.Errorf("expected DomainMonitor.IntervalMinutes 60, got %d", cfg.DomainMonitor.IntervalMinutes)
	}
	if cfg.Auth.SessionExpiryHours != 24 {
		t.Errorf("expected Auth.SessionExpiryHours 24, got %d", cfg.Auth.SessionExpiryHours)
	}
}

func TestValidateConfig_SessionExpiryHours(t *testing.T) {
	t.Run("zero is rejected", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Auth.SessionExpiryHours = 0
		if err := ValidateConfig(cfg); err == nil {
			t.Error("expected error for zero session_expiry_hours")
		}
	})

	t.Run("negative is rejected", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Auth.SessionExpiryHours = -1
		if err := ValidateConfig(cfg); err == nil {
			t.Error("expected error for negative session_expiry_hours")
		}
	})

	t.Run("exceeds max is rejected", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Auth.SessionExpiryHours = MaxSessionExpiryHours + 1
		if err := ValidateConfig(cfg); err == nil {
			t.Error("expected error for session_expiry_hours exceeding MaxSessionExpiryHours")
		}
	})

	t.Run("at max is accepted", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Auth.SessionExpiryHours = MaxSessionExpiryHours
		if err := ValidateConfig(cfg); err != nil {
			t.Errorf("expected no error at MaxSessionExpiryHours, got: %v", err)
		}
	})

	t.Run("positive value within range is accepted", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Auth.SessionExpiryHours = 1
		if err := ValidateConfig(cfg); err != nil {
			t.Errorf("expected no error for session_expiry_hours=1, got: %v", err)
		}
	})
}

func TestDefaultConfigPath(t *testing.T) {
	if DefaultConfigPath != "./data/config.json" {
		t.Errorf("expected DefaultConfigPath ./data/config.json, got %s", DefaultConfigPath)
	}
}

func TestValidateConfig_Valid(t *testing.T) {
	cfg := DefaultConfig()
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("expected no error for default config, got: %v", err)
	}
}

func TestValidateConfig_Nil(t *testing.T) {
	if err := ValidateConfig(nil); err == nil {
		t.Error("expected error for nil config")
	}
}

func TestValidateConfig_EmptyExternalURL(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.ExternalURL = ""
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error for empty ExternalURL")
	}
}

func TestValidateConfig_EmptyListenAddr(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.ListenAddr = ""
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error for empty ListenAddr")
	}
}

func TestValidateConfig_InvalidHeartbeatTimeout(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Agent.HeartbeatTimeoutSeconds = 0
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error for zero HeartbeatTimeoutSeconds")
	}

	cfg.Agent.HeartbeatTimeoutSeconds = -1
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error for negative HeartbeatTimeoutSeconds")
	}
}

func TestValidateConfig_InvalidPollInterval(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Agent.PollIntervalSeconds = 0
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error for zero PollIntervalSeconds")
	}

	cfg.Agent.PollIntervalSeconds = -1
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error for negative PollIntervalSeconds")
	}
}

func TestValidateConfig_InvalidAlertBeforeDays(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Alert.DefaultBeforeDays = 0
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error for zero DefaultBeforeDays")
	}
}

func TestValidateConfig_ReadonlyEnabledNoPassword(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Readonly.Enabled = true
	cfg.Readonly.ViewPassword = ""
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error when readonly enabled without password")
	}
}

func TestValidateConfig_ReadonlyEnabledWithPassword(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Readonly.Enabled = true
	cfg.Readonly.ViewPassword = "secret123"
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidateConfig_InvalidPort(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DomainMonitor.DefaultPort = 0
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error for port 0")
	}

	cfg.DomainMonitor.DefaultPort = 65536
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error for port > 65535")
	}
}

func TestValidateConfig_InvalidIntervalMinutes(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DomainMonitor.IntervalMinutes = 0
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error for zero IntervalMinutes")
	}
}

func TestSaveAndLoadConfig(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")

	cfg := DefaultConfig()
	cfg.Server.ExternalURL = "https://ssl.example.com"
	cfg.Certbot.Email = "admin@example.com"
	cfg.Readonly.Enabled = true
	cfg.Readonly.ViewPassword = "viewpass"

	// Save
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file not created: %v", err)
	}

	// Verify file permissions on Unix
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0600 {
			t.Errorf("expected file permissions 0600, got %04o", info.Mode().Perm())
		}
	}

	// Load
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if loaded.Server.ExternalURL != cfg.Server.ExternalURL {
		t.Errorf("Server.ExternalURL mismatch: got %s, want %s", loaded.Server.ExternalURL, cfg.Server.ExternalURL)
	}
	if loaded.Certbot.Email != cfg.Certbot.Email {
		t.Errorf("Certbot.Email mismatch: got %s, want %s", loaded.Certbot.Email, cfg.Certbot.Email)
	}
	if loaded.Readonly.Enabled != cfg.Readonly.Enabled {
		t.Errorf("Readonly.Enabled mismatch: got %v, want %v", loaded.Readonly.Enabled, cfg.Readonly.Enabled)
	}
	if loaded.Readonly.ViewPassword != cfg.Readonly.ViewPassword {
		t.Errorf("Readonly.ViewPassword mismatch: got %s, want %s", loaded.Readonly.ViewPassword, cfg.Readonly.ViewPassword)
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/config.json")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")

	if err := os.WriteFile(path, []byte("not valid json{"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoadConfig_InvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")

	// Write config with invalid values (heartbeat_timeout_seconds = 0)
	cfg := map[string]interface{}{
		"server": map[string]interface{}{
			"external_url": "http://localhost:8080",
			"listen_addr":  ":8080",
		},
		"agent": map[string]interface{}{
			"heartbeat_timeout_seconds": 0,
			"poll_interval_seconds":     60,
		},
	}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Error("expected validation error for invalid config")
	}
}

func TestSaveConfig_InvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")

	cfg := DefaultConfig()
	cfg.Server.ExternalURL = "" // Invalid

	if err := SaveConfig(path, cfg); err == nil {
		t.Error("expected error when saving invalid config")
	}
}

func TestSaveConfig_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "subdir", "nested", "config.json")

	cfg := DefaultConfig()
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig failed to create directories: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file not created in nested directory: %v", err)
	}
}

func TestCheckFilePermissions_NonexistentFile(t *testing.T) {
	err := CheckFilePermissions("/nonexistent/config.json")
	if err != nil {
		t.Errorf("expected nil error for nonexistent file, got: %v", err)
	}
}

func TestCheckFilePermissions_CorrectPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission check not applicable on Windows")
	}

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")

	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}

	err := CheckFilePermissions(path)
	if err != nil {
		t.Errorf("expected nil error for correct permissions, got: %v", err)
	}
}

func TestCheckFilePermissions_TooOpen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission check not applicable on Windows")
	}

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")

	if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	// Should not return error, just log a warning
	err := CheckFilePermissions(path)
	if err != nil {
		t.Errorf("expected nil error (warning only), got: %v", err)
	}
}

func TestRoundTrip_JSONSerialization(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.ExternalURL = "https://example.com:9443"
	cfg.Server.ListenAddr = ":9443"
	cfg.Agent.HeartbeatTimeoutSeconds = 600
	cfg.Agent.PollIntervalSeconds = 30
	cfg.Alert.DefaultBeforeDays = 14
	cfg.Certbot.Email = "test@example.com"
	cfg.Certbot.BinaryPath = "/usr/bin/certbot"
	cfg.Certbot.DataDir = "/var/lib/certbot"
	cfg.Readonly.Enabled = true
	cfg.Readonly.ViewPassword = "pass123"
	cfg.DomainMonitor.DefaultPort = 8443
	cfg.DomainMonitor.IntervalMinutes = 30

	// Serialize
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Deserialize
	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Compare all fields
	if loaded.Server != cfg.Server {
		t.Errorf("Server: got %+v, want %+v", loaded.Server, cfg.Server)
	}
	if loaded.Agent != cfg.Agent {
		t.Errorf("Agent: got %+v, want %+v", loaded.Agent, cfg.Agent)
	}
	if loaded.Alert != cfg.Alert {
		t.Errorf("Alert: got %+v, want %+v", loaded.Alert, cfg.Alert)
	}
	if loaded.Certbot != cfg.Certbot {
		t.Errorf("Certbot: got %+v, want %+v", loaded.Certbot, cfg.Certbot)
	}
	if loaded.Readonly != cfg.Readonly {
		t.Errorf("Readonly: got %+v, want %+v", loaded.Readonly, cfg.Readonly)
	}
	if loaded.DomainMonitor != cfg.DomainMonitor {
		t.Errorf("DomainMonitor: got %+v, want %+v", loaded.DomainMonitor, cfg.DomainMonitor)
	}
}

func TestLoadConfig_PartialJSON_UsesDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")

	// Write a minimal valid config - only required fields
	cfg := map[string]interface{}{
		"server": map[string]interface{}{
			"external_url": "https://mysite.com",
			"listen_addr":  ":443",
		},
	}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Should use defaults for unspecified fields
	if loaded.Agent.HeartbeatTimeoutSeconds != 120 {
		t.Errorf("expected default HeartbeatTimeoutSeconds 120, got %d", loaded.Agent.HeartbeatTimeoutSeconds)
	}
	if loaded.Agent.PollIntervalSeconds != 60 {
		t.Errorf("expected default PollIntervalSeconds 60, got %d", loaded.Agent.PollIntervalSeconds)
	}
	if loaded.Alert.DefaultBeforeDays != 15 {
		t.Errorf("expected default DefaultBeforeDays 15, got %d", loaded.Alert.DefaultBeforeDays)
	}
	if loaded.DomainMonitor.DefaultPort != 443 {
		t.Errorf("expected default DomainMonitor.DefaultPort 443, got %d", loaded.DomainMonitor.DefaultPort)
	}
}

func TestLoadConfig_SyncIntervalZero_PersistsAsDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")

	// Save a config with sync_interval_minutes = 0 (explicitly disabled)
	cfg := DefaultConfig()
	cfg.ThirdpartDNS.SyncIntervalMinutes = 0
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	// Load it back — should remain 0, not get overwritten to 360
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if loaded.ThirdpartDNS.SyncIntervalMinutes != 0 {
		t.Errorf("expected SyncIntervalMinutes=0 (disabled), got %d", loaded.ThirdpartDNS.SyncIntervalMinutes)
	}
}

func TestLoadConfig_MissingThirdpartDNS_UsesDefault360(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")

	// Write config without thirdpart_dns field (simulates old config file)
	cfg := map[string]interface{}{
		"server": map[string]interface{}{
			"external_url": "https://mysite.com",
			"listen_addr":  ":443",
		},
	}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Should use default 360 from DefaultConfig() (since the field is missing, JSON
	// unmarshal into DefaultConfig() leaves the 360 default intact)
	if loaded.ThirdpartDNS.SyncIntervalMinutes != 360 {
		t.Errorf("expected default SyncIntervalMinutes=360 for missing field, got %d", loaded.ThirdpartDNS.SyncIntervalMinutes)
	}
}

func TestDefaultConfig_DomainExpiry(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.DomainExpiry.ExpiryThresholdDays != 14 {
		t.Errorf("expected DomainExpiry.ExpiryThresholdDays 14, got %d", cfg.DomainExpiry.ExpiryThresholdDays)
	}
	if cfg.DomainExpiry.RefreshIntervalMinutes != 1440 {
		t.Errorf("expected DomainExpiry.RefreshIntervalMinutes 1440, got %d", cfg.DomainExpiry.RefreshIntervalMinutes)
	}
	if cfg.DomainExpiry.WhoisTimeoutSeconds != 10 {
		t.Errorf("expected DomainExpiry.WhoisTimeoutSeconds 10, got %d", cfg.DomainExpiry.WhoisTimeoutSeconds)
	}
}

func TestValidateConfig_InvalidExpiryThresholdDays(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DomainExpiry.ExpiryThresholdDays = 0
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error for zero ExpiryThresholdDays")
	}

	cfg.DomainExpiry.ExpiryThresholdDays = -1
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error for negative ExpiryThresholdDays")
	}
}

func TestValidateConfig_InvalidWhoisTimeoutSeconds(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DomainExpiry.WhoisTimeoutSeconds = 0
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error for zero WhoisTimeoutSeconds")
	}

	cfg.DomainExpiry.WhoisTimeoutSeconds = -5
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error for negative WhoisTimeoutSeconds")
	}
}

func TestValidateConfig_RefreshIntervalMinutesNonPositiveAllowed(t *testing.T) {
	cfg := DefaultConfig()

	// refresh_interval_minutes <= 0 is a valid "disabled" state that stops periodic
	// WHOIS refresh (mirrors thirdpart_dns.sync_interval_minutes semantics). It must
	// NOT cause a validation error.
	cfg.DomainExpiry.RefreshIntervalMinutes = 0
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("expected no error for zero RefreshIntervalMinutes (disabled), got: %v", err)
	}

	cfg.DomainExpiry.RefreshIntervalMinutes = -100
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("expected no error for negative RefreshIntervalMinutes (disabled), got: %v", err)
	}
}

func TestValidateConfig_RefreshIntervalMinutesUpperBound(t *testing.T) {
	cfg := DefaultConfig()

	// Boundary: exactly the max (365 days) is accepted.
	cfg.DomainExpiry.RefreshIntervalMinutes = MaxRefreshIntervalMinutes
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("expected no error for RefreshIntervalMinutes == MaxRefreshIntervalMinutes (%d), got: %v", MaxRefreshIntervalMinutes, err)
	}

	// One above the max is rejected. Beyond this cap the value could eventually
	// overflow when multiplied by time.Minute, producing a non-positive duration
	// that makes time.NewTicker panic; rejecting it here prevents that.
	cfg.DomainExpiry.RefreshIntervalMinutes = MaxRefreshIntervalMinutes + 1
	if err := ValidateConfig(cfg); err == nil {
		t.Errorf("expected error for RefreshIntervalMinutes > MaxRefreshIntervalMinutes (%d)", MaxRefreshIntervalMinutes+1)
	}

	// Sanity: 0 and a negative value remain valid "disabled" states even alongside
	// the new upper-bound check (non-positive is never rejected).
	cfg.DomainExpiry.RefreshIntervalMinutes = 0
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("expected no error for zero RefreshIntervalMinutes (disabled), got: %v", err)
	}
	cfg.DomainExpiry.RefreshIntervalMinutes = -1
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("expected no error for negative RefreshIntervalMinutes (disabled), got: %v", err)
	}
}
