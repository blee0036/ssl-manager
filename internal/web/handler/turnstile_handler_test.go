package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/model"
)

func TestTurnstileHandler_GetConfig_Enabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Turnstile.Enabled = true
	cfg.Turnstile.SiteKey = "test-site-key-123"
	cfg.Turnstile.SecretKey = "super-secret-key-should-never-appear"

	runtimeCfg := config.NewRuntimeConfig(cfg)
	handler := NewTurnstileHandler(runtimeCfg)

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/turnstile-config", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp model.SuccessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Code != 200 {
		t.Errorf("expected code 200, got %d", resp.Code)
	}
	if resp.Message != "success" {
		t.Errorf("expected message 'success', got %q", resp.Message)
	}

	// Parse the data field
	dataBytes, err := json.Marshal(resp.Data)
	if err != nil {
		t.Fatalf("failed to marshal data: %v", err)
	}

	var turnstileResp TurnstileConfigResponse
	if err := json.Unmarshal(dataBytes, &turnstileResp); err != nil {
		t.Fatalf("failed to unmarshal turnstile config: %v", err)
	}

	if !turnstileResp.Enabled {
		t.Error("expected enabled to be true")
	}
	if turnstileResp.SiteKey != "test-site-key-123" {
		t.Errorf("expected site_key 'test-site-key-123', got %q", turnstileResp.SiteKey)
	}

	// Verify secret_key is NOT in the response body
	bodyStr := w.Body.String()
	if containsString(bodyStr, "super-secret-key-should-never-appear") {
		t.Error("response body must NOT contain the secret_key")
	}
	if containsString(bodyStr, "secret_key") {
		t.Error("response body must NOT contain the field name 'secret_key'")
	}
}

func TestTurnstileHandler_GetConfig_Disabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Turnstile.Enabled = false
	cfg.Turnstile.SiteKey = ""
	cfg.Turnstile.SecretKey = "some-secret"

	runtimeCfg := config.NewRuntimeConfig(cfg)
	handler := NewTurnstileHandler(runtimeCfg)

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/turnstile-config", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp model.SuccessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	dataBytes, _ := json.Marshal(resp.Data)
	var turnstileResp TurnstileConfigResponse
	if err := json.Unmarshal(dataBytes, &turnstileResp); err != nil {
		t.Fatalf("failed to unmarshal turnstile config: %v", err)
	}

	if turnstileResp.Enabled {
		t.Error("expected enabled to be false")
	}
	if turnstileResp.SiteKey != "" {
		t.Errorf("expected empty site_key, got %q", turnstileResp.SiteKey)
	}

	// Verify secret_key is NOT in the response body
	bodyStr := w.Body.String()
	if containsString(bodyStr, "some-secret") {
		t.Error("response body must NOT contain the secret_key value")
	}
}

func TestTurnstileHandler_GetConfig_ReflectsRuntimeConfigUpdate(t *testing.T) {
	// Start with disabled config
	cfg := config.DefaultConfig()
	cfg.Turnstile.Enabled = false
	cfg.Turnstile.SiteKey = ""

	runtimeCfg := config.NewRuntimeConfig(cfg)
	handler := NewTurnstileHandler(runtimeCfg)

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	// First request: disabled
	req := httptest.NewRequest(http.MethodGet, "/api/auth/turnstile-config", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp1 model.SuccessResponse
	json.NewDecoder(w.Body).Decode(&resp1)
	dataBytes1, _ := json.Marshal(resp1.Data)
	var tr1 TurnstileConfigResponse
	json.Unmarshal(dataBytes1, &tr1)

	if tr1.Enabled {
		t.Error("expected disabled initially")
	}

	// Update runtime config
	newCfg := config.DefaultConfig()
	newCfg.Turnstile.Enabled = true
	newCfg.Turnstile.SiteKey = "updated-key"
	newCfg.Turnstile.SecretKey = "new-secret"
	runtimeCfg.Update(newCfg)

	// Second request: should reflect the update
	req2 := httptest.NewRequest(http.MethodGet, "/api/auth/turnstile-config", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	var resp2 model.SuccessResponse
	json.NewDecoder(w2.Body).Decode(&resp2)
	dataBytes2, _ := json.Marshal(resp2.Data)
	var tr2 TurnstileConfigResponse
	json.Unmarshal(dataBytes2, &tr2)

	if !tr2.Enabled {
		t.Error("expected enabled after update")
	}
	if tr2.SiteKey != "updated-key" {
		t.Errorf("expected site_key 'updated-key', got %q", tr2.SiteKey)
	}

	// Verify secret_key is NOT in the response body
	bodyStr := w2.Body.String()
	if containsString(bodyStr, "new-secret") {
		t.Error("response body must NOT contain the secret_key value after update")
	}
}

func TestTurnstileHandler_RegisterRoutes(t *testing.T) {
	cfg := config.DefaultConfig()
	runtimeCfg := config.NewRuntimeConfig(cfg)
	handler := NewTurnstileHandler(runtimeCfg)

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	// Verify route is registered - no panic means success
	// Also verify it responds to GET
	req := httptest.NewRequest(http.MethodGet, "/api/auth/turnstile-config", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
}

func TestTurnstileHandler_GetConfig_MethodNotAllowed(t *testing.T) {
	cfg := config.DefaultConfig()
	runtimeCfg := config.NewRuntimeConfig(cfg)
	handler := NewTurnstileHandler(runtimeCfg)

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	// POST should not be allowed
	req := httptest.NewRequest(http.MethodPost, "/api/auth/turnstile-config", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Error("POST should not return 200")
	}
}

// containsString checks if s contains substr.
func containsString(s, substr string) bool {
	return len(substr) > 0 && len(s) >= len(substr) && (s == substr || len(s) > 0 && stringContains(s, substr))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
