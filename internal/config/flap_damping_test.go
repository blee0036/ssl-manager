package config

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// These tests pin down the alert flap-damping settings: their defaults, the lenient
// validation that keeps an older client able to save, and the Effective* accessors that
// resolve an unset value to the built-in default.

func TestDefaultConfig_FlapDampingDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Agent.OfflineAlertAfterSeconds != 600 {
		t.Errorf("expected Agent.OfflineAlertAfterSeconds 600, got %d", cfg.Agent.OfflineAlertAfterSeconds)
	}
	if cfg.DomainMonitor.TimeoutSeconds != 15 {
		t.Errorf("expected DomainMonitor.TimeoutSeconds 15, got %d", cfg.DomainMonitor.TimeoutSeconds)
	}
	if cfg.DomainMonitor.ProbeRetries != 2 {
		t.Errorf("expected DomainMonitor.ProbeRetries 2, got %d", cfg.DomainMonitor.ProbeRetries)
	}
	if cfg.DomainMonitor.RetryDelaySeconds != 2 {
		t.Errorf("expected DomainMonitor.RetryDelaySeconds 2, got %d", cfg.DomainMonitor.RetryDelaySeconds)
	}
	if cfg.DomainMonitor.AlertAfterFailures != 2 {
		t.Errorf("expected DomainMonitor.AlertAfterFailures 2, got %d", cfg.DomainMonitor.AlertAfterFailures)
	}
}

func TestFlapDampingFields_JSONTagsMatchAPIContract(t *testing.T) {
	// The PUT /api/system/config body is config.Config itself, so these json tags are the
	// field names the web UI must send. Drift here silently breaks the settings page.
	data, err := json.Marshal(DefaultConfig())
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	var raw map[string]map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}

	if _, ok := raw["agent"]["offline_alert_after_seconds"]; !ok {
		t.Error("missing agent.offline_alert_after_seconds in serialized config")
	}
	for _, key := range []string{"timeout_seconds", "probe_retries", "retry_delay_seconds", "alert_after_failures"} {
		if _, ok := raw["domain_monitor"][key]; !ok {
			t.Errorf("missing domain_monitor.%s in serialized config", key)
		}
	}
}

func TestValidateConfig_FlapDampingAcceptsZero(t *testing.T) {
	// A client that predates these fields sends them as 0 under the full-overwrite PUT
	// contract. Rejecting that would lock it out of saving any setting at all, so 0 must
	// validate and be resolved to the default by the Effective* accessors.
	cfg := DefaultConfig()
	cfg.Agent.OfflineAlertAfterSeconds = 0
	cfg.DomainMonitor.TimeoutSeconds = 0
	cfg.DomainMonitor.ProbeRetries = 0
	cfg.DomainMonitor.RetryDelaySeconds = 0
	cfg.DomainMonitor.AlertAfterFailures = 0

	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("expected zero values to validate, got: %v", err)
	}
}

func TestValidateConfig_FlapDampingRejectsNegative(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*Config)
	}{
		{"negative offline alert delay", func(c *Config) { c.Agent.OfflineAlertAfterSeconds = -1 }},
		{"negative probe timeout", func(c *Config) { c.DomainMonitor.TimeoutSeconds = -1 }},
		{"negative probe retries", func(c *Config) { c.DomainMonitor.ProbeRetries = -1 }},
		{"negative retry delay", func(c *Config) { c.DomainMonitor.RetryDelaySeconds = -1 }},
		{"negative alert threshold", func(c *Config) { c.DomainMonitor.AlertAfterFailures = -1 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.apply(cfg)
			if err := ValidateConfig(cfg); err == nil {
				t.Error("expected validation to reject a negative value")
			}
		})
	}
}

func TestValidateConfig_FlapDampingRejectsAboveCap(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*Config)
	}{
		{"offline alert delay above cap", func(c *Config) { c.Agent.OfflineAlertAfterSeconds = MaxOfflineAlertAfterSeconds + 1 }},
		{"probe timeout above cap", func(c *Config) { c.DomainMonitor.TimeoutSeconds = MaxDomainMonitorTimeoutSeconds + 1 }},
		{"probe retries above cap", func(c *Config) { c.DomainMonitor.ProbeRetries = MaxDomainMonitorProbeRetries + 1 }},
		{"retry delay above cap", func(c *Config) { c.DomainMonitor.RetryDelaySeconds = MaxDomainMonitorRetryDelaySeconds + 1 }},
		{"alert threshold above cap", func(c *Config) { c.DomainMonitor.AlertAfterFailures = MaxDomainMonitorAlertAfterFailures + 1 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.apply(cfg)
			if err := ValidateConfig(cfg); err == nil {
				t.Error("expected validation to reject a value above its cap")
			}
		})
	}
}

func TestLoadConfig_OldFileInheritsDampingDefaults(t *testing.T) {
	// A config.json written before these fields existed must come back with damping on,
	// not with zeroes.
	path := t.TempDir() + "/config.json"
	old := `{
		"server": {"external_url": "https://ssl.example.com", "listen_addr": ":8080"},
		"agent": {"heartbeat_timeout_seconds": 120, "poll_interval_seconds": 60},
		"domain_monitor": {"default_port": 443, "interval_minutes": 60}
	}`
	if err := os.WriteFile(path, []byte(old), 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Agent.OfflineAlertAfterSeconds != 600 {
		t.Errorf("expected the default offline alert delay 600, got %d", cfg.Agent.OfflineAlertAfterSeconds)
	}
	if cfg.DomainMonitor.AlertAfterFailures != 2 {
		t.Errorf("expected the default alert threshold 2, got %d", cfg.DomainMonitor.AlertAfterFailures)
	}
	if cfg.DomainMonitor.ProbeRetries != 2 {
		t.Errorf("expected the default probe retries 2, got %d", cfg.DomainMonitor.ProbeRetries)
	}
}

func TestAgentConfig_EffectiveOfflineAlertAfterSeconds(t *testing.T) {
	tests := []struct {
		name             string
		heartbeatTimeout int
		offlineAlert     int
		want             int
	}{
		{"configured value is used", 120, 900, 900},
		{"unset falls back to the default", 120, 0, 600},
		{"never below the offline threshold", 1200, 300, 1200},
		{"equal values are kept", 600, 600, 600},
		{"unset heartbeat timeout falls back too", 0, 0, 600},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := AgentConfig{
				HeartbeatTimeoutSeconds:  tt.heartbeatTimeout,
				OfflineAlertAfterSeconds: tt.offlineAlert,
			}
			if got := c.EffectiveOfflineAlertAfterSeconds(); got != tt.want {
				t.Errorf("expected %d, got %d", tt.want, got)
			}
		})
	}
}

func TestDomainMonitorConfig_EffectiveAccessors(t *testing.T) {
	// Unset (zero) resolves to the built-in default for the timeout and the alert
	// threshold, so damping cannot be silently turned off.
	unset := DomainMonitorConfig{}
	if got := unset.EffectiveTimeout(); got != 15*time.Second {
		t.Errorf("expected a 15s default timeout, got %v", got)
	}
	if got := unset.EffectiveAlertAfterFailures(); got != 2 {
		t.Errorf("expected a default alert threshold of 2, got %d", got)
	}

	// Retries and the retry delay honour an explicit 0 literally: "do not retry" and
	// "retry immediately" are meaningful settings.
	if got := unset.EffectiveProbeRetries(); got != 0 {
		t.Errorf("expected 0 retries to be honoured, got %d", got)
	}
	if got := unset.EffectiveRetryDelay(); got != 0 {
		t.Errorf("expected a zero retry delay to be honoured, got %v", got)
	}

	configured := DomainMonitorConfig{
		TimeoutSeconds:     30,
		ProbeRetries:       4,
		RetryDelaySeconds:  5,
		AlertAfterFailures: 3,
	}
	if got := configured.EffectiveTimeout(); got != 30*time.Second {
		t.Errorf("expected 30s, got %v", got)
	}
	if got := configured.EffectiveProbeRetries(); got != 4 {
		t.Errorf("expected 4 retries, got %d", got)
	}
	if got := configured.EffectiveRetryDelay(); got != 5*time.Second {
		t.Errorf("expected a 5s retry delay, got %v", got)
	}
	if got := configured.EffectiveAlertAfterFailures(); got != 3 {
		t.Errorf("expected an alert threshold of 3, got %d", got)
	}

	// A threshold of 1 is the documented way to opt out of damping.
	eager := DomainMonitorConfig{AlertAfterFailures: 1}
	if got := eager.EffectiveAlertAfterFailures(); got != 1 {
		t.Errorf("expected a threshold of 1 to be honoured, got %d", got)
	}
}
