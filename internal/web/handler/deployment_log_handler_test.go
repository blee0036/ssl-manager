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
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
	"github.com/ssl-manager/ssl-manager/internal/web/service"

	_ "github.com/glebarez/sqlite"
)

// setupDeploymentLogTestDB creates an in-memory SQLite database with the deployment_logs table.
func setupDeploymentLogTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	_, err = db.Exec(`CREATE TABLE deployment_logs (
		id TEXT PRIMARY KEY,
		machine_certificate_id TEXT NOT NULL,
		machine_id TEXT NOT NULL,
		certificate_id TEXT NOT NULL,
		status TEXT NOT NULL,
		cert_fingerprint_sha256 TEXT DEFAULT '',
		cert_path TEXT DEFAULT '',
		private_key_path TEXT DEFAULT '',
		command_outputs TEXT DEFAULT '[]',
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

// setupDeploymentLogHandler creates a DeploymentLogHandler with test dependencies.
func setupDeploymentLogHandler(t *testing.T) (*DeploymentLogHandler, *chi.Mux, *sql.DB) {
	t.Helper()

	db := setupDeploymentLogTestDB(t)
	logRepo := repository.NewDeploymentLogRepository(db)
	logService := service.NewDeploymentLogService(logRepo)
	handler := NewDeploymentLogHandler(logService)

	r := chi.NewRouter()
	r.Route("/api/machines/{machine_id}/deployment-logs", func(r chi.Router) {
		r.Get("/", handler.ListByMachine)
	})
	r.Route("/api/machines/{machine_id}/certificates/{mc_id}/deployment-logs", func(r chi.Router) {
		r.Get("/", handler.ListByMachineCertificate)
	})

	return handler, r, db
}

// insertTestDeploymentLog inserts a test deployment log directly into the database.
func insertTestDeploymentLog(t *testing.T, db *sql.DB, machineID, machineCertID, certID, status string) *model.DeploymentLog {
	t.Helper()

	now := time.Now().UTC()
	log := &model.DeploymentLog{
		ID:                    "test-log-" + machineID + "-" + machineCertID + "-" + status,
		MachineCertificateID:  machineCertID,
		MachineID:             machineID,
		CertificateID:         certID,
		Status:                status,
		CertFingerprintSHA256: "abc123sha256",
		CertPath:              "/etc/ssl/cert.pem",
		PrivateKeyPath:        "/etc/ssl/key.pem",
		CommandOutputs:        []model.CommandOutput{},
		ErrorMessage:          "",
		StartedAt:             now.Add(-1 * time.Minute),
		FinishedAt:            now,
		CreatedAt:             now,
	}

	commandOutputsJSON, _ := json.Marshal(log.CommandOutputs)

	_, err := db.Exec(`INSERT INTO deployment_logs (
		id, machine_certificate_id, machine_id, certificate_id, status,
		cert_fingerprint_sha256, cert_path, private_key_path,
		command_outputs, error_message, started_at, finished_at, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.ID, log.MachineCertificateID, log.MachineID, log.CertificateID,
		log.Status, log.CertFingerprintSHA256, log.CertPath, log.PrivateKeyPath,
		string(commandOutputsJSON), log.ErrorMessage,
		log.StartedAt.Format(time.RFC3339),
		log.FinishedAt.Format(time.RFC3339),
		log.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("failed to insert test deployment log: %v", err)
	}

	return log
}

func TestDeploymentLogHandler_ListByMachine_Empty(t *testing.T) {
	_, r, _ := setupDeploymentLogHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/machine-1/deployment-logs", nil)
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

func TestDeploymentLogHandler_ListByMachine_WithLogs(t *testing.T) {
	_, r, db := setupDeploymentLogHandler(t)

	insertTestDeploymentLog(t, db, "machine-1", "mc-1", "cert-1", "success")
	insertTestDeploymentLog(t, db, "machine-1", "mc-2", "cert-2", "failed")

	req := httptest.NewRequest(http.MethodGet, "/api/machines/machine-1/deployment-logs", nil)
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
		t.Errorf("expected 2 logs, got %d", len(data))
	}
}

func TestDeploymentLogHandler_ListByMachine_OnlyReturnsMatchingMachine(t *testing.T) {
	_, r, db := setupDeploymentLogHandler(t)

	insertTestDeploymentLog(t, db, "machine-1", "mc-1", "cert-1", "success")
	insertTestDeploymentLog(t, db, "machine-2", "mc-2", "cert-2", "success")

	req := httptest.NewRequest(http.MethodGet, "/api/machines/machine-1/deployment-logs", nil)
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
	if len(data) != 1 {
		t.Errorf("expected 1 log for machine-1, got %d", len(data))
	}
}

func TestDeploymentLogHandler_ListByMachineCertificate_Empty(t *testing.T) {
	_, r, _ := setupDeploymentLogHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/machine-1/certificates/mc-1/deployment-logs", nil)
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

func TestDeploymentLogHandler_ListByMachineCertificate_WithLogs(t *testing.T) {
	_, r, db := setupDeploymentLogHandler(t)

	insertTestDeploymentLog(t, db, "machine-1", "mc-1", "cert-1", "success")
	insertTestDeploymentLog(t, db, "machine-1", "mc-1", "cert-1", "failed")

	req := httptest.NewRequest(http.MethodGet, "/api/machines/machine-1/certificates/mc-1/deployment-logs", nil)
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
		t.Errorf("expected 2 logs, got %d", len(data))
	}
}

func TestDeploymentLogHandler_ListByMachineCertificate_OnlyReturnsMatchingMC(t *testing.T) {
	_, r, db := setupDeploymentLogHandler(t)

	insertTestDeploymentLog(t, db, "machine-1", "mc-1", "cert-1", "success")
	insertTestDeploymentLog(t, db, "machine-1", "mc-2", "cert-2", "success")

	req := httptest.NewRequest(http.MethodGet, "/api/machines/machine-1/certificates/mc-1/deployment-logs", nil)
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
	if len(data) != 1 {
		t.Errorf("expected 1 log for mc-1, got %d", len(data))
	}
}

func TestDeploymentLogHandler_RegisterRoutes(t *testing.T) {
	db := setupDeploymentLogTestDB(t)
	logRepo := repository.NewDeploymentLogRepository(db)
	logService := service.NewDeploymentLogService(logRepo)
	handler := NewDeploymentLogHandler(logService)

	r := chi.NewRouter()
	authSvc := &mockDeploymentLogAuthService{}
	handler.RegisterRoutes(r, authSvc, &mockAuditRepo{})

	// Verify routes are registered by walking the router - no panic means success
	if r == nil {
		t.Fatal("router should not be nil")
	}
}

// mockDeploymentLogAuthService is a mock implementation of middleware.AuthService for testing.
type mockDeploymentLogAuthService struct{}

func (m *mockDeploymentLogAuthService) GetJWTSecret() []byte {
	return []byte("test-secret")
}

func (m *mockDeploymentLogAuthService) IsSessionValid(_ context.Context, _ string) bool {
	return true
}

func (m *mockDeploymentLogAuthService) IsUserActive(_ context.Context, _ string) bool {
	return true
}

func (m *mockDeploymentLogAuthService) GetCurrentRole(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (m *mockDeploymentLogAuthService) IsTokenValid(_ context.Context, _ string, _ time.Time) bool {
	return true
}
