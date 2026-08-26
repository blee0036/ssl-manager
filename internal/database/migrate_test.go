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

// TestMigrate_DropsLeftoverDomainsNameNormalizedUniqueIndex verifies the
// bugfix regression scenario: a database that ran an OLDER version of Migrate()
// (from the now-reverted cloudflare-domain-auto-sync feature) already has the
// idx_domains_name_normalized unique index in place. That index was a design
// error — it enforced global uniqueness of hostnames, which breaks legitimate
// multi-record scenarios (e.g. two A records, or an A and an AAAA record, for
// the same hostname). Migrate() must now DROP it so ThirdpartDNSService's
// per-dns_record_id sync semantics work correctly again, and must remain
// idempotent whether or not the index was present beforehand.
func TestMigrate_DropsLeftoverDomainsNameNormalizedUniqueIndex(t *testing.T) {
	db := openTestDB(t)

	// Simulate a database that already ran the old migration and has the
	// leftover unique index in place, pre-dating a call to the current Migrate().
	if _, err := db.Exec(`CREATE TABLE domains (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		source TEXT DEFAULT 'manual',
		thirdpart_dns_id TEXT DEFAULT '',
		dns_record_id TEXT DEFAULT '',
		dns_record_type TEXT DEFAULT '',
		dns_record_value TEXT DEFAULT '',
		monitor_port INTEGER NOT NULL DEFAULT 443,
		linked_machine_id TEXT,
		linked_certificate_id TEXT,
		linked_machine_certificate_id TEXT,
		monitor_enabled INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("failed to create legacy domains table: %v", err)
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX idx_domains_name_normalized ON domains(LOWER(RTRIM(name, '.')))`); err != nil {
		t.Fatalf("failed to create leftover unique index: %v", err)
	}

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate should drop the leftover unique index and succeed, got: %v", err)
	}

	// Two rows sharing the same normalized hostname (e.g. two A records for the
	// same host, or an A + AAAA pair) must now be insertable — the unique index
	// no longer exists.
	if _, err := db.Exec(`INSERT INTO domains (id, name, dns_record_id, created_at, updated_at) VALUES ('rec-a', 'www.example.com', 'cf-rec-1', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("first insert failed: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO domains (id, name, dns_record_id, created_at, updated_at) VALUES ('rec-b', 'www.example.com', 'cf-rec-2', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("expected a second row sharing the same hostname (different dns_record_id, e.g. a second A record) to succeed now that the unique index is gone, got: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM domains WHERE name = 'www.example.com'`).Scan(&count); err != nil {
		t.Fatalf("failed to count rows: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 rows for www.example.com, got %d", count)
	}

	// Idempotency: a second Migrate() call (index already gone) must still succeed.
	if err := db.Migrate(); err != nil {
		t.Fatalf("second Migrate() call should be idempotent, got: %v", err)
	}
}

// TestMigrate_DropIndexNoopWhenNeverExisted verifies that on a database that
// never had idx_domains_name_normalized (e.g. a brand new database, or one
// that never ran the reverted feature's migration), Migrate() still succeeds:
// DROP INDEX IF EXISTS is a safe no-op when the index is absent.
func TestMigrate_DropIndexNoopWhenNeverExisted(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := NewDB(tmpDir)
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	defer db.Close()

	// Multiple A/AAAA records for the same hostname must coexist without error.
	if _, err := db.Exec(`INSERT INTO domains (id, name, source, dns_record_id, monitor_port, monitor_enabled, alert_ignored, created_at, updated_at)
		VALUES ('rec-a', 'multi.example.com', 'cloudflare', 'cf-1', 443, 1, 0, '2024-01-01', '2024-01-01')`); err != nil {
		t.Fatalf("first insert failed: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO domains (id, name, source, dns_record_id, monitor_port, monitor_enabled, alert_ignored, created_at, updated_at)
		VALUES ('rec-b', 'multi.example.com', 'cloudflare', 'cf-2', 443, 1, 0, '2024-01-01', '2024-01-01')`); err != nil {
		t.Fatalf("second insert for the same hostname (different dns_record_id) should succeed, got: %v", err)
	}
}
