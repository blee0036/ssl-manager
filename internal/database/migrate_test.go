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

// TestMigrate_DedupesPreExistingDuplicateDomainsBeforeUniqueIndex reproduces
// the production bug: a domains table created by an OLDER version of the
// schema (before idx_domains_name_normalized existed) already contains rows
// whose names collide once normalized (case/trailing-dot differences). This
// must no longer make Migrate() fail — the duplicates must be cleaned up
// before the unique index is created, and Migrate() must succeed.
func TestMigrate_DedupesPreExistingDuplicateDomainsBeforeUniqueIndex(t *testing.T) {
	db := openTestDB(t)

	// Simulate a pre-migration domains table (no unique index yet) already
	// containing case/trailing-dot duplicates, as would exist on a real
	// production database that predates this feature.
	if _, err := db.Exec(`CREATE TABLE domains (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		source TEXT DEFAULT 'manual',
		thirdpart_dns_id TEXT DEFAULT '',
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

	rows := []struct {
		id, name, createdAt, linkedMachineID string
	}{
		{"dup-1", "Example.com", "2024-01-01T00:00:00Z", ""},           // earliest, no association
		{"dup-2", "example.com.", "2024-01-02T00:00:00Z", "machine-1"}, // later, but has an association
		{"dup-3", "EXAMPLE.COM", "2024-01-03T00:00:00Z", ""},           // latest, no association
		{"keep-me", "other.com", "2024-01-01T00:00:00Z", ""},           // not part of any collision
	}
	for _, r := range rows {
		linkedMachineID := interface{}(nil)
		if r.linkedMachineID != "" {
			linkedMachineID = r.linkedMachineID
		}
		if _, err := db.Exec(`INSERT INTO domains (id, name, linked_machine_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
			r.id, r.name, linkedMachineID, r.createdAt, r.createdAt); err != nil {
			t.Fatalf("failed to seed domain %s: %v", r.id, err)
		}
	}

	// Migrate() must succeed despite the pre-existing collisions.
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate should dedupe pre-existing duplicates and succeed, got: %v", err)
	}

	// Exactly one row should survive from the {dup-1, dup-2, dup-3} group,
	// and it must be dup-2 since it's the only one carrying a real
	// association (linked_machine_id), even though it's not the earliest.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM domains WHERE LOWER(RTRIM(name, '.')) = 'example.com'`).Scan(&count); err != nil {
		t.Fatalf("failed to count surviving example.com rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 surviving row for the example.com group, got %d", count)
	}

	var survivorID string
	if err := db.QueryRow(`SELECT id FROM domains WHERE LOWER(RTRIM(name, '.')) = 'example.com'`).Scan(&survivorID); err != nil {
		t.Fatalf("failed to fetch surviving row id: %v", err)
	}
	if survivorID != "dup-2" {
		t.Errorf("expected surviving row to be 'dup-2' (the only one with an association), got %q", survivorID)
	}

	// The unrelated row must be untouched.
	if err := db.QueryRow(`SELECT COUNT(*) FROM domains WHERE id = 'keep-me'`).Scan(&count); err != nil {
		t.Fatalf("failed to count keep-me row: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected the unrelated 'keep-me' row to be untouched, got count %d", count)
	}

	// The unique index must now be genuinely in effect: a fresh duplicate
	// insert without ON CONFLICT must fail.
	if _, err := db.Exec(`INSERT INTO domains (id, name, created_at, updated_at) VALUES ('dup-4', 'example.COM', '2024-01-04T00:00:00Z', '2024-01-04T00:00:00Z')`); err == nil {
		t.Fatal("expected insert of another normalized-duplicate name to fail now that the unique index exists")
	}
}

// TestMigrate_DedupeRepointsMonitorResultsToSurvivor verifies that when a
// losing duplicate row is deleted, any domain_monitor_results rows that
// referenced it are repointed to the surviving row's id first, so the
// FK constraint (domain_monitor_results.domain_id REFERENCES domains(id),
// enforced via PRAGMA foreign_keys=ON) is never violated and no historical
// probe result is lost or left dangling.
func TestMigrate_DedupeRepointsMonitorResultsToSurvivor(t *testing.T) {
	db := openTestDB(t)

	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatalf("failed to enable foreign keys: %v", err)
	}

	if _, err := db.Exec(`CREATE TABLE domains (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		source TEXT DEFAULT 'manual',
		thirdpart_dns_id TEXT DEFAULT '',
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
	if _, err := db.Exec(`CREATE TABLE domain_monitor_results (
		id TEXT PRIMARY KEY,
		domain_id TEXT NOT NULL REFERENCES domains(id),
		checked_port INTEGER NOT NULL,
		tls_success INTEGER NOT NULL DEFAULT 0,
		domain_matched INTEGER NOT NULL DEFAULT 0,
		chain_valid INTEGER NOT NULL DEFAULT 0,
		checked_at TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("failed to create domain_monitor_results table: %v", err)
	}

	// Two rows collide (no association on either), "loser" is earlier so it
	// would win under a pure earliest-created_at rule, but here neither row
	// has an association so earliest-created_at IS the deciding rule and
	// "loser" (earlier) should actually survive. To specifically exercise the
	// repoint-then-delete path, seed a monitor result against the row that
	// will be deleted (the later one, "later").
	if _, err := db.Exec(`INSERT INTO domains (id, name, created_at, updated_at) VALUES ('earlier', 'dup.com', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("failed to seed earlier domain: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO domains (id, name, created_at, updated_at) VALUES ('later', 'DUP.com.', '2024-01-02T00:00:00Z', '2024-01-02T00:00:00Z')`); err != nil {
		t.Fatalf("failed to seed later domain: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO domain_monitor_results (id, domain_id, checked_port, checked_at) VALUES ('result-1', 'later', 443, '2024-01-02T00:00:00Z')`); err != nil {
		t.Fatalf("failed to seed monitor result for the losing row: %v", err)
	}

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate should dedupe and repoint monitor results without violating the FK constraint, got: %v", err)
	}

	// "earlier" must have survived (earliest created_at, neither row had an
	// association) and "later" must be gone.
	var survivorID string
	if err := db.QueryRow(`SELECT id FROM domains WHERE LOWER(RTRIM(name, '.')) = 'dup.com'`).Scan(&survivorID); err != nil {
		t.Fatalf("failed to fetch surviving row id: %v", err)
	}
	if survivorID != "earlier" {
		t.Errorf("expected 'earlier' to survive (earliest created_at, no associations in group), got %q", survivorID)
	}

	// The monitor result must now point at the survivor, not be deleted.
	var repointedDomainID string
	if err := db.QueryRow(`SELECT domain_id FROM domain_monitor_results WHERE id = 'result-1'`).Scan(&repointedDomainID); err != nil {
		t.Fatalf("expected monitor result 'result-1' to still exist (repointed, not deleted), got error: %v", err)
	}
	if repointedDomainID != "earlier" {
		t.Errorf("expected monitor result to be repointed to the surviving domain id 'earlier', got %q", repointedDomainID)
	}
}

// TestMigrate_DedupeIsIdempotent verifies that once duplicates have been
// cleaned up and the unique index created, repeated Migrate() calls remain
// idempotent: no error, no further row changes.
func TestMigrate_DedupeIsIdempotent(t *testing.T) {
	db := openTestDB(t)

	if _, err := db.Exec(`CREATE TABLE domains (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		source TEXT DEFAULT 'manual',
		thirdpart_dns_id TEXT DEFAULT '',
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
	if _, err := db.Exec(`INSERT INTO domains (id, name, created_at, updated_at) VALUES ('a', 'Foo.com', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("failed to seed domain: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO domains (id, name, created_at, updated_at) VALUES ('b', 'foo.com.', '2024-01-02T00:00:00Z', '2024-01-02T00:00:00Z')`); err != nil {
		t.Fatalf("failed to seed domain: %v", err)
	}

	if err := db.Migrate(); err != nil {
		t.Fatalf("first Migrate() failed: %v", err)
	}
	var countAfterFirst int
	if err := db.QueryRow(`SELECT COUNT(*) FROM domains`).Scan(&countAfterFirst); err != nil {
		t.Fatalf("failed to count domains after first Migrate: %v", err)
	}
	if countAfterFirst != 1 {
		t.Fatalf("expected exactly 1 domain to survive after first Migrate, got %d", countAfterFirst)
	}

	// Second call must succeed and must not change anything further.
	if err := db.Migrate(); err != nil {
		t.Fatalf("second Migrate() should be idempotent, got: %v", err)
	}
	var countAfterSecond int
	if err := db.QueryRow(`SELECT COUNT(*) FROM domains`).Scan(&countAfterSecond); err != nil {
		t.Fatalf("failed to count domains after second Migrate: %v", err)
	}
	if countAfterSecond != countAfterFirst {
		t.Fatalf("expected row count to stay at %d after a repeated Migrate() call, got %d", countAfterFirst, countAfterSecond)
	}
}
