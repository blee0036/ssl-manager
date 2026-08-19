package database

import (
	"database/sql"
	"fmt"
)

// Migrate creates all database tables if they don't exist,
// then runs idempotent column migrations for columns added in later versions.
func (db *DB) Migrate() error {
	for _, stmt := range migrationStatements {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("migration failed: %w\nStatement: %s", err, stmt)
		}
	}

	// 幂等列迁移：旧库补列，新库/重复调用不报错
	if err := db.migrateAddColumnIfNotExists("domains", "alert_ignored", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return fmt.Errorf("migrate alert_ignored column: %w", err)
	}
	if err := db.migrateAddColumnIfNotExists("domains", "dns_record_id", "TEXT DEFAULT ''"); err != nil {
		return fmt.Errorf("migrate dns_record_id column: %w", err)
	}
	if err := db.migrateAddColumnIfNotExists("thirdpart_dns_sync_logs", "new_domains", "TEXT DEFAULT '[]'"); err != nil {
		return fmt.Errorf("migrate new_domains column: %w", err)
	}
	if err := db.migrateAddColumnIfNotExists("thirdpart_dns_sync_logs", "updated_domains", "TEXT DEFAULT '[]'"); err != nil {
		return fmt.Errorf("migrate updated_domains column: %w", err)
	}
	if err := db.migrateAddColumnIfNotExists("thirdpart_dns_sync_logs", "removed_domains", "TEXT DEFAULT '[]'"); err != nil {
		return fmt.Errorf("migrate removed_domains column: %w", err)
	}
	if err := db.migrateAddColumnIfNotExists("root_domains", "expiry_source", "TEXT NOT NULL DEFAULT 'whois'"); err != nil {
		return fmt.Errorf("migrate expiry_source column: %w", err)
	}

	// init_state 表迁移
	initStateStatements := []string{
		`CREATE TABLE IF NOT EXISTS init_state (
			id TEXT PRIMARY KEY,
			admin_id TEXT NOT NULL,
			token_hash TEXT NOT NULL DEFAULT '',
			expires_at TEXT NOT NULL,
			pending_init INTEGER NOT NULL DEFAULT 1,
			completed_at TEXT
		)`,
		// 单活跃 pending 约束：同一时刻最多只能有一个 pending_init=1 的行
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_init_state_single_pending
			ON init_state(pending_init) WHERE pending_init = 1`,
	}
	for _, stmt := range initStateStatements {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("init_state migration failed: %w\nStatement: %s", err, stmt)
		}
	}

	// 幂等索引创建：使用 CREATE INDEX IF NOT EXISTS 确保重复调用安全
	indexStatements := []string{
		// domain_monitor_results: critical for ListWithSort JOIN, Dashboard getDomainAnomalies, GetLatestMonitorResult(sBatch)
		`CREATE INDEX IF NOT EXISTS idx_dmr_domain_checked ON domain_monitor_results(domain_id, checked_at DESC)`,
		// alerts: used by FindActiveByTarget, SuppressActiveByTarget
		`CREATE INDEX IF NOT EXISTS idx_alerts_target_status ON alerts(target_type, target_id, status)`,
		// alerts: used by Dashboard getRenewFailures24h
		`CREATE INDEX IF NOT EXISTS idx_alerts_type_created ON alerts(type, created_at)`,
		// deployment_logs: used by GetByMachineCertificateID, EnforceRetentionLimit
		`CREATE INDEX IF NOT EXISTS idx_deplogs_mc_created ON deployment_logs(machine_certificate_id, created_at DESC)`,
		// deployment_logs: used by Dashboard getDeployFailures24h
		`CREATE INDEX IF NOT EXISTS idx_deplogs_status_created ON deployment_logs(status, created_at)`,
		// machines: used by GetByTokenHash (auth on every request!)
		`CREATE INDEX IF NOT EXISTS idx_machines_token_hash ON machines(agent_token_hash)`,
		// machines: used by CheckHeartbeatTimeouts, ListByHeartbeatBefore
		`CREATE INDEX IF NOT EXISTS idx_machines_status ON machines(status)`,
		// certificates: used by ListExpiringSoon, Dashboard getCertificateStats
		`CREATE INDEX IF NOT EXISTS idx_certs_expire_at ON certificates(expire_at)`,
		// domains: used by DNS sync List filter
		`CREATE INDEX IF NOT EXISTS idx_domains_source_dnsid ON domains(source, thirdpart_dns_id)`,
		// domains: used by ProbeAll filter
		`CREATE INDEX IF NOT EXISTS idx_domains_monitor_enabled ON domains(monitor_enabled)`,
		// domains: 规范化（大小写/尾点）唯一约束，避免重复主机名（CreateIfNotExists 去重原语依赖此索引）
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_domains_name_normalized ON domains(LOWER(RTRIM(name, '.')))`,
		// machine_certificates: used by GetByMachineID, CountByCertificateIDs
		`CREATE INDEX IF NOT EXISTS idx_mc_machine_id ON machine_certificates(machine_id)`,
		`CREATE INDEX IF NOT EXISTS idx_mc_certificate_id ON machine_certificates(certificate_id)`,
		// thirdpart_dns_sync_logs: used by GetSyncLogs
		`CREATE INDEX IF NOT EXISTS idx_sync_logs_dnsid_synced ON thirdpart_dns_sync_logs(thirdpart_dns_id, synced_at DESC)`,
		// audit_logs: used by List ORDER BY
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_created ON audit_logs(created_at DESC)`,
		// root_domains: 去重唯一键，可注册域名全局唯一（GetByRegistrableDomain / 导入去重）
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_root_domains_registrable ON root_domains(registrable_domain)`,
		// root_domains: 周期刷新扫描启用项（ListEnabled）
		`CREATE INDEX IF NOT EXISTS idx_root_domains_enabled ON root_domains(monitor_enabled)`,
		// root_domains: 按到期日排序/筛选（ListWithSort）
		`CREATE INDEX IF NOT EXISTS idx_root_domains_expiry ON root_domains(expiry_date)`,
	}
	for _, stmt := range indexStatements {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("create index failed: %w\nStatement: %s", err, stmt)
		}
	}

	return nil
}

// migrateAddColumnIfNotExists checks if a column exists in the table using PRAGMA table_info.
// If the column does not exist, it executes ALTER TABLE ADD COLUMN.
// Idempotent: returns nil silently when the column already exists.
func (db *DB) migrateAddColumnIfNotExists(table, column, definition string) error {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return nil // Column already exists
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	_, err = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
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

	// 根域名注册到期监控表（独立于 domains / domain_monitor_results）
	`CREATE TABLE IF NOT EXISTS root_domains (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		source TEXT NOT NULL DEFAULT 'manual' CHECK(source IN ('manual', 'cloudflare')),
		registrable_domain TEXT NOT NULL,
		expiry_date TEXT,
		expiry_source TEXT NOT NULL DEFAULT 'whois' CHECK(expiry_source IN ('whois', 'manual')),
		last_checked_at TEXT,
		last_status TEXT NOT NULL DEFAULT '',
		last_error TEXT NOT NULL DEFAULT '',
		monitor_enabled INTEGER NOT NULL DEFAULT 1,
		alert_ignored INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`,
}
