package database

import "fmt"

// Migrate creates all database tables if they don't exist.
func (db *DB) Migrate() error {
	for _, stmt := range migrationStatements {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("migration failed: %w\nStatement: %s", err, stmt)
		}
	}
	return nil
}

var migrationStatements = []string{
	// 用户表
	`CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL CHECK(role IN ('admin', 'user')),
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`,

	// 机器表
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

	// 证书表
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

	// 机器证书部署配置表
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

	// 部署日志表
	`CREATE TABLE IF NOT EXISTS deployment_logs (
		id TEXT PRIMARY KEY,
		machine_certificate_id TEXT NOT NULL REFERENCES machine_certificates(id),
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

	// 域名监控表
	`CREATE TABLE IF NOT EXISTS domains (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		source TEXT DEFAULT 'manual' CHECK(source IN ('manual', 'certificate', 'cloudflare')),
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
	)`,

	// 域名监控结果表
	`CREATE TABLE IF NOT EXISTS domain_monitor_results (
		id TEXT PRIMARY KEY,
		domain_id TEXT NOT NULL REFERENCES domains(id),
		checked_port INTEGER NOT NULL,
		resolved_ips TEXT DEFAULT '',
		tls_success INTEGER NOT NULL DEFAULT 0,
		certificate_fingerprint_sha256 TEXT DEFAULT '',
		issuer TEXT DEFAULT '',
		expire_at TEXT,
		days_remaining INTEGER,
		domain_matched INTEGER NOT NULL DEFAULT 0,
		chain_valid INTEGER NOT NULL DEFAULT 0,
		error_message TEXT DEFAULT '',
		checked_at TEXT NOT NULL
	)`,

	// 告警表
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

	// 通知渠道配置表
	`CREATE TABLE IF NOT EXISTS notification_channels (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL CHECK(type IN ('lark', 'telegram')),
		name TEXT NOT NULL,
		config_json TEXT NOT NULL DEFAULT '{}',
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`,

	// 审计日志表
	`CREATE TABLE IF NOT EXISTS audit_logs (
		id TEXT PRIMARY KEY,
		actor_type TEXT NOT NULL CHECK(actor_type IN ('user', 'agent', 'system')),
		actor_id TEXT NOT NULL,
		action TEXT NOT NULL,
		target_type TEXT NOT NULL,
		target_id TEXT DEFAULT '',
		detail TEXT DEFAULT '',
		ip TEXT DEFAULT '',
		created_at TEXT NOT NULL
	)`,

	// 第三方 DNS 上游配置表
	`CREATE TABLE IF NOT EXISTS thirdpart_dns (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		type TEXT NOT NULL DEFAULT 'cloudflare' CHECK(type IN ('cloudflare')),
		api_token TEXT NOT NULL,
		config_json TEXT NOT NULL DEFAULT '{}',
		main_domains TEXT NOT NULL DEFAULT '[]',
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`,

	// 第三方 DNS 同步日志表
	`CREATE TABLE IF NOT EXISTS thirdpart_dns_sync_logs (
		id TEXT PRIMARY KEY,
		thirdpart_dns_id TEXT NOT NULL REFERENCES thirdpart_dns(id),
		records_count INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL CHECK(status IN ('success', 'failed')),
		error_message TEXT DEFAULT '',
		synced_at TEXT NOT NULL
	)`,
}
