package repository

import (
	"database/sql"
	"testing"

	_ "github.com/glebarez/sqlite"
)

// setupTestDB creates an in-memory SQLite database with all required tables for testing.
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("failed to enable foreign keys: %v", err)
	}

	// Create all tables
	tables := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL CHECK(role IN ('admin', 'user')),
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
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
		`CREATE TABLE IF NOT EXISTS deployment_logs (
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
		)`,
		`CREATE TABLE IF NOT EXISTS domains (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			source TEXT DEFAULT 'manual' CHECK(source IN ('manual', 'certificate', 'cloudflare')),
			thirdpart_dns_id TEXT DEFAULT '',
			dns_record_id TEXT DEFAULT '',
			dns_record_type TEXT DEFAULT '',
			dns_record_value TEXT DEFAULT '',
			monitor_port INTEGER NOT NULL DEFAULT 443,
			linked_machine_id TEXT,
			linked_certificate_id TEXT,
			linked_machine_certificate_id TEXT,
			monitor_enabled INTEGER NOT NULL DEFAULT 1,
			alert_ignored INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
	}

	for _, stmt := range tables {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("failed to create table: %v", err)
		}
	}

	t.Cleanup(func() {
		db.Close()
	})

	return db
}
