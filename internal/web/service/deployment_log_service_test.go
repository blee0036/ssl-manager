package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// setupDeploymentLogServiceTestDB creates a test DB with the deployment_logs table and returns a service.
func setupDeploymentLogServiceTestDB(t *testing.T) *DeploymentLogService {
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

	repo := repository.NewDeploymentLogRepository(db)
	sanitizer, err := NewSanitizer()
	if err != nil {
		t.Fatalf("failed to create sanitizer: %v", err)
	}
	return NewDeploymentLogService(repo, sanitizer)
}

func newTestDeploymentLog(machineCertID, machineID, certID, status string, createdAt time.Time) *model.DeploymentLog {
	return &model.DeploymentLog{
		MachineCertificateID:  machineCertID,
		MachineID:             machineID,
		CertificateID:         certID,
		Status:                status,
		CertFingerprintSHA256: "sha256fingerprint",
		CertPath:              "/etc/ssl/cert.pem",
		PrivateKeyPath:        "/etc/ssl/key.pem",
		CommandOutputs: []model.CommandOutput{
			{Command: "nginx -s reload", ExitCode: 0, Stdout: "", Stderr: ""},
		},
		ErrorMessage: "",
		StartedAt:    createdAt.Add(-5 * time.Second),
		FinishedAt:   createdAt,
		CreatedAt:    createdAt,
	}
}

func TestDeploymentLogService_Create(t *testing.T) {
	svc := setupDeploymentLogServiceTestDB(t)
	ctx := context.Background()

	log := newTestDeploymentLog("mc-1", "m-1", "c-1", "success", time.Now().UTC())

	err := svc.Create(ctx, log)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if log.ID == "" {
		t.Fatal("expected ID to be set")
	}
}

func TestDeploymentLogService_Create_EnforcesRetentionLimit(t *testing.T) {
	svc := setupDeploymentLogServiceTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC()

	// Create 35 logs via the service (each call enforces retention)
	for i := 0; i < 35; i++ {
		log := newTestDeploymentLog("mc-1", "m-1", "c-1", "success", now.Add(time.Duration(i)*time.Second))
		if err := svc.Create(ctx, log); err != nil {
			t.Fatalf("failed to create log %d: %v", i, err)
		}
	}

	// Query all logs - should be at most 30
	logs, err := svc.GetByMachineCertificateID(ctx, "mc-1")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(logs) != 30 {
		t.Fatalf("expected 30 logs after retention enforcement, got %d", len(logs))
	}

	// Verify the newest logs are kept (the last 30 created)
	// The oldest remaining should be log index 5 (created at now + 5s)
	oldestRemaining := logs[len(logs)-1]
	expectedOldestTime := now.Add(5 * time.Second)
	diff := oldestRemaining.CreatedAt.Sub(expectedOldestTime)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("expected oldest remaining log around %v, got %v", expectedOldestTime, oldestRemaining.CreatedAt)
	}
}

func TestDeploymentLogService_GetByMachineCertificateID_TimeDescOrder(t *testing.T) {
	svc := setupDeploymentLogServiceTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC()

	// Create logs with different timestamps
	for i := 0; i < 5; i++ {
		log := newTestDeploymentLog("mc-1", "m-1", "c-1", "success", now.Add(time.Duration(i)*time.Minute))
		if err := svc.Create(ctx, log); err != nil {
			t.Fatalf("failed to create log %d: %v", i, err)
		}
	}

	logs, err := svc.GetByMachineCertificateID(ctx, "mc-1")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(logs) != 5 {
		t.Fatalf("expected 5 logs, got %d", len(logs))
	}

	// Verify DESC order
	for i := 1; i < len(logs); i++ {
		if logs[i].CreatedAt.After(logs[i-1].CreatedAt) {
			t.Errorf("logs not in DESC order at index %d: %v > %v",
				i, logs[i].CreatedAt, logs[i-1].CreatedAt)
		}
	}
}

func TestDeploymentLogService_GetByMachineID(t *testing.T) {
	svc := setupDeploymentLogServiceTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC()

	// Create logs for machine m-1 across different machine certificates
	for i := 0; i < 3; i++ {
		log := newTestDeploymentLog(fmt.Sprintf("mc-%d", i), "m-1", "c-1", "success", now.Add(time.Duration(i)*time.Minute))
		if err := svc.Create(ctx, log); err != nil {
			t.Fatalf("failed to create log %d: %v", i, err)
		}
	}

	// Create log for different machine
	otherLog := newTestDeploymentLog("mc-10", "m-2", "c-1", "failed", now)
	if err := svc.Create(ctx, otherLog); err != nil {
		t.Fatalf("failed to create other log: %v", err)
	}

	logs, err := svc.GetByMachineID(ctx, "m-1")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(logs) != 3 {
		t.Fatalf("expected 3 logs for m-1, got %d", len(logs))
	}

	// Verify DESC order
	for i := 1; i < len(logs); i++ {
		if logs[i].CreatedAt.After(logs[i-1].CreatedAt) {
			t.Errorf("logs not in DESC order")
		}
	}
}

func TestDeploymentLogService_RetentionPerMachineCertificate(t *testing.T) {
	svc := setupDeploymentLogServiceTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC()

	// Create 35 logs for mc-1
	for i := 0; i < 35; i++ {
		log := newTestDeploymentLog("mc-1", "m-1", "c-1", "success", now.Add(time.Duration(i)*time.Second))
		if err := svc.Create(ctx, log); err != nil {
			t.Fatalf("failed to create log for mc-1: %v", err)
		}
	}

	// Create 10 logs for mc-2
	for i := 0; i < 10; i++ {
		log := newTestDeploymentLog("mc-2", "m-1", "c-2", "success", now.Add(time.Duration(i)*time.Second))
		if err := svc.Create(ctx, log); err != nil {
			t.Fatalf("failed to create log for mc-2: %v", err)
		}
	}

	// mc-1 should have exactly 30 logs
	logs1, err := svc.GetByMachineCertificateID(ctx, "mc-1")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(logs1) != 30 {
		t.Fatalf("expected 30 logs for mc-1, got %d", len(logs1))
	}

	// mc-2 should have all 10 logs (under limit)
	logs2, err := svc.GetByMachineCertificateID(ctx, "mc-2")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(logs2) != 10 {
		t.Fatalf("expected 10 logs for mc-2, got %d", len(logs2))
	}
}
