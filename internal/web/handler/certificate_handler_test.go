package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/middleware"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
	"github.com/ssl-manager/ssl-manager/internal/web/service"

	_ "github.com/glebarez/sqlite"
)

// setupTestDB creates an in-memory SQLite database for testing.
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	// Create certificates table
	_, err = db.Exec(`CREATE TABLE certificates (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		domains TEXT NOT NULL,
		source TEXT NOT NULL,
		expire_at TEXT NOT NULL,
		auto_renew INTEGER NOT NULL DEFAULT 0,
		issuer TEXT DEFAULT '',
		fingerprint_sha256 TEXT NOT NULL,
		chain_valid INTEGER NOT NULL DEFAULT 1,
		cert_dir_path TEXT NOT NULL,
		thirdpart_dns_id TEXT DEFAULT '',
		last_renew_at TEXT,
		renew_status TEXT DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("failed to create certificates table: %v", err)
	}

	// Create machine_certificates table (needed for MarkAssociatedPendingSync)
	_, err = db.Exec(`CREATE TABLE machine_certificates (
		id TEXT PRIMARY KEY,
		machine_id TEXT NOT NULL,
		certificate_id TEXT NOT NULL,
		cert_path TEXT NOT NULL,
		private_key_path TEXT NOT NULL,
		post_deploy_commands TEXT DEFAULT '',
		config_revision INTEGER NOT NULL DEFAULT 1,
		last_deploy_status TEXT DEFAULT '',
		last_deploy_at TEXT,
		last_deploy_message TEXT DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("failed to create machine_certificates table: %v", err)
	}

	t.Cleanup(func() { db.Close() })
	return db
}

// mockAuthService implements middleware.AuthService for testing.
type mockAuthService struct{}

func (m *mockAuthService) GetJWTSecret() []byte {
	return []byte("test-secret")
}

func (m *mockAuthService) IsSessionValid(ctx context.Context, sessionID string) bool {
	return true
}

func (m *mockAuthService) IsUserActive(ctx context.Context, userID string) bool {
	return true
}

func (m *mockAuthService) GetCurrentRole(ctx context.Context, userID string) (string, error) {
	return "", nil
}

func (m *mockAuthService) IsTokenValid(ctx context.Context, userID string, issuedAt time.Time) bool {
	return true
}

type mockCertificateRenewer struct {
	cert *model.Certificate
	err  error
}

func (m *mockCertificateRenewer) RenewCertificate(_ context.Context, _ string) (*model.Certificate, error) {
	return m.cert, m.err
}

// setupCertHandler creates a CertificateHandler with test dependencies.
func setupCertHandler(t *testing.T) (*CertificateHandler, *chi.Mux, string) {
	t.Helper()

	// Create temp data directory
	dataDir := t.TempDir()

	db := setupTestDB(t)
	certRepo := repository.NewCertificateRepository(db, dataDir)
	certService := service.NewCertificateService(certRepo, db)
	handler := NewCertificateHandler(certService, nil, nil, nil, dataDir)

	// Setup router without auth middleware for testing
	r := chi.NewRouter()
	r.Route("/api/certificates", func(r chi.Router) {
		r.Get("/", handler.List)
		r.Post("/", handler.Create)
		r.Get("/{id}", handler.GetByID)
		r.Put("/{id}", handler.Update)
		r.Delete("/{id}", handler.Delete)
		r.Post("/{id}/renew", handler.Renew)
	})

	return handler, r, dataDir
}

// insertTestCert inserts a test certificate directly into the database.
func insertTestCert(t *testing.T, db *sql.DB, dataDir string) *model.Certificate {
	t.Helper()

	cert := &model.Certificate{
		ID:                "test-cert-id",
		Name:              "Test Certificate",
		Domains:           []string{"example.com", "www.example.com"},
		Source:            "upload",
		ExpireAt:          time.Now().Add(90 * 24 * time.Hour).UTC(),
		AutoRenew:         false,
		Issuer:            "Test CA",
		FingerprintSHA256: "abc123def456",
		ChainValid:        true,
		CertDirPath:       "certificates/test-cert-id",
		RenewStatus:       "",
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}

	domainsJSON, _ := json.Marshal(cert.Domains)
	_, err := db.Exec(`INSERT INTO certificates (
		id, name, domains, source, expire_at, auto_renew, issuer,
		fingerprint_sha256, chain_valid, cert_dir_path, thirdpart_dns_id,
		last_renew_at, renew_status, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cert.ID, cert.Name, string(domainsJSON), cert.Source,
		cert.ExpireAt.Format(time.RFC3339), 0, cert.Issuer,
		cert.FingerprintSHA256, 1, cert.CertDirPath, "",
		nil, cert.RenewStatus,
		cert.CreatedAt.Format(time.RFC3339), cert.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("failed to insert test cert: %v", err)
	}

	// Create cert directory and privkey file
	certDir := filepath.Join(dataDir, "certificates", cert.ID)
	os.MkdirAll(certDir, 0755)
	os.WriteFile(filepath.Join(certDir, "privkey.pem"), []byte("fake-key"), 0600)

	return cert
}

func TestCertificateHandler_List_Empty(t *testing.T) {
	_, r, _ := setupCertHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/certificates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Code != http.StatusOK {
		t.Errorf("expected response code 200, got %d", resp.Code)
	}

	// Data should be an empty array
	data, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("expected data to be an array, got %T", resp.Data)
	}
	if len(data) != 0 {
		t.Errorf("expected empty array, got %d items", len(data))
	}
}

func TestCertificateHandler_GetByID_NotFound(t *testing.T) {
	_, r, _ := setupCertHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/certificates/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}

	var resp model.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Message != "certificate not found" {
		t.Errorf("expected 'certificate not found', got '%s'", resp.Message)
	}
}

func TestCertificateHandler_Delete_NotFound(t *testing.T) {
	_, r, _ := setupCertHandler(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/certificates/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestCertificateHandler_Renew_Success(t *testing.T) {
	handler, r, _ := setupCertHandler(t)
	now := time.Now().UTC()
	handler.SetCertificateRenewer(&mockCertificateRenewer{
		cert: &model.Certificate{
			ID:                "renewed-cert",
			Name:              "Renewed Certificate",
			Domains:           []string{"example.com"},
			Source:            "certbot_cloudflare_dns",
			ExpireAt:          now.Add(90 * 24 * time.Hour),
			AutoRenew:         true,
			Issuer:            "Let's Encrypt",
			FingerprintSHA256: "fingerprint",
			ChainValid:        true,
			RenewStatus:       "success",
			CreatedAt:         now,
			UpdatedAt:         now,
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/certificates/renewed-cert/renew", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.Message != "certificate renewed" {
		t.Errorf("expected renewal success message, got %q", resp.Message)
	}
}

func TestCertificateHandler_Renew_NotConfigured(t *testing.T) {
	_, r, _ := setupCertHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/certificates/test-cert/renew", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

func TestCertificateHandler_Create_MissingFields(t *testing.T) {
	_, r, _ := setupCertHandler(t)

	tests := []struct {
		name     string
		body     map[string]interface{}
		expected string
	}{
		{
			name:     "missing name",
			body:     map[string]interface{}{"cert_pem": "Y2VydA==", "key_pem": "a2V5"},
			expected: "name is required",
		},
		{
			name:     "missing cert_pem",
			body:     map[string]interface{}{"name": "test", "key_pem": "a2V5"},
			expected: "cert_pem is required",
		},
		{
			name:     "missing key_pem",
			body:     map[string]interface{}{"name": "test", "cert_pem": "Y2VydA=="},
			expected: "key_pem is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/api/certificates", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected status 400, got %d", w.Code)
			}

			var resp model.ErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			if resp.Message != tt.expected {
				t.Errorf("expected message '%s', got '%s'", tt.expected, resp.Message)
			}
		})
	}
}

func TestCertificateHandler_Create_InvalidBody(t *testing.T) {
	_, r, _ := setupCertHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/certificates", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestCertificateHandler_RegisterRoutes(t *testing.T) {
	dataDir := t.TempDir()
	db := setupTestDB(t)
	certRepo := repository.NewCertificateRepository(db, dataDir)
	certService := service.NewCertificateService(certRepo, db)
	handler := NewCertificateHandler(certService, nil, nil, nil, dataDir)

	r := chi.NewRouter()
	authSvc := &mockAuthService{}
	handler.RegisterRoutes(r, authSvc, &mockAuditRepo{})

	// Verify routes are registered by walking the router
	// Just ensure no panic occurs during registration
	if r == nil {
		t.Fatal("router should not be nil")
	}
}

func TestCertificateHandler_toCertificateResponse(t *testing.T) {
	dataDir := t.TempDir()
	handler := &CertificateHandler{dataDir: dataDir}

	now := time.Now().UTC()
	cert := &model.Certificate{
		ID:                "cert-123",
		Name:              "My Cert",
		Domains:           []string{"example.com"},
		Source:            "upload",
		ExpireAt:          now.Add(30 * 24 * time.Hour),
		AutoRenew:         true,
		Issuer:            "Let's Encrypt",
		FingerprintSHA256: "sha256hash",
		ChainValid:        true,
		CertDirPath:       "certificates/cert-123",
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	resp := handler.toCertificateResponse(cert)

	// Verify all fields are mapped correctly
	if resp.ID != cert.ID {
		t.Errorf("expected ID %s, got %s", cert.ID, resp.ID)
	}
	if resp.Name != cert.Name {
		t.Errorf("expected Name %s, got %s", cert.Name, resp.Name)
	}
	if len(resp.Domains) != 1 || resp.Domains[0] != "example.com" {
		t.Errorf("expected Domains [example.com], got %v", resp.Domains)
	}
	if resp.Source != cert.Source {
		t.Errorf("expected Source %s, got %s", cert.Source, resp.Source)
	}
	if resp.AutoRenew != cert.AutoRenew {
		t.Errorf("expected AutoRenew %v, got %v", cert.AutoRenew, resp.AutoRenew)
	}
	if resp.Issuer != cert.Issuer {
		t.Errorf("expected Issuer %s, got %s", cert.Issuer, resp.Issuer)
	}
	if resp.FingerprintSHA256 != cert.FingerprintSHA256 {
		t.Errorf("expected FingerprintSHA256 %s, got %s", cert.FingerprintSHA256, resp.FingerprintSHA256)
	}
	if resp.ChainValid != cert.ChainValid {
		t.Errorf("expected ChainValid %v, got %v", cert.ChainValid, resp.ChainValid)
	}
	// HasPrivateKey should be false since no file exists
	if resp.HasPrivateKey {
		t.Error("expected HasPrivateKey false when no privkey file exists")
	}
}

func TestCertificateHandler_hasPrivateKey(t *testing.T) {
	dataDir := t.TempDir()
	handler := &CertificateHandler{dataDir: dataDir}

	// No file exists
	if handler.hasPrivateKey("nonexistent") {
		t.Error("expected false when privkey file does not exist")
	}

	// Create the file
	certDir := filepath.Join(dataDir, "certificates", "test-id")
	os.MkdirAll(certDir, 0755)
	os.WriteFile(filepath.Join(certDir, "privkey.pem"), []byte("key"), 0600)

	if !handler.hasPrivateKey("test-id") {
		t.Error("expected true when privkey file exists")
	}
}

func TestCertificateHandler_ResponseDoesNotContainPrivateKey(t *testing.T) {
	dataDir := t.TempDir()
	handler := &CertificateHandler{dataDir: dataDir}

	cert := &model.Certificate{
		ID:                "cert-456",
		Name:              "Secure Cert",
		Domains:           []string{"secure.example.com"},
		Source:            "upload",
		ExpireAt:          time.Now().Add(60 * 24 * time.Hour).UTC(),
		AutoRenew:         false,
		Issuer:            "DigiCert",
		FingerprintSHA256: "fingerprint123",
		ChainValid:        true,
		CertDirPath:       "certificates/cert-456",
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}

	resp := handler.toCertificateResponse(cert)

	// Marshal to JSON and verify no private_key or cert_dir_path fields
	jsonBytes, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal response: %v", err)
	}

	var rawMap map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &rawMap); err != nil {
		t.Fatalf("failed to unmarshal to map: %v", err)
	}

	// Ensure no private key or file path fields are present
	forbiddenFields := []string{"private_key_pem", "cert_dir_path", "key_pem", "privkey_pem"}
	for _, field := range forbiddenFields {
		if _, exists := rawMap[field]; exists {
			t.Errorf("response should NOT contain field '%s'", field)
		}
	}

	// Ensure expected fields ARE present
	expectedFields := []string{"id", "name", "domains", "source", "expire_at", "auto_renew",
		"issuer", "fingerprint_sha256", "chain_valid", "has_private_key", "renew_status",
		"created_at", "updated_at"}
	for _, field := range expectedFields {
		if _, exists := rawMap[field]; !exists {
			t.Errorf("response should contain field '%s'", field)
		}
	}
}

// Verify that the middleware.AuthService interface is satisfied by our mock.
var _ middleware.AuthService = (*mockAuthService)(nil)

// mockAuditRepo is a no-op implementation of middleware.AuditRepository for testing.
type mockAuditRepo struct{}

func (m *mockAuditRepo) CreateAuditLog(_ context.Context, _ *model.AuditLog) error {
	return nil
}
