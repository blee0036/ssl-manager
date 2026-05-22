package service

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/glebarez/sqlite"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

func setupMachineCertServiceTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("failed to enable foreign keys: %v", err)
	}

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
	}

	for _, stmt := range tables {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("failed to create table: %v", err)
		}
	}

	// Insert test machine and certificate
	if _, err := db.Exec(`INSERT INTO machines (id, name, ip, agent_token_hash, created_at, updated_at) VALUES ('machine-1', 'test-machine', '10.0.0.1', 'hash1', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("failed to insert test machine: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO certificates (id, name, domains, source, expire_at, fingerprint_sha256, cert_dir_path, created_at, updated_at) VALUES ('cert-1', 'test-cert', '["example.com"]', 'upload', '2025-01-01T00:00:00Z', 'abc123', 'certificates/cert-1', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("failed to insert test certificate: %v", err)
	}

	t.Cleanup(func() { db.Close() })
	return db
}

func newMachineCertService(t *testing.T) (*MachineCertificateService, *sql.DB) {
	t.Helper()
	db := setupMachineCertServiceTestDB(t)
	repo := repository.NewMachineCertificateRepository(db)
	svc := NewMachineCertificateService(repo)
	return svc, db
}

func TestMachineCertificateService_Create(t *testing.T) {
	svc, _ := newMachineCertService(t)
	ctx := context.Background()

	input := model.CreateMachineCertInput{
		MachineID:          "machine-1",
		CertificateID:      "cert-1",
		CertPath:           "/etc/ssl/cert.pem",
		PrivateKeyPath:     "/etc/ssl/key.pem",
		PostDeployCommands: "nginx -s reload",
	}

	mc, err := svc.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if mc.ID == "" {
		t.Error("expected ID to be generated")
	}
	if mc.MachineID != "machine-1" {
		t.Errorf("expected machine_id=machine-1, got %q", mc.MachineID)
	}
	if mc.CertificateID != "cert-1" {
		t.Errorf("expected certificate_id=cert-1, got %q", mc.CertificateID)
	}
	if mc.CertPath != "/etc/ssl/cert.pem" {
		t.Errorf("expected cert_path=/etc/ssl/cert.pem, got %q", mc.CertPath)
	}
	if mc.PrivateKeyPath != "/etc/ssl/key.pem" {
		t.Errorf("expected private_key_path=/etc/ssl/key.pem, got %q", mc.PrivateKeyPath)
	}
	if mc.ConfigRevision != 1 {
		t.Errorf("expected config_revision=1, got %d", mc.ConfigRevision)
	}
	if mc.LastDeployStatus != "pending" {
		t.Errorf("expected last_deploy_status=pending, got %q", mc.LastDeployStatus)
	}
}

func TestMachineCertificateService_Create_EmptyCertPath(t *testing.T) {
	svc, _ := newMachineCertService(t)
	ctx := context.Background()

	input := model.CreateMachineCertInput{
		MachineID:      "machine-1",
		CertificateID:  "cert-1",
		CertPath:       "",
		PrivateKeyPath: "/etc/ssl/key.pem",
	}

	_, err := svc.Create(ctx, input)
	if err == nil {
		t.Error("expected error for empty cert_path")
	}
}

func TestMachineCertificateService_Create_EmptyPrivateKeyPath(t *testing.T) {
	svc, _ := newMachineCertService(t)
	ctx := context.Background()

	input := model.CreateMachineCertInput{
		MachineID:      "machine-1",
		CertificateID:  "cert-1",
		CertPath:       "/etc/ssl/cert.pem",
		PrivateKeyPath: "",
	}

	_, err := svc.Create(ctx, input)
	if err == nil {
		t.Error("expected error for empty private_key_path")
	}
}

func TestMachineCertificateService_Create_WhitespacePaths(t *testing.T) {
	svc, _ := newMachineCertService(t)
	ctx := context.Background()

	// cert_path is whitespace only
	input := model.CreateMachineCertInput{
		MachineID:      "machine-1",
		CertificateID:  "cert-1",
		CertPath:       "   ",
		PrivateKeyPath: "/etc/ssl/key.pem",
	}

	_, err := svc.Create(ctx, input)
	if err == nil {
		t.Error("expected error for whitespace-only cert_path")
	}

	// private_key_path is whitespace only
	input2 := model.CreateMachineCertInput{
		MachineID:      "machine-1",
		CertificateID:  "cert-1",
		CertPath:       "/etc/ssl/cert.pem",
		PrivateKeyPath: "   ",
	}

	_, err = svc.Create(ctx, input2)
	if err == nil {
		t.Error("expected error for whitespace-only private_key_path")
	}
}

func TestMachineCertificateService_Update(t *testing.T) {
	svc, _ := newMachineCertService(t)
	ctx := context.Background()

	// Create first
	input := model.CreateMachineCertInput{
		MachineID:      "machine-1",
		CertificateID:  "cert-1",
		CertPath:       "/etc/ssl/cert.pem",
		PrivateKeyPath: "/etc/ssl/key.pem",
	}
	mc, err := svc.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Update
	newCertPath := "/etc/nginx/ssl/cert.pem"
	newKeyPath := "/etc/nginx/ssl/key.pem"
	updateInput := model.UpdateMachineCertInput{
		CertPath:       &newCertPath,
		PrivateKeyPath: &newKeyPath,
	}

	updated, err := svc.Update(ctx, mc.ID, updateInput)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if updated.CertPath != newCertPath {
		t.Errorf("expected cert_path=%q, got %q", newCertPath, updated.CertPath)
	}
	if updated.PrivateKeyPath != newKeyPath {
		t.Errorf("expected private_key_path=%q, got %q", newKeyPath, updated.PrivateKeyPath)
	}
	if updated.ConfigRevision != 2 {
		t.Errorf("expected config_revision=2, got %d", updated.ConfigRevision)
	}
	if updated.LastDeployStatus != "pending" {
		t.Errorf("expected last_deploy_status=pending, got %q", updated.LastDeployStatus)
	}
}

func TestMachineCertificateService_Update_EmptyCertPath(t *testing.T) {
	svc, _ := newMachineCertService(t)
	ctx := context.Background()

	// Create first
	input := model.CreateMachineCertInput{
		MachineID:      "machine-1",
		CertificateID:  "cert-1",
		CertPath:       "/etc/ssl/cert.pem",
		PrivateKeyPath: "/etc/ssl/key.pem",
	}
	mc, err := svc.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Try to update with empty cert_path
	emptyPath := ""
	updateInput := model.UpdateMachineCertInput{
		CertPath: &emptyPath,
	}

	_, err = svc.Update(ctx, mc.ID, updateInput)
	if err == nil {
		t.Error("expected error for empty cert_path in update")
	}
}

func TestMachineCertificateService_Update_EmptyPrivateKeyPath(t *testing.T) {
	svc, _ := newMachineCertService(t)
	ctx := context.Background()

	// Create first
	input := model.CreateMachineCertInput{
		MachineID:      "machine-1",
		CertificateID:  "cert-1",
		CertPath:       "/etc/ssl/cert.pem",
		PrivateKeyPath: "/etc/ssl/key.pem",
	}
	mc, err := svc.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Try to update with empty private_key_path
	emptyPath := ""
	updateInput := model.UpdateMachineCertInput{
		PrivateKeyPath: &emptyPath,
	}

	_, err = svc.Update(ctx, mc.ID, updateInput)
	if err == nil {
		t.Error("expected error for empty private_key_path in update")
	}
}

func TestMachineCertificateService_Update_IncrementsRevision(t *testing.T) {
	svc, _ := newMachineCertService(t)
	ctx := context.Background()

	// Create
	input := model.CreateMachineCertInput{
		MachineID:      "machine-1",
		CertificateID:  "cert-1",
		CertPath:       "/etc/ssl/cert.pem",
		PrivateKeyPath: "/etc/ssl/key.pem",
	}
	mc, err := svc.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Update multiple times and verify revision increments
	newCommands := "systemctl reload nginx"
	updateInput := model.UpdateMachineCertInput{
		PostDeployCommands: &newCommands,
	}

	updated1, err := svc.Update(ctx, mc.ID, updateInput)
	if err != nil {
		t.Fatalf("Update 1 failed: %v", err)
	}
	if updated1.ConfigRevision != 2 {
		t.Errorf("expected config_revision=2 after first update, got %d", updated1.ConfigRevision)
	}

	newCommands2 := "systemctl restart nginx"
	updateInput2 := model.UpdateMachineCertInput{
		PostDeployCommands: &newCommands2,
	}

	updated2, err := svc.Update(ctx, mc.ID, updateInput2)
	if err != nil {
		t.Fatalf("Update 2 failed: %v", err)
	}
	if updated2.ConfigRevision != 3 {
		t.Errorf("expected config_revision=3 after second update, got %d", updated2.ConfigRevision)
	}
}

func TestMachineCertificateService_Delete(t *testing.T) {
	svc, _ := newMachineCertService(t)
	ctx := context.Background()

	// Create
	input := model.CreateMachineCertInput{
		MachineID:      "machine-1",
		CertificateID:  "cert-1",
		CertPath:       "/etc/ssl/cert.pem",
		PrivateKeyPath: "/etc/ssl/key.pem",
	}
	mc, err := svc.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Delete
	err = svc.Delete(ctx, mc.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify it's gone
	results, err := svc.GetByMachineID(ctx, "machine-1")
	if err != nil {
		t.Fatalf("GetByMachineID failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results after delete, got %d", len(results))
	}
}

func TestMachineCertificateService_GetByMachineID(t *testing.T) {
	svc, db := newMachineCertService(t)
	ctx := context.Background()

	// Insert a second certificate
	if _, err := db.Exec(`INSERT INTO certificates (id, name, domains, source, expire_at, fingerprint_sha256, cert_dir_path, created_at, updated_at) VALUES ('cert-2', 'test-cert-2', '["test.com"]', 'upload', '2025-06-01T00:00:00Z', 'def456', 'certificates/cert-2', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("failed to insert second certificate: %v", err)
	}

	// Create two machine certificates
	input1 := model.CreateMachineCertInput{
		MachineID:      "machine-1",
		CertificateID:  "cert-1",
		CertPath:       "/etc/ssl/cert1.pem",
		PrivateKeyPath: "/etc/ssl/key1.pem",
	}
	input2 := model.CreateMachineCertInput{
		MachineID:      "machine-1",
		CertificateID:  "cert-2",
		CertPath:       "/etc/ssl/cert2.pem",
		PrivateKeyPath: "/etc/ssl/key2.pem",
	}

	if _, err := svc.Create(ctx, input1); err != nil {
		t.Fatalf("Create 1 failed: %v", err)
	}
	if _, err := svc.Create(ctx, input2); err != nil {
		t.Fatalf("Create 2 failed: %v", err)
	}

	results, err := svc.GetByMachineID(ctx, "machine-1")
	if err != nil {
		t.Fatalf("GetByMachineID failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestMachineCertificateService_TriggerManualDeploy(t *testing.T) {
	svc, _ := newMachineCertService(t)
	ctx := context.Background()

	// Create
	input := model.CreateMachineCertInput{
		MachineID:      "machine-1",
		CertificateID:  "cert-1",
		CertPath:       "/etc/ssl/cert.pem",
		PrivateKeyPath: "/etc/ssl/key.pem",
	}
	mc, err := svc.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Trigger manual deploy
	err = svc.TriggerManualDeploy(ctx, mc.ID)
	if err != nil {
		t.Fatalf("TriggerManualDeploy failed: %v", err)
	}

	// Verify the state
	results, err := svc.GetByMachineID(ctx, "machine-1")
	if err != nil {
		t.Fatalf("GetByMachineID failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	got := results[0]
	if got.LastDeployStatus != "pending" {
		t.Errorf("expected last_deploy_status=pending, got %q", got.LastDeployStatus)
	}
	// Initial revision is 1, TriggerManualDeploy increments to 2
	// But Create already sets it to pending, so the revision goes from 1 to 2
	if got.ConfigRevision != 2 {
		t.Errorf("expected config_revision=2, got %d", got.ConfigRevision)
	}
}

func TestMachineCertificateService_TriggerManualDeploy_NotFound(t *testing.T) {
	svc, _ := newMachineCertService(t)
	ctx := context.Background()

	err := svc.TriggerManualDeploy(ctx, "nonexistent-id")
	if err == nil {
		t.Error("expected error for nonexistent ID")
	}
}

func TestMachineCertificateService_MarkPendingSync(t *testing.T) {
	svc, db := newMachineCertService(t)
	ctx := context.Background()

	// Insert a second machine
	if _, err := db.Exec(`INSERT INTO machines (id, name, ip, agent_token_hash, created_at, updated_at) VALUES ('machine-2', 'test-machine-2', '10.0.0.2', 'hash2', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("failed to insert second machine: %v", err)
	}

	// Create two machine certificates for the same certificate on different machines
	input1 := model.CreateMachineCertInput{
		MachineID:      "machine-1",
		CertificateID:  "cert-1",
		CertPath:       "/etc/ssl/cert.pem",
		PrivateKeyPath: "/etc/ssl/key.pem",
	}
	input2 := model.CreateMachineCertInput{
		MachineID:      "machine-2",
		CertificateID:  "cert-1",
		CertPath:       "/opt/ssl/cert.pem",
		PrivateKeyPath: "/opt/ssl/key.pem",
	}

	mc1, err := svc.Create(ctx, input1)
	if err != nil {
		t.Fatalf("Create 1 failed: %v", err)
	}
	mc2, err := svc.Create(ctx, input2)
	if err != nil {
		t.Fatalf("Create 2 failed: %v", err)
	}

	// Mark pending sync for cert-1
	err = svc.MarkPendingSync(ctx, "cert-1")
	if err != nil {
		t.Fatalf("MarkPendingSync failed: %v", err)
	}

	// Verify both are updated
	results1, _ := svc.GetByMachineID(ctx, "machine-1")
	results2, _ := svc.GetByMachineID(ctx, "machine-2")

	var got1, got2 *model.MachineCertificate
	for _, r := range results1 {
		if r.ID == mc1.ID {
			got1 = r
		}
	}
	for _, r := range results2 {
		if r.ID == mc2.ID {
			got2 = r
		}
	}

	if got1 == nil || got2 == nil {
		t.Fatal("could not find created machine certificates")
	}

	if got1.LastDeployStatus != "pending" {
		t.Errorf("mc1: expected last_deploy_status=pending, got %q", got1.LastDeployStatus)
	}
	if got1.ConfigRevision != 2 {
		t.Errorf("mc1: expected config_revision=2, got %d", got1.ConfigRevision)
	}
	if got2.LastDeployStatus != "pending" {
		t.Errorf("mc2: expected last_deploy_status=pending, got %q", got2.LastDeployStatus)
	}
	if got2.ConfigRevision != 2 {
		t.Errorf("mc2: expected config_revision=2, got %d", got2.ConfigRevision)
	}
}
