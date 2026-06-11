package service

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"

	_ "github.com/glebarez/sqlite"
)

// Feature: ux-improvements-batch1, Property 8: Dashboard Anomaly Count Excludes Ignored Domains
// **Validates: Requirements 6.6**

// dashboardDomainEntry represents a generated domain for the dashboard property test.
type dashboardDomainEntry struct {
	MonitorEnabled bool
	AlertIgnored   bool
	HasResult      bool
	TLSSuccess     bool
	DomainMatched  bool
	HasFPMismatch  bool // error_message contains "fingerprint mismatch:"
}

// genDashboardDomainEntries generates a slice of domain entries with varied configurations.
func genDashboardDomainEntries() gopter.Gen {
	return gopter.CombineGens(
		gen.IntRange(1, 20), // numDomains (at least 1)
		gen.Int64(),         // random seed
	).Map(func(values []interface{}) []dashboardDomainEntry {
		numDomains := values[0].(int)
		seed := values[1].(int64)
		rng := rand.New(rand.NewSource(seed))

		entries := make([]dashboardDomainEntry, numDomains)
		for i := range entries {
			entries[i] = dashboardDomainEntry{
				MonitorEnabled: rng.Intn(2) == 1,
				AlertIgnored:   rng.Intn(2) == 1,
				HasResult:      rng.Intn(2) == 1,
				TLSSuccess:     rng.Intn(2) == 1,
				DomainMatched:  rng.Intn(2) == 1,
				HasFPMismatch:  rng.Intn(4) == 0, // 25% chance
			}
		}
		return entries
	})
}

// setupDashboardPropertyDB creates an in-memory SQLite database with all tables needed
// for the DashboardService to compute domain_anomalies.
func setupDashboardPropertyDB(t *testing.T, entries []dashboardDomainEntry) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}

	// Create tables needed by DashboardService.GetStats()
	tables := []string{
		`CREATE TABLE certificates (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			domains TEXT NOT NULL,
			source TEXT NOT NULL,
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
		`CREATE TABLE machines (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			ip TEXT NOT NULL,
			hostname TEXT DEFAULT '',
			os TEXT DEFAULT '',
			arch TEXT DEFAULT '',
			tags TEXT DEFAULT '',
			remark TEXT DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			agent_version TEXT DEFAULT '',
			agent_token_hash TEXT NOT NULL,
			agent_token_revoked_at TEXT,
			last_heartbeat_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE deployment_logs (
			id TEXT PRIMARY KEY,
			machine_certificate_id TEXT NOT NULL,
			machine_id TEXT NOT NULL,
			certificate_id TEXT NOT NULL,
			status TEXT NOT NULL,
			cert_fingerprint_sha256 TEXT NOT NULL,
			cert_path TEXT NOT NULL,
			private_key_path TEXT NOT NULL,
			command_outputs TEXT DEFAULT '',
			error_message TEXT DEFAULT '',
			started_at TEXT NOT NULL,
			finished_at TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE alerts (
			id TEXT PRIMARY KEY,
			level TEXT NOT NULL,
			type TEXT NOT NULL,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			target_type TEXT DEFAULT '',
			target_id TEXT DEFAULT '',
			sent_channels TEXT DEFAULT '',
			created_at TEXT NOT NULL,
			resolved_at TEXT
		)`,
		`CREATE TABLE domains (
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
			alert_ignored INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE domain_monitor_results (
			id TEXT PRIMARY KEY,
			domain_id TEXT NOT NULL,
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
	}

	for _, ddl := range tables {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatalf("failed to create table: %v", err)
		}
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	// Insert domains and their monitor results
	for i, entry := range entries {
		monitorEnabled := 0
		if entry.MonitorEnabled {
			monitorEnabled = 1
		}
		alertIgnored := 0
		if entry.AlertIgnored {
			alertIgnored = 1
		}
		domainID := fmt.Sprintf("domain-%d", i)

		_, err := db.Exec(`INSERT INTO domains (
			id, name, source, monitor_port, monitor_enabled, alert_ignored, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			domainID, fmt.Sprintf("domain-%d.example.com", i), "manual", 443,
			monitorEnabled, alertIgnored, nowStr, nowStr,
		)
		if err != nil {
			t.Fatalf("insert domain %d: %v", i, err)
		}

		if entry.HasResult {
			tlsInt := 0
			if entry.TLSSuccess {
				tlsInt = 1
			}
			matchedInt := 0
			if entry.DomainMatched {
				matchedInt = 1
			}
			errorMsg := ""
			if entry.HasFPMismatch {
				errorMsg = "fingerprint mismatch: expected abc123 got def456"
			}

			_, err := db.Exec(`INSERT INTO domain_monitor_results (
				id, domain_id, checked_port, tls_success, domain_matched, chain_valid, error_message, checked_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				fmt.Sprintf("dmr-%d", i), domainID, 443, tlsInt, matchedInt, 1, errorMsg, nowStr,
			)
			if err != nil {
				t.Fatalf("insert domain_monitor_results %d: %v", i, err)
			}
		}
	}

	t.Cleanup(func() {
		db.Close()
	})

	return db
}

// computeExpectedAnomalyCount computes the expected domain_anomalies count
// using the same logic as getDomainAnomalies SQL:
// - monitor_enabled = 1
// - alert_ignored = 0
// - has a monitor result (latest)
// - (tls_success = 0 OR domain_matched = 0 OR error_message LIKE 'fingerprint mismatch:%')
func computeExpectedAnomalyCount(entries []dashboardDomainEntry) int {
	count := 0
	for _, e := range entries {
		if !e.MonitorEnabled {
			continue
		}
		if e.AlertIgnored {
			continue
		}
		if !e.HasResult {
			continue
		}
		isAnomalous := !e.TLSSuccess || !e.DomainMatched || e.HasFPMismatch
		if isAnomalous {
			count++
		}
	}
	return count
}

// TestProperty_DashboardAnomalyCountExcludesIgnoredDomains verifies that for any mix of
// domains where some are alert_ignored=true with TLS anomalies, those ignored domains
// should NOT be counted in the dashboard anomaly statistic.
//
// Feature: ux-improvements-batch1, Property 8: Dashboard Anomaly Count Excludes Ignored Domains
// **Validates: Requirements 6.6**
func TestProperty_DashboardAnomalyCountExcludesIgnoredDomains(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	properties.Property("dashboard anomaly count excludes alert_ignored domains", prop.ForAll(
		func(entries []dashboardDomainEntry) bool {
			db := setupDashboardPropertyDB(t, entries)

			// Create DashboardService and call GetStats
			svc := NewDashboardService(db)
			stats, err := svc.GetStats(context.Background())
			if err != nil {
				t.Logf("GetStats failed: %v", err)
				return false
			}

			expected := computeExpectedAnomalyCount(entries)

			if stats.DomainAnomalies != expected {
				t.Logf("DomainAnomalies mismatch: got %d, want %d", stats.DomainAnomalies, expected)
				t.Logf("Entries:")
				for i, e := range entries {
					t.Logf("  [%d] enabled=%v ignored=%v hasResult=%v tlsOk=%v matched=%v fpMismatch=%v",
						i, e.MonitorEnabled, e.AlertIgnored, e.HasResult, e.TLSSuccess, e.DomainMatched, e.HasFPMismatch)
				}
				return false
			}

			return true
		},
		genDashboardDomainEntries(),
	))

	properties.TestingRun(t)
}
