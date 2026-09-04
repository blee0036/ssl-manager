package repository

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/glebarez/sqlite"
	"github.com/ssl-manager/ssl-manager/internal/model"
)

func setupMachineCertTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("failed to enable foreign keys: %v", err)
	}

	// Create required tables (machines and certificates first for FK)
	tables := []string{
		`CREATE TABLE IF NOT EXISTS machines (
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
		)`,
		`CREATE TABLE IF NOT EXISTS certificates (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			domains TEXT NOT NULL,
			source TEXT NOT NULL CHECK(source IN ('upload', 'certbot_cloudflare_dns', 'certbot_manual_dns')),
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
		`CREATE TABLE IF NOT EXISTS machine_certificates (
			id TEXT PRIMARY KEY,
			machine_id TEXT NOT NULL REFERENCES machines(id),
			certificate_id TEXT NOT NULL REFERENCES certificates(id),
			cert_path TEXT NOT NULL,
			private_key_path TEXT NOT NULL,
			post_deploy_commands TEXT DEFAULT '',
			config_revision INTEGER NOT NULL DEFAULT 1,
			last_deploy_status TEXT DEFAULT '' CHECK(last_deploy_status IN ('', 'success', 'failed', 'pending', 'skipped')),
			last_deploy_at TEXT,
			last_deploy_message TEXT DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS deployment_logs (
			id TEXT PRIMARY KEY,
			machine_certificate_id TEXT NOT NULL REFERENCES machine_certificates(id),
			machine_id TEXT NOT NULL,
			certificate_id TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS domains (
			id TEXT PRIMARY KEY,
			linked_machine_id TEXT DEFAULT '',
			linked_certificate_id TEXT DEFAULT '',
			linked_machine_certificate_id TEXT DEFAULT ''
		)`,
	}

	for _, stmt := range tables {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("failed to create table: %v", err)
		}
	}

	// Insert a test machine and certificate for FK references
	if _, err := db.Exec(`INSERT INTO machines (id, name, ip, agent_token_hash, created_at, updated_at) VALUES ('machine-1', 'test-machine', '10.0.0.1', 'hash1', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("failed to insert test machine: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO certificates (id, name, domains, source, expire_at, fingerprint_sha256, cert_dir_path, created_at, updated_at) VALUES ('cert-1', 'test-cert', '["example.com"]', 'upload', '2025-01-01T00:00:00Z', 'abc123', 'certificates/cert-1', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("failed to insert test certificate: %v", err)
	}

	t.Cleanup(func() { db.Close() })
	return db
}

func TestMachineCertificateRepository_Create(t *testing.T) {
	db := setupMachineCertTestDB(t)
	repo := NewMachineCertificateRepository(db)
	ctx := context.Background()

	mc := &model.MachineCertificate{
		MachineID:          "machine-1",
		CertificateID:      "cert-1",
		CertPath:           "/etc/ssl/cert.pem",
		PrivateKeyPath:     "/etc/ssl/key.pem",
		PostDeployCommands: "nginx -s reload",
	}

	err := repo.Create(ctx, mc)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if mc.ID == "" {
		t.Error("expected ID to be generated")
	}
	if mc.ConfigRevision != 1 {
		t.Errorf("expected config_revision=1, got %d", mc.ConfigRevision)
	}
	if mc.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestMachineCertificateRepository_Create_WithExplicitRevision(t *testing.T) {
	db := setupMachineCertTestDB(t)
	repo := NewMachineCertificateRepository(db)
	ctx := context.Background()

	mc := &model.MachineCertificate{
		MachineID:      "machine-1",
		CertificateID:  "cert-1",
		CertPath:       "/etc/ssl/cert.pem",
		PrivateKeyPath: "/etc/ssl/key.pem",
		ConfigRevision: 5,
	}

	err := repo.Create(ctx, mc)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if mc.ConfigRevision != 5 {
		t.Errorf("expected config_revision=5, got %d", mc.ConfigRevision)
	}
}

func TestMachineCertificateRepository_GetByID(t *testing.T) {
	db := setupMachineCertTestDB(t)
	repo := NewMachineCertificateRepository(db)
	ctx := context.Background()

	mc := &model.MachineCertificate{
		MachineID:          "machine-1",
		CertificateID:      "cert-1",
		CertPath:           "/etc/ssl/cert.pem",
		PrivateKeyPath:     "/etc/ssl/key.pem",
		PostDeployCommands: "systemctl reload nginx",
	}
	if err := repo.Create(ctx, mc); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := repo.GetByID(ctx, mc.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if got.MachineID != "machine-1" {
		t.Errorf("expected machine_id=machine-1, got %q", got.MachineID)
	}
	if got.CertificateID != "cert-1" {
		t.Errorf("expected certificate_id=cert-1, got %q", got.CertificateID)
	}
	if got.CertPath != "/etc/ssl/cert.pem" {
		t.Errorf("expected cert_path=/etc/ssl/cert.pem, got %q", got.CertPath)
	}
	if got.PrivateKeyPath != "/etc/ssl/key.pem" {
		t.Errorf("expected private_key_path=/etc/ssl/key.pem, got %q", got.PrivateKeyPath)
	}
	if got.PostDeployCommands != "systemctl reload nginx" {
		t.Errorf("expected post_deploy_commands='systemctl reload nginx', got %q", got.PostDeployCommands)
	}
	if got.ConfigRevision != 1 {
		t.Errorf("expected config_revision=1, got %d", got.ConfigRevision)
	}
}

func TestMachineCertificateRepository_GetByID_NotFound(t *testing.T) {
	db := setupMachineCertTestDB(t)
	repo := NewMachineCertificateRepository(db)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, "nonexistent-id")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestMachineCertificateRepository_GetByMachineID(t *testing.T) {
	db := setupMachineCertTestDB(t)
	repo := NewMachineCertificateRepository(db)
	ctx := context.Background()

	// Insert a second certificate for testing multiple configs
	if _, err := db.Exec(`INSERT INTO certificates (id, name, domains, source, expire_at, fingerprint_sha256, cert_dir_path, created_at, updated_at) VALUES ('cert-2', 'test-cert-2', '["test.com"]', 'upload', '2025-06-01T00:00:00Z', 'def456', 'certificates/cert-2', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("failed to insert second certificate: %v", err)
	}

	// Create two machine certificates for the same machine
	mc1 := &model.MachineCertificate{
		MachineID:      "machine-1",
		CertificateID:  "cert-1",
		CertPath:       "/etc/ssl/cert1.pem",
		PrivateKeyPath: "/etc/ssl/key1.pem",
	}
	mc2 := &model.MachineCertificate{
		MachineID:      "machine-1",
		CertificateID:  "cert-2",
		CertPath:       "/etc/ssl/cert2.pem",
		PrivateKeyPath: "/etc/ssl/key2.pem",
	}

	if err := repo.Create(ctx, mc1); err != nil {
		t.Fatalf("Create mc1 failed: %v", err)
	}
	if err := repo.Create(ctx, mc2); err != nil {
		t.Fatalf("Create mc2 failed: %v", err)
	}

	results, err := repo.GetByMachineID(ctx, "machine-1")
	if err != nil {
		t.Fatalf("GetByMachineID failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 machine certificates, got %d", len(results))
	}
}

func TestMachineCertificateRepository_GetByMachineID_Empty(t *testing.T) {
	db := setupMachineCertTestDB(t)
	repo := NewMachineCertificateRepository(db)
	ctx := context.Background()

	results, err := repo.GetByMachineID(ctx, "machine-1")
	if err != nil {
		t.Fatalf("GetByMachineID failed: %v", err)
	}
	if results != nil && len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestMachineCertificateRepository_Update(t *testing.T) {
	db := setupMachineCertTestDB(t)
	repo := NewMachineCertificateRepository(db)
	ctx := context.Background()

	mc := &model.MachineCertificate{
		MachineID:      "machine-1",
		CertificateID:  "cert-1",
		CertPath:       "/etc/ssl/cert.pem",
		PrivateKeyPath: "/etc/ssl/key.pem",
	}
	if err := repo.Create(ctx, mc); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	newCertPath := "/etc/nginx/ssl/cert.pem"
	newKeyPath := "/etc/nginx/ssl/key.pem"
	newCommands := "nginx -t && nginx -s reload"
	input := model.UpdateMachineCertInput{
		CertPath:           &newCertPath,
		PrivateKeyPath:     &newKeyPath,
		PostDeployCommands: &newCommands,
	}

	updated, err := repo.Update(ctx, mc.ID, input)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if updated.CertPath != newCertPath {
		t.Errorf("expected cert_path=%q, got %q", newCertPath, updated.CertPath)
	}
	if updated.PrivateKeyPath != newKeyPath {
		t.Errorf("expected private_key_path=%q, got %q", newKeyPath, updated.PrivateKeyPath)
	}
	if updated.PostDeployCommands != newCommands {
		t.Errorf("expected post_deploy_commands=%q, got %q", newCommands, updated.PostDeployCommands)
	}
	if updated.ConfigRevision != 2 {
		t.Errorf("expected config_revision=2 after update, got %d", updated.ConfigRevision)
	}
	if updated.LastDeployStatus != "pending" {
		t.Errorf("expected last_deploy_status=pending after update, got %q", updated.LastDeployStatus)
	}
}

func TestMachineCertificateRepository_Update_NotFound(t *testing.T) {
	db := setupMachineCertTestDB(t)
	repo := NewMachineCertificateRepository(db)
	ctx := context.Background()

	newPath := "/etc/ssl/new.pem"
	input := model.UpdateMachineCertInput{
		CertPath: &newPath,
	}

	_, err := repo.Update(ctx, "nonexistent-id", input)
	if err == nil {
		t.Error("expected error for nonexistent ID")
	}
}

func TestMachineCertificateRepository_Delete(t *testing.T) {
	db := setupMachineCertTestDB(t)
	repo := NewMachineCertificateRepository(db)
	ctx := context.Background()

	mc := &model.MachineCertificate{
		MachineID:      "machine-1",
		CertificateID:  "cert-1",
		CertPath:       "/etc/ssl/cert.pem",
		PrivateKeyPath: "/etc/ssl/key.pem",
	}
	if err := repo.Create(ctx, mc); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	err := repo.Delete(ctx, mc.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err = repo.GetByID(ctx, mc.ID)
	if err != sql.ErrNoRows {
		t.Errorf("expected machine certificate to be deleted, got err=%v", err)
	}
}

func TestMachineCertificateRepository_Delete_CascadesDeploymentData(t *testing.T) {
	db := setupMachineCertTestDB(t)
	repo := NewMachineCertificateRepository(db)
	ctx := context.Background()

	mc := &model.MachineCertificate{
		MachineID:      "machine-1",
		CertificateID:  "cert-1",
		CertPath:       "/etc/ssl/cert.pem",
		PrivateKeyPath: "/etc/ssl/key.pem",
	}
	if err := repo.Create(ctx, mc); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO deployment_logs (id, machine_certificate_id, machine_id, certificate_id) VALUES (?, ?, ?, ?)",
		"log-1", mc.ID, mc.MachineID, mc.CertificateID,
	); err != nil {
		t.Fatalf("failed to insert deployment log: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO domains (id, linked_machine_certificate_id) VALUES (?, ?)",
		"domain-1", mc.ID,
	); err != nil {
		t.Fatalf("failed to insert domain: %v", err)
	}

	if err := repo.Delete(ctx, mc.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	for _, table := range []string{"machine_certificates", "deployment_logs"} {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("failed to count %s: %v", table, err)
		}
		if count != 0 {
			t.Errorf("expected %s to be empty after deletion, got %d row(s)", table, count)
		}
	}

	var linkedMachineCertificateID string
	if err := db.QueryRowContext(ctx,
		"SELECT linked_machine_certificate_id FROM domains WHERE id = ?",
		"domain-1",
	).Scan(&linkedMachineCertificateID); err != nil {
		t.Fatalf("failed to query domain link: %v", err)
	}
	if linkedMachineCertificateID != "" {
		t.Errorf("expected machine certificate domain link to be cleared, got %q", linkedMachineCertificateID)
	}
}

func TestMachineCertificateRepository_Delete_NotFound(t *testing.T) {
	db := setupMachineCertTestDB(t)
	repo := NewMachineCertificateRepository(db)
	ctx := context.Background()

	err := repo.Delete(ctx, "nonexistent-id")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestMachineCertificateRepository_MarkPendingSync(t *testing.T) {
	db := setupMachineCertTestDB(t)
	repo := NewMachineCertificateRepository(db)
	ctx := context.Background()

	// Create two machine certificates for the same certificate
	if _, err := db.Exec(`INSERT INTO machines (id, name, ip, agent_token_hash, created_at, updated_at) VALUES ('machine-2', 'test-machine-2', '10.0.0.2', 'hash2', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("failed to insert second machine: %v", err)
	}

	mc1 := &model.MachineCertificate{
		MachineID:      "machine-1",
		CertificateID:  "cert-1",
		CertPath:       "/etc/ssl/cert.pem",
		PrivateKeyPath: "/etc/ssl/key.pem",
	}
	mc2 := &model.MachineCertificate{
		MachineID:      "machine-2",
		CertificateID:  "cert-1",
		CertPath:       "/opt/ssl/cert.pem",
		PrivateKeyPath: "/opt/ssl/key.pem",
	}

	if err := repo.Create(ctx, mc1); err != nil {
		t.Fatalf("Create mc1 failed: %v", err)
	}
	if err := repo.Create(ctx, mc2); err != nil {
		t.Fatalf("Create mc2 failed: %v", err)
	}

	// Mark all machine certificates for cert-1 as pending
	err := repo.MarkPendingSync(ctx, "cert-1")
	if err != nil {
		t.Fatalf("MarkPendingSync failed: %v", err)
	}

	// Verify both are marked as pending with incremented revision
	got1, err := repo.GetByID(ctx, mc1.ID)
	if err != nil {
		t.Fatalf("GetByID mc1 failed: %v", err)
	}
	if got1.LastDeployStatus != "pending" {
		t.Errorf("mc1: expected last_deploy_status=pending, got %q", got1.LastDeployStatus)
	}
	if got1.ConfigRevision != 2 {
		t.Errorf("mc1: expected config_revision=2, got %d", got1.ConfigRevision)
	}

	got2, err := repo.GetByID(ctx, mc2.ID)
	if err != nil {
		t.Fatalf("GetByID mc2 failed: %v", err)
	}
	if got2.LastDeployStatus != "pending" {
		t.Errorf("mc2: expected last_deploy_status=pending, got %q", got2.LastDeployStatus)
	}
	if got2.ConfigRevision != 2 {
		t.Errorf("mc2: expected config_revision=2, got %d", got2.ConfigRevision)
	}
}

func TestMachineCertificateRepository_IncrementRevision(t *testing.T) {
	db := setupMachineCertTestDB(t)
	repo := NewMachineCertificateRepository(db)
	ctx := context.Background()

	mc := &model.MachineCertificate{
		MachineID:      "machine-1",
		CertificateID:  "cert-1",
		CertPath:       "/etc/ssl/cert.pem",
		PrivateKeyPath: "/etc/ssl/key.pem",
	}
	if err := repo.Create(ctx, mc); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Increment revision
	err := repo.IncrementRevision(ctx, mc.ID)
	if err != nil {
		t.Fatalf("IncrementRevision failed: %v", err)
	}

	got, err := repo.GetByID(ctx, mc.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.ConfigRevision != 2 {
		t.Errorf("expected config_revision=2, got %d", got.ConfigRevision)
	}

	// Increment again
	err = repo.IncrementRevision(ctx, mc.ID)
	if err != nil {
		t.Fatalf("IncrementRevision (2nd) failed: %v", err)
	}

	got, err = repo.GetByID(ctx, mc.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.ConfigRevision != 3 {
		t.Errorf("expected config_revision=3, got %d", got.ConfigRevision)
	}
}

func TestMachineCertificateRepository_IncrementRevision_NotFound(t *testing.T) {
	db := setupMachineCertTestDB(t)
	repo := NewMachineCertificateRepository(db)
	ctx := context.Background()

	err := repo.IncrementRevision(ctx, "nonexistent-id")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestMachineCertificateRepository_TriggerManualDeploy(t *testing.T) {
	db := setupMachineCertTestDB(t)
	repo := NewMachineCertificateRepository(db)
	ctx := context.Background()

	mc := &model.MachineCertificate{
		MachineID:        "machine-1",
		CertificateID:    "cert-1",
		CertPath:         "/etc/ssl/cert.pem",
		PrivateKeyPath:   "/etc/ssl/key.pem",
		LastDeployStatus: "success",
	}
	if err := repo.Create(ctx, mc); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Trigger manual deploy
	err := repo.TriggerManualDeploy(ctx, mc.ID)
	if err != nil {
		t.Fatalf("TriggerManualDeploy failed: %v", err)
	}

	got, err := repo.GetByID(ctx, mc.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.LastDeployStatus != "pending" {
		t.Errorf("expected last_deploy_status=pending, got %q", got.LastDeployStatus)
	}
	if got.ConfigRevision != 2 {
		t.Errorf("expected config_revision=2, got %d", got.ConfigRevision)
	}
}

func TestMachineCertificateRepository_TriggerManualDeploy_NotFound(t *testing.T) {
	db := setupMachineCertTestDB(t)
	repo := NewMachineCertificateRepository(db)
	ctx := context.Background()

	err := repo.TriggerManualDeploy(ctx, "nonexistent-id")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}
