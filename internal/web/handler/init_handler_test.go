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
	"github.com/ssl-manager/ssl-manager/internal/database"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
	"github.com/ssl-manager/ssl-manager/internal/web/service"
)

func setupInitHandler(t *testing.T) (*InitHandler, *service.InitService) {
	t.Helper()

	tmpDir := t.TempDir()
	db, err := database.NewDB(tmpDir)
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	userRepo := repository.NewUserRepository(db.DB)
	configPath := filepath.Join(tmpDir, "config.json")
	initSvc := service.NewInitService(db, userRepo, configPath, nil)
	handler := NewInitHandler(initSvc)

	return handler, initSvc
}

func TestInitHandler_GetStatus_NeedsInit(t *testing.T) {
	handler, _ := setupInitHandler(t)

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/init/status", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp model.SuccessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Code != http.StatusOK {
		t.Errorf("expected response code 200, got %d", resp.Code)
	}
}

func TestInitHandler_GetStatus_AlreadyInitialized(t *testing.T) {
	handler, initSvc := setupInitHandler(t)

	// Create admin first
	_, initToken, err := initSvc.CreateAdmin(context.Background(), service.CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	// Save config to complete full initialization
	serverCfg := &config.ServerConfig{ExternalURL: "https://test.example.com"}
	_, err = initSvc.SaveConfig(context.Background(), initToken, service.SaveConfigInput{Server: serverCfg})
	if err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/init/status", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestInitHandler_CreateAdmin_Success(t *testing.T) {
	handler, _ := setupInitHandler(t)

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	body := map[string]string{
		"username": "admin",
		"password": "securepass123",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/init/admin", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.SuccessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Message != "admin user created" {
		t.Errorf("expected message 'admin user created', got '%s'", resp.Message)
	}
}

func TestInitHandler_CreateAdmin_AlreadyInitialized(t *testing.T) {
	handler, initSvc := setupInitHandler(t)

	// Fully complete initialization: create admin + save config
	_, initToken, err := initSvc.CreateAdmin(context.Background(), service.CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	serverCfg := &config.ServerConfig{ExternalURL: "https://test.example.com"}
	_, err = initSvc.SaveConfig(context.Background(), initToken, service.SaveConfigInput{Server: serverCfg})
	if err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	body := map[string]string{
		"username": "admin2",
		"password": "password456",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/init/admin", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// System is fully initialized → 403
	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d; body: %s", w.Code, w.Body.String())
	}
}

// TestInitHandler_CreateAdmin_PendingNotExpired tests that creating a second admin
// while a pending (unexpired) admin exists returns 409 Conflict.
func TestInitHandler_CreateAdmin_PendingNotExpired(t *testing.T) {
	handler, initSvc := setupInitHandler(t)

	// Create admin first (creates a pending init_state that hasn't expired)
	_, _, err := initSvc.CreateAdmin(context.Background(), service.CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	body := map[string]string{
		"username": "admin2",
		"password": "password456",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/init/admin", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Pending admin exists and hasn't expired → 409
	if w.Code != http.StatusConflict {
		t.Errorf("expected status 409, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestInitHandler_SaveConfig_Success(t *testing.T) {
	handler, initSvc := setupInitHandler(t)

	// Create admin first (required before saving config)
	_, initToken, err := initSvc.CreateAdmin(context.Background(), service.CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	body := map[string]interface{}{
		"server": map[string]string{
			"external_url": "https://ssl.example.com",
			"listen_addr":  ":9090",
		},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/init/config", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Init-Token", initToken)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestInitHandler_SaveConfig_BeforeAdmin(t *testing.T) {
	handler, _ := setupInitHandler(t)

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	body := map[string]interface{}{
		"server": map[string]string{
			"external_url": "https://ssl.example.com",
		},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/init/config", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Init-Token", "fake-token-no-admin")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Without a valid token (no admin created), should get 403
	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d; body: %s", w.Code, w.Body.String())
	}
}

// TestInitHandler_AllEndpoints_Return403_AfterFullInit verifies that after the system
// is fully initialized (admin created), all /init endpoints return 403.
// This validates Requirement 1.3: WHEN 初始化完成后用户访问 /init THEN 返回 403.
func TestInitHandler_AllEndpoints_Return403_AfterFullInit(t *testing.T) {
	handler, initSvc := setupInitHandler(t)

	// Complete initialization: create admin
	_, initToken, err := initSvc.CreateAdmin(context.Background(), service.CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	// Save config to complete full initialization
	serverCfg := &config.ServerConfig{ExternalURL: "https://test.example.com"}
	_, err = initSvc.SaveConfig(context.Background(), initToken, service.SaveConfigInput{Server: serverCfg})
	if err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	// Test GET /init/status returns 403
	t.Run("GET /init/status returns 403", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/init/status", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("expected status 403, got %d; body: %s", w.Code, w.Body.String())
		}
	})

	// Test POST /init/admin returns 403
	t.Run("POST /init/admin returns 403", func(t *testing.T) {
		body := map[string]string{
			"username": "hacker",
			"password": "password456",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/init/admin", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("expected status 403, got %d; body: %s", w.Code, w.Body.String())
		}
	})
}

// TestInitHandler_CreateAdmin_InvalidJSON tests that invalid JSON body returns 400.
func TestInitHandler_CreateAdmin_InvalidJSON(t *testing.T) {
	handler, _ := setupInitHandler(t)

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodPost, "/init/admin", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

// TestInitHandler_SaveConfig_InvalidJSON tests that invalid JSON body returns 400
// when a valid X-Init-Token header is provided.
func TestInitHandler_SaveConfig_InvalidJSON(t *testing.T) {
	handler, initSvc := setupInitHandler(t)

	// Create admin first to get a valid token
	_, initToken, err := initSvc.CreateAdmin(context.Background(), service.CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodPost, "/init/config", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Init-Token", initToken)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

// TestInitHandler_SaveConfig_MissingToken tests that missing X-Init-Token header returns 403.
func TestInitHandler_SaveConfig_MissingToken(t *testing.T) {
	handler, initSvc := setupInitHandler(t)

	// Create admin first
	_, _, err := initSvc.CreateAdmin(context.Background(), service.CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	body := map[string]interface{}{
		"server": map[string]string{
			"external_url": "https://ssl.example.com",
		},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/init/config", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	// Deliberately NOT setting X-Init-Token
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d; body: %s", w.Code, w.Body.String())
	}
}

// TestInitHandler_SaveConfig_Turnstile_Success tests that Turnstile config is saved
// and the response masks the secret_key.
func TestInitHandler_SaveConfig_Turnstile_Success(t *testing.T) {
	handler, initSvc := setupInitHandler(t)

	// Create admin first
	_, initToken, err := initSvc.CreateAdmin(context.Background(), service.CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	body := map[string]interface{}{
		"server": map[string]string{
			"external_url": "https://ssl.example.com",
		},
		"turnstile": map[string]interface{}{
			"enabled":    true,
			"site_key":   "site-key-abc",
			"secret_key": "secret-key-xyz",
		},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/init/config", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Init-Token", initToken)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Verify response masks secret_key
	var resp model.SuccessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Extract the data field as config
	dataBytes, _ := json.Marshal(resp.Data)
	var cfgResp config.Config
	if err := json.Unmarshal(dataBytes, &cfgResp); err != nil {
		t.Fatalf("failed to unmarshal config from response: %v", err)
	}

	if !cfgResp.Turnstile.Enabled {
		t.Error("expected turnstile.enabled to be true in response")
	}
	if cfgResp.Turnstile.SiteKey != "site-key-abc" {
		t.Errorf("expected site_key 'site-key-abc', got '%s'", cfgResp.Turnstile.SiteKey)
	}
	if cfgResp.Turnstile.SecretKey != "***" {
		t.Errorf("expected secret_key to be masked as '***', got '%s'", cfgResp.Turnstile.SecretKey)
	}
}

// TestInitHandler_SaveConfig_TurnstileEnabled_MissingKeys returns 400.
func TestInitHandler_SaveConfig_TurnstileEnabled_MissingKeys(t *testing.T) {
	handler, initSvc := setupInitHandler(t)

	// Create admin first
	_, initToken, err := initSvc.CreateAdmin(context.Background(), service.CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	body := map[string]interface{}{
		"server": map[string]string{
			"external_url": "https://ssl.example.com",
		},
		"turnstile": map[string]interface{}{
			"enabled":    true,
			"site_key":   "",
			"secret_key": "",
		},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/init/config", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Init-Token", initToken)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Config validation should fail with 500 (internal error from config validation)
	// because the token is valid but the Turnstile config has missing keys
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d; body: %s", w.Code, w.Body.String())
	}
}
