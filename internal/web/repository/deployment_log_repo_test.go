package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/model"
)

// setupDeploymentLogTestDB creates a test DB with the deployment_logs table.
func setupDeploymentLogTestDB(t *testing.T) *DeploymentLogRepository {
	t.Helper()
	db := setupTestDB(t)

	// Add deployment_logs table
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS deployment_logs (
		id TEXT PRIMARY KEY,
		machine_certificate_id TEXT NOT NULL,
		machine_id TEXT NOT NULL,
		certificate_id TEXT NOT NULL,
		status TEXT NOT NULL CHECK(status IN ('success', 'failed', 'skipped')),
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

	return NewDeploymentLogRepository(db)
}

func createTestDeploymentLog(machineCertID, machineID, certID, status string, createdAt time.Time) *model.DeploymentLog {
	return &model.DeploymentLog{
		MachineCertificateID:  machineCertID,
		MachineID:             machineID,
		CertificateID:         certID,
		Status:                status,
		CertFingerprintSHA256: "abc123fingerprint",
		CertPath:              "/etc/ssl/cert.pem",
		PrivateKeyPath:        "/etc/ssl/key.pem",
		CommandOutputs: []model.CommandOutput{
			{Command: "nginx -t", ExitCode: 0, Stdout: "ok", Stderr: ""},
		},
		ErrorMessage: "",
		StartedAt:    createdAt.Add(-10 * time.Second),
		FinishedAt:   createdAt.Add(-5 * time.Second),
		CreatedAt:    createdAt,
	}
}

func TestDeploymentLogRepo_Create(t *testing.T) {
	repo := setupDeploymentLogTestDB(t)
	ctx := context.Background()

	log := createTestDeploymentLog("mc-1", "m-1", "c-1", "success", time.Now().UTC())

	err := repo.Create(ctx, log)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if log.ID == "" {
		t.Fatal("expected ID to be set")
	}
}

func TestDeploymentLogRepo_Create_WithExistingID(t *testing.T) {
	repo := setupDeploymentLogTestDB(t)
	ctx := context.Background()

	log := createTestDeploymentLog("mc-1", "m-1", "c-1", "success", time.Now().UTC())
	log.ID = "custom-id-123"

	err := repo.Create(ctx, log)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if log.ID != "custom-id-123" {
		t.Errorf("expected ID 'custom-id-123', got '%s'", log.ID)
	}
}

func TestDeploymentLogRepo_GetByMachineCertificateID(t *testing.T) {
	repo := setupDeploymentLogTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC()

	// Create 3 logs for the same machine certificate
	for i := 0; i < 3; i++ {
		log := createTestDeploymentLog("mc-1", "m-1", "c-1", "success", now.Add(time.Duration(i)*time.Minute))
		if err := repo.Create(ctx, log); err != nil {
			t.Fatalf("failed to create log %d: %v", i, err)
		}
	}

	// Create 1 log for a different machine certificate
	otherLog := createTestDeploymentLog("mc-2", "m-1", "c-1", "failed", now)
	if err := repo.Create(ctx, otherLog); err != nil {
		t.Fatalf("failed to create other log: %v", err)
	}

	logs, err := repo.GetByMachineCertificateID(ctx, "mc-1", 10)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(logs) != 3 {
		t.Fatalf("expected 3 logs, got %d", len(logs))
	}

	// Verify time DESC order
	for i := 1; i < len(logs); i++ {
		if logs[i].CreatedAt.After(logs[i-1].CreatedAt) {
			t.Errorf("logs not in DESC order: log[%d].CreatedAt=%v > log[%d].CreatedAt=%v",
				i, logs[i].CreatedAt, i-1, logs[i-1].CreatedAt)
		}
	}
}

func TestDeploymentLogRepo_GetByMachineCertificateID_WithLimit(t *testing.T) {
	repo := setupDeploymentLogTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC()

	// Create 5 logs
	for i := 0; i < 5; i++ {
		log := createTestDeploymentLog("mc-1", "m-1", "c-1", "success", now.Add(time.Duration(i)*time.Minute))
		if err := repo.Create(ctx, log); err != nil {
			t.Fatalf("failed to create log %d: %v", i, err)
		}
	}

	// Query with limit 3
	logs, err := repo.GetByMachineCertificateID(ctx, "mc-1", 3)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(logs) != 3 {
		t.Fatalf("expected 3 logs, got %d", len(logs))
	}
}

func TestDeploymentLogRepo_GetByMachineID(t *testing.T) {
	repo := setupDeploymentLogTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC()

	// Create logs for machine m-1 with different machine certificates
	for i := 0; i < 3; i++ {
		log := createTestDeploymentLog(fmt.Sprintf("mc-%d", i), "m-1", "c-1", "success", now.Add(time.Duration(i)*time.Minute))
		if err := repo.Create(ctx, log); err != nil {
			t.Fatalf("failed to create log %d: %v", i, err)
		}
	}

	// Create log for a different machine
	otherLog := createTestDeploymentLog("mc-10", "m-2", "c-1", "failed", now)
	if err := repo.Create(ctx, otherLog); err != nil {
		t.Fatalf("failed to create other log: %v", err)
	}

	logs, err := repo.GetByMachineID(ctx, "m-1", 10)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(logs) != 3 {
		t.Fatalf("expected 3 logs, got %d", len(logs))
	}

	// Verify time DESC order
	for i := 1; i < len(logs); i++ {
		if logs[i].CreatedAt.After(logs[i-1].CreatedAt) {
			t.Errorf("logs not in DESC order")
		}
	}
}

func TestDeploymentLogRepo_EnforceRetentionLimit(t *testing.T) {
	repo := setupDeploymentLogTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC()

	// Create 35 logs for the same machine certificate
	for i := 0; i < 35; i++ {
		log := createTestDeploymentLog("mc-1", "m-1", "c-1", "success", now.Add(time.Duration(i)*time.Second))
		if err := repo.Create(ctx, log); err != nil {
			t.Fatalf("failed to create log %d: %v", i, err)
		}
	}

	// Enforce retention limit of 30
	err := repo.EnforceRetentionLimit(ctx, "mc-1", 30)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify only 30 logs remain
	logs, err := repo.GetByMachineCertificateID(ctx, "mc-1", 0)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(logs) != 30 {
		t.Fatalf("expected 30 logs after retention, got %d", len(logs))
	}

	// Verify the remaining logs are the newest ones (highest created_at)
	// The oldest 5 should have been deleted
	oldestRemaining := logs[len(logs)-1].CreatedAt
	expectedOldest := now.Add(5 * time.Second) // logs 0-4 should be deleted, log 5 is the oldest remaining
	if oldestRemaining.Before(expectedOldest.Add(-time.Second)) {
		t.Errorf("expected oldest remaining log to be around %v, got %v", expectedOldest, oldestRemaining)
	}
}

func TestDeploymentLogRepo_EnforceRetentionLimit_UnderLimit(t *testing.T) {
	repo := setupDeploymentLogTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC()

	// Create only 5 logs
	for i := 0; i < 5; i++ {
		log := createTestDeploymentLog("mc-1", "m-1", "c-1", "success", now.Add(time.Duration(i)*time.Second))
		if err := repo.Create(ctx, log); err != nil {
			t.Fatalf("failed to create log %d: %v", i, err)
		}
	}

	// Enforce retention limit of 30 - should not delete anything
	err := repo.EnforceRetentionLimit(ctx, "mc-1", 30)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify all 5 logs remain
	logs, err := repo.GetByMachineCertificateID(ctx, "mc-1", 0)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(logs) != 5 {
		t.Fatalf("expected 5 logs, got %d", len(logs))
	}
}

func TestDeploymentLogRepo_CommandOutputsSerialization(t *testing.T) {
	repo := setupDeploymentLogTestDB(t)
	ctx := context.Background()

	log := &model.DeploymentLog{
		MachineCertificateID:  "mc-1",
		MachineID:             "m-1",
		CertificateID:         "c-1",
		Status:                "success",
		CertFingerprintSHA256: "fingerprint123",
		CertPath:              "/etc/ssl/cert.pem",
		PrivateKeyPath:        "/etc/ssl/key.pem",
		CommandOutputs: []model.CommandOutput{
			{Command: "nginx -t", ExitCode: 0, Stdout: "syntax ok", Stderr: ""},
			{Command: "systemctl reload nginx", ExitCode: 0, Stdout: "", Stderr: ""},
		},
		ErrorMessage: "",
		StartedAt:    time.Now().UTC().Add(-10 * time.Second),
		FinishedAt:   time.Now().UTC(),
		CreatedAt:    time.Now().UTC(),
	}

	if err := repo.Create(ctx, log); err != nil {
		t.Fatalf("failed to create log: %v", err)
	}

	// Retrieve and verify command outputs
	logs, err := repo.GetByMachineCertificateID(ctx, "mc-1", 1)
	if err != nil {
		t.Fatalf("failed to get logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}

	retrieved := logs[0]
	if len(retrieved.CommandOutputs) != 2 {
		t.Fatalf("expected 2 command outputs, got %d", len(retrieved.CommandOutputs))
	}
	if retrieved.CommandOutputs[0].Command != "nginx -t" {
		t.Errorf("expected command 'nginx -t', got '%s'", retrieved.CommandOutputs[0].Command)
	}
	if retrieved.CommandOutputs[0].Stdout != "syntax ok" {
		t.Errorf("expected stdout 'syntax ok', got '%s'", retrieved.CommandOutputs[0].Stdout)
	}
	if retrieved.CommandOutputs[1].Command != "systemctl reload nginx" {
		t.Errorf("expected command 'systemctl reload nginx', got '%s'", retrieved.CommandOutputs[1].Command)
	}
}
