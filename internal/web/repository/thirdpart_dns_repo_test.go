package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/model"
)

// setupThirdpartDNSTestDB creates a test DB with thirdpart_dns and sync_logs tables.
func setupThirdpartDNSTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db := setupTestDB(t)

	// Add thirdpart_dns table
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS thirdpart_dns (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		type TEXT NOT NULL DEFAULT 'cloudflare' CHECK(type IN ('cloudflare')),
		api_token TEXT NOT NULL,
		config_json TEXT NOT NULL DEFAULT '{}',
		main_domains TEXT NOT NULL DEFAULT '[]',
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("failed to create thirdpart_dns table: %v", err)
	}

	// Add thirdpart_dns_sync_logs table
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS thirdpart_dns_sync_logs (
		id TEXT PRIMARY KEY,
		thirdpart_dns_id TEXT NOT NULL REFERENCES thirdpart_dns(id),
		records_count INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL CHECK(status IN ('success', 'failed')),
		error_message TEXT DEFAULT '',
		synced_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("failed to create thirdpart_dns_sync_logs table: %v", err)
	}

	return db
}

func TestThirdpartDNSRepository_Create(t *testing.T) {
	db := setupThirdpartDNSTestDB(t)
	repo := NewThirdpartDNSRepository(db)
	ctx := context.Background()

	config := &model.ThirdpartDNS{
		Name:        "My Cloudflare",
		Type:        "cloudflare",
		APIToken:    "test-token-123",
		ConfigJSON:  `{"zone_id": "abc"}`,
		MainDomains: []string{"example.com", "test.com"},
		Enabled:     true,
	}

	err := repo.Create(ctx, config)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if config.ID == "" {
		t.Fatal("expected ID to be generated")
	}
	if config.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be set")
	}
}

func TestThirdpartDNSRepository_GetByID(t *testing.T) {
	db := setupThirdpartDNSTestDB(t)
	repo := NewThirdpartDNSRepository(db)
	ctx := context.Background()

	config := &model.ThirdpartDNS{
		Name:        "My Cloudflare",
		Type:        "cloudflare",
		APIToken:    "test-token-123",
		ConfigJSON:  `{"zone_id": "abc"}`,
		MainDomains: []string{"example.com"},
		Enabled:     true,
	}

	if err := repo.Create(ctx, config); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := repo.GetByID(ctx, config.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if got.Name != "My Cloudflare" {
		t.Errorf("expected name 'My Cloudflare', got '%s'", got.Name)
	}
	if got.Type != "cloudflare" {
		t.Errorf("expected type 'cloudflare', got '%s'", got.Type)
	}
	if got.APIToken != "test-token-123" {
		t.Errorf("expected api_token 'test-token-123', got '%s'", got.APIToken)
	}
	if len(got.MainDomains) != 1 || got.MainDomains[0] != "example.com" {
		t.Errorf("expected main_domains ['example.com'], got %v", got.MainDomains)
	}
	if !got.Enabled {
		t.Error("expected Enabled to be true")
	}
}

func TestThirdpartDNSRepository_GetByID_NotFound(t *testing.T) {
	db := setupThirdpartDNSTestDB(t)
	repo := NewThirdpartDNSRepository(db)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, "nonexistent")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestThirdpartDNSRepository_List(t *testing.T) {
	db := setupThirdpartDNSTestDB(t)
	repo := NewThirdpartDNSRepository(db)
	ctx := context.Background()

	// Create multiple configs
	for i := 0; i < 3; i++ {
		config := &model.ThirdpartDNS{
			Name:        fmt.Sprintf("Config %d", i),
			Type:        "cloudflare",
			APIToken:    fmt.Sprintf("token-%d", i),
			ConfigJSON:  "{}",
			MainDomains: []string{},
			Enabled:     true,
		}
		if err := repo.Create(ctx, config); err != nil {
			t.Fatalf("Create %d failed: %v", i, err)
		}
	}

	configs, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(configs) != 3 {
		t.Errorf("expected 3 configs, got %d", len(configs))
	}
}

func TestThirdpartDNSRepository_Update(t *testing.T) {
	db := setupThirdpartDNSTestDB(t)
	repo := NewThirdpartDNSRepository(db)
	ctx := context.Background()

	config := &model.ThirdpartDNS{
		Name:        "Original",
		Type:        "cloudflare",
		APIToken:    "token-1",
		ConfigJSON:  "{}",
		MainDomains: []string{"example.com"},
		Enabled:     true,
	}

	if err := repo.Create(ctx, config); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Update name and main_domains
	updates := map[string]interface{}{
		"name":         "Updated",
		"main_domains": []string{"new.com", "other.com"},
		"enabled":      false,
	}

	if err := repo.Update(ctx, config.ID, updates); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	got, err := repo.GetByID(ctx, config.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if got.Name != "Updated" {
		t.Errorf("expected name 'Updated', got '%s'", got.Name)
	}
	if len(got.MainDomains) != 2 || got.MainDomains[0] != "new.com" {
		t.Errorf("expected main_domains ['new.com', 'other.com'], got %v", got.MainDomains)
	}
	if got.Enabled {
		t.Error("expected Enabled to be false")
	}
}

func TestThirdpartDNSRepository_Update_NotFound(t *testing.T) {
	db := setupThirdpartDNSTestDB(t)
	repo := NewThirdpartDNSRepository(db)
	ctx := context.Background()

	err := repo.Update(ctx, "nonexistent", map[string]interface{}{"name": "test"})
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestThirdpartDNSRepository_Delete(t *testing.T) {
	db := setupThirdpartDNSTestDB(t)
	repo := NewThirdpartDNSRepository(db)
	ctx := context.Background()

	config := &model.ThirdpartDNS{
		Name:        "To Delete",
		Type:        "cloudflare",
		APIToken:    "token-del",
		ConfigJSON:  "{}",
		MainDomains: []string{},
		Enabled:     true,
	}

	if err := repo.Create(ctx, config); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Add a sync log
	syncLog := &model.ThirdpartDNSSyncLog{
		ThirdpartDNSID: config.ID,
		RecordsCount:   5,
		Status:         "success",
		SyncedAt:       time.Now().UTC(),
	}
	if err := repo.SaveSyncLog(ctx, syncLog); err != nil {
		t.Fatalf("SaveSyncLog failed: %v", err)
	}

	// Delete should remove both config and sync logs
	if err := repo.Delete(ctx, config.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify config is gone
	_, err := repo.GetByID(ctx, config.ID)
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows after delete, got %v", err)
	}

	// Verify sync logs are gone
	logs, err := repo.GetSyncLogs(ctx, config.ID)
	if err != nil {
		t.Fatalf("GetSyncLogs failed: %v", err)
	}
	if len(logs) != 0 {
		t.Errorf("expected 0 sync logs after delete, got %d", len(logs))
	}
}

func TestThirdpartDNSRepository_Delete_NotFound(t *testing.T) {
	db := setupThirdpartDNSTestDB(t)
	repo := NewThirdpartDNSRepository(db)
	ctx := context.Background()

	err := repo.Delete(ctx, "nonexistent")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestThirdpartDNSRepository_SaveSyncLog(t *testing.T) {
	db := setupThirdpartDNSTestDB(t)
	repo := NewThirdpartDNSRepository(db)
	ctx := context.Background()

	// Create a config first
	config := &model.ThirdpartDNS{
		Name:        "Test Config",
		Type:        "cloudflare",
		APIToken:    "token",
		ConfigJSON:  "{}",
		MainDomains: []string{},
		Enabled:     true,
	}
	if err := repo.Create(ctx, config); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Save a sync log
	syncLog := &model.ThirdpartDNSSyncLog{
		ThirdpartDNSID: config.ID,
		RecordsCount:   10,
		Status:         "success",
		SyncedAt:       time.Now().UTC(),
	}

	if err := repo.SaveSyncLog(ctx, syncLog); err != nil {
		t.Fatalf("SaveSyncLog failed: %v", err)
	}

	if syncLog.ID == "" {
		t.Fatal("expected sync log ID to be generated")
	}
}

func TestThirdpartDNSRepository_GetSyncLogs(t *testing.T) {
	db := setupThirdpartDNSTestDB(t)
	repo := NewThirdpartDNSRepository(db)
	ctx := context.Background()

	// Create a config
	config := &model.ThirdpartDNS{
		Name:        "Test Config",
		Type:        "cloudflare",
		APIToken:    "token",
		ConfigJSON:  "{}",
		MainDomains: []string{},
		Enabled:     true,
	}
	if err := repo.Create(ctx, config); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Save multiple sync logs
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		syncLog := &model.ThirdpartDNSSyncLog{
			ThirdpartDNSID: config.ID,
			RecordsCount:   i * 5,
			Status:         "success",
			SyncedAt:       now.Add(time.Duration(i) * time.Minute),
		}
		if err := repo.SaveSyncLog(ctx, syncLog); err != nil {
			t.Fatalf("SaveSyncLog %d failed: %v", i, err)
		}
	}

	// Save a failed log
	failedLog := &model.ThirdpartDNSSyncLog{
		ThirdpartDNSID: config.ID,
		RecordsCount:   0,
		Status:         "failed",
		ErrorMessage:   "connection timeout",
		SyncedAt:       now.Add(5 * time.Minute),
	}
	if err := repo.SaveSyncLog(ctx, failedLog); err != nil {
		t.Fatalf("SaveSyncLog failed: %v", err)
	}

	logs, err := repo.GetSyncLogs(ctx, config.ID)
	if err != nil {
		t.Fatalf("GetSyncLogs failed: %v", err)
	}

	if len(logs) != 4 {
		t.Fatalf("expected 4 sync logs, got %d", len(logs))
	}

	// Should be ordered by synced_at DESC (most recent first)
	if logs[0].Status != "failed" {
		t.Errorf("expected first log to be 'failed' (most recent), got '%s'", logs[0].Status)
	}
	if logs[0].ErrorMessage != "connection timeout" {
		t.Errorf("expected error message 'connection timeout', got '%s'", logs[0].ErrorMessage)
	}
}

func TestThirdpartDNSRepository_EmptyMainDomains(t *testing.T) {
	db := setupThirdpartDNSTestDB(t)
	repo := NewThirdpartDNSRepository(db)
	ctx := context.Background()

	config := &model.ThirdpartDNS{
		Name:        "Empty Domains",
		Type:        "cloudflare",
		APIToken:    "token",
		ConfigJSON:  "{}",
		MainDomains: []string{},
		Enabled:     true,
	}

	if err := repo.Create(ctx, config); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := repo.GetByID(ctx, config.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if got.MainDomains == nil {
		t.Error("expected MainDomains to be non-nil empty slice")
	}
	if len(got.MainDomains) != 0 {
		t.Errorf("expected 0 main domains, got %d", len(got.MainDomains))
	}
}
