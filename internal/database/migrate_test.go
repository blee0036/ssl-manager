package database

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/glebarez/sqlite"
)

// openTestDB opens an in-memory SQLite database wrapped in the DB struct.
func openTestDB(t *testing.T) *DB {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return &DB{DB: sqlDB, dataDir: ""}
}

func TestMigrateAddColumnIfNotExists_AddsNewColumn(t *testing.T) {
	db := openTestDB(t)

	// Create a table without alert_ignored column
	_, err := db.Exec(`CREATE TABLE domains (id TEXT PRIMARY KEY, name TEXT NOT NULL)`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	// Add column
	err = db.migrateAddColumnIfNotExists("domains", "alert_ignored", "INTEGER NOT NULL DEFAULT 0")
	if err != nil {
		t.Fatalf("migrateAddColumnIfNotExists failed: %v", err)
	}

	// Verify column exists by inserting data
	_, err = db.Exec(`INSERT INTO domains (id, name, alert_ignored) VALUES ('1', 'example.com', 0)`)
	if err != nil {
		t.Fatalf("insert with new column failed: %v", err)
	}

	var alertIgnored int
	err = db.QueryRow(`SELECT alert_ignored FROM domains WHERE id = '1'`).Scan(&alertIgnored)
	if err != nil {
		t.Fatalf("select new column failed: %v", err)
	}
	if alertIgnored != 0 {
		t.Errorf("expected alert_ignored = 0, got %d", alertIgnored)
	}
}

func TestMigrateAddColumnIfNotExists_IdempotentWhenColumnExists(t *testing.T) {
	db := openTestDB(t)

	// Create table that already has alert_ignored
	_, err := db.Exec(`CREATE TABLE domains (id TEXT PRIMARY KEY, name TEXT NOT NULL, alert_ignored INTEGER NOT NULL DEFAULT 0)`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	// Call should succeed silently (column already exists)
	err = db.migrateAddColumnIfNotExists("domains", "alert_ignored", "INTEGER NOT NULL DEFAULT 0")
	if err != nil {
		t.Fatalf("migrateAddColumnIfNotExists should be idempotent, got: %v", err)
	}
}

func TestMigrateAddColumnIfNotExists_RepeatedCallsIdempotent(t *testing.T) {
	db := openTestDB(t)

	_, err := db.Exec(`CREATE TABLE domains (id TEXT PRIMARY KEY, name TEXT NOT NULL)`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	// First call adds the column
	err = db.migrateAddColumnIfNotExists("domains", "alert_ignored", "INTEGER NOT NULL DEFAULT 0")
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	// Second call should not error
	err = db.migrateAddColumnIfNotExists("domains", "alert_ignored", "INTEGER NOT NULL DEFAULT 0")
	if err != nil {
		t.Fatalf("second call should be idempotent, got: %v", err)
	}

	// Third call should not error
	err = db.migrateAddColumnIfNotExists("domains", "alert_ignored", "INTEGER NOT NULL DEFAULT 0")
	if err != nil {
		t.Fatalf("third call should be idempotent, got: %v", err)
	}
}

func TestMigrate_FullFlow_NewDB(t *testing.T) {
	// Use a temp directory to test with NewDB which calls Migrate() internally
	tmpDir := t.TempDir()
	db, err := NewDB(tmpDir)
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	defer db.Close()

	// Verify alert_ignored column exists in domains
	var alertIgnored int
	_, err = db.Exec(`INSERT INTO domains (id, name, source, monitor_port, monitor_enabled, alert_ignored, created_at, updated_at)
		VALUES ('test1', 'example.com', 'manual', 443, 1, 0, '2024-01-01', '2024-01-01')`)
	if err != nil {
		t.Fatalf("insert into domains with alert_ignored failed: %v", err)
	}
	err = db.QueryRow(`SELECT alert_ignored FROM domains WHERE id = 'test1'`).Scan(&alertIgnored)
	if err != nil {
		t.Fatalf("select alert_ignored failed: %v", err)
	}

	// Verify dns_record_id column exists in domains
	var dnsRecordID string
	err = db.QueryRow(`SELECT dns_record_id FROM domains WHERE id = 'test1'`).Scan(&dnsRecordID)
	if err != nil {
		t.Fatalf("select dns_record_id failed: %v", err)
	}

	// Verify sync log columns exist (insert parent row first for FK)
	_, err = db.Exec(`INSERT INTO thirdpart_dns (id, name, type, api_token, config_json, main_domains, enabled, created_at, updated_at)
		VALUES ('dns1', 'test-cf', 'cloudflare', 'token123', '{}', '[]', 1, '2024-01-01', '2024-01-01')`)
	if err != nil {
		t.Fatalf("insert into thirdpart_dns failed: %v", err)
	}
	_, err = db.Exec(`INSERT INTO thirdpart_dns_sync_logs (id, thirdpart_dns_id, records_count, status, error_message, new_domains, updated_domains, removed_domains, synced_at)
		VALUES ('log1', 'dns1', 5, 'success', '', '["a.com"]', '["b.com"]', '["c.com"]', '2024-01-01')`)
	if err != nil {
		t.Fatalf("insert into sync_logs with new columns failed: %v", err)
	}

	var newDomains, updatedDomains, removedDomains string
	err = db.QueryRow(`SELECT new_domains, updated_domains, removed_domains FROM thirdpart_dns_sync_logs WHERE id = 'log1'`).
		Scan(&newDomains, &updatedDomains, &removedDomains)
	if err != nil {
		t.Fatalf("select sync log columns failed: %v", err)
	}
	if newDomains != `["a.com"]` || updatedDomains != `["b.com"]` || removedDomains != `["c.com"]` {
		t.Errorf("unexpected sync log values: new=%s updated=%s removed=%s", newDomains, updatedDomains, removedDomains)
	}
}

func TestMigrate_RepeatedCallsOnExistingDB(t *testing.T) {
	tmpDir := t.TempDir()

	// First call: creates DB and runs migration
	db1, err := NewDB(tmpDir)
	if err != nil {
		t.Fatalf("first NewDB failed: %v", err)
	}
	db1.Close()

	// Second call: opens same DB, Migrate() should be idempotent
	db2, err := NewDB(tmpDir)
	if err != nil {
		t.Fatalf("second NewDB (repeated migrate) failed: %v", err)
	}
	db2.Close()

	// Verify DB file exists
	dbPath := filepath.Join(tmpDir, "data.sqlite3")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("database file not created")
	}
}

func TestMigrate_DomainsNameNormalizedUniqueIndex(t *testing.T) {
	db := openTestDB(t)
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// First insert succeeds.
	_, err := db.Exec(`INSERT INTO domains (id, name, source, monitor_port, monitor_enabled, alert_ignored, created_at, updated_at)
		VALUES ('domain-1', 'example.com', 'manual', 443, 1, 0, '2024-01-01', '2024-01-01')`)
	if err != nil {
		t.Fatalf("first insert failed: %v", err)
	}

	// Second insert with the exact same name must fail: idx_domains_name_normalized
	// is a UNIQUE index on LOWER(RTRIM(name, '.')), and this raw INSERT has no
	// ON CONFLICT clause, so a uniqueness violation must surface as an error.
	_, err = db.Exec(`INSERT INTO domains (id, name, source, monitor_port, monitor_enabled, alert_ignored, created_at, updated_at)
		VALUES ('domain-2', 'example.com', 'manual', 443, 1, 0, '2024-01-01', '2024-01-01')`)
	if err == nil {
		t.Fatal("expected duplicate name insert to fail due to unique index, got nil error")
	}

	// A name that differs only by case and a trailing dot normalizes to the same
	// value (LOWER(RTRIM('Example.COM.', '.')) == 'example.com') and must also conflict.
	_, err = db.Exec(`INSERT INTO domains (id, name, source, monitor_port, monitor_enabled, alert_ignored, created_at, updated_at)
		VALUES ('domain-3', 'Example.COM.', 'manual', 443, 1, 0, '2024-01-01', '2024-01-01')`)
	if err == nil {
		t.Fatal("expected case/trailing-dot normalized duplicate insert to fail due to unique index, got nil error")
	}

	// A genuinely different domain name must not be blocked by the index.
	_, err = db.Exec(`INSERT INTO domains (id, name, source, monitor_port, monitor_enabled, alert_ignored, created_at, updated_at)
		VALUES ('domain-4', 'other.com', 'manual', 443, 1, 0, '2024-01-01', '2024-01-01')`)
	if err != nil {
		t.Fatalf("insert of a genuinely different domain name should succeed, got error: %v", err)
	}
}
