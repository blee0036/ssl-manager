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
	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
	"github.com/ssl-manager/ssl-manager/internal/web/service"

	_ "github.com/glebarez/sqlite"
)

// setupMachineTestDB creates an in-memory SQLite database with the machines table.
func setupMachineTestDB(t *testing.T) *sql.DB {
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

	tables := []string{
		`CREATE TABLE certificates (
			id TEXT PRIMARY KEY
		)`,
		`CREATE TABLE machine_certificates (
			id TEXT PRIMARY KEY,
			machine_id TEXT NOT NULL,
			certificate_id TEXT NOT NULL
		)`,
		`CREATE TABLE deployment_logs (
			id TEXT PRIMARY KEY,
			machine_certificate_id TEXT NOT NULL,
			machine_id TEXT NOT NULL,
			certificate_id TEXT NOT NULL
		)`,
		`CREATE TABLE domains (
			id TEXT PRIMARY KEY,
			linked_machine_id TEXT DEFAULT '',
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

// setupMachineHandler creates a MachineHandler with test dependencies.
func setupMachineHandler(t *testing.T) (*MachineHandler, *chi.Mux, *sql.DB) {
	t.Helper()

	db := setupMachineTestDB(t)
	machineRepo := repository.NewMachineRepository(db)
	cfg := &config.Config{
		Server: config.ServerConfig{
			ExternalURL: "https://ssl.example.com",
		},
	}
	machineService := service.NewMachineService(machineRepo, config.NewRuntimeConfig(cfg))
	handler := NewMachineHandler(machineService)

	// Setup router without auth middleware for testing
	r := chi.NewRouter()
	r.Route("/api/machines", func(r chi.Router) {
		r.Get("/", handler.List)
		r.Post("/", handler.Create)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", handler.GetByID)
			r.Put("/", handler.Update)
			r.Delete("/", handler.Delete)
			r.Post("/revoke-token", handler.RevokeToken)
			r.Post("/regenerate-token", handler.RegenerateToken)
			r.Get("/install-command", handler.GetInstallCommand)
		})
	})

	return handler, r, db
}

// insertTestMachine inserts a test machine directly into the database.
func insertTestMachine(t *testing.T, db *sql.DB) *model.Machine {
	t.Helper()

	now := time.Now().UTC()
	machine := &model.Machine{
		ID:             "test-machine-id",
		Name:           "Test Machine",
		IP:             "192.168.1.100",
		Hostname:       "test-host",
		OS:             "linux",
		Arch:           "amd64",
		Tags:           []string{"web", "prod"},
		Remark:         "test machine",
		Status:         "pending",
		AgentVersion:   "",
		AgentTokenHash: service.HashToken("test-token-plaintext"),
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	tagsJSON, _ := json.Marshal(machine.Tags)
	_, err := db.Exec(`INSERT INTO machines (
		id, name, ip, hostname, os, arch, tags, remark, status,
		agent_version, agent_token_hash, agent_token_revoked_at,
		last_heartbeat_at, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		machine.ID, machine.Name, machine.IP, machine.Hostname,
		machine.OS, machine.Arch, string(tagsJSON), machine.Remark,
		machine.Status, machine.AgentVersion, machine.AgentTokenHash,
		nil, nil,
		machine.CreatedAt.Format(time.RFC3339),
		machine.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("failed to insert test machine: %v", err)
	}

	return machine
}

func TestMachineHandler_List_Empty(t *testing.T) {
	_, r, _ := setupMachineHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/machines", nil)
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

func TestMachineHandler_List_WithMachines(t *testing.T) {
	_, r, db := setupMachineHandler(t)
	insertTestMachine(t, db)

	req := httptest.NewRequest(http.MethodGet, "/api/machines", nil)
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
		t.Errorf("expected 1 machine, got %d", len(data))
	}
}

func TestMachineHandler_List_WithFilter(t *testing.T) {
	_, r, db := setupMachineHandler(t)
	insertTestMachine(t, db)

	// Filter by status that doesn't match
	req := httptest.NewRequest(http.MethodGet, "/api/machines?status=online", nil)
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
		t.Errorf("expected 0 machines for status=online, got %d", len(data))
	}
}

func TestMachineHandler_Create_Success(t *testing.T) {
	_, r, _ := setupMachineHandler(t)

	body := map[string]interface{}{
		"name":   "New Machine",
		"ip":     "10.0.0.1",
		"tags":   []string{"staging"},
		"remark": "staging server",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/machines", bytes.NewReader(bodyBytes))
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

	// Should contain machine and agent_token
	if _, exists := data["machine"]; !exists {
		t.Error("response should contain 'machine' field")
	}
	if _, exists := data["agent_token"]; !exists {
		t.Error("response should contain 'agent_token' field")
	}

	// Token should be a non-empty string
	token, ok := data["agent_token"].(string)
	if !ok || token == "" {
		t.Error("agent_token should be a non-empty string")
	}
}

func TestMachineHandler_Create_MissingName(t *testing.T) {
	_, r, _ := setupMachineHandler(t)

	body := map[string]interface{}{
		"ip": "10.0.0.1",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/machines", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestMachineHandler_Create_MissingIP(t *testing.T) {
	_, r, _ := setupMachineHandler(t)

	body := map[string]interface{}{
		"name": "Machine",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/machines", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestMachineHandler_Create_InvalidBody(t *testing.T) {
	_, r, _ := setupMachineHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/machines", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestMachineHandler_GetByID_Success(t *testing.T) {
	_, r, db := setupMachineHandler(t)
	machine := insertTestMachine(t, db)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/"+machine.ID, nil)
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

	if data["id"] != machine.ID {
		t.Errorf("expected id %s, got %v", machine.ID, data["id"])
	}
	if data["name"] != machine.Name {
		t.Errorf("expected name %s, got %v", machine.Name, data["name"])
	}
}

func TestMachineHandler_GetByID_NotFound(t *testing.T) {
	_, r, _ := setupMachineHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestMachineHandler_Update_Success(t *testing.T) {
	_, r, db := setupMachineHandler(t)
	machine := insertTestMachine(t, db)

	newName := "Updated Machine"
	body := map[string]interface{}{
		"name": newName,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/machines/"+machine.ID, bytes.NewReader(bodyBytes))
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

	if data["name"] != newName {
		t.Errorf("expected name %s, got %v", newName, data["name"])
	}
}

func TestMachineHandler_Update_NotFound(t *testing.T) {
	_, r, _ := setupMachineHandler(t)

	body := map[string]interface{}{"name": "Updated"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/machines/nonexistent", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestMachineHandler_Delete_Success(t *testing.T) {
	_, r, db := setupMachineHandler(t)
	machine := insertTestMachine(t, db)

	req := httptest.NewRequest(http.MethodDelete, "/api/machines/"+machine.ID, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Verify machine is deleted
	req = httptest.NewRequest(http.MethodGet, "/api/machines/"+machine.ID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404 after delete, got %d", w.Code)
	}
}

func TestMachineHandler_Delete_NotFound(t *testing.T) {
	_, r, _ := setupMachineHandler(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/machines/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestMachineHandler_RevokeToken_Success(t *testing.T) {
	_, r, db := setupMachineHandler(t)
	machine := insertTestMachine(t, db)

	req := httptest.NewRequest(http.MethodPost, "/api/machines/"+machine.ID+"/revoke-token", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Verify machine status is now revoked
	var status string
	err := db.QueryRow("SELECT status FROM machines WHERE id = ?", machine.ID).Scan(&status)
	if err != nil {
		t.Fatalf("failed to query machine status: %v", err)
	}
	if status != "revoked" {
		t.Errorf("expected status 'revoked', got '%s'", status)
	}
}

func TestMachineHandler_RevokeToken_NotFound(t *testing.T) {
	_, r, _ := setupMachineHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/machines/nonexistent/revoke-token", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestMachineHandler_RegenerateToken_Success(t *testing.T) {
	_, r, db := setupMachineHandler(t)
	machine := insertTestMachine(t, db)

	req := httptest.NewRequest(http.MethodPost, "/api/machines/"+machine.ID+"/regenerate-token", nil)
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

	token, ok := data["agent_token"].(string)
	if !ok || token == "" {
		t.Error("agent_token should be a non-empty string")
	}

	// Verify the token hash was updated in the database
	var tokenHash string
	err := db.QueryRow("SELECT agent_token_hash FROM machines WHERE id = ?", machine.ID).Scan(&tokenHash)
	if err != nil {
		t.Fatalf("failed to query token hash: %v", err)
	}

	expectedHash := service.HashToken(token)
	if tokenHash != expectedHash {
		t.Errorf("stored token hash doesn't match the returned token")
	}
}

func TestMachineHandler_RegenerateToken_NotFound(t *testing.T) {
	_, r, _ := setupMachineHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/machines/nonexistent/regenerate-token", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestMachineHandler_GetInstallCommand_Success(t *testing.T) {
	_, r, db := setupMachineHandler(t)
	machine := insertTestMachine(t, db)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/"+machine.ID+"/install-command", nil)
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

	cmd, ok := data["install_command"].(string)
	if !ok || cmd == "" {
		t.Error("install_command should be a non-empty string")
	}

	// Verify the install command contains required components
	if !containsSubstring(cmd, "https://ssl.example.com") {
		t.Error("install command should contain the external URL")
	}
	if !containsSubstring(cmd, machine.ID) {
		t.Error("install command should contain the machine ID")
	}

	// The install command now uses a placeholder for the token
	// Users must call POST /regenerate-token to get a fresh token
	if !containsSubstring(cmd, "<AGENT_TOKEN>") {
		t.Error("install command should contain the <AGENT_TOKEN> placeholder")
	}

	// Verify note field is present
	note, ok := data["note"].(string)
	if !ok || note == "" {
		t.Error("note should be a non-empty string")
	}
}

func TestMachineHandler_GetInstallCommand_NotFound(t *testing.T) {
	_, r, _ := setupMachineHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/nonexistent/install-command", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestMachineHandler_RegisterRoutes(t *testing.T) {
	db := setupMachineTestDB(t)
	machineRepo := repository.NewMachineRepository(db)
	cfg := &config.Config{
		Server: config.ServerConfig{
			ExternalURL: "https://ssl.example.com",
		},
	}
	machineService := service.NewMachineService(machineRepo, config.NewRuntimeConfig(cfg))
	handler := NewMachineHandler(machineService)

	r := chi.NewRouter()
	authSvc := &mockAuthService{}
	handler.RegisterRoutes(r, authSvc, &mockAuditRepo{})

	// Verify routes are registered by walking the router - no panic means success
	if r == nil {
		t.Fatal("router should not be nil")
	}
}

func TestMachineHandler_AgentTokenNotExposedInGetByID(t *testing.T) {
	_, r, db := setupMachineHandler(t)
	insertTestMachine(t, db)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/test-machine-id", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	// Verify the response does not contain agent_token_hash
	var rawResp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &rawResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	data, ok := rawResp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be a map, got %T", rawResp["data"])
	}

	// agent_token_hash should not be in the response (json:"-" tag)
	if _, exists := data["agent_token_hash"]; exists {
		t.Error("response should NOT contain 'agent_token_hash' field")
	}
}

// containsSubstring checks if s contains substr.
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
