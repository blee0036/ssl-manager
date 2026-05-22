package repository

import (
	"context"
	"testing"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/model"
)

func setupAuditLogTestDB(t *testing.T) *AuditLogRepository {
	t.Helper()
	db := setupTestDB(t)

	// Create audit_logs table
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS audit_logs (
		id TEXT PRIMARY KEY,
		actor_type TEXT NOT NULL CHECK(actor_type IN ('user', 'agent', 'system')),
		actor_id TEXT NOT NULL,
		action TEXT NOT NULL,
		target_type TEXT NOT NULL,
		target_id TEXT DEFAULT '',
		detail TEXT DEFAULT '',
		ip TEXT DEFAULT '',
		created_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("failed to create audit_logs table: %v", err)
	}

	return NewAuditLogRepository(db)
}

func TestAuditLogRepository_Create(t *testing.T) {
	repo := setupAuditLogTestDB(t)
	ctx := context.Background()

	log := &model.AuditLog{
		ActorType:  "user",
		ActorID:    "user-1",
		Action:     "POST /api/certificates",
		TargetType: "certificate",
		TargetID:   "cert-1",
		Detail:     "created certificate for example.com",
		IP:         "192.168.1.1",
	}

	err := repo.Create(ctx, log)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if log.ID == "" {
		t.Error("expected ID to be generated")
	}
	if log.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestAuditLogRepository_Create_WithExistingID(t *testing.T) {
	repo := setupAuditLogTestDB(t)
	ctx := context.Background()

	log := &model.AuditLog{
		ID:         "custom-id-123",
		ActorType:  "agent",
		ActorID:    "machine-1",
		Action:     "deploy",
		TargetType: "machine_certificate",
		TargetID:   "mc-1",
		Detail:     "deployed certificate",
		IP:         "10.0.0.1",
		CreatedAt:  time.Now().UTC(),
	}

	err := repo.Create(ctx, log)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if log.ID != "custom-id-123" {
		t.Errorf("expected ID to remain custom-id-123, got %s", log.ID)
	}
}

func TestAuditLogRepository_List_TimeDescOrder(t *testing.T) {
	repo := setupAuditLogTestDB(t)
	ctx := context.Background()

	// Create logs with different timestamps
	now := time.Now().UTC()
	logs := []*model.AuditLog{
		{
			ActorType:  "user",
			ActorID:    "user-1",
			Action:     "POST /api/certificates",
			TargetType: "certificate",
			TargetID:   "cert-1",
			Detail:     "first action",
			IP:         "192.168.1.1",
			CreatedAt:  now.Add(-2 * time.Hour),
		},
		{
			ActorType:  "user",
			ActorID:    "user-1",
			Action:     "DELETE /api/certificates/cert-2",
			TargetType: "certificate",
			TargetID:   "cert-2",
			Detail:     "second action",
			IP:         "192.168.1.1",
			CreatedAt:  now.Add(-1 * time.Hour),
		},
		{
			ActorType:  "agent",
			ActorID:    "machine-1",
			Action:     "deploy",
			TargetType: "machine_certificate",
			TargetID:   "mc-1",
			Detail:     "third action",
			IP:         "10.0.0.1",
			CreatedAt:  now,
		},
	}

	for _, log := range logs {
		if err := repo.Create(ctx, log); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	// List all logs - should be in time DESC order
	result, err := repo.List(ctx, AuditLogFilter{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 logs, got %d", len(result))
	}

	// Verify DESC order: newest first
	if result[0].Detail != "third action" {
		t.Errorf("expected first result to be 'third action', got '%s'", result[0].Detail)
	}
	if result[1].Detail != "second action" {
		t.Errorf("expected second result to be 'second action', got '%s'", result[1].Detail)
	}
	if result[2].Detail != "first action" {
		t.Errorf("expected third result to be 'first action', got '%s'", result[2].Detail)
	}
}

func TestAuditLogRepository_List_FilterByActorType(t *testing.T) {
	repo := setupAuditLogTestDB(t)
	ctx := context.Background()

	// Create logs with different actor types
	logs := []*model.AuditLog{
		{ActorType: "user", ActorID: "user-1", Action: "create", TargetType: "certificate", IP: "1.1.1.1"},
		{ActorType: "agent", ActorID: "machine-1", Action: "deploy", TargetType: "machine_certificate", IP: "2.2.2.2"},
		{ActorType: "system", ActorID: "scheduler", Action: "renew", TargetType: "certificate", IP: ""},
	}

	for _, log := range logs {
		if err := repo.Create(ctx, log); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	// Filter by actor_type = "agent"
	result, err := repo.List(ctx, AuditLogFilter{ActorType: "agent"})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 log, got %d", len(result))
	}
	if result[0].ActorType != "agent" {
		t.Errorf("expected actor_type 'agent', got '%s'", result[0].ActorType)
	}
}

func TestAuditLogRepository_List_FilterByTargetType(t *testing.T) {
	repo := setupAuditLogTestDB(t)
	ctx := context.Background()

	logs := []*model.AuditLog{
		{ActorType: "user", ActorID: "user-1", Action: "create", TargetType: "certificate", IP: "1.1.1.1"},
		{ActorType: "user", ActorID: "user-1", Action: "create", TargetType: "machine", IP: "1.1.1.1"},
		{ActorType: "user", ActorID: "user-1", Action: "delete", TargetType: "certificate", IP: "1.1.1.1"},
	}

	for _, log := range logs {
		if err := repo.Create(ctx, log); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	result, err := repo.List(ctx, AuditLogFilter{TargetType: "certificate"})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(result))
	}
}

func TestAuditLogRepository_List_LimitAndOffset(t *testing.T) {
	repo := setupAuditLogTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		log := &model.AuditLog{
			ActorType:  "user",
			ActorID:    "user-1",
			Action:     "action",
			TargetType: "certificate",
			IP:         "1.1.1.1",
			CreatedAt:  now.Add(time.Duration(i) * time.Minute),
		}
		if err := repo.Create(ctx, log); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	// Get first 2
	result, err := repo.List(ctx, AuditLogFilter{Limit: 2})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(result))
	}

	// Get with offset
	result2, err := repo.List(ctx, AuditLogFilter{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("List with offset failed: %v", err)
	}
	if len(result2) != 2 {
		t.Fatalf("expected 2 logs with offset, got %d", len(result2))
	}

	// Ensure different results
	if result[0].ID == result2[0].ID {
		t.Error("expected different results with offset")
	}
}

func TestAuditLogRepository_CreateAuditLog_Interface(t *testing.T) {
	repo := setupAuditLogTestDB(t)
	ctx := context.Background()

	log := &model.AuditLog{
		ActorType:  "system",
		ActorID:    "scheduler",
		Action:     "renew",
		TargetType: "certificate",
		TargetID:   "cert-1",
		Detail:     "auto renewal triggered",
		IP:         "",
	}

	// Test the CreateAuditLog method (used by middleware interface)
	err := repo.CreateAuditLog(ctx, log)
	if err != nil {
		t.Fatalf("CreateAuditLog failed: %v", err)
	}

	// Verify it was saved
	result, err := repo.List(ctx, AuditLogFilter{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 log, got %d", len(result))
	}
	if result[0].Action != "renew" {
		t.Errorf("expected action 'renew', got '%s'", result[0].Action)
	}
}
