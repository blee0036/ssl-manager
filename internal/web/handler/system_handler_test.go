package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/web/middleware"
)

// setupSystemHandler creates a SystemHandler with a temp config file for testing.
func setupSystemHandler(t *testing.T) (*SystemHandler, string) {
	t.Helper()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Write a default config to the temp file
	cfg := config.DefaultConfig()
	cfg.Readonly.Enabled = true
	cfg.Readonly.ViewPassword = "secret123"
	if err := config.SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("failed to save test config: %v", err)
	}

	handler := NewSystemHandler(configPath, config.NewRuntimeConfig(cfg))
	return handler, configPath
}

// withAdminContext adds admin user claims to the request context.
func withAdminContext(r *http.Request) *http.Request {
	claims := &middleware.TokenClaims{
		UserID:   "user-1",
		Username: "admin",
		Role:     "admin",
	}
	ctx := context.WithValue(r.Context(), middleware.UserClaimsKey, claims)
	return r.WithContext(ctx)
}

// withUserContext adds a regular user claims to the request context.
func withUserContext(r *http.Request) *http.Request {
	claims := &middleware.TokenClaims{
		UserID:   "user-2",
		Username: "viewer",
		Role:     "user",
	}
	ctx := context.WithValue(r.Context(), middleware.UserClaimsKey, claims)
	return r.WithContext(ctx)
}

func TestSystemHandler_GetConfig_Success(t *testing.T) {
	handler, _ := setupSystemHandler(t)

	// Setup router without auth middleware for testing
	r := chi.NewRouter()
	r.Get("/api/system/config", handler.GetConfig)

	req := httptest.NewRequest(http.MethodGet, "/api/system/config", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Code    int            `json:"code"`
		Message string         `json:"message"`
		Data    config.Config  `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Verify sensitive fields are masked
	if resp.Data.Readonly.ViewPassword != "***" {
		t.Errorf("expected view_password to be masked, got %q", resp.Data.Readonly.ViewPassword)
	}

	// Verify non-sensitive fields are returned correctly
	if resp.Data.Server.ListenAddr != ":8080" {
		t.Errorf("expected listen_addr ':8080', got %q", resp.Data.Server.ListenAddr)
	}

	if resp.Data.Readonly.Enabled != true {
		t.Error("expected readonly.enabled to be true")
	}
}

func TestSystemHandler_GetConfig_MasksEmptyPassword(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Write config with empty password (readonly disabled)
	cfg := config.DefaultConfig()
	cfg.Readonly.Enabled = false
	cfg.Readonly.ViewPassword = ""
	if err := config.SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("failed to save test config: %v", err)
	}

	handler := NewSystemHandler(configPath, config.NewRuntimeConfig(config.DefaultConfig()))

	r := chi.NewRouter()
	r.Get("/api/system/config", handler.GetConfig)

	req := httptest.NewRequest(http.MethodGet, "/api/system/config", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Code int           `json:"code"`
		Data config.Config `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Empty password should NOT be masked (stays empty)
	if resp.Data.Readonly.ViewPassword != "" {
		t.Errorf("expected empty view_password to remain empty, got %q", resp.Data.Readonly.ViewPassword)
	}
}

func TestSystemHandler_GetConfig_FileNotFound(t *testing.T) {
	handler := NewSystemHandler("/nonexistent/path/config.json", config.NewRuntimeConfig(config.DefaultConfig()))

	r := chi.NewRouter()
	r.Get("/api/system/config", handler.GetConfig)

	req := httptest.NewRequest(http.MethodGet, "/api/system/config", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestSystemHandler_UpdateConfig_AdminSuccess(t *testing.T) {
	handler, configPath := setupSystemHandler(t)

	r := chi.NewRouter()
	r.Put("/api/system/config", handler.UpdateConfig)

	body := config.DefaultConfig()
	body.Server.ExternalURL = "https://new.example.com"
	body.Server.ListenAddr = ":9090"
	body.Readonly.Enabled = true
	body.Readonly.ViewPassword = "newpassword"
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/system/config", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req = withAdminContext(req)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Code int           `json:"code"`
		Data config.Config `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Response should have masked password
	if resp.Data.Readonly.ViewPassword != "***" {
		t.Errorf("expected masked password in response, got %q", resp.Data.Readonly.ViewPassword)
	}

	// Verify the actual file was updated
	saved, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load saved config: %v", err)
	}
	if saved.Server.ExternalURL != "https://new.example.com" {
		t.Errorf("expected external_url 'https://new.example.com', got %q", saved.Server.ExternalURL)
	}
	if saved.Server.ListenAddr != ":9090" {
		t.Errorf("expected listen_addr ':9090', got %q", saved.Server.ListenAddr)
	}
	if saved.Readonly.ViewPassword != "newpassword" {
		t.Errorf("expected view_password 'newpassword' in saved file, got %q", saved.Readonly.ViewPassword)
	}
}

func TestSystemHandler_UpdateConfig_PreservesMaskedPassword(t *testing.T) {
	handler, configPath := setupSystemHandler(t)

	r := chi.NewRouter()
	r.Put("/api/system/config", handler.UpdateConfig)

	// Send update with masked password (client didn't change it)
	body := config.DefaultConfig()
	body.Readonly.Enabled = true
	body.Readonly.ViewPassword = "***" // masked value from GET response
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/system/config", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req = withAdminContext(req)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Verify the original password was preserved
	saved, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load saved config: %v", err)
	}
	if saved.Readonly.ViewPassword != "secret123" {
		t.Errorf("expected original password 'secret123' to be preserved, got %q", saved.Readonly.ViewPassword)
	}
}

func TestSystemHandler_UpdateConfig_ReadonlyForbidden(t *testing.T) {
	handler, _ := setupSystemHandler(t)

	r := chi.NewRouter()
	r.Put("/api/system/config", handler.UpdateConfig)

	body := config.DefaultConfig()
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/system/config", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	// Set readonly role - should be forbidden
	claims := &middleware.TokenClaims{
		UserID:   "readonly-1",
		Username: "readonly",
		Role:     "readonly",
	}
	ctx := context.WithValue(req.Context(), middleware.UserClaimsKey, claims)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403 for readonly role, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestSystemHandler_UpdateConfig_UserAllowed(t *testing.T) {
	handler, _ := setupSystemHandler(t)

	r := chi.NewRouter()
	r.Put("/api/system/config", handler.UpdateConfig)

	body := config.DefaultConfig()
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/system/config", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req = withUserContext(req) // user role should be allowed
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for user role, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestSystemHandler_UpdateConfig_NoClaims(t *testing.T) {
	handler, _ := setupSystemHandler(t)

	r := chi.NewRouter()
	r.Put("/api/system/config", handler.UpdateConfig)

	body := config.DefaultConfig()
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/system/config", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	// No context claims set
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestSystemHandler_UpdateConfig_InvalidJSON(t *testing.T) {
	handler, _ := setupSystemHandler(t)

	r := chi.NewRouter()
	r.Put("/api/system/config", handler.UpdateConfig)

	req := httptest.NewRequest(http.MethodPut, "/api/system/config", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	req = withAdminContext(req)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestSystemHandler_UpdateConfig_ValidationError(t *testing.T) {
	handler, _ := setupSystemHandler(t)

	r := chi.NewRouter()
	r.Put("/api/system/config", handler.UpdateConfig)

	// Send config with invalid values (empty external_url)
	body := config.DefaultConfig()
	body.Server.ExternalURL = ""
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/system/config", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req = withAdminContext(req)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestMaskConfig_MasksSensitiveFields(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Readonly.ViewPassword = "mysecret"

	masked := maskConfig(cfg)

	if masked.Readonly.ViewPassword != "***" {
		t.Errorf("expected masked password '***', got %q", masked.Readonly.ViewPassword)
	}

	// Original should not be modified
	if cfg.Readonly.ViewPassword != "mysecret" {
		t.Errorf("original config was modified, got %q", cfg.Readonly.ViewPassword)
	}
}

func TestMaskConfig_EmptyPasswordNotMasked(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Readonly.ViewPassword = ""

	masked := maskConfig(cfg)

	if masked.Readonly.ViewPassword != "" {
		t.Errorf("expected empty password to remain empty, got %q", masked.Readonly.ViewPassword)
	}
}
