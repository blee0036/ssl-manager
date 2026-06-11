package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/middleware"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
	"github.com/ssl-manager/ssl-manager/internal/web/service"

	_ "github.com/glebarez/sqlite"
)

// mockMachineRepo implements middleware.MachineRepository for agent handler tests.
type mockMachineRepo struct {
	machines map[string]*model.Machine // keyed by token hash
}

func (m *mockMachineRepo) GetByTokenHash(_ context.Context, tokenHash string) (*model.Machine, error) {
	machine, ok := m.machines[tokenHash]
	if !ok {
		return nil, nil
	}
	return machine, nil
}

func (m *mockMachineRepo) GetByTokenHashIncludingRevoked(_ context.Context, tokenHash string) (*model.Machine, error) {
	machine, ok := m.machines[tokenHash]
	if !ok {
		return nil, nil
	}
	return machine, nil
}

// mockAgentAlertSender is a no-op alert sender for testing.
type mockAgentAlertSender struct{}

func (m *mockAgentAlertSender) SendAlert(_ context.Context, _, _, _, _, _, _ string) error {
	return nil
}

func (m *mockAgentAlertSender) AutoResolve(_ context.Context, _, _, _ string) {}

func (m *mockAgentAlertSender) SuppressActiveByTarget(_ context.Context, _, _ string) error {
	return nil
}

// Verify interface compliance.
var _ middleware.MachineRepository = (*mockMachineRepo)(nil)

// hashToken computes SHA256 hash of a token string.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// setupAgentTestDB creates an in-memory SQLite database with all required tables.
func setupAgentTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	_, err = db.Exec(`CREATE TABLE machines (
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
	)`)
	if err != nil {
		t.Fatalf("failed to create machines table: %v", err)
	}

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

	_, err = db.Exec(`CREATE TABLE deployment_logs (
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
	)`)
	if err != nil {
		t.Fatalf("failed to create deployment_logs table: %v", err)
	}

	t.Cleanup(func() { db.Close() })
	return db
}

// setupAgentHandler creates an AgentHandler with test dependencies.
func setupAgentHandler(t *testing.T) (*AgentHandler, *chi.Mux, *sql.DB, string, string) {
	t.Helper()

	db := setupAgentTestDB(t)

	// Create a temp data dir for certificate files
	dataDir := t.TempDir()

	machineRepo := repository.NewMachineRepository(db)
	mcRepo := repository.NewMachineCertificateRepository(db)
	certRepo := repository.NewCertificateRepository(db, dataDir)
	deployLogRepo := repository.NewDeploymentLogRepository(db)

	cfg := &config.Config{
		Server: config.ServerConfig{
			ExternalURL: "https://ssl.example.com",
		},
	}
	machineService := service.NewMachineService(machineRepo, config.NewRuntimeConfig(cfg))
	mcService := service.NewMachineCertificateService(mcRepo)
	deployLogService := service.NewDeploymentLogService(deployLogRepo)

	handler := NewAgentHandler(machineService, mcService, deployLogService, certRepo, mcRepo, &mockAgentAlertSender{})

	// The token for our test machine
	token := "test-agent-token-heartbeat"
	tokenHash := hashToken(token)

	// Insert a test machine with this token hash
	now := time.Now().UTC()
	_, err := db.Exec(`INSERT INTO machines (
		id, name, ip, hostname, os, arch, tags, remark, status,
		agent_version, agent_token_hash, agent_token_revoked_at,
		last_heartbeat_at, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"machine-hb-1", "Heartbeat Machine", "192.168.1.10", "", "", "",
		"[]", "", "pending", "", tokenHash, nil, nil,
		now.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("failed to insert test machine: %v", err)
	}

	// Create a mock machine repo for the middleware (looks up by token hash)
	mockRepo := &mockMachineRepo{
		machines: map[string]*model.Machine{
			tokenHash: {
				ID:     "machine-hb-1",
				Name:   "Heartbeat Machine",
				IP:     "192.168.1.10",
				Status: "pending",
			},
		},
	}

	// Setup router with AgentAuthMiddleware
	r := chi.NewRouter()
	handler.RegisterRoutes(r, mockRepo, &mockAgentAlertSender{}, &mockAuditRepo{})

	return handler, r, db, token, dataDir
}

func TestAgentHandler_Heartbeat_Success(t *testing.T) {
	_, r, db, token, _ := setupAgentHandler(t)

	body := model.HeartbeatInfo{
		AgentVersion: "1.0.0",
		Hostname:     "web-server-01",
		IP:           "10.0.0.5",
		OS:           "linux",
		Arch:         "amd64",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/agent/heartbeat", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Message != "heartbeat received" {
		t.Errorf("expected message 'heartbeat received', got '%s'", resp.Message)
	}

	// Verify the machine was updated in the database
	var hostname, ip, osField, arch, agentVersion, status string
	var lastHeartbeatAt sql.NullString
	err := db.QueryRow(`SELECT hostname, ip, os, arch, agent_version, status, last_heartbeat_at 
		FROM machines WHERE id = ?`, "machine-hb-1").Scan(
		&hostname, &ip, &osField, &arch, &agentVersion, &status, &lastHeartbeatAt,
	)
	if err != nil {
		t.Fatalf("failed to query machine: %v", err)
	}

	if hostname != "web-server-01" {
		t.Errorf("expected hostname 'web-server-01', got '%s'", hostname)
	}
	if ip != "10.0.0.5" {
		t.Errorf("expected ip '10.0.0.5', got '%s'", ip)
	}
	if osField != "linux" {
		t.Errorf("expected os 'linux', got '%s'", osField)
	}
	if arch != "amd64" {
		t.Errorf("expected arch 'amd64', got '%s'", arch)
	}
	if agentVersion != "1.0.0" {
		t.Errorf("expected agent_version '1.0.0', got '%s'", agentVersion)
	}
	if status != "online" {
		t.Errorf("expected status 'online', got '%s'", status)
	}
	if !lastHeartbeatAt.Valid {
		t.Error("expected last_heartbeat_at to be set")
	}
}

func TestAgentHandler_Heartbeat_NoAuthHeader(t *testing.T) {
	_, r, _, _, _ := setupAgentHandler(t)

	body := model.HeartbeatInfo{
		AgentVersion: "1.0.0",
		Hostname:     "web-server-01",
		IP:           "10.0.0.5",
		OS:           "linux",
		Arch:         "amd64",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/agent/heartbeat", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestAgentHandler_Heartbeat_InvalidToken(t *testing.T) {
	_, r, _, _, _ := setupAgentHandler(t)

	body := model.HeartbeatInfo{
		AgentVersion: "1.0.0",
		Hostname:     "web-server-01",
		IP:           "10.0.0.5",
		OS:           "linux",
		Arch:         "amd64",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/agent/heartbeat", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer invalid-token-xyz")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestAgentHandler_Heartbeat_InvalidBody(t *testing.T) {
	_, r, _, token, _ := setupAgentHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/agent/heartbeat", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

// --- ListMachineCertificates Tests ---

func TestAgentHandler_ListMachineCertificates_Success(t *testing.T) {
	_, r, db, token, _ := setupAgentHandler(t)

	now := time.Now().UTC().Format(time.RFC3339)

	// Insert a certificate
	_, err := db.Exec(`INSERT INTO certificates (id, name, domains, source, expire_at, auto_renew, issuer, fingerprint_sha256, chain_valid, cert_dir_path, thirdpart_dns_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"cert-1", "Test Cert", `["example.com"]`, "upload", now, 0, "Let's Encrypt", "abc123fingerprint", 1, "certificates/cert-1", "", now, now)
	if err != nil {
		t.Fatalf("failed to insert certificate: %v", err)
	}

	// Insert a machine certificate config
	_, err = db.Exec(`INSERT INTO machine_certificates (id, machine_id, certificate_id, cert_path, private_key_path, post_deploy_commands, config_revision, last_deploy_status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"mc-1", "machine-hb-1", "cert-1", "/etc/ssl/cert.pem", "/etc/ssl/key.pem", "nginx -s reload", 2, "success", now, now)
	if err != nil {
		t.Fatalf("failed to insert machine certificate: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/agent/machines/machine-hb-1/certificates", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
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
		t.Fatalf("expected 1 certificate config, got %d", len(data))
	}

	item := data[0].(map[string]interface{})
	if item["machine_certificate_id"] != "mc-1" {
		t.Errorf("expected machine_certificate_id 'mc-1', got '%v'", item["machine_certificate_id"])
	}
	if item["certificate_id"] != "cert-1" {
		t.Errorf("expected certificate_id 'cert-1', got '%v'", item["certificate_id"])
	}
	if item["fingerprint_sha256"] != "abc123fingerprint" {
		t.Errorf("expected fingerprint 'abc123fingerprint', got '%v'", item["fingerprint_sha256"])
	}
	if item["cert_path"] != "/etc/ssl/cert.pem" {
		t.Errorf("expected cert_path '/etc/ssl/cert.pem', got '%v'", item["cert_path"])
	}
	if item["private_key_path"] != "/etc/ssl/key.pem" {
		t.Errorf("expected private_key_path '/etc/ssl/key.pem', got '%v'", item["private_key_path"])
	}
	if item["post_deploy_commands"] != "nginx -s reload" {
		t.Errorf("expected post_deploy_commands 'nginx -s reload', got '%v'", item["post_deploy_commands"])
	}
	if int(item["config_revision"].(float64)) != 2 {
		t.Errorf("expected config_revision 2, got '%v'", item["config_revision"])
	}
	if item["last_deploy_status"] != "success" {
		t.Errorf("expected last_deploy_status 'success', got '%v'", item["last_deploy_status"])
	}
}

func TestAgentHandler_ListMachineCertificates_WrongMachineID(t *testing.T) {
	_, r, _, token, _ := setupAgentHandler(t)

	// Try to access a different machine's certificates
	req := httptest.NewRequest(http.MethodGet, "/api/agent/machines/other-machine-id/certificates", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// The AgentAuthMiddleware should reject this because the machine_id in URL doesn't match the token
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestAgentHandler_ListMachineCertificates_EmptyList(t *testing.T) {
	_, r, _, token, _ := setupAgentHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/agent/machines/machine-hb-1/certificates", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
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
		t.Errorf("expected 0 certificate configs, got %d", len(data))
	}
}

// --- DownloadCertificate Tests ---

func TestAgentHandler_DownloadCertificate_Success(t *testing.T) {
	_, r, db, token, dataDir := setupAgentHandler(t)

	now := time.Now().UTC().Format(time.RFC3339)

	// Insert a certificate
	_, err := db.Exec(`INSERT INTO certificates (id, name, domains, source, expire_at, auto_renew, issuer, fingerprint_sha256, chain_valid, cert_dir_path, thirdpart_dns_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"cert-dl-1", "Download Cert", `["example.com"]`, "upload", now, 0, "Let's Encrypt", "sha256fingerprint", 1, "certificates/cert-dl-1", "", now, now)
	if err != nil {
		t.Fatalf("failed to insert certificate: %v", err)
	}

	// Create certificate files on disk
	certDir := filepath.Join(dataDir, "certificates", "cert-dl-1")
	if err := os.MkdirAll(certDir, 0755); err != nil {
		t.Fatalf("failed to create cert dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(certDir, "fullchain.pem"), []byte("-----BEGIN CERTIFICATE-----\nfullchain content\n-----END CERTIFICATE-----"), 0644); err != nil {
		t.Fatalf("failed to write fullchain.pem: %v", err)
	}
	if err := os.WriteFile(filepath.Join(certDir, "privkey.pem"), []byte("-----BEGIN PRIVATE KEY-----\nprivate key content\n-----END PRIVATE KEY-----"), 0600); err != nil {
		t.Fatalf("failed to write privkey.pem: %v", err)
	}

	// Insert a machine certificate config belonging to our machine
	_, err = db.Exec(`INSERT INTO machine_certificates (id, machine_id, certificate_id, cert_path, private_key_path, post_deploy_commands, config_revision, last_deploy_status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"mc-dl-1", "machine-hb-1", "cert-dl-1", "/etc/ssl/cert.pem", "/etc/ssl/key.pem", "", 1, "pending", now, now)
	if err != nil {
		t.Fatalf("failed to insert machine certificate: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/agent/machine-certificates/mc-dl-1/download", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be an object, got %T", resp.Data)
	}

	if data["certificate_id"] != "cert-dl-1" {
		t.Errorf("expected certificate_id 'cert-dl-1', got '%v'", data["certificate_id"])
	}
	if data["fingerprint_sha256"] != "sha256fingerprint" {
		t.Errorf("expected fingerprint 'sha256fingerprint', got '%v'", data["fingerprint_sha256"])
	}
	if data["fullchain_pem"] != "-----BEGIN CERTIFICATE-----\nfullchain content\n-----END CERTIFICATE-----" {
		t.Errorf("unexpected fullchain_pem content: %v", data["fullchain_pem"])
	}
	if data["private_key_pem"] != "-----BEGIN PRIVATE KEY-----\nprivate key content\n-----END PRIVATE KEY-----" {
		t.Errorf("unexpected private_key_pem content: %v", data["private_key_pem"])
	}
}

func TestAgentHandler_DownloadCertificate_WrongMachine(t *testing.T) {
	_, r, db, token, _ := setupAgentHandler(t)

	now := time.Now().UTC().Format(time.RFC3339)

	// Insert a certificate
	_, err := db.Exec(`INSERT INTO certificates (id, name, domains, source, expire_at, auto_renew, issuer, fingerprint_sha256, chain_valid, cert_dir_path, thirdpart_dns_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"cert-dl-2", "Other Cert", `["other.com"]`, "upload", now, 0, "Let's Encrypt", "otherfingerprint", 1, "certificates/cert-dl-2", "", now, now)
	if err != nil {
		t.Fatalf("failed to insert certificate: %v", err)
	}

	// Insert a machine certificate config belonging to a DIFFERENT machine
	_, err = db.Exec(`INSERT INTO machine_certificates (id, machine_id, certificate_id, cert_path, private_key_path, post_deploy_commands, config_revision, last_deploy_status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"mc-dl-2", "other-machine-id", "cert-dl-2", "/etc/ssl/cert.pem", "/etc/ssl/key.pem", "", 1, "pending", now, now)
	if err != nil {
		t.Fatalf("failed to insert machine certificate: %v", err)
	}

	// Try to download a certificate that belongs to another machine
	req := httptest.NewRequest(http.MethodGet, "/api/agent/machine-certificates/mc-dl-2/download", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestAgentHandler_DownloadCertificate_NotFound(t *testing.T) {
	_, r, _, token, _ := setupAgentHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/agent/machine-certificates/nonexistent-id/download", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestAgentHandler_DownloadCertificate_NoAuth(t *testing.T) {
	_, r, _, _, _ := setupAgentHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/agent/machine-certificates/mc-dl-1/download", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d; body: %s", w.Code, w.Body.String())
	}
}

// --- CreateDeploymentLog Tests ---

func TestAgentHandler_CreateDeploymentLog_Success(t *testing.T) {
	_, r, db, token, _ := setupAgentHandler(t)

	now := time.Now().UTC().Format(time.RFC3339)

	// Insert a certificate
	_, err := db.Exec(`INSERT INTO certificates (id, name, domains, source, expire_at, auto_renew, issuer, fingerprint_sha256, chain_valid, cert_dir_path, thirdpart_dns_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"cert-log-1", "Log Cert", `["log.com"]`, "upload", now, 0, "Let's Encrypt", "logfingerprint", 1, "certificates/cert-log-1", "", now, now)
	if err != nil {
		t.Fatalf("failed to insert certificate: %v", err)
	}

	// Insert a machine certificate config belonging to our machine
	_, err = db.Exec(`INSERT INTO machine_certificates (id, machine_id, certificate_id, cert_path, private_key_path, post_deploy_commands, config_revision, last_deploy_status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"mc-log-1", "machine-hb-1", "cert-log-1", "/etc/ssl/cert.pem", "/etc/ssl/key.pem", "nginx -s reload", 1, "pending", now, now)
	if err != nil {
		t.Fatalf("failed to insert machine certificate: %v", err)
	}

	startedAt := time.Now().UTC().Add(-10 * time.Second)
	finishedAt := time.Now().UTC()

	body := createDeploymentLogRequest{
		MachineCertificateID:  "mc-log-1",
		CertificateID:         "cert-log-1",
		Status:                "success",
		CertFingerprintSHA256: "logfingerprint",
		CertPath:              "/etc/ssl/cert.pem",
		PrivateKeyPath:        "/etc/ssl/key.pem",
		CommandOutputs: []model.CommandOutput{
			{Command: "nginx -s reload", ExitCode: 0, Stdout: "ok", Stderr: ""},
		},
		ErrorMessage: "",
		StartedAt:    startedAt,
		FinishedAt:   finishedAt,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/agent/deployment-logs", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Verify deployment log was saved
	var logCount int
	err = db.QueryRow(`SELECT COUNT(*) FROM deployment_logs WHERE machine_certificate_id = ?`, "mc-log-1").Scan(&logCount)
	if err != nil {
		t.Fatalf("failed to count deployment logs: %v", err)
	}
	if logCount != 1 {
		t.Errorf("expected 1 deployment log, got %d", logCount)
	}

	// Verify machine_certificate deploy status was updated
	var deployStatus string
	var deployAt sql.NullString
	err = db.QueryRow(`SELECT last_deploy_status, last_deploy_at FROM machine_certificates WHERE id = ?`, "mc-log-1").Scan(&deployStatus, &deployAt)
	if err != nil {
		t.Fatalf("failed to query machine certificate: %v", err)
	}
	if deployStatus != "success" {
		t.Errorf("expected last_deploy_status 'success', got '%s'", deployStatus)
	}
	if !deployAt.Valid {
		t.Error("expected last_deploy_at to be set")
	}
}

func TestAgentHandler_CreateDeploymentLog_WrongMachine(t *testing.T) {
	_, r, db, token, _ := setupAgentHandler(t)

	now := time.Now().UTC().Format(time.RFC3339)

	// Insert a machine certificate config belonging to a DIFFERENT machine
	_, err := db.Exec(`INSERT INTO machine_certificates (id, machine_id, certificate_id, cert_path, private_key_path, post_deploy_commands, config_revision, last_deploy_status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"mc-log-other", "other-machine-id", "cert-1", "/etc/ssl/cert.pem", "/etc/ssl/key.pem", "", 1, "pending", now, now)
	if err != nil {
		t.Fatalf("failed to insert machine certificate: %v", err)
	}

	body := createDeploymentLogRequest{
		MachineCertificateID:  "mc-log-other",
		CertificateID:         "cert-1",
		Status:                "success",
		CertFingerprintSHA256: "fingerprint",
		CertPath:              "/etc/ssl/cert.pem",
		PrivateKeyPath:        "/etc/ssl/key.pem",
		StartedAt:             time.Now().UTC(),
		FinishedAt:            time.Now().UTC(),
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/agent/deployment-logs", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestAgentHandler_CreateDeploymentLog_InvalidStatus(t *testing.T) {
	_, r, _, token, _ := setupAgentHandler(t)

	body := createDeploymentLogRequest{
		MachineCertificateID:  "mc-1",
		CertificateID:         "cert-1",
		Status:                "invalid_status",
		CertFingerprintSHA256: "fingerprint",
		CertPath:              "/etc/ssl/cert.pem",
		PrivateKeyPath:        "/etc/ssl/key.pem",
		StartedAt:             time.Now().UTC(),
		FinishedAt:            time.Now().UTC(),
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/agent/deployment-logs", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestAgentHandler_CreateDeploymentLog_MissingFields(t *testing.T) {
	_, r, _, token, _ := setupAgentHandler(t)

	body := createDeploymentLogRequest{
		// Missing machine_certificate_id and status
		CertificateID: "cert-1",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/agent/deployment-logs", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestAgentHandler_CreateDeploymentLog_InvalidBody(t *testing.T) {
	_, r, _, token, _ := setupAgentHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/agent/deployment-logs", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestAgentHandler_CreateDeploymentLog_NotFound(t *testing.T) {
	_, r, _, token, _ := setupAgentHandler(t)

	body := createDeploymentLogRequest{
		MachineCertificateID:  "nonexistent-mc",
		CertificateID:         "cert-1",
		Status:                "success",
		CertFingerprintSHA256: "fingerprint",
		CertPath:              "/etc/ssl/cert.pem",
		PrivateKeyPath:        "/etc/ssl/key.pem",
		StartedAt:             time.Now().UTC(),
		FinishedAt:            time.Now().UTC(),
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/agent/deployment-logs", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestAgentHandler_RegisterRoutes(t *testing.T) {
	db := setupAgentTestDB(t)
	dataDir := t.TempDir()
	machineRepo := repository.NewMachineRepository(db)
	mcRepo := repository.NewMachineCertificateRepository(db)
	certRepo := repository.NewCertificateRepository(db, dataDir)
	deployLogRepo := repository.NewDeploymentLogRepository(db)

	cfg := &config.Config{
		Server: config.ServerConfig{
			ExternalURL: "https://ssl.example.com",
		},
	}
	machineService := service.NewMachineService(machineRepo, config.NewRuntimeConfig(cfg))
	mcService := service.NewMachineCertificateService(mcRepo)
	deployLogService := service.NewDeploymentLogService(deployLogRepo)

	handler := NewAgentHandler(machineService, mcService, deployLogService, certRepo, mcRepo, &mockAgentAlertSender{})

	mockRepo := &mockMachineRepo{machines: map[string]*model.Machine{}}

	r := chi.NewRouter()
	handler.RegisterRoutes(r, mockRepo, &mockAgentAlertSender{}, &mockAuditRepo{})

	// Verify routes are registered - no panic means success
	if r == nil {
		t.Fatal("router should not be nil")
	}
}
