package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/service"

	_ "github.com/glebarez/sqlite"
)

// dashboardScenario holds all generated data for a single property test run.
type dashboardScenario struct {
	// Certificate expiry offsets in hours from now (negative = expired)
	CertExpiryOffsets []int
	// Machine statuses
	MachineStatuses []string
	// Deploy log entries: [status_index, hours_offset]
	DeployLogs []deployLogEntry
	// Alert entries
	Alerts []alertEntry
	// Domain entries
	Domains []domainEntry
}

type deployLogEntry struct {
	StatusIdx   int // 0=success, 1=failed, 2=skipped
	HoursOffset int // negative offset from now
}

type alertEntry struct {
	LevelIdx    int // 0=info, 1=warning, 2=critical
	TypeIdx     int // 0=renew_failed, 1=cert_renew_failed, 2=cert_expired, 3=agent_offline, 4=deploy_failed
	HoursOffset int // negative offset from now
}

type domainEntry struct {
	MonitorEnabled bool
	HasResult      bool
	TLSSuccess     bool
	DomainMatched  bool
}

var deployStatuses = []string{"success", "failed", "skipped"}
var alertLevels = []string{"info", "warning", "critical"}
var alertTypes = []string{"renew_failed", "cert_renew_failed", "cert_expired", "agent_offline", "deploy_failed"}
var machineStatusOptions = []string{"online", "offline", "pending", "revoked", "disabled"}

// genDashboardScenario generates a complete test scenario.
func genDashboardScenario() gopter.Gen {
	return gopter.CombineGens(
		gen.IntRange(0, 10),  // numCerts
		gen.IntRange(0, 8),   // numMachines
		gen.IntRange(0, 10),  // numDeployLogs
		gen.IntRange(0, 8),   // numAlerts
		gen.IntRange(0, 6),   // numDomains
		gen.Int64(),          // random seed for sub-generation
	).Map(func(values []interface{}) dashboardScenario {
		numCerts := values[0].(int)
		numMachines := values[1].(int)
		numDeployLogs := values[2].(int)
		numAlerts := values[3].(int)
		numDomains := values[4].(int)
		seed := values[5].(int64)

		rng := rand.New(rand.NewSource(seed))

		scenario := dashboardScenario{}

		// Generate certificate expiry offsets
		for i := 0; i < numCerts; i++ {
			// Range: -720 to +720 hours (~-30 to +30 days)
			offset := rng.Intn(1441) - 720
			scenario.CertExpiryOffsets = append(scenario.CertExpiryOffsets, offset)
		}

		// Generate machine statuses
		for i := 0; i < numMachines; i++ {
			scenario.MachineStatuses = append(scenario.MachineStatuses, machineStatusOptions[rng.Intn(len(machineStatusOptions))])
		}

		// Generate deploy logs
		for i := 0; i < numDeployLogs; i++ {
			scenario.DeployLogs = append(scenario.DeployLogs, deployLogEntry{
				StatusIdx:   rng.Intn(3),
				HoursOffset: -(rng.Intn(49)), // 0 to -48 hours
			})
		}

		// Generate alerts
		for i := 0; i < numAlerts; i++ {
			scenario.Alerts = append(scenario.Alerts, alertEntry{
				LevelIdx:    rng.Intn(3),
				TypeIdx:     rng.Intn(5),
				HoursOffset: -(rng.Intn(49)), // 0 to -48 hours
			})
		}

		// Generate domains
		for i := 0; i < numDomains; i++ {
			scenario.Domains = append(scenario.Domains, domainEntry{
				MonitorEnabled: rng.Intn(2) == 1,
				HasResult:      rng.Intn(2) == 1,
				TLSSuccess:     rng.Intn(2) == 1,
				DomainMatched:  rng.Intn(2) == 1,
			})
		}

		return scenario
	})
}

// setupScenarioDB creates an in-memory SQLite database and inserts scenario data.
func setupScenarioDB(scenario dashboardScenario) (*sql.DB, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}

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
			db.Close()
			return nil, fmt.Errorf("failed to create table: %w", err)
		}
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	// Insert certificates
	for i, offset := range scenario.CertExpiryOffsets {
		expireAt := now.Add(time.Duration(offset) * time.Hour).Format(time.RFC3339)
		_, err := db.Exec(`INSERT INTO certificates (
			id, name, domains, source, expire_at, auto_renew, issuer,
			fingerprint_sha256, chain_valid, cert_dir_path, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("cert-%d", i), fmt.Sprintf("Cert %d", i), "example.com", "upload",
			expireAt, 0, "Test Issuer",
			fmt.Sprintf("fp-cert-%d", i), 1, fmt.Sprintf("./data/certificates/cert-%d", i), nowStr, nowStr,
		)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("insert cert %d: %w", i, err)
		}
	}

	// Insert machines
	for i, status := range scenario.MachineStatuses {
		_, err := db.Exec(`INSERT INTO machines (
			id, name, ip, status, agent_token_hash, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("machine-%d", i), fmt.Sprintf("Machine %d", i), "192.168.1.1",
			status, fmt.Sprintf("hash-%d", i), nowStr, nowStr,
		)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("insert machine %d: %w", i, err)
		}
	}

	// Insert deployment logs
	for i, dl := range scenario.DeployLogs {
		ts := now.Add(time.Duration(dl.HoursOffset) * time.Hour).Format(time.RFC3339)
		status := deployStatuses[dl.StatusIdx]
		_, err := db.Exec(`INSERT INTO deployment_logs (
			id, machine_certificate_id, machine_id, certificate_id, status,
			cert_fingerprint_sha256, cert_path, private_key_path,
			started_at, finished_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("dl-%d", i), "mc-1", "m-1", "cert-1", status,
			fmt.Sprintf("fp-dl-%d", i), "/etc/ssl/cert.pem", "/etc/ssl/key.pem",
			ts, ts, ts,
		)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("insert deploy log %d: %w", i, err)
		}
	}

	// Insert alerts
	for i, alert := range scenario.Alerts {
		ts := now.Add(time.Duration(alert.HoursOffset) * time.Hour).Format(time.RFC3339)
		level := alertLevels[alert.LevelIdx]
		alertType := alertTypes[alert.TypeIdx]
		_, err := db.Exec(`INSERT INTO alerts (
			id, level, type, title, content, status, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("alert-%d", i), level, alertType, "Test Alert", "test content", "active", ts,
		)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("insert alert %d: %w", i, err)
		}
	}

	// Insert domains and their monitor results
	for i, domain := range scenario.Domains {
		enabled := 0
		if domain.MonitorEnabled {
			enabled = 1
		}
		domainID := fmt.Sprintf("domain-%d", i)
		_, err := db.Exec(`INSERT INTO domains (
			id, name, source, monitor_port, monitor_enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			domainID, fmt.Sprintf("domain-%d.example.com", i), "manual", 443, enabled, nowStr, nowStr,
		)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("insert domain %d: %w", i, err)
		}

		if domain.HasResult {
			tlsInt := 0
			if domain.TLSSuccess {
				tlsInt = 1
			}
			matchedInt := 0
			if domain.DomainMatched {
				matchedInt = 1
			}
			_, err := db.Exec(`INSERT INTO domain_monitor_results (
				id, domain_id, checked_port, tls_success, domain_matched, checked_at
			) VALUES (?, ?, ?, ?, ?, ?)`,
				fmt.Sprintf("dmr-%d", i), domainID, 443, tlsInt, matchedInt, nowStr,
			)
			if err != nil {
				db.Close()
				return nil, fmt.Errorf("insert domain monitor result %d: %w", i, err)
			}
		}
	}

	return db, nil
}

// computeExpectedDashboardStats computes the expected dashboard statistics from the scenario.
func computeExpectedDashboardStats(scenario dashboardScenario) service.DashboardStats {
	now := time.Now().UTC()
	stats := service.DashboardStats{}

	// Certificate stats
	stats.CertificatesTotal = len(scenario.CertExpiryOffsets)
	for _, offset := range scenario.CertExpiryOffsets {
		expireAt := now.Add(time.Duration(offset) * time.Hour)
		if !expireAt.After(now) {
			// expired: expire_at <= now
			stats.CertificatesExpired++
		} else if !expireAt.After(now.Add(15 * 24 * time.Hour)) {
			// expiring within 15 days: now < expire_at <= now+15d
			stats.CertificatesExpiring15d++
		}
	}

	// Machine stats
	for _, status := range scenario.MachineStatuses {
		switch status {
		case "online":
			stats.MachinesOnline++
		case "offline":
			stats.MachinesOffline++
		}
	}

	// Deploy failures in last 24 hours
	for _, dl := range scenario.DeployLogs {
		status := deployStatuses[dl.StatusIdx]
		if status == "failed" && dl.HoursOffset >= -24 {
			stats.DeployFailures24h++
		}
	}

	// Renew failures in last 24 hours
	for _, alert := range scenario.Alerts {
		level := alertLevels[alert.LevelIdx]
		alertType := alertTypes[alert.TypeIdx]
		if (level == "warning" || level == "critical") &&
			containsRenewStr(alertType) &&
			alert.HoursOffset >= -24 {
			stats.RenewFailures24h++
		}
	}

	// Domain anomalies
	for _, domain := range scenario.Domains {
		if domain.MonitorEnabled && domain.HasResult {
			if !domain.TLSSuccess || !domain.DomainMatched {
				stats.DomainAnomalies++
			}
		}
	}

	// Has anomalies
	stats.HasAnomalies = stats.CertificatesExpiring15d > 0 ||
		stats.CertificatesExpired > 0 ||
		stats.MachinesOffline > 0 ||
		stats.DeployFailures24h > 0 ||
		stats.RenewFailures24h > 0 ||
		stats.DomainAnomalies > 0

	return stats
}

// containsRenewStr checks if the alert type contains "renew".
func containsRenewStr(alertType string) bool {
	for i := 0; i <= len(alertType)-5; i++ {
		if alertType[i:i+5] == "renew" {
			return true
		}
	}
	return false
}

// TestPropertyDashboardStatsAccuracy verifies that for any given set of certificates,
// machines, deployment logs, alerts, and domain monitor results, the dashboard statistics
// accurately reflect the counts.
//
// **Validates: Requirements 15.1**
func TestPropertyDashboardStatsAccuracy(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	properties.Property("Dashboard stats accurately reflect database contents", prop.ForAll(
		func(scenario dashboardScenario) bool {
			// Set up fresh database with scenario data
			db, err := setupScenarioDB(scenario)
			if err != nil {
				t.Logf("failed to setup scenario db: %v", err)
				return false
			}
			defer db.Close()

			// Compute expected stats from the scenario
			expected := computeExpectedDashboardStats(scenario)

			// Call the dashboard handler
			dashboardService := service.NewDashboardService(db)
			handler := NewDashboardHandler(dashboardService)

			r := chi.NewRouter()
			r.Get("/api/dashboard", handler.GetDashboard)

			req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Logf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
				return false
			}

			var resp model.SuccessResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Logf("failed to unmarshal response: %v", err)
				return false
			}

			data, ok := resp.Data.(map[string]interface{})
			if !ok {
				t.Logf("expected data to be a map, got %T", resp.Data)
				return false
			}

			// Verify each statistic matches expected
			actual := service.DashboardStats{
				CertificatesTotal:       int(data["certificates_total"].(float64)),
				CertificatesExpiring15d: int(data["certificates_expiring_15d"].(float64)),
				CertificatesExpired:     int(data["certificates_expired"].(float64)),
				MachinesOnline:          int(data["machines_online"].(float64)),
				MachinesOffline:         int(data["machines_offline"].(float64)),
				DeployFailures24h:       int(data["deploy_failures_24h"].(float64)),
				RenewFailures24h:        int(data["renew_failures_24h"].(float64)),
				DomainAnomalies:         int(data["domain_anomalies"].(float64)),
				HasAnomalies:            data["has_anomalies"].(bool),
			}

			if actual.CertificatesTotal != expected.CertificatesTotal {
				t.Logf("CertificatesTotal: got %d, want %d", actual.CertificatesTotal, expected.CertificatesTotal)
				return false
			}
			if actual.CertificatesExpiring15d != expected.CertificatesExpiring15d {
				t.Logf("CertificatesExpiring15d: got %d, want %d", actual.CertificatesExpiring15d, expected.CertificatesExpiring15d)
				return false
			}
			if actual.CertificatesExpired != expected.CertificatesExpired {
				t.Logf("CertificatesExpired: got %d, want %d", actual.CertificatesExpired, expected.CertificatesExpired)
				return false
			}
			if actual.MachinesOnline != expected.MachinesOnline {
				t.Logf("MachinesOnline: got %d, want %d", actual.MachinesOnline, expected.MachinesOnline)
				return false
			}
			if actual.MachinesOffline != expected.MachinesOffline {
				t.Logf("MachinesOffline: got %d, want %d", actual.MachinesOffline, expected.MachinesOffline)
				return false
			}
			if actual.DeployFailures24h != expected.DeployFailures24h {
				t.Logf("DeployFailures24h: got %d, want %d", actual.DeployFailures24h, expected.DeployFailures24h)
				return false
			}
			if actual.RenewFailures24h != expected.RenewFailures24h {
				t.Logf("RenewFailures24h: got %d, want %d", actual.RenewFailures24h, expected.RenewFailures24h)
				return false
			}
			if actual.DomainAnomalies != expected.DomainAnomalies {
				t.Logf("DomainAnomalies: got %d, want %d", actual.DomainAnomalies, expected.DomainAnomalies)
				return false
			}
			if actual.HasAnomalies != expected.HasAnomalies {
				t.Logf("HasAnomalies: got %v, want %v", actual.HasAnomalies, expected.HasAnomalies)
				return false
			}

			return true
		},
		genDashboardScenario(),
	))

	properties.TestingRun(t)
}
