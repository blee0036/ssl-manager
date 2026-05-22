package config

import "sync"

// RuntimeConfig is a thread-safe holder for the application's runtime configuration.
// All services that need config values should reference this instead of a static *Config pointer.
// When config is saved (via /init/config or /api/system/config), call Update() to propagate changes.
type RuntimeConfig struct {
	mu  sync.RWMutex
	cfg *Config
}

// NewRuntimeConfig creates a new RuntimeConfig with the given initial configuration.
func NewRuntimeConfig(cfg *Config) *RuntimeConfig {
	return &RuntimeConfig{cfg: cfg}
}

// Get returns the current configuration. Callers should not cache the returned pointer
// across operations — call Get() each time a fresh value is needed.
func (rc *RuntimeConfig) Get() *Config {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc.cfg
}

// Update replaces the in-memory configuration atomically.
func (rc *RuntimeConfig) Update(cfg *Config) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.cfg = cfg
}
