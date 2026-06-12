package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/model"
)

func setupAlertTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := setupTestDB(t)

	tables := []string{
		`CREATE TABLE IF NOT EXISTS alerts (
			id TEXT PRIMARY KEY,
			level TEXT NOT NULL CHECK(level IN ('info', 'warning', 'critical')),
			type TEXT NOT NULL,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active', 'resolved', 'suppressed')),
			target_type TEXT DEFAULT '',
			target_id TEXT DEFAULT '',
			sent_channels TEXT DEFAULT '',
			created_at TEXT NOT NULL,
			resolved_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS notification_channels (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL CHECK(type IN ('lark', 'telegram')),
			name TEXT NOT NULL,
			config_json TEXT NOT NULL DEFAULT '{}',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
	}

	for _, stmt := range tables {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("failed to create table: %v", err)
		}
	}

	return db
}

func TestAlertRepository_Create_And_GetByID(t *testing.T) {
	db := setupAlertTestDB(t)
	repo := NewAlertRepository(db)
	ctx := context.Background()

	alert := &model.Alert{
		Level:        "warning",
		Type:         "cert_expiring",
		Title:        "Certificate Expiring",
		Content:      "Certificate xyz expires in 10 days",
		TargetType:   "certificate",
		TargetID:     "cert-123",
		SentChannels: []string{"ch-1", "ch-2"},
	}

	if err := repo.Create(ctx, alert); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if alert.ID == "" {
		t.Fatal("expected ID to be set")
	}

	// Get by ID
	got, err := repo.GetByID(ctx, alert.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if got.Level != "warning" {
		t.Errorf("expected level 'warning', got %q", got.Level)
	}
	if got.Type != "cert_expiring" {
		t.Errorf("expected type 'cert_expiring', got %q", got.Type)
	}
	if got.Title != "Certificate Expiring" {
		t.Errorf("expected title 'Certificate Expiring', got %q", got.Title)
	}
	if got.Status != "active" {
		t.Errorf("expected status 'active', got %q", got.Status)
	}
	if got.TargetType != "certificate" {
		t.Errorf("expected target_type 'certificate', got %q", got.TargetType)
	}
	if got.TargetID != "cert-123" {
		t.Errorf("expected target_id 'cert-123', got %q", got.TargetID)
	}
	if len(got.SentChannels) != 2 {
		t.Errorf("expected 2 sent channels, got %d", len(got.SentChannels))
	}
}

func TestAlertRepository_List_WithFilter(t *testing.T) {
	db := setupAlertTestDB(t)
	repo := NewAlertRepository(db)
	ctx := context.Background()

	// Create multiple alerts
	alerts := []*model.Alert{
		{Level: "warning", Type: "cert_expiring", Title: "Alert 1", Content: "Content 1", TargetType: "certificate", TargetID: "c1"},
		{Level: "critical", Type: "agent_offline", Title: "Alert 2", Content: "Content 2", TargetType: "machine", TargetID: "m1"},
		{Level: "warning", Type: "cert_expiring", Title: "Alert 3", Content: "Content 3", TargetType: "certificate", TargetID: "c2"},
	}

	for _, a := range alerts {
		if err := repo.Create(ctx, a); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	// Filter by level
	result, _, err := repo.List(ctx, model.AlertFilter{Level: "warning"})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 warning alerts, got %d", len(result))
	}

	// Filter by type
	result, _, err = repo.List(ctx, model.AlertFilter{Type: "agent_offline"})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 agent_offline alert, got %d", len(result))
	}

	// No filter
	result, _, err = repo.List(ctx, model.AlertFilter{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("expected 3 alerts, got %d", len(result))
	}
}

func TestAlertRepository_FindActiveByTarget(t *testing.T) {
	db := setupAlertTestDB(t)
	repo := NewAlertRepository(db)
	ctx := context.Background()

	// Create an active alert
	alert := &model.Alert{
		Level:      "warning",
		Type:       "cert_expiring",
		Title:      "Certificate Expiring",
		Content:    "Content",
		Status:     "active",
		TargetType: "certificate",
		TargetID:   "cert-123",
	}
	if err := repo.Create(ctx, alert); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Should find the active alert
	found, err := repo.FindActiveByTarget(ctx, "certificate", "cert-123", "cert_expiring")
	if err != nil {
		t.Fatalf("FindActiveByTarget failed: %v", err)
	}
	if found == nil {
		t.Fatal("expected to find active alert")
	}
	if found.ID != alert.ID {
		t.Errorf("expected alert ID %q, got %q", alert.ID, found.ID)
	}

	// Should not find for different target
	found, err = repo.FindActiveByTarget(ctx, "certificate", "cert-999", "cert_expiring")
	if err != nil {
		t.Fatalf("FindActiveByTarget failed: %v", err)
	}
	if found != nil {
		t.Error("expected nil for non-existent target")
	}

	// Should not find for different type
	found, err = repo.FindActiveByTarget(ctx, "certificate", "cert-123", "agent_offline")
	if err != nil {
		t.Fatalf("FindActiveByTarget failed: %v", err)
	}
	if found != nil {
		t.Error("expected nil for different alert type")
	}
}

func TestAlertRepository_UpdateStatus(t *testing.T) {
	db := setupAlertTestDB(t)
	repo := NewAlertRepository(db)
	ctx := context.Background()

	alert := &model.Alert{
		Level:      "critical",
		Type:       "agent_offline",
		Title:      "Agent Offline",
		Content:    "Machine xyz is offline",
		TargetType: "machine",
		TargetID:   "m-1",
	}
	if err := repo.Create(ctx, alert); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Update status to resolved
	now := time.Now().UTC()
	if err := repo.UpdateStatus(ctx, alert.ID, "resolved", &now); err != nil {
		t.Fatalf("UpdateStatus failed: %v", err)
	}

	// Verify
	got, err := repo.GetByID(ctx, alert.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.Status != "resolved" {
		t.Errorf("expected status 'resolved', got %q", got.Status)
	}
	if got.ResolvedAt == nil {
		t.Error("expected resolved_at to be set")
	}

	// After resolving, FindActiveByTarget should not find it
	found, err := repo.FindActiveByTarget(ctx, "machine", "m-1", "agent_offline")
	if err != nil {
		t.Fatalf("FindActiveByTarget failed: %v", err)
	}
	if found != nil {
		t.Error("expected nil after resolving alert")
	}
}

// --- NotificationChannelRepository Tests ---

func TestNotificationChannelRepository_CRUD(t *testing.T) {
	db := setupAlertTestDB(t)
	repo := NewNotificationChannelRepository(db)
	ctx := context.Background()

	// Create
	ch := &model.NotificationChannel{
		Type:       "lark",
		Name:       "My Lark Channel",
		ConfigJSON: `{"webhook_url":"https://open.lark.com/hook/abc"}`,
		Enabled:    true,
	}
	if err := repo.Create(ctx, ch); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if ch.ID == "" {
		t.Fatal("expected ID to be set")
	}

	// GetByID
	got, err := repo.GetByID(ctx, ch.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.Type != "lark" {
		t.Errorf("expected type 'lark', got %q", got.Type)
	}
	if got.Name != "My Lark Channel" {
		t.Errorf("expected name 'My Lark Channel', got %q", got.Name)
	}
	if !got.Enabled {
		t.Error("expected enabled to be true")
	}

	// Update
	if err := repo.Update(ctx, ch.ID, map[string]interface{}{"name": "Updated Name", "enabled": 0}); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	got, _ = repo.GetByID(ctx, ch.ID)
	if got.Name != "Updated Name" {
		t.Errorf("expected name 'Updated Name', got %q", got.Name)
	}
	if got.Enabled {
		t.Error("expected enabled to be false after update")
	}

	// List
	ch2 := &model.NotificationChannel{
		Type:       "telegram",
		Name:       "My Telegram",
		ConfigJSON: `{"bot_token":"123","chat_id":"456"}`,
		Enabled:    true,
	}
	if err := repo.Create(ctx, ch2); err != nil {
		t.Fatalf("Create ch2 failed: %v", err)
	}

	all, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 channels, got %d", len(all))
	}

	// ListEnabled (ch1 is disabled, ch2 is enabled)
	enabled, err := repo.ListEnabled(ctx)
	if err != nil {
		t.Fatalf("ListEnabled failed: %v", err)
	}
	if len(enabled) != 1 {
		t.Errorf("expected 1 enabled channel, got %d", len(enabled))
	}
	if enabled[0].Type != "telegram" {
		t.Errorf("expected telegram channel, got %q", enabled[0].Type)
	}

	// Delete
	if err := repo.Delete(ctx, ch.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	all, _ = repo.List(ctx)
	if len(all) != 1 {
		t.Errorf("expected 1 channel after delete, got %d", len(all))
	}
}
