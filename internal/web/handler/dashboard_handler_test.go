package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/service"

	_ "github.com/glebarez/sqlite"
)

// setupDashboardTestDB creates an in-memory SQLite database with all tables needed for dashboard stats.
func setupDashboardTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	tables := []string{
		`CREATE TABLE certificates (
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
		)`,
		`CREATE TABLE machines (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			ip TEXT NOT NULL,
			hostname TEXT DEFAULT '',
			os TEXT DEFAULT '',
			arch TEXT DEFAULT '',
			tags TEXT DEFAULT '',
			remark TEXT DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			agent_version TEXT DEFAULT '',
			agent_token_hash TEXT NOT NULL,
			agent_token_revoked_at TEXT,
			last_heartbeat_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE deployment_logs (
			id TEXT PRIMARY KEY,
			machine_certificate_id TEXT NOT NULL,
			machine_id TEXT NOT NULL,
			certificate_id TEXT NOT NULL,
			status TEXT NOT NULL,
			cert_fingerprint_sha256 TEXT NOT NULL,
			cert_path TEXT NOT NULL,
			private_key_path TEXT NOT NULL,
			command_outputs TEXT DEFAULT '',
			error_message TEXT DEFAULT '',
			started_at TEXT NOT NULL,
			finished_at TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE alerts (
			id TEXT PRIMARY KEY,
			level TEXT NOT NULL,
			type TEXT NOT NULL,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			target_type TEXT DEFAULT '',
			target_id TEXT DEFAULT '',
			sent_channels TEXT DEFAULT '',
			created_at TEXT NOT NULL,
			resolved_at TEXT
		)`,
		`CREATE TABLE domains (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			source TEXT DEFAULT 'manual',
			thirdpart_dns_id TEXT DEFAULT '',
			dns_record_id TEXT DEFAULT '',
			dns_record_type TEXT DEFAULT '',
			dns_record_value TEXT DEFAULT '',
			monitor_port INTEGER NOT NULL DEFAULT 443,
			linked_machine_id TEXT,
			linked_certificate_id TEXT,
			linked_machine_certificate_id TEXT,
			monitor_enabled INTEGER NOT NULL DEFAULT 1,
			alert_ignored INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE domain_monitor_results (
			id TEXT PRIMARY KEY,
			domain_id TEXT NOT NULL,
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
		)`,
	}

	for _, ddl := range tables {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatalf("failed to create table: %v", err)
		}
	}

	t.Cleanup(func() { db.Close() })
	return db
}

// setupDashboardHandler creates a DashboardHandler with test dependencies.
func setupDashboardHandler(t *testing.T) (*DashboardHandler, *chi.Mux, *sql.DB) {
	t.Helper()

	db := setupDashboardTestDB(t)
	dashboardService := service.NewDashboardService(db)
	handler := NewDashboardHandler(dashboardService)

	r := chi.NewRouter()
	r.Get("/api/dashboard", handler.GetDashboard)

	return handler, r, db
}

func TestDashboardHandler_GetDashboard_Empty(t *testing.T) {
	_, r, _ := setupDashboardHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
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

	// All counts should be 0 with empty database
	assertFloat64(t, data, "certificates_total", 0)
	assertFloat64(t, data, "certificates_expiring_15d", 0)
	assertFloat64(t, data, "certificates_expired", 0)
	assertFloat64(t, data, "machines_online", 0)
	assertFloat64(t, data, "machines_offline", 0)
	assertFloat64(t, data, "deploy_failures_24h", 0)
	assertFloat64(t, data, "renew_failures_24h", 0)
	assertFloat64(t, data, "domain_anomalies", 0)

	if data["has_anomalies"] != false {
		t.Errorf("expected has_anomalies to be false, got %v", data["has_anomalies"])
	}
}

func TestDashboardHandler_GetDashboard_WithCertificates(t *testing.T) {
	_, r, db := setupDashboardHandler(t)

	now := time.Now().UTC()
	// Insert certificates with various expiry states
	insertCert(t, db, "cert-1", now.Add(30*24*time.Hour))  // valid, not expiring soon
	insertCert(t, db, "cert-2", now.Add(10*24*time.Hour))  // expiring within 15 days
	insertCert(t, db, "cert-3", now.Add(5*24*time.Hour))   // expiring within 15 days
	insertCert(t, db, "cert-4", now.Add(-1*24*time.Hour))  // expired

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
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

	assertFloat64(t, data, "certificates_total", 4)
	assertFloat64(t, data, "certificates_expiring_15d", 2)
	assertFloat64(t, data, "certificates_expired", 1)

	if data["has_anomalies"] != true {
		t.Errorf("expected has_anomalies to be true (expired certs), got %v", data["has_anomalies"])
	}
}

func TestDashboardHandler_GetDashboard_WithMachines(t *testing.T) {
	_, r, db := setupDashboardHandler(t)

	now := time.Now().UTC().Format(time.RFC3339)
	insertMachineWithStatus(t, db, "m-1", "online", now)
	insertMachineWithStatus(t, db, "m-2", "online", now)
	insertMachineWithStatus(t, db, "m-3", "offline", now)
	insertMachineWithStatus(t, db, "m-4", "pending", now)

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
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

	assertFloat64(t, data, "machines_online", 2)
	assertFloat64(t, data, "machines_offline", 1)
}

func TestDashboardHandler_GetDashboard_WithDeployFailures(t *testing.T) {
	_, r, db := setupDashboardHandler(t)

	now := time.Now().UTC()
	// Recent failures (within 24h)
	insertDeployLog(t, db, "dl-1", "failed", now.Add(-1*time.Hour))
	insertDeployLog(t, db, "dl-2", "failed", now.Add(-12*time.Hour))
	// Old failure (more than 24h ago)
	insertDeployLog(t, db, "dl-3", "failed", now.Add(-48*time.Hour))
	// Recent success (should not count)
	insertDeployLog(t, db, "dl-4", "success", now.Add(-2*time.Hour))

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
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

	assertFloat64(t, data, "deploy_failures_24h", 2)
}

func TestDashboardHandler_GetDashboard_WithRenewFailures(t *testing.T) {
	_, r, db := setupDashboardHandler(t)

	now := time.Now().UTC()
	// Recent renew failure alerts
	insertAlertWithTime(t, db, "a-1", "critical", "renew_failed", now.Add(-2*time.Hour))
	insertAlertWithTime(t, db, "a-2", "warning", "cert_renew_failed", now.Add(-6*time.Hour))
	// Old renew failure (more than 24h ago)
	insertAlertWithTime(t, db, "a-3", "critical", "renew_failed", now.Add(-48*time.Hour))
	// Non-renew alert (should not count)
	insertAlertWithTime(t, db, "a-4", "critical", "cert_expired", now.Add(-1*time.Hour))

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
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

	assertFloat64(t, data, "renew_failures_24h", 2)
}

func TestDashboardHandler_GetDashboard_WithDomainAnomalies(t *testing.T) {
	_, r, db := setupDashboardHandler(t)

	now := time.Now().UTC()
	// Domain with TLS failure
	insertDomain(t, db, "d-1", "example.com", true)
	insertDomainMonitorResult(t, db, "dmr-1", "d-1", false, false, now)

	// Domain with domain mismatch
	insertDomain(t, db, "d-2", "test.com", true)
	insertDomainMonitorResult(t, db, "dmr-2", "d-2", true, false, now)

	// Domain with successful check (no anomaly)
	insertDomain(t, db, "d-3", "good.com", true)
	insertDomainMonitorResult(t, db, "dmr-3", "d-3", true, true, now)

	// Domain with monitoring disabled (should not count)
	insertDomain(t, db, "d-4", "disabled.com", false)
	insertDomainMonitorResult(t, db, "dmr-4", "d-4", false, false, now)

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
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

	assertFloat64(t, data, "domain_anomalies", 2)
}

func TestDashboardHandler_GetDashboard_HasAnomaliesTrue(t *testing.T) {
	_, r, db := setupDashboardHandler(t)

	now := time.Now().UTC()
	// Insert an expired certificate to trigger has_anomalies
	insertCert(t, db, "cert-expired", now.Add(-1*24*time.Hour))

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
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

	if data["has_anomalies"] != true {
		t.Errorf("expected has_anomalies to be true, got %v", data["has_anomalies"])
	}
}

func TestDashboardHandler_GetDashboard_HasAnomaliesFalse(t *testing.T) {
	_, r, db := setupDashboardHandler(t)

	now := time.Now().UTC()
	// Insert only valid certificates and online machines
	insertCert(t, db, "cert-valid", now.Add(60*24*time.Hour))
	insertMachineWithStatus(t, db, "m-1", "online", now.Format(time.RFC3339))

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
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

	if data["has_anomalies"] != false {
		t.Errorf("expected has_anomalies to be false, got %v", data["has_anomalies"])
	}
}

func TestDashboardHandler_RegisterRoutes(t *testing.T) {
	db := setupDashboardTestDB(t)
	dashboardService := service.NewDashboardService(db)
	handler := NewDashboardHandler(dashboardService)

	r := chi.NewRouter()
	authSvc := &dashboardTestAuthService{}
	handler.RegisterRoutes(r, authSvc, &mockAuditRepo{})

	// Verify routes are registered - no panic means success
	if r == nil {
		t.Fatal("router should not be nil")
	}
}

// --- Helper functions ---

func insertCert(t *testing.T, db *sql.DB, id string, expireAt time.Time) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`INSERT INTO certificates (
		id, name, domains, source, expire_at, auto_renew, issuer,
		fingerprint_sha256, chain_valid, cert_dir_path, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, "Test Cert "+id, "example.com", "upload",
		expireAt.Format(time.RFC3339), 0, "Let's Encrypt",
		"abc123"+id, 1, "./data/certificates/"+id, now, now,
	)
	if err != nil {
		t.Fatalf("failed to insert test certificate: %v", err)
	}
}

func insertMachineWithStatus(t *testing.T, db *sql.DB, id, status, createdAt string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO machines (
		id, name, ip, status, agent_token_hash, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, "Machine "+id, "192.168.1.1", status, "hash-"+id, createdAt, createdAt,
	)
	if err != nil {
		t.Fatalf("failed to insert test machine: %v", err)
	}
}

func insertDeployLog(t *testing.T, db *sql.DB, id, status string, createdAt time.Time) {
	t.Helper()
	ts := createdAt.Format(time.RFC3339)
	_, err := db.Exec(`INSERT INTO deployment_logs (
		id, machine_certificate_id, machine_id, certificate_id, status,
		cert_fingerprint_sha256, cert_path, private_key_path,
		started_at, finished_at, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, "mc-1", "m-1", "cert-1", status,
		"fingerprint-"+id, "/etc/ssl/cert.pem", "/etc/ssl/key.pem",
		ts, ts, ts,
	)
	if err != nil {
		t.Fatalf("failed to insert test deployment log: %v", err)
	}
}

func insertAlertWithTime(t *testing.T, db *sql.DB, id, level, alertType string, createdAt time.Time) {
	t.Helper()
	ts := createdAt.Format(time.RFC3339)
	_, err := db.Exec(`INSERT INTO alerts (
		id, level, type, title, content, status, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, level, alertType, "Test Alert", "test content", "active", ts,
	)
	if err != nil {
		t.Fatalf("failed to insert test alert: %v", err)
	}
}

func insertDomain(t *testing.T, db *sql.DB, id, name string, monitorEnabled bool) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	enabled := 0
	if monitorEnabled {
		enabled = 1
	}
	_, err := db.Exec(`INSERT INTO domains (
		id, name, source, monitor_port, monitor_enabled, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, name, "manual", 443, enabled, now, now,
	)
	if err != nil {
		t.Fatalf("failed to insert test domain: %v", err)
	}
}

func insertDomainMonitorResult(t *testing.T, db *sql.DB, id, domainID string, tlsSuccess, domainMatched bool, checkedAt time.Time) {
	t.Helper()
	tlsInt := 0
	if tlsSuccess {
		tlsInt = 1
	}
	matchedInt := 0
	if domainMatched {
		matchedInt = 1
	}
	_, err := db.Exec(`INSERT INTO domain_monitor_results (
		id, domain_id, checked_port, tls_success, domain_matched, checked_at
	) VALUES (?, ?, ?, ?, ?, ?)`,
		id, domainID, 443, tlsInt, matchedInt, checkedAt.Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("failed to insert test domain monitor result: %v", err)
	}
}

func assertFloat64(t *testing.T, data map[string]interface{}, key string, expected float64) {
	t.Helper()
	val, ok := data[key].(float64)
	if !ok {
		t.Errorf("expected %s to be a number, got %T (%v)", key, data[key], data[key])
		return
	}
	if val != expected {
		t.Errorf("expected %s to be %v, got %v", key, expected, val)
	}
}

// dashboardTestAuthService is a minimal implementation of middleware.AuthService for testing RegisterRoutes.
type dashboardTestAuthService struct{}

func (s *dashboardTestAuthService) GetJWTSecret() []byte {
	return []byte("test-secret")
}

func (s *dashboardTestAuthService) IsSessionValid(_ context.Context, _ string) bool {
	return true
}

func (s *dashboardTestAuthService) IsUserActive(_ context.Context, _ string) bool {
	return true
}

func (s *dashboardTestAuthService) GetCurrentRole(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (s *dashboardTestAuthService) IsTokenValid(_ context.Context, _ string, _ time.Time) bool {
	return true
}
