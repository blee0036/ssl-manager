package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/glebarez/sqlite"
	"github.com/ssl-manager/ssl-manager/internal/model"
)

const createMachinesTable = `CREATE TABLE IF NOT EXISTS machines (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	ip TEXT NOT NULL,
	hostname TEXT DEFAULT '',
	os TEXT DEFAULT '',
	arch TEXT DEFAULT '',
	tags TEXT DEFAULT '',
	remark TEXT DEFAULT '',
	status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'online', 'offline', 'revoked', 'disabled')),
	agent_version TEXT DEFAULT '',
	agent_token_hash TEXT NOT NULL,
	agent_token_revoked_at TEXT,
	last_heartbeat_at TEXT,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
)`

func setupMachineTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("failed to enable foreign keys: %v", err)
	}
	tables := []string{
		createMachinesTable,
		`CREATE TABLE certificates (
			id TEXT PRIMARY KEY
		)`,
		`CREATE TABLE machine_certificates (
			id TEXT PRIMARY KEY,
			machine_id TEXT NOT NULL REFERENCES machines(id),
			certificate_id TEXT NOT NULL REFERENCES certificates(id)
		)`,
		`CREATE TABLE deployment_logs (
			id TEXT PRIMARY KEY,
			machine_certificate_id TEXT NOT NULL REFERENCES machine_certificates(id),
			machine_id TEXT NOT NULL,
			certificate_id TEXT NOT NULL
		)`,
		`CREATE TABLE domains (
			id TEXT PRIMARY KEY,
			linked_machine_id TEXT DEFAULT '',
			linked_certificate_id TEXT DEFAULT '',
			linked_machine_certificate_id TEXT DEFAULT ''
		)`,
	}
	for _, stmt := range tables {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("failed to create test table: %v", err)
		}
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestMachineRepository_Create(t *testing.T) {
	db := setupMachineTestDB(t)
	repo := NewMachineRepository(db)
	ctx := context.Background()

	machine := &model.Machine{
		Name:           "test-machine",
		IP:             "192.168.1.100",
		Tags:           []string{"web", "prod"},
		Remark:         "test remark",
		AgentTokenHash: "hashed_token_value",
	}

	err := repo.Create(ctx, machine)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if machine.ID == "" {
		t.Error("expected ID to be generated")
	}
	if machine.Status != "pending" {
		t.Errorf("expected status=pending, got %q", machine.Status)
	}
	if machine.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestMachineRepository_GetByID(t *testing.T) {
	db := setupMachineTestDB(t)
	repo := NewMachineRepository(db)
	ctx := context.Background()

	machine := &model.Machine{
		Name:           "get-test",
		IP:             "10.0.0.1",
		Tags:           []string{"staging"},
		AgentTokenHash: "hash123",
	}
	if err := repo.Create(ctx, machine); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := repo.GetByID(ctx, machine.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if got.Name != "get-test" {
		t.Errorf("expected name=get-test, got %q", got.Name)
	}
	if got.IP != "10.0.0.1" {
		t.Errorf("expected ip=10.0.0.1, got %q", got.IP)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "staging" {
		t.Errorf("expected tags=[staging], got %v", got.Tags)
	}
}

func TestMachineRepository_GetByID_NotFound(t *testing.T) {
	db := setupMachineTestDB(t)
	repo := NewMachineRepository(db)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, "nonexistent-id")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestMachineRepository_List(t *testing.T) {
	db := setupMachineTestDB(t)
	repo := NewMachineRepository(db)
	ctx := context.Background()

	// Create multiple machines
	machines := []*model.Machine{
		{Name: "web-1", IP: "10.0.0.1", Status: "online", AgentTokenHash: "h1"},
		{Name: "web-2", IP: "10.0.0.2", Status: "online", AgentTokenHash: "h2"},
		{Name: "db-1", IP: "10.0.0.3", Status: "offline", AgentTokenHash: "h3"},
	}
	for _, m := range machines {
		if err := repo.Create(ctx, m); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	// List all
	all, err := repo.List(ctx, model.MachineFilter{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 machines, got %d", len(all))
	}

	// Filter by status
	online, err := repo.List(ctx, model.MachineFilter{Status: "online"})
	if err != nil {
		t.Fatalf("List with status filter failed: %v", err)
	}
	if len(online) != 2 {
		t.Errorf("expected 2 online machines, got %d", len(online))
	}

	// Filter by search
	searched, err := repo.List(ctx, model.MachineFilter{Search: "web"})
	if err != nil {
		t.Fatalf("List with search filter failed: %v", err)
	}
	if len(searched) != 2 {
		t.Errorf("expected 2 machines matching 'web', got %d", len(searched))
	}
}

func TestMachineRepository_Update(t *testing.T) {
	db := setupMachineTestDB(t)
	repo := NewMachineRepository(db)
	ctx := context.Background()

	machine := &model.Machine{
		Name:           "update-test",
		IP:             "10.0.0.1",
		AgentTokenHash: "hash",
	}
	if err := repo.Create(ctx, machine); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	err := repo.Update(ctx, machine.ID, map[string]interface{}{
		"name": "updated-name",
		"ip":   "10.0.0.99",
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	got, err := repo.GetByID(ctx, machine.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.Name != "updated-name" {
		t.Errorf("expected name=updated-name, got %q", got.Name)
	}
	if got.IP != "10.0.0.99" {
		t.Errorf("expected ip=10.0.0.99, got %q", got.IP)
	}
}

func TestMachineRepository_Update_NotFound(t *testing.T) {
	db := setupMachineTestDB(t)
	repo := NewMachineRepository(db)
	ctx := context.Background()

	err := repo.Update(ctx, "nonexistent", map[string]interface{}{"name": "x"})
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestMachineRepository_Delete(t *testing.T) {
	db := setupMachineTestDB(t)
	repo := NewMachineRepository(db)
	ctx := context.Background()

	machine := &model.Machine{
		Name:           "delete-test",
		IP:             "10.0.0.1",
		AgentTokenHash: "hash",
	}
	if err := repo.Create(ctx, machine); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	err := repo.Delete(ctx, machine.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err = repo.GetByID(ctx, machine.ID)
	if err != sql.ErrNoRows {
		t.Errorf("expected machine to be deleted, got err=%v", err)
	}
}

func TestMachineRepository_Delete_CascadesDeploymentData(t *testing.T) {
	db := setupMachineTestDB(t)
	repo := NewMachineRepository(db)
	ctx := context.Background()

	machine := &model.Machine{
		Name:           "delete-with-deployment-data",
		IP:             "10.0.0.1",
		AgentTokenHash: "hash",
	}
	if err := repo.Create(ctx, machine); err != nil {
		t.Fatalf("Create machine failed: %v", err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO certificates (id) VALUES (?)", "cert-1"); err != nil {
		t.Fatalf("failed to insert certificate: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO machine_certificates (id, machine_id, certificate_id) VALUES (?, ?, ?)",
		"mc-1", machine.ID, "cert-1",
	); err != nil {
		t.Fatalf("failed to insert machine certificate: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO deployment_logs (id, machine_certificate_id, machine_id, certificate_id) VALUES (?, ?, ?, ?)",
		"log-1", "mc-1", machine.ID, "cert-1",
	); err != nil {
		t.Fatalf("failed to insert deployment log: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO domains (
			id, linked_machine_id, linked_certificate_id, linked_machine_certificate_id
		) VALUES (?, ?, ?, ?)`,
		"domain-1", machine.ID, "cert-keep", "mc-1",
	); err != nil {
		t.Fatalf("failed to insert domain: %v", err)
	}

	if err := repo.Delete(ctx, machine.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	for _, table := range []string{"machines", "machine_certificates", "deployment_logs"} {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("failed to count %s: %v", table, err)
		}
		if count != 0 {
			t.Errorf("expected %s to be empty after deletion, got %d row(s)", table, count)
		}
	}

	var linkedMachineID, linkedCertificateID, linkedMachineCertificateID string
	if err := db.QueryRowContext(ctx,
		"SELECT linked_machine_id, linked_certificate_id, linked_machine_certificate_id FROM domains WHERE id = ?",
		"domain-1",
	).Scan(&linkedMachineID, &linkedCertificateID, &linkedMachineCertificateID); err != nil {
		t.Fatalf("failed to query domain links: %v", err)
	}
	if linkedMachineID != "" || linkedMachineCertificateID != "" || linkedCertificateID != "cert-keep" {
		t.Errorf("expected machine links cleared and certificate link preserved, got machine=%q certificate=%q machine_certificate=%q", linkedMachineID, linkedCertificateID, linkedMachineCertificateID)
	}
}

func TestMachineRepository_Delete_NotFound(t *testing.T) {
	db := setupMachineTestDB(t)
	repo := NewMachineRepository(db)
	ctx := context.Background()

	err := repo.Delete(ctx, "nonexistent")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestMachineRepository_UpdateHeartbeat(t *testing.T) {
	db := setupMachineTestDB(t)
	repo := NewMachineRepository(db)
	ctx := context.Background()

	machine := &model.Machine{
		Name:           "heartbeat-test",
		IP:             "10.0.0.1",
		AgentTokenHash: "hash",
	}
	if err := repo.Create(ctx, machine); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	info := model.HeartbeatInfo{
		AgentVersion: "1.0.0",
		Hostname:     "server-01",
		IP:           "10.0.0.1",
		OS:           "linux",
		Arch:         "amd64",
	}
	err := repo.UpdateHeartbeat(ctx, machine.ID, info)
	if err != nil {
		t.Fatalf("UpdateHeartbeat failed: %v", err)
	}

	got, err := repo.GetByID(ctx, machine.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.Status != "online" {
		t.Errorf("expected status=online, got %q", got.Status)
	}
	if got.AgentVersion != "1.0.0" {
		t.Errorf("expected agent_version=1.0.0, got %q", got.AgentVersion)
	}
	if got.Hostname != "server-01" {
		t.Errorf("expected hostname=server-01, got %q", got.Hostname)
	}
	if got.LastHeartbeatAt == nil {
		t.Error("expected last_heartbeat_at to be set")
	}
}

func TestMachineRepository_UpdateTokenHash(t *testing.T) {
	db := setupMachineTestDB(t)
	repo := NewMachineRepository(db)
	ctx := context.Background()

	machine := &model.Machine{
		Name:           "token-test",
		IP:             "10.0.0.1",
		AgentTokenHash: "old_hash",
	}
	if err := repo.Create(ctx, machine); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	err := repo.UpdateTokenHash(ctx, machine.ID, "new_hash")
	if err != nil {
		t.Fatalf("UpdateTokenHash failed: %v", err)
	}

	got, err := repo.GetByID(ctx, machine.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.AgentTokenHash != "new_hash" {
		t.Errorf("expected token hash=new_hash, got %q", got.AgentTokenHash)
	}
}

func TestMachineRepository_RevokeToken(t *testing.T) {
	db := setupMachineTestDB(t)
	repo := NewMachineRepository(db)
	ctx := context.Background()

	machine := &model.Machine{
		Name:           "revoke-test",
		IP:             "10.0.0.1",
		Status:         "online",
		AgentTokenHash: "hash",
	}
	if err := repo.Create(ctx, machine); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	err := repo.RevokeToken(ctx, machine.ID)
	if err != nil {
		t.Fatalf("RevokeToken failed: %v", err)
	}

	got, err := repo.GetByID(ctx, machine.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.Status != "revoked" {
		t.Errorf("expected status=revoked, got %q", got.Status)
	}
	if got.AgentTokenRevokedAt == nil {
		t.Error("expected agent_token_revoked_at to be set")
	}
}

func TestMachineRepository_GetByTokenHash(t *testing.T) {
	db := setupMachineTestDB(t)
	repo := NewMachineRepository(db)
	ctx := context.Background()

	machine := &model.Machine{
		Name:           "token-lookup",
		IP:             "10.0.0.1",
		AgentTokenHash: "unique_hash_123",
	}
	if err := repo.Create(ctx, machine); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := repo.GetByTokenHash(ctx, "unique_hash_123")
	if err != nil {
		t.Fatalf("GetByTokenHash failed: %v", err)
	}
	if got.ID != machine.ID {
		t.Errorf("expected id=%s, got %s", machine.ID, got.ID)
	}
}

func TestMachineRepository_GetByTokenHash_RevokedNotFound(t *testing.T) {
	db := setupMachineTestDB(t)
	repo := NewMachineRepository(db)
	ctx := context.Background()

	machine := &model.Machine{
		Name:           "revoked-lookup",
		IP:             "10.0.0.1",
		AgentTokenHash: "revoked_hash",
	}
	if err := repo.Create(ctx, machine); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Revoke the token
	if err := repo.RevokeToken(ctx, machine.ID); err != nil {
		t.Fatalf("RevokeToken failed: %v", err)
	}

	// Should not find revoked token
	_, err := repo.GetByTokenHash(ctx, "revoked_hash")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows for revoked token, got %v", err)
	}
}

func TestMachineRepository_UpdateStatus(t *testing.T) {
	db := setupMachineTestDB(t)
	repo := NewMachineRepository(db)
	ctx := context.Background()

	machine := &model.Machine{
		Name:           "status-test",
		IP:             "10.0.0.1",
		AgentTokenHash: "hash",
	}
	if err := repo.Create(ctx, machine); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	err := repo.UpdateStatus(ctx, machine.ID, "disabled")
	if err != nil {
		t.Fatalf("UpdateStatus failed: %v", err)
	}

	got, err := repo.GetByID(ctx, machine.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.Status != "disabled" {
		t.Errorf("expected status=disabled, got %q", got.Status)
	}
}

func TestMachineRepository_ListByHeartbeatBefore(t *testing.T) {
	db := setupMachineTestDB(t)
	repo := NewMachineRepository(db)
	ctx := context.Background()

	// Create a machine and set it online with heartbeat
	machine := &model.Machine{
		Name:           "heartbeat-before-test",
		IP:             "10.0.0.1",
		AgentTokenHash: "hash",
	}
	if err := repo.Create(ctx, machine); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Update heartbeat to make it online
	info := model.HeartbeatInfo{
		AgentVersion: "1.0.0",
		Hostname:     "server",
		IP:           "10.0.0.1",
		OS:           "linux",
		Arch:         "amd64",
	}
	if err := repo.UpdateHeartbeat(ctx, machine.ID, info); err != nil {
		t.Fatalf("UpdateHeartbeat failed: %v", err)
	}

	// Query with a future time - should find the machine
	future := time.Now().Add(1 * time.Hour)
	machines, err := repo.ListByHeartbeatBefore(ctx, future)
	if err != nil {
		t.Fatalf("ListByHeartbeatBefore failed: %v", err)
	}
	if len(machines) != 1 {
		t.Errorf("expected 1 machine, got %d", len(machines))
	}

	// Query with a past time - should not find the machine
	past := time.Now().Add(-1 * time.Hour)
	machines, err = repo.ListByHeartbeatBefore(ctx, past)
	if err != nil {
		t.Fatalf("ListByHeartbeatBefore failed: %v", err)
	}
	if len(machines) != 0 {
		t.Errorf("expected 0 machines, got %d", len(machines))
	}
}

func TestMachineRepository_UpdateTokenHash_ClearsRevokedAt(t *testing.T) {
	db := setupMachineTestDB(t)
	repo := NewMachineRepository(db)
	ctx := context.Background()

	machine := &model.Machine{
		Name:           "clear-revoke-test",
		IP:             "10.0.0.1",
		AgentTokenHash: "old_hash",
	}
	if err := repo.Create(ctx, machine); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Revoke first
	if err := repo.RevokeToken(ctx, machine.ID); err != nil {
		t.Fatalf("RevokeToken failed: %v", err)
	}

	// Update token hash should clear revoked_at
	if err := repo.UpdateTokenHash(ctx, machine.ID, "new_hash"); err != nil {
		t.Fatalf("UpdateTokenHash failed: %v", err)
	}

	got, err := repo.GetByID(ctx, machine.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.AgentTokenRevokedAt != nil {
		t.Error("expected agent_token_revoked_at to be cleared after token update")
	}
}
