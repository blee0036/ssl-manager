package database

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewDB_CreatesDirectoryAndDatabase(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")

	db, err := NewDB(dataDir)
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	defer db.Close()

	// Verify data directory was created
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		t.Fatal("data directory was not created")
	}

	// Verify database file exists
	dbPath := filepath.Join(dataDir, "data.sqlite3")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("database file was not created")
	}
}

func TestNewDB_AllTablesCreated(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")

	db, err := NewDB(dataDir)
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	defer db.Close()

	expectedTables := []string{
		"users",
		"machines",
		"certificates",
		"machine_certificates",
		"deployment_logs",
		"domains",
		"domain_monitor_results",
		"alerts",
		"notification_channels",
		"audit_logs",
		"thirdpart_dns",
		"thirdpart_dns_sync_logs",
	}

	for _, table := range expectedTables {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestNewDB_WALModeEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")

	db, err := NewDB(dataDir)
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	defer db.Close()

	var journalMode string
	err = db.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if err != nil {
		t.Fatalf("failed to query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("expected journal_mode=wal, got %q", journalMode)
	}
}

func TestNewDB_ForeignKeysEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")

	db, err := NewDB(dataDir)
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	defer db.Close()

	var fkEnabled int
	err = db.QueryRow("PRAGMA foreign_keys").Scan(&fkEnabled)
	if err != nil {
		t.Fatalf("failed to query foreign_keys: %v", err)
	}
	if fkEnabled != 1 {
		t.Errorf("expected foreign_keys=1, got %d", fkEnabled)
	}
}

func TestNewDB_CloseAndReopen(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")

	// First open
	db, err := NewDB(dataDir)
	if err != nil {
		t.Fatalf("first NewDB failed: %v", err)
	}

	// Insert a test row
	_, err = db.Exec(`INSERT INTO users (id, username, password_hash, role, enabled, created_at, updated_at) 
		VALUES ('test-id', 'admin', 'hash', 'admin', 1, '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("failed to insert test row: %v", err)
	}

	// Close
	if err := db.Close(); err != nil {
		t.Fatalf("failed to close database: %v", err)
	}

	// Reopen
	db2, err := NewDB(dataDir)
	if err != nil {
		t.Fatalf("second NewDB failed: %v", err)
	}
	defer db2.Close()

	// Verify data persisted
	var username string
	err = db2.QueryRow("SELECT username FROM users WHERE id = 'test-id'").Scan(&username)
	if err != nil {
		t.Fatalf("failed to query after reopen: %v", err)
	}
	if username != "admin" {
		t.Errorf("expected username=admin, got %q", username)
	}
}

func TestHasAdminUser_NoUsers(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")

	db, err := NewDB(dataDir)
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	defer db.Close()

	hasAdmin, err := db.HasAdminUser()
	if err != nil {
		t.Fatalf("HasAdminUser failed: %v", err)
	}
	if hasAdmin {
		t.Error("expected no admin user in empty database")
	}
}

func TestHasAdminUser_WithAdminUser(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")

	db, err := NewDB(dataDir)
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	defer db.Close()

	// Insert an admin user
	_, err = db.Exec(`INSERT INTO users (id, username, password_hash, role, enabled, created_at, updated_at) 
		VALUES ('admin-id', 'admin', 'hash', 'admin', 1, '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("failed to insert admin user: %v", err)
	}

	hasAdmin, err := db.HasAdminUser()
	if err != nil {
		t.Fatalf("HasAdminUser failed: %v", err)
	}
	if !hasAdmin {
		t.Error("expected admin user to exist")
	}
}

func TestHasAdminUser_DisabledAdminNotCounted(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")

	db, err := NewDB(dataDir)
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	defer db.Close()

	// Insert a disabled admin user
	_, err = db.Exec(`INSERT INTO users (id, username, password_hash, role, enabled, created_at, updated_at) 
		VALUES ('admin-id', 'admin', 'hash', 'admin', 0, '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("failed to insert disabled admin user: %v", err)
	}

	hasAdmin, err := db.HasAdminUser()
	if err != nil {
		t.Fatalf("HasAdminUser failed: %v", err)
	}
	if hasAdmin {
		t.Error("expected disabled admin user not to be counted")
	}
}

func TestHasAdminUser_RegularUserNotCounted(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")

	db, err := NewDB(dataDir)
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	defer db.Close()

	// Insert a regular user
	_, err = db.Exec(`INSERT INTO users (id, username, password_hash, role, enabled, created_at, updated_at) 
		VALUES ('user-id', 'viewer', 'hash', 'user', 1, '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("failed to insert regular user: %v", err)
	}

	hasAdmin, err := db.HasAdminUser()
	if err != nil {
		t.Fatalf("HasAdminUser failed: %v", err)
	}
	if hasAdmin {
		t.Error("expected regular user not to be counted as admin")
	}
}

func TestNewDB_MigrateIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")

	// First open runs migration
	db, err := NewDB(dataDir)
	if err != nil {
		t.Fatalf("first NewDB failed: %v", err)
	}

	// Run migrate again explicitly - should not fail
	if err := db.Migrate(); err != nil {
		t.Fatalf("second Migrate failed: %v", err)
	}

	db.Close()

	// Third open also runs migration - should not fail
	db2, err := NewDB(dataDir)
	if err != nil {
		t.Fatalf("third NewDB failed: %v", err)
	}
	db2.Close()
}
