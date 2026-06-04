package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// newAuthServiceWithTurnstileServer creates an AuthService that uses a custom HTTP client
// pointing to the given test server URL for Turnstile siteverify requests.
// Since verifyTurnstile uses the constant TurnstileSiteverifyURL, we override the httpClient
// with a custom transport that redirects all requests to the test server.
func newAuthServiceWithTurnstileServer(t *testing.T, userRepo *repository.UserRepository, runtimeCfg *config.RuntimeConfig, serverURL string) *AuthService {
	t.Helper()
	svc := NewAuthService(userRepo, runtimeCfg, testJWTSecret)
	// Override httpClient with a transport that redirects to the test server
	svc.httpClient = &http.Client{
		Timeout: 5 * time.Second,
		Transport: &redirectTransport{targetURL: serverURL},
	}
	return svc
}

// redirectTransport is a custom http.RoundTripper that redirects all requests
// to a target URL (the test server), preserving the request body and headers.
type redirectTransport struct {
	targetURL string
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Replace the URL with the test server URL
	newReq := req.Clone(req.Context())
	newReq.URL.Scheme = "http"
	newReq.URL.Host = t.targetURL[len("http://"):]
	newReq.URL.Path = "/turnstile/v0/siteverify"
	return http.DefaultTransport.RoundTrip(newReq)
}

// --- Turnstile Siteverify Mock Tests ---

func TestVerifyTurnstile_Success(t *testing.T) {
	// Mock server that returns success
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify it's a POST with correct content type
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("expected content-type application/x-www-form-urlencoded, got %s", ct)
		}

		// Parse form data
		if err := r.ParseForm(); err != nil {
			t.Errorf("failed to parse form: %v", err)
		}
		if r.FormValue("secret") != "test-secret-key" {
			t.Errorf("expected secret 'test-secret-key', got %q", r.FormValue("secret"))
		}
		if r.FormValue("response") != "valid-token" {
			t.Errorf("expected response 'valid-token', got %q", r.FormValue("response"))
		}
		if r.FormValue("remoteip") != "1.2.3.4" {
			t.Errorf("expected remoteip '1.2.3.4', got %q", r.FormValue("remoteip"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TurnstileSiteverifyResponse{
			Success: true,
		})
	}))
	defer server.Close()

	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{
		Turnstile: config.TurnstileConfig{
			Enabled:   true,
			SiteKey:   "site-key",
			SecretKey: "test-secret-key",
		},
	}
	svc := newAuthServiceWithTurnstileServer(t, userRepo, config.NewRuntimeConfig(cfg), server.URL)

	// Call verifyTurnstile directly
	err := svc.verifyTurnstile(context.Background(), "test-secret-key", "valid-token", "1.2.3.4")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
}

func TestVerifyTurnstile_Failure(t *testing.T) {
	// Mock server that returns failure
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TurnstileSiteverifyResponse{
			Success:    false,
			ErrorCodes: []string{"invalid-input-response"},
		})
	}))
	defer server.Close()

	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{}
	svc := newAuthServiceWithTurnstileServer(t, userRepo, config.NewRuntimeConfig(cfg), server.URL)

	err := svc.verifyTurnstile(context.Background(), "test-secret", "invalid-token", "1.2.3.4")
	if err == nil {
		t.Fatal("expected error for failed verification, got nil")
	}
}

func TestVerifyTurnstile_Timeout(t *testing.T) {
	// Mock server that delays longer than the client timeout
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(6 * time.Second) // longer than the 5s client timeout
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TurnstileSiteverifyResponse{Success: true})
	}))
	defer server.Close()

	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{}
	svc := newAuthServiceWithTurnstileServer(t, userRepo, config.NewRuntimeConfig(cfg), server.URL)

	err := svc.verifyTurnstile(context.Background(), "test-secret", "some-token", "1.2.3.4")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestVerifyTurnstile_NonOKStatus(t *testing.T) {
	// Mock server that returns HTTP 500
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{}
	svc := newAuthServiceWithTurnstileServer(t, userRepo, config.NewRuntimeConfig(cfg), server.URL)

	err := svc.verifyTurnstile(context.Background(), "test-secret", "some-token", "1.2.3.4")
	if err == nil {
		t.Fatal("expected error for non-200 status, got nil")
	}
}

func TestVerifyTurnstile_NoRemoteIP(t *testing.T) {
	// Mock server that verifies remoteip is NOT sent when empty
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("failed to parse form: %v", err)
		}
		if r.FormValue("remoteip") != "" {
			t.Errorf("expected empty remoteip, got %q", r.FormValue("remoteip"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TurnstileSiteverifyResponse{Success: true})
	}))
	defer server.Close()

	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{}
	svc := newAuthServiceWithTurnstileServer(t, userRepo, config.NewRuntimeConfig(cfg), server.URL)

	err := svc.verifyTurnstile(context.Background(), "test-secret", "some-token", "")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
}

// --- GetBestEffortRemoteIP Tests ---

func TestGetBestEffortRemoteIP_CFConnectingIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("CF-Connecting-IP", "10.0.0.1")
	req.Header.Set("X-Forwarded-For", "192.168.1.1, 10.0.0.2")
	req.Header.Set("X-Real-IP", "172.16.0.1")

	ip := GetBestEffortRemoteIP(req)
	if ip != "10.0.0.1" {
		t.Errorf("expected '10.0.0.1', got %q", ip)
	}
}

func TestGetBestEffortRemoteIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "192.168.1.1, 10.0.0.2")
	req.Header.Set("X-Real-IP", "172.16.0.1")

	ip := GetBestEffortRemoteIP(req)
	if ip != "192.168.1.1" {
		t.Errorf("expected '192.168.1.1', got %q", ip)
	}
}

func TestGetBestEffortRemoteIP_XForwardedForSingleIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.50")

	ip := GetBestEffortRemoteIP(req)
	if ip != "203.0.113.50" {
		t.Errorf("expected '203.0.113.50', got %q", ip)
	}
}

func TestGetBestEffortRemoteIP_XRealIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Real-IP", "172.16.0.1")

	ip := GetBestEffortRemoteIP(req)
	if ip != "172.16.0.1" {
		t.Errorf("expected '172.16.0.1', got %q", ip)
	}
}

func TestGetBestEffortRemoteIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.1:12345"

	ip := GetBestEffortRemoteIP(req)
	if ip != "198.51.100.1" {
		t.Errorf("expected '198.51.100.1', got %q", ip)
	}
}

func TestGetBestEffortRemoteIP_NoHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "" // empty RemoteAddr

	ip := GetBestEffortRemoteIP(req)
	if ip != "" {
		t.Errorf("expected empty string, got %q", ip)
	}
}

func TestGetBestEffortRemoteIP_PriorityOrder(t *testing.T) {
	// All headers set - CF-Connecting-IP should win
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("CF-Connecting-IP", "1.1.1.1")
	req.Header.Set("X-Forwarded-For", "2.2.2.2")
	req.Header.Set("X-Real-IP", "3.3.3.3")
	req.RemoteAddr = "4.4.4.4:8080"

	ip := GetBestEffortRemoteIP(req)
	if ip != "1.1.1.1" {
		t.Errorf("expected CF-Connecting-IP '1.1.1.1', got %q", ip)
	}
}

// --- Login with Turnstile Integration Tests ---

func TestLogin_TurnstileEnabled_MissingToken(t *testing.T) {
	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{
		Turnstile: config.TurnstileConfig{
			Enabled:   true,
			SiteKey:   "site-key",
			SecretKey: "secret-key",
		},
	}
	svc := NewAuthService(userRepo, config.NewRuntimeConfig(cfg), testJWTSecret)

	createAuthTestUser(t, userRepo, "admin", "password123", "admin")

	// Login with empty turnstile token should fail
	_, err := svc.Login(context.Background(), "admin", "password123", "", "1.2.3.4")
	if err == nil {
		t.Fatal("expected error when turnstile token is missing")
	}
	if err != ErrTurnstileRequired {
		t.Errorf("expected ErrTurnstileRequired, got: %v", err)
	}
}

func TestLogin_TurnstileEnabled_InvalidToken(t *testing.T) {
	// Mock server that returns failure
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TurnstileSiteverifyResponse{
			Success:    false,
			ErrorCodes: []string{"invalid-input-response"},
		})
	}))
	defer server.Close()

	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{
		Turnstile: config.TurnstileConfig{
			Enabled:   true,
			SiteKey:   "site-key",
			SecretKey: "secret-key",
		},
	}
	svc := newAuthServiceWithTurnstileServer(t, userRepo, config.NewRuntimeConfig(cfg), server.URL)

	createAuthTestUser(t, userRepo, "admin", "password123", "admin")

	// Login with invalid turnstile token should fail
	_, err := svc.Login(context.Background(), "admin", "password123", "invalid-token", "1.2.3.4")
	if err == nil {
		t.Fatal("expected error when turnstile verification fails")
	}
	if err != ErrTurnstileFailed {
		t.Errorf("expected ErrTurnstileFailed, got: %v", err)
	}
}

func TestLogin_TurnstileEnabled_ValidToken(t *testing.T) {
	// Mock server that returns success
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TurnstileSiteverifyResponse{Success: true})
	}))
	defer server.Close()

	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{
		Turnstile: config.TurnstileConfig{
			Enabled:   true,
			SiteKey:   "site-key",
			SecretKey: "secret-key",
		},
	}
	svc := newAuthServiceWithTurnstileServer(t, userRepo, config.NewRuntimeConfig(cfg), server.URL)

	createAuthTestUser(t, userRepo, "admin", "password123", "admin")

	// Login with valid turnstile token should succeed
	token, err := svc.Login(context.Background(), "admin", "password123", "valid-token", "1.2.3.4")
	if err != nil {
		t.Fatalf("expected successful login, got error: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
}

func TestLogin_TurnstileDisabled_NoVerification(t *testing.T) {
	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{
		Turnstile: config.TurnstileConfig{
			Enabled: false,
		},
	}
	svc := NewAuthService(userRepo, config.NewRuntimeConfig(cfg), testJWTSecret)

	createAuthTestUser(t, userRepo, "admin", "password123", "admin")

	// Login without turnstile token should succeed when disabled
	token, err := svc.Login(context.Background(), "admin", "password123", "", "")
	if err != nil {
		t.Fatalf("expected successful login when turnstile disabled, got error: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
}

func TestLoginReadonly_TurnstileEnabled_MissingToken(t *testing.T) {
	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{
		Readonly: config.ReadonlyConfig{
			Enabled:      true,
			ViewPassword: "readonly-pass",
		},
		Turnstile: config.TurnstileConfig{
			Enabled:   true,
			SiteKey:   "site-key",
			SecretKey: "secret-key",
		},
	}
	svc := NewAuthService(userRepo, config.NewRuntimeConfig(cfg), testJWTSecret)

	// Readonly login with empty turnstile token should fail
	_, err := svc.LoginReadonly(context.Background(), "readonly-pass", "", "1.2.3.4")
	if err == nil {
		t.Fatal("expected error when turnstile token is missing for readonly login")
	}
	if err != ErrTurnstileRequired {
		t.Errorf("expected ErrTurnstileRequired, got: %v", err)
	}
}

func TestLoginReadonly_TurnstileEnabled_InvalidToken(t *testing.T) {
	// Mock server that returns failure
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TurnstileSiteverifyResponse{
			Success:    false,
			ErrorCodes: []string{"timeout-or-duplicate"},
		})
	}))
	defer server.Close()

	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{
		Readonly: config.ReadonlyConfig{
			Enabled:      true,
			ViewPassword: "readonly-pass",
		},
		Turnstile: config.TurnstileConfig{
			Enabled:   true,
			SiteKey:   "site-key",
			SecretKey: "secret-key",
		},
	}
	svc := newAuthServiceWithTurnstileServer(t, userRepo, config.NewRuntimeConfig(cfg), server.URL)

	// Readonly login with invalid turnstile token should fail
	_, err := svc.LoginReadonly(context.Background(), "readonly-pass", "bad-token", "1.2.3.4")
	if err == nil {
		t.Fatal("expected error when turnstile verification fails for readonly login")
	}
	if err != ErrTurnstileFailed {
		t.Errorf("expected ErrTurnstileFailed, got: %v", err)
	}
}

func TestLoginReadonly_TurnstileEnabled_ValidToken(t *testing.T) {
	// Mock server that returns success
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TurnstileSiteverifyResponse{Success: true})
	}))
	defer server.Close()

	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{
		Readonly: config.ReadonlyConfig{
			Enabled:      true,
			ViewPassword: "readonly-pass",
		},
		Turnstile: config.TurnstileConfig{
			Enabled:   true,
			SiteKey:   "site-key",
			SecretKey: "secret-key",
		},
	}
	svc := newAuthServiceWithTurnstileServer(t, userRepo, config.NewRuntimeConfig(cfg), server.URL)

	// Readonly login with valid turnstile token should succeed
	token, err := svc.LoginReadonly(context.Background(), "readonly-pass", "valid-token", "1.2.3.4")
	if err != nil {
		t.Fatalf("expected successful readonly login, got error: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
}

func TestLoginReadonly_TurnstileDisabled_NoVerification(t *testing.T) {
	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{
		Readonly: config.ReadonlyConfig{
			Enabled:      true,
			ViewPassword: "readonly-pass",
		},
		Turnstile: config.TurnstileConfig{
			Enabled: false,
		},
	}
	svc := NewAuthService(userRepo, config.NewRuntimeConfig(cfg), testJWTSecret)

	// Readonly login without turnstile token should succeed when disabled
	token, err := svc.LoginReadonly(context.Background(), "readonly-pass", "", "")
	if err != nil {
		t.Fatalf("expected successful readonly login when turnstile disabled, got error: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
}
