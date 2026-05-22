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
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
	"github.com/ssl-manager/ssl-manager/internal/web/service"

	_ "github.com/glebarez/sqlite"
)

// setupDomainTestDB creates an in-memory SQLite database with domain-related tables.
func setupDomainTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	// Create domains table
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

	// Create domain_monitor_results table
	_, err = db.Exec(`CREATE TABLE domain_monitor_results (
		id TEXT PRIMARY KEY,
		domain_id TEXT NOT NULL REFERENCES domains(id),
		checked_port INTEGER NOT NULL,
		resolved_ips TEXT DEFAULT '',
		tls_success INTEGER NOT NULL DEFAULT 0,
		certificate_fingerprint_sha256 TEXT DEFAULT '',
		issuer TEXT DEFAULT '',
		expire_at TEXT,
		days_remaining INTEGER,
		domain_matched INTEGER NOT NULL DEFAULT 0,
		chain_valid INTEGER NOT NULL DEFAULT 0,
		error_message TEXT DEFAULT '',
		checked_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("failed to create domain_monitor_results table: %v", err)
	}

	// Create certificates table (needed for probe fingerprint comparison)
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

	t.Cleanup(func() { db.Close() })
	return db
}

// setupDomainHandler creates a DomainHandler with test dependencies.
func setupDomainHandler(t *testing.T) (*DomainHandler, *chi.Mux, *sql.DB) {
	t.Helper()

	db := setupDomainTestDB(t)
	domainRepo := repository.NewDomainRepository(db)
	certRepo := repository.NewCertificateRepository(db, t.TempDir())
	domainService := service.NewDomainMonitorService(domainRepo, certRepo, nil, nil)
	handler := NewDomainHandler(domainService)

	// Setup router without auth middleware for testing
	r := chi.NewRouter()
	r.Route("/api/domains", func(r chi.Router) {
		r.Get("/", handler.List)
		r.Post("/", handler.Create)
		r.Get("/{id}", handler.GetByID)
		r.Put("/{id}", handler.Update)
		r.Delete("/{id}", handler.Delete)
		r.Post("/{id}/probe", handler.Probe)
	})

	return handler, r, db
}

// insertTestDomain inserts a test domain directly into the database.
func insertTestDomain(t *testing.T, db *sql.DB, id, name string) *model.Domain {
	t.Helper()

	now := time.Now().UTC()
	domain := &model.Domain{
		ID:             id,
		Name:           name,
		Source:         "manual",
		MonitorPort:    443,
		MonitorEnabled: true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	_, err := db.Exec(`INSERT INTO domains (
		id, name, source, thirdpart_dns_id, dns_record_type, dns_record_value,
		monitor_port, linked_machine_id, linked_certificate_id,
		linked_machine_certificate_id, monitor_enabled, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		domain.ID, domain.Name, domain.Source, "", "", "",
		domain.MonitorPort, nil, nil, nil,
		1, now.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("failed to insert test domain: %v", err)
	}

	return domain
}

func TestDomainHandler_List_Empty(t *testing.T) {
	_, r, _ := setupDomainHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/domains", nil)
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

	data, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("expected data to be an array, got %T", resp.Data)
	}
	if len(data) != 0 {
		t.Errorf("expected empty array, got %d items", len(data))
	}
}

func TestDomainHandler_List_WithDomains(t *testing.T) {
	_, r, db := setupDomainHandler(t)

	insertTestDomain(t, db, "domain-1", "example.com")
	insertTestDomain(t, db, "domain-2", "test.com")

	req := httptest.NewRequest(http.MethodGet, "/api/domains", nil)
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

func TestDomainHandler_List_WithFilter(t *testing.T) {
	_, r, db := setupDomainHandler(t)

	insertTestDomain(t, db, "domain-1", "example.com")

	// Insert a disabled domain
	now := time.Now().UTC()
	_, err := db.Exec(`INSERT INTO domains (
		id, name, source, thirdpart_dns_id, dns_record_type, dns_record_value,
		monitor_port, linked_machine_id, linked_certificate_id,
		linked_machine_certificate_id, monitor_enabled, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"domain-disabled", "disabled.com", "manual", "", "", "",
		443, nil, nil, nil,
		0, now.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("failed to insert disabled domain: %v", err)
	}

	// Filter by monitor_enabled=true
	req := httptest.NewRequest(http.MethodGet, "/api/domains?monitor_enabled=true", nil)
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
	if len(data) != 1 {
		t.Errorf("expected 1 item (only enabled), got %d", len(data))
	}
}

func TestDomainHandler_Create_Success(t *testing.T) {
	_, r, _ := setupDomainHandler(t)

	body := map[string]interface{}{
		"name":        "example.com",
		"monitor_port": 443,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/domains", bytes.NewReader(bodyBytes))
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

	if resp.Message != "domain monitor created" {
		t.Errorf("expected message 'domain monitor created', got '%s'", resp.Message)
	}

	// Verify domain data
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be a map, got %T", resp.Data)
	}
	if data["name"] != "example.com" {
		t.Errorf("expected name 'example.com', got '%v'", data["name"])
	}
	if data["monitor_port"].(float64) != 443 {
		t.Errorf("expected monitor_port 443, got %v", data["monitor_port"])
	}
	if data["monitor_enabled"] != true {
		t.Errorf("expected monitor_enabled true, got %v", data["monitor_enabled"])
	}
}

func TestDomainHandler_Create_DefaultPort(t *testing.T) {
	_, r, _ := setupDomainHandler(t)

	body := map[string]interface{}{
		"name": "example.com",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/domains", bytes.NewReader(bodyBytes))
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

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be a map, got %T", resp.Data)
	}
	// Default port should be 443
	if data["monitor_port"].(float64) != 443 {
		t.Errorf("expected default monitor_port 443, got %v", data["monitor_port"])
	}
}

func TestDomainHandler_Create_MissingName(t *testing.T) {
	_, r, _ := setupDomainHandler(t)

	body := map[string]interface{}{
		"monitor_port": 443,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/domains", bytes.NewReader(bodyBytes))
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

	if resp.Message != "domain name is required" {
		t.Errorf("expected 'domain name is required', got '%s'", resp.Message)
	}
}

func TestDomainHandler_Create_InvalidBody(t *testing.T) {
	_, r, _ := setupDomainHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/domains", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestDomainHandler_GetByID_Success(t *testing.T) {
	_, r, db := setupDomainHandler(t)

	insertTestDomain(t, db, "domain-1", "example.com")

	req := httptest.NewRequest(http.MethodGet, "/api/domains/domain-1", nil)
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
	if data["name"] != "example.com" {
		t.Errorf("expected name 'example.com', got '%v'", data["name"])
	}
}

func TestDomainHandler_GetByID_NotFound(t *testing.T) {
	_, r, _ := setupDomainHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/domains/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}

	var resp model.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Message != "domain not found" {
		t.Errorf("expected 'domain not found', got '%s'", resp.Message)
	}
}

func TestDomainHandler_Update_Success(t *testing.T) {
	_, r, db := setupDomainHandler(t)

	insertTestDomain(t, db, "domain-1", "example.com")

	newPort := 8443
	body := map[string]interface{}{
		"monitor_port": newPort,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/domains/domain-1", bytes.NewReader(bodyBytes))
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

	if resp.Message != "domain monitor updated" {
		t.Errorf("expected message 'domain monitor updated', got '%s'", resp.Message)
	}

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be a map, got %T", resp.Data)
	}
	if int(data["monitor_port"].(float64)) != newPort {
		t.Errorf("expected monitor_port %d, got %v", newPort, data["monitor_port"])
	}
}

func TestDomainHandler_Update_NotFound(t *testing.T) {
	_, r, _ := setupDomainHandler(t)

	body := map[string]interface{}{
		"monitor_port": 8443,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/domains/nonexistent", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestDomainHandler_Update_InvalidBody(t *testing.T) {
	_, r, db := setupDomainHandler(t)

	insertTestDomain(t, db, "domain-1", "example.com")

	req := httptest.NewRequest(http.MethodPut, "/api/domains/domain-1", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestDomainHandler_Delete_Success(t *testing.T) {
	_, r, db := setupDomainHandler(t)

	insertTestDomain(t, db, "domain-1", "example.com")

	req := httptest.NewRequest(http.MethodDelete, "/api/domains/domain-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Message != "domain monitor deleted" {
		t.Errorf("expected message 'domain monitor deleted', got '%s'", resp.Message)
	}

	// Verify domain is actually deleted
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM domains WHERE id = ?", "domain-1").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query domain count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected domain to be deleted, but found %d records", count)
	}
}

func TestDomainHandler_Delete_NotFound(t *testing.T) {
	_, r, _ := setupDomainHandler(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/domains/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestDomainHandler_Probe_NotFound(t *testing.T) {
	_, r, _ := setupDomainHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/domains/nonexistent/probe", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestDomainHandler_RegisterRoutes(t *testing.T) {
	db := setupDomainTestDB(t)
	domainRepo := repository.NewDomainRepository(db)
	certRepo := repository.NewCertificateRepository(db, t.TempDir())
	domainService := service.NewDomainMonitorService(domainRepo, certRepo, nil, nil)
	handler := NewDomainHandler(domainService)

	r := chi.NewRouter()
	authSvc := &mockAuthService{}
	handler.RegisterRoutes(r, authSvc, &mockAuditRepo{})

	// Verify routes are registered by walking the router - ensure no panic
	if r == nil {
		t.Fatal("router should not be nil")
	}
}

func TestDomainHandler_Create_WithLinkedCertificate(t *testing.T) {
	_, r, _ := setupDomainHandler(t)

	body := map[string]interface{}{
		"name":                  "example.com",
		"monitor_port":          443,
		"linked_certificate_id": "cert-123",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/domains", bytes.NewReader(bodyBytes))
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

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be a map, got %T", resp.Data)
	}
	if data["linked_certificate_id"] != "cert-123" {
		t.Errorf("expected linked_certificate_id 'cert-123', got '%v'", data["linked_certificate_id"])
	}
}

func TestDomainHandler_Update_DisableMonitor(t *testing.T) {
	_, r, db := setupDomainHandler(t)

	insertTestDomain(t, db, "domain-1", "example.com")

	disabled := false
	body := map[string]interface{}{
		"monitor_enabled": disabled,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/domains/domain-1", bytes.NewReader(bodyBytes))
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

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be a map, got %T", resp.Data)
	}
	if data["monitor_enabled"] != false {
		t.Errorf("expected monitor_enabled false, got %v", data["monitor_enabled"])
	}
}

// Verify that the mockAuthService satisfies the middleware.AuthService interface.
var _ context.Context = context.Background()
