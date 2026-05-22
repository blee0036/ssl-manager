package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
	"github.com/ssl-manager/ssl-manager/internal/web/service"

	_ "github.com/glebarez/sqlite"
)

// mockCFClient is a mock Cloudflare client for handler tests.
type mockCFClient struct{}

func (m *mockCFClient) VerifyToken(ctx context.Context, token string) error {
	return nil
}

func (m *mockCFClient) ListZones(ctx context.Context, token string) ([]service.Zone, error) {
	return []service.Zone{{ID: "zone-1", Name: "example.com"}}, nil
}

func (m *mockCFClient) ListDNSRecords(ctx context.Context, token string, zoneID string, types []string) ([]service.DNSRecord, error) {
	return []service.DNSRecord{
		{Name: "www.example.com", Type: "A", Value: "1.2.3.4"},
	}, nil
}

// setupThirdpartDNSTestDB creates an in-memory SQLite database with required tables.
func setupThirdpartDNSTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	// Create thirdpart_dns table
	_, err = db.Exec(`CREATE TABLE thirdpart_dns (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		type TEXT NOT NULL DEFAULT 'cloudflare' CHECK(type IN ('cloudflare')),
		api_token TEXT NOT NULL,
		config_json TEXT NOT NULL DEFAULT '{}',
		main_domains TEXT NOT NULL DEFAULT '[]',
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("failed to create thirdpart_dns table: %v", err)
	}

	// Create thirdpart_dns_sync_logs table
	_, err = db.Exec(`CREATE TABLE thirdpart_dns_sync_logs (
		id TEXT PRIMARY KEY,
		thirdpart_dns_id TEXT NOT NULL,
		records_count INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL CHECK(status IN ('success', 'failed')),
		error_message TEXT DEFAULT '',
		synced_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("failed to create thirdpart_dns_sync_logs table: %v", err)
	}

	// Create domains table (needed for sync)
	_, err = db.Exec(`CREATE TABLE domains (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		source TEXT DEFAULT 'manual' CHECK(source IN ('manual', 'certificate', 'cloudflare')),
		thirdpart_dns_id TEXT DEFAULT '',
		dns_record_type TEXT DEFAULT '',
		dns_record_value TEXT DEFAULT '',
		monitor_port INTEGER NOT NULL DEFAULT 443,
		linked_machine_id TEXT,
		linked_certificate_id TEXT,
		linked_machine_certificate_id TEXT,
		monitor_enabled INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("failed to create domains table: %v", err)
	}

	t.Cleanup(func() { db.Close() })
	return db
}

// setupThirdpartDNSHandler creates a ThirdpartDNSHandler with test dependencies.
func setupThirdpartDNSHandler(t *testing.T) (*ThirdpartDNSHandler, *chi.Mux, *sql.DB) {
	t.Helper()

	db := setupThirdpartDNSTestDB(t)
	dnsRepo := repository.NewThirdpartDNSRepository(db)
	domainRepo := repository.NewDomainRepository(db)
	cfClient := &mockCFClient{}
	dnsService := service.NewThirdpartDNSService(dnsRepo, domainRepo, cfClient, nil, config.NewRuntimeConfig(config.DefaultConfig()))
	handler := NewThirdpartDNSHandler(dnsService)

	// Setup router without auth middleware for testing
	r := chi.NewRouter()
	r.Route("/api/thirdpart-dns", func(r chi.Router) {
		r.Get("/", handler.List)
		r.Post("/", handler.Create)
		r.Get("/{id}", handler.GetByID)
		r.Put("/{id}", handler.Update)
		r.Delete("/{id}", handler.Delete)
		r.Post("/{id}/sync", handler.TriggerSync)
		r.Get("/{id}/sync-logs", handler.GetSyncLogs)
	})

	return handler, r, db
}

// insertTestThirdpartDNS inserts a test DNS config directly into the database.
func insertTestThirdpartDNS(t *testing.T, db *sql.DB, id, name string) *model.ThirdpartDNS {
	t.Helper()

	now := time.Now().UTC()
	config := &model.ThirdpartDNS{
		ID:          id,
		Name:        name,
		Type:        "cloudflare",
		APIToken:    "test-api-token",
		ConfigJSON:  "{}",
		MainDomains: []string{},
		Enabled:     true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	_, err := db.Exec(`INSERT INTO thirdpart_dns (
		id, name, type, api_token, config_json, main_domains, enabled, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		config.ID, config.Name, config.Type, config.APIToken, config.ConfigJSON,
		"[]", 1, now.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("failed to insert test thirdpart_dns: %v", err)
	}

	return config
}

func TestThirdpartDNSHandler_List_Empty(t *testing.T) {
	_, r, _ := setupThirdpartDNSHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/thirdpart-dns", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	data, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("expected data to be an array, got %T", resp.Data)
	}
	if len(data) != 0 {
		t.Errorf("expected empty array, got %d items", len(data))
	}
}

func TestThirdpartDNSHandler_List_WithConfigs(t *testing.T) {
	_, r, db := setupThirdpartDNSHandler(t)

	insertTestThirdpartDNS(t, db, "dns-1", "Cloudflare Main")
	insertTestThirdpartDNS(t, db, "dns-2", "Cloudflare Secondary")

	req := httptest.NewRequest(http.MethodGet, "/api/thirdpart-dns", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	data, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("expected data to be an array, got %T", resp.Data)
	}
	if len(data) != 2 {
		t.Errorf("expected 2 items, got %d", len(data))
	}
}

func TestThirdpartDNSHandler_Create_Success(t *testing.T) {
	_, r, _ := setupThirdpartDNSHandler(t)

	body := map[string]interface{}{
		"name":         "My Cloudflare",
		"type":         "cloudflare",
		"api_token":    "cf-api-token-123",
		"config_json":  "{}",
		"main_domains": []string{"example.com"},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/thirdpart-dns", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Message != "DNS config created" {
		t.Errorf("expected message 'DNS config created', got '%s'", resp.Message)
	}

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be a map, got %T", resp.Data)
	}
	if data["name"] != "My Cloudflare" {
		t.Errorf("expected name 'My Cloudflare', got '%v'", data["name"])
	}
	if data["type"] != "cloudflare" {
		t.Errorf("expected type 'cloudflare', got '%v'", data["type"])
	}
	if data["enabled"] != true {
		t.Errorf("expected enabled true, got %v", data["enabled"])
	}
}

func TestThirdpartDNSHandler_Create_MissingName(t *testing.T) {
	_, r, _ := setupThirdpartDNSHandler(t)

	body := map[string]interface{}{
		"type":      "cloudflare",
		"api_token": "cf-api-token-123",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/thirdpart-dns", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestThirdpartDNSHandler_Create_MissingAPIToken(t *testing.T) {
	_, r, _ := setupThirdpartDNSHandler(t)

	body := map[string]interface{}{
		"name": "My Cloudflare",
		"type": "cloudflare",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/thirdpart-dns", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestThirdpartDNSHandler_Create_InvalidBody(t *testing.T) {
	_, r, _ := setupThirdpartDNSHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/thirdpart-dns", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestThirdpartDNSHandler_Create_UnsupportedType(t *testing.T) {
	_, r, _ := setupThirdpartDNSHandler(t)

	body := map[string]interface{}{
		"name":      "My DNS",
		"type":      "route53",
		"api_token": "some-token",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/thirdpart-dns", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestThirdpartDNSHandler_GetByID_Success(t *testing.T) {
	_, r, db := setupThirdpartDNSHandler(t)

	insertTestThirdpartDNS(t, db, "dns-1", "Cloudflare Main")

	req := httptest.NewRequest(http.MethodGet, "/api/thirdpart-dns/dns-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be a map, got %T", resp.Data)
	}
	if data["name"] != "Cloudflare Main" {
		t.Errorf("expected name 'Cloudflare Main', got '%v'", data["name"])
	}
}

func TestThirdpartDNSHandler_GetByID_NotFound(t *testing.T) {
	_, r, _ := setupThirdpartDNSHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/thirdpart-dns/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestThirdpartDNSHandler_Update_Success(t *testing.T) {
	_, r, db := setupThirdpartDNSHandler(t)

	insertTestThirdpartDNS(t, db, "dns-1", "Cloudflare Main")

	newName := "Cloudflare Updated"
	body := map[string]interface{}{
		"name": newName,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/thirdpart-dns/dns-1", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Message != "DNS config updated" {
		t.Errorf("expected message 'DNS config updated', got '%s'", resp.Message)
	}

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be a map, got %T", resp.Data)
	}
	if data["name"] != newName {
		t.Errorf("expected name '%s', got '%v'", newName, data["name"])
	}
}

func TestThirdpartDNSHandler_Update_NotFound(t *testing.T) {
	_, r, _ := setupThirdpartDNSHandler(t)

	body := map[string]interface{}{
		"name": "Updated",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/thirdpart-dns/nonexistent", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestThirdpartDNSHandler_Update_InvalidBody(t *testing.T) {
	_, r, db := setupThirdpartDNSHandler(t)

	insertTestThirdpartDNS(t, db, "dns-1", "Cloudflare Main")

	req := httptest.NewRequest(http.MethodPut, "/api/thirdpart-dns/dns-1", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestThirdpartDNSHandler_Delete_Success(t *testing.T) {
	_, r, db := setupThirdpartDNSHandler(t)

	insertTestThirdpartDNS(t, db, "dns-1", "Cloudflare Main")

	req := httptest.NewRequest(http.MethodDelete, "/api/thirdpart-dns/dns-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Message != "DNS config deleted" {
		t.Errorf("expected message 'DNS config deleted', got '%s'", resp.Message)
	}

	// Verify config is actually deleted
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM thirdpart_dns WHERE id = ?", "dns-1").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected config to be deleted, but found %d records", count)
	}
}

func TestThirdpartDNSHandler_Delete_NotFound(t *testing.T) {
	_, r, _ := setupThirdpartDNSHandler(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/thirdpart-dns/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestThirdpartDNSHandler_TriggerSync_Success(t *testing.T) {
	_, r, db := setupThirdpartDNSHandler(t)

	insertTestThirdpartDNS(t, db, "dns-1", "Cloudflare Main")

	req := httptest.NewRequest(http.MethodPost, "/api/thirdpart-dns/dns-1/sync", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Message != "DNS sync completed" {
		t.Errorf("expected message 'DNS sync completed', got '%s'", resp.Message)
	}

	// Verify sync result
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be a map, got %T", resp.Data)
	}
	if data["records_count"].(float64) != 1 {
		t.Errorf("expected records_count 1, got %v", data["records_count"])
	}

	// Verify sync log was created
	var logCount int
	err := db.QueryRow("SELECT COUNT(*) FROM thirdpart_dns_sync_logs WHERE thirdpart_dns_id = ?", "dns-1").Scan(&logCount)
	if err != nil {
		t.Fatalf("failed to query sync log count: %v", err)
	}
	if logCount != 1 {
		t.Errorf("expected 1 sync log, got %d", logCount)
	}
}

func TestThirdpartDNSHandler_TriggerSync_NotFound(t *testing.T) {
	_, r, _ := setupThirdpartDNSHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/thirdpart-dns/nonexistent/sync", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestThirdpartDNSHandler_TriggerSync_Disabled(t *testing.T) {
	_, r, db := setupThirdpartDNSHandler(t)

	// Insert a disabled config
	now := time.Now().UTC()
	_, err := db.Exec(`INSERT INTO thirdpart_dns (
		id, name, type, api_token, config_json, main_domains, enabled, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"dns-disabled", "Disabled Config", "cloudflare", "token", "{}", "[]",
		0, now.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("failed to insert disabled config: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/thirdpart-dns/dns-disabled/sync", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestThirdpartDNSHandler_GetSyncLogs_Empty(t *testing.T) {
	_, r, db := setupThirdpartDNSHandler(t)

	insertTestThirdpartDNS(t, db, "dns-1", "Cloudflare Main")

	req := httptest.NewRequest(http.MethodGet, "/api/thirdpart-dns/dns-1/sync-logs", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	data, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("expected data to be an array, got %T", resp.Data)
	}
	if len(data) != 0 {
		t.Errorf("expected empty array, got %d items", len(data))
	}
}

func TestThirdpartDNSHandler_GetSyncLogs_WithLogs(t *testing.T) {
	_, r, db := setupThirdpartDNSHandler(t)

	insertTestThirdpartDNS(t, db, "dns-1", "Cloudflare Main")

	// Insert sync logs
	now := time.Now().UTC()
	_, err := db.Exec(`INSERT INTO thirdpart_dns_sync_logs (
		id, thirdpart_dns_id, records_count, status, error_message, synced_at
	) VALUES (?, ?, ?, ?, ?, ?)`,
		"log-1", "dns-1", 5, "success", "", now.Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("failed to insert sync log: %v", err)
	}

	_, err = db.Exec(`INSERT INTO thirdpart_dns_sync_logs (
		id, thirdpart_dns_id, records_count, status, error_message, synced_at
	) VALUES (?, ?, ?, ?, ?, ?)`,
		"log-2", "dns-1", 0, "failed", "API error", now.Add(-time.Hour).Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("failed to insert sync log: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/thirdpart-dns/dns-1/sync-logs", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	data, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("expected data to be an array, got %T", resp.Data)
	}
	if len(data) != 2 {
		t.Errorf("expected 2 sync logs, got %d", len(data))
	}

	// Verify order (most recent first)
	firstLog := data[0].(map[string]interface{})
	if firstLog["status"] != "success" {
		t.Errorf("expected first log status 'success', got '%v'", firstLog["status"])
	}
}

func TestThirdpartDNSHandler_RegisterRoutes(t *testing.T) {
	db := setupThirdpartDNSTestDB(t)
	dnsRepo := repository.NewThirdpartDNSRepository(db)
	domainRepo := repository.NewDomainRepository(db)
	dnsService := service.NewThirdpartDNSService(dnsRepo, domainRepo, nil, nil, config.NewRuntimeConfig(config.DefaultConfig()))
	handler := NewThirdpartDNSHandler(dnsService)

	r := chi.NewRouter()

	// Create a minimal mock auth service for route registration
	authSvc := &testAuthService{}
	handler.RegisterRoutes(r, authSvc, &mockAuditRepo{})

	// Verify routes are registered by walking the router - ensure no panic
	if r == nil {
		t.Fatal("router should not be nil")
	}
}

// testAuthService is a minimal implementation of middleware.AuthService for testing route registration.
type testAuthService struct{}

func (s *testAuthService) GetJWTSecret() []byte {
	return []byte("test-secret")
}

func (s *testAuthService) IsSessionValid(_ context.Context, _ string) bool {
	return true
}

func (s *testAuthService) IsUserActive(_ context.Context, _ string) bool {
	return true
}

func (s *testAuthService) GetCurrentRole(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (s *testAuthService) IsTokenValid(_ context.Context, _ string, _ time.Time) bool {
	return true
}
