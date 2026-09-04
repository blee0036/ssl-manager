package handler

import (
	"bytes"
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

// setupMCTestDB creates an in-memory SQLite database with the machine_certificates table.
func setupMCTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
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

	tables := []string{
		`CREATE TABLE deployment_logs (
			id TEXT PRIMARY KEY,
			machine_certificate_id TEXT NOT NULL,
			machine_id TEXT NOT NULL,
			certificate_id TEXT NOT NULL
		)`,
		`CREATE TABLE domains (
			id TEXT PRIMARY KEY,
			linked_machine_certificate_id TEXT DEFAULT ''
		)`,
	}
	for _, stmt := range tables {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("failed to create related test table: %v", err)
		}
	}

	t.Cleanup(func() { db.Close() })
	return db
}

// setupMCHandler creates a MachineCertificateHandler with test dependencies.
func setupMCHandler(t *testing.T) (*MachineCertificateHandler, *chi.Mux, *sql.DB) {
	t.Helper()

	db := setupMCTestDB(t)
	mcRepo := repository.NewMachineCertificateRepository(db)
	mcService := service.NewMachineCertificateService(mcRepo)
	handler := NewMachineCertificateHandler(mcService)

	r := chi.NewRouter()
	r.Route("/api/machines/{machine_id}/certificates", func(r chi.Router) {
		r.Get("/", handler.List)
		r.Post("/", handler.Create)
		r.Route("/{mc_id}", func(r chi.Router) {
			r.Put("/", handler.Update)
			r.Delete("/", handler.Delete)
			r.Post("/deploy", handler.TriggerDeploy)
		})
	})

	return handler, r, db
}

// insertTestMC inserts a test machine certificate directly into the database.
func insertTestMC(t *testing.T, db *sql.DB, machineID, certID string) *model.MachineCertificate {
	t.Helper()

	now := time.Now().UTC()
	mc := &model.MachineCertificate{
		ID:                 "test-mc-id",
		MachineID:          machineID,
		CertificateID:      certID,
		CertPath:           "/etc/ssl/cert.pem",
		PrivateKeyPath:     "/etc/ssl/privkey.pem",
		PostDeployCommands: "systemctl reload nginx",
		ConfigRevision:     1,
		LastDeployStatus:   "pending",
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	_, err := db.Exec(`INSERT INTO machine_certificates (
		id, machine_id, certificate_id, cert_path, private_key_path,
		post_deploy_commands, config_revision, last_deploy_status,
		last_deploy_at, last_deploy_message, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		mc.ID, mc.MachineID, mc.CertificateID, mc.CertPath, mc.PrivateKeyPath,
		mc.PostDeployCommands, mc.ConfigRevision, mc.LastDeployStatus,
		nil, "",
		mc.CreatedAt.Format(time.RFC3339),
		mc.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("failed to insert test machine certificate: %v", err)
	}

	return mc
}

func TestMachineCertificateHandler_List_Empty(t *testing.T) {
	_, r, _ := setupMCHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/machine-1/certificates", nil)
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

func TestMachineCertificateHandler_List_WithConfigs(t *testing.T) {
	_, r, db := setupMCHandler(t)
	insertTestMC(t, db, "machine-1", "cert-1")

	req := httptest.NewRequest(http.MethodGet, "/api/machines/machine-1/certificates", nil)
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
		t.Errorf("expected 1 config, got %d", len(data))
	}
}

func TestMachineCertificateHandler_List_FiltersByMachineID(t *testing.T) {
	_, r, db := setupMCHandler(t)
	insertTestMC(t, db, "machine-1", "cert-1")

	// Query for a different machine should return empty
	req := httptest.NewRequest(http.MethodGet, "/api/machines/machine-2/certificates", nil)
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
		t.Errorf("expected 0 configs for machine-2, got %d", len(data))
	}
}

func TestMachineCertificateHandler_Create_Success(t *testing.T) {
	_, r, _ := setupMCHandler(t)

	body := map[string]interface{}{
		"certificate_id":       "cert-1",
		"cert_path":            "/etc/ssl/cert.pem",
		"private_key_path":     "/etc/ssl/privkey.pem",
		"post_deploy_commands": "systemctl reload nginx",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/machines/machine-1/certificates", bytes.NewReader(bodyBytes))
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

	if data["machine_id"] != "machine-1" {
		t.Errorf("expected machine_id 'machine-1', got %v", data["machine_id"])
	}
	if data["certificate_id"] != "cert-1" {
		t.Errorf("expected certificate_id 'cert-1', got %v", data["certificate_id"])
	}
	if data["cert_path"] != "/etc/ssl/cert.pem" {
		t.Errorf("expected cert_path '/etc/ssl/cert.pem', got %v", data["cert_path"])
	}
	if data["config_revision"] != float64(1) {
		t.Errorf("expected config_revision 1, got %v", data["config_revision"])
	}
}

func TestMachineCertificateHandler_Create_EmptyCertPath(t *testing.T) {
	_, r, _ := setupMCHandler(t)

	body := map[string]interface{}{
		"certificate_id":   "cert-1",
		"cert_path":        "",
		"private_key_path": "/etc/ssl/privkey.pem",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/machines/machine-1/certificates", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestMachineCertificateHandler_Create_EmptyPrivateKeyPath(t *testing.T) {
	_, r, _ := setupMCHandler(t)

	body := map[string]interface{}{
		"certificate_id":   "cert-1",
		"cert_path":        "/etc/ssl/cert.pem",
		"private_key_path": "  ",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/machines/machine-1/certificates", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestMachineCertificateHandler_Create_InvalidBody(t *testing.T) {
	_, r, _ := setupMCHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/machines/machine-1/certificates", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestMachineCertificateHandler_Update_Success(t *testing.T) {
	_, r, db := setupMCHandler(t)
	mc := insertTestMC(t, db, "machine-1", "cert-1")

	newPath := "/etc/ssl/new-cert.pem"
	body := map[string]interface{}{
		"cert_path": newPath,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/machines/machine-1/certificates/"+mc.ID, bytes.NewReader(bodyBytes))
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

	if data["cert_path"] != newPath {
		t.Errorf("expected cert_path '%s', got %v", newPath, data["cert_path"])
	}
	// config_revision should be incremented
	if data["config_revision"] != float64(2) {
		t.Errorf("expected config_revision 2, got %v", data["config_revision"])
	}
	// last_deploy_status should be pending after update
	if data["last_deploy_status"] != "pending" {
		t.Errorf("expected last_deploy_status 'pending', got %v", data["last_deploy_status"])
	}
}

func TestMachineCertificateHandler_Update_NotFound(t *testing.T) {
	_, r, _ := setupMCHandler(t)

	body := map[string]interface{}{
		"cert_path": "/new/path",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/machines/machine-1/certificates/nonexistent", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestMachineCertificateHandler_Update_EmptyCertPath(t *testing.T) {
	_, r, db := setupMCHandler(t)
	mc := insertTestMC(t, db, "machine-1", "cert-1")

	body := map[string]interface{}{
		"cert_path": "",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/machines/machine-1/certificates/"+mc.ID, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestMachineCertificateHandler_Update_InvalidBody(t *testing.T) {
	_, r, db := setupMCHandler(t)
	mc := insertTestMC(t, db, "machine-1", "cert-1")

	req := httptest.NewRequest(http.MethodPut, "/api/machines/machine-1/certificates/"+mc.ID, bytes.NewReader([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestMachineCertificateHandler_Delete_Success(t *testing.T) {
	_, r, db := setupMCHandler(t)
	mc := insertTestMC(t, db, "machine-1", "cert-1")

	req := httptest.NewRequest(http.MethodDelete, "/api/machines/machine-1/certificates/"+mc.ID, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Verify it's deleted - list should be empty
	req = httptest.NewRequest(http.MethodGet, "/api/machines/machine-1/certificates", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	data, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("expected data to be an array, got %T", resp.Data)
	}
	if len(data) != 0 {
		t.Errorf("expected 0 configs after delete, got %d", len(data))
	}
}

func TestMachineCertificateHandler_Delete_NotFound(t *testing.T) {
	_, r, _ := setupMCHandler(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/machines/machine-1/certificates/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestMachineCertificateHandler_TriggerDeploy_Success(t *testing.T) {
	_, r, db := setupMCHandler(t)
	mc := insertTestMC(t, db, "machine-1", "cert-1")

	req := httptest.NewRequest(http.MethodPost, "/api/machines/machine-1/certificates/"+mc.ID+"/deploy", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Verify config_revision was incremented and status is pending
	var revision int
	var status string
	err := db.QueryRow("SELECT config_revision, last_deploy_status FROM machine_certificates WHERE id = ?", mc.ID).Scan(&revision, &status)
	if err != nil {
		t.Fatalf("failed to query machine certificate: %v", err)
	}
	if revision != 2 {
		t.Errorf("expected config_revision 2 after trigger, got %d", revision)
	}
	if status != "pending" {
		t.Errorf("expected last_deploy_status 'pending', got '%s'", status)
	}
}

func TestMachineCertificateHandler_TriggerDeploy_NotFound(t *testing.T) {
	_, r, _ := setupMCHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/machines/machine-1/certificates/nonexistent/deploy", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestMachineCertificateHandler_RegisterRoutes(t *testing.T) {
	db := setupMCTestDB(t)
	mcRepo := repository.NewMachineCertificateRepository(db)
	mcService := service.NewMachineCertificateService(mcRepo)
	handler := NewMachineCertificateHandler(mcService)

	r := chi.NewRouter()
	authSvc := &mockAuthService{}
	handler.RegisterRoutes(r, authSvc, &mockAuditRepo{})

	// Verify routes are registered - no panic means success
	if r == nil {
		t.Fatal("router should not be nil")
	}
}
