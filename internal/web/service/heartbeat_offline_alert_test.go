package service

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// These tests cover the split between the two heartbeat thresholds:
//
//   - agent.heartbeat_timeout_seconds flips a machine to "offline" in the machine list.
//   - agent.offline_alert_after_seconds decides when an agent_offline notification is
//     actually pushed.
//
// Before the split, a single threshold did both, so two missed heartbeats pushed a warning
// and the next successful heartbeat pushed a [Recovered] right behind it — two messages for
// a hiccup that needed none. Nothing in the suite pinned the alerting side of
// CheckHeartbeatTimeouts down, which is why these tests exist.
//
// Machines are inserted directly with an explicit last_heartbeat_at so the thresholds can
// be crossed deterministically without sleeping.

// insertMachineWithHeartbeat inserts a machine whose last heartbeat is `silentFor` in the
// past, with the given status.
func insertMachineWithHeartbeat(t *testing.T, db *sql.DB, id, status string, silentFor time.Duration) {
	t.Helper()

	now := time.Now().UTC()
	lastHeartbeat := now.Add(-silentFor).Format(time.RFC3339)

	_, err := db.Exec(
		`INSERT INTO machines (id, name, ip, status, agent_token_hash, last_heartbeat_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, id, "10.0.0.1", status, "hash-"+id, lastHeartbeat,
		now.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("failed to insert machine %s: %v", id, err)
	}
}

// machineStatus reads a machine's current status straight from the DB.
func machineStatus(t *testing.T, db *sql.DB, id string) string {
	t.Helper()

	var status string
	if err := db.QueryRow("SELECT status FROM machines WHERE id = ?", id).Scan(&status); err != nil {
		t.Fatalf("failed to read status of %s: %v", id, err)
	}
	return status
}

// countOfflineAlerts counts recorded agent_offline alerts for a machine.
func countOfflineAlerts(alerter *mockAlertSender, machineID string) int {
	n := 0
	for _, a := range alerter.alerts {
		if a.AlertType == "agent_offline" && a.TargetID == machineID {
			n++
		}
	}
	return n
}

func TestCheckHeartbeatTimeouts_MarksOfflineWithoutAlertingBeforeAlertThreshold(t *testing.T) {
	scheduler, db, _, _, alerter, _ := setupSchedulerTest(t)
	ctx := context.Background()

	cfg := scheduler.runtimeCfg.Get()
	cfg.Agent.HeartbeatTimeoutSeconds = 120
	cfg.Agent.OfflineAlertAfterSeconds = 600

	// Silent for 5 minutes: past the offline threshold, well short of the alert threshold.
	insertMachineWithHeartbeat(t, db, "m-brief", "online", 5*time.Minute)

	if err := scheduler.CheckHeartbeatTimeouts(ctx); err != nil {
		t.Fatalf("CheckHeartbeatTimeouts failed: %v", err)
	}

	if got := machineStatus(t, db, "m-brief"); got != "offline" {
		t.Errorf("expected the machine to be marked offline for the UI, got %q", got)
	}
	if n := countOfflineAlerts(alerter, "m-brief"); n != 0 {
		t.Errorf("expected no alert before offline_alert_after_seconds elapses, got %d", n)
	}
}

func TestCheckHeartbeatTimeouts_AlertsOnceAlertThresholdPassed(t *testing.T) {
	scheduler, db, _, _, alerter, _ := setupSchedulerTest(t)
	ctx := context.Background()

	cfg := scheduler.runtimeCfg.Get()
	cfg.Agent.HeartbeatTimeoutSeconds = 120
	cfg.Agent.OfflineAlertAfterSeconds = 600

	// Silent for 20 minutes: a real outage, past both thresholds.
	insertMachineWithHeartbeat(t, db, "m-down", "online", 20*time.Minute)

	if err := scheduler.CheckHeartbeatTimeouts(ctx); err != nil {
		t.Fatalf("CheckHeartbeatTimeouts failed: %v", err)
	}

	if got := machineStatus(t, db, "m-down"); got != "offline" {
		t.Errorf("expected status 'offline', got %q", got)
	}
	if n := countOfflineAlerts(alerter, "m-down"); n != 1 {
		t.Fatalf("expected 1 agent_offline alert, got %d", n)
	}
}

func TestCheckHeartbeatTimeouts_AlertsMachineAlreadyMarkedOffline(t *testing.T) {
	// The alert threshold is crossed long after the machine was flipped to 'offline', so
	// the alert query has to keep matching machines that are already offline. This is the
	// case the previous online-only query could not see, which would have made a delayed
	// alert never fire at all.
	scheduler, db, _, _, alerter, _ := setupSchedulerTest(t)
	ctx := context.Background()

	cfg := scheduler.runtimeCfg.Get()
	cfg.Agent.HeartbeatTimeoutSeconds = 120
	cfg.Agent.OfflineAlertAfterSeconds = 600

	insertMachineWithHeartbeat(t, db, "m-already-offline", "offline", 20*time.Minute)

	if err := scheduler.CheckHeartbeatTimeouts(ctx); err != nil {
		t.Fatalf("CheckHeartbeatTimeouts failed: %v", err)
	}

	if n := countOfflineAlerts(alerter, "m-already-offline"); n != 1 {
		t.Fatalf("expected 1 agent_offline alert for an already-offline machine, got %d", n)
	}
}

func TestCheckHeartbeatTimeouts_DoesNotAlertPendingOrRevokedMachines(t *testing.T) {
	// Silence is expected for machines that never enrolled or were deliberately taken out
	// of service, so they must never produce an alert regardless of how stale they look.
	scheduler, db, _, _, alerter, _ := setupSchedulerTest(t)
	ctx := context.Background()

	cfg := scheduler.runtimeCfg.Get()
	cfg.Agent.HeartbeatTimeoutSeconds = 120
	cfg.Agent.OfflineAlertAfterSeconds = 600

	insertMachineWithHeartbeat(t, db, "m-pending", "pending", 20*time.Minute)
	insertMachineWithHeartbeat(t, db, "m-revoked", "revoked", 20*time.Minute)
	insertMachineWithHeartbeat(t, db, "m-disabled", "disabled", 20*time.Minute)

	if err := scheduler.CheckHeartbeatTimeouts(ctx); err != nil {
		t.Fatalf("CheckHeartbeatTimeouts failed: %v", err)
	}

	for _, id := range []string{"m-pending", "m-revoked", "m-disabled"} {
		if n := countOfflineAlerts(alerter, id); n != 0 {
			t.Errorf("expected no alert for %s, got %d", id, n)
		}
	}
}

func TestCheckHeartbeatTimeouts_AlertThresholdNeverBelowOfflineThreshold(t *testing.T) {
	// A misconfigured alert delay shorter than the offline timeout must not alert earlier
	// than the machine is even considered offline; EffectiveOfflineAlertAfterSeconds
	// clamps it up to the offline threshold.
	scheduler, db, _, _, alerter, _ := setupSchedulerTest(t)
	ctx := context.Background()

	cfg := scheduler.runtimeCfg.Get()
	cfg.Agent.HeartbeatTimeoutSeconds = 600
	cfg.Agent.OfflineAlertAfterSeconds = 1

	// Silent for 5 minutes: under the 600s offline threshold, so neither effect applies.
	insertMachineWithHeartbeat(t, db, "m-clamped", "online", 5*time.Minute)

	if err := scheduler.CheckHeartbeatTimeouts(ctx); err != nil {
		t.Fatalf("CheckHeartbeatTimeouts failed: %v", err)
	}

	if got := machineStatus(t, db, "m-clamped"); got != "online" {
		t.Errorf("expected the machine to stay online below the offline threshold, got %q", got)
	}
	if n := countOfflineAlerts(alerter, "m-clamped"); n != 0 {
		t.Errorf("expected no alert while the machine is still within its offline threshold, got %d", n)
	}
}

func TestCheckHeartbeatTimeouts_AutoResolvesWhenMachineIsBackOnline(t *testing.T) {
	// A machine reporting normally must have its agent_offline alert auto-resolved, which
	// is what turns into the [Recovered] notification.
	scheduler, db, _, _, alerter, _ := setupSchedulerTest(t)
	ctx := context.Background()

	cfg := scheduler.runtimeCfg.Get()
	cfg.Agent.HeartbeatTimeoutSeconds = 120
	cfg.Agent.OfflineAlertAfterSeconds = 600

	insertMachineWithHeartbeat(t, db, "m-healthy", "online", 10*time.Second)

	if err := scheduler.CheckHeartbeatTimeouts(ctx); err != nil {
		t.Fatalf("CheckHeartbeatTimeouts failed: %v", err)
	}

	if n := countOfflineAlerts(alerter, "m-healthy"); n != 0 {
		t.Errorf("expected no alert for a healthy machine, got %d", n)
	}

	found := false
	for _, r := range alerter.resolved {
		if r.TargetType == "machine" && r.TargetID == "m-healthy" && r.AlertType == "agent_offline" {
			found = true
		}
	}
	if !found {
		t.Error("expected AutoResolve to be called for the online machine's agent_offline alert")
	}
}

func TestCheckHeartbeatTimeouts_AlertContentReportsAlertThreshold(t *testing.T) {
	// The message used to quote heartbeat_timeout_seconds, which after the split would
	// understate how long the machine had actually been silent.
	scheduler, db, _, _, alerter, _ := setupSchedulerTest(t)
	ctx := context.Background()

	cfg := scheduler.runtimeCfg.Get()
	cfg.Agent.HeartbeatTimeoutSeconds = 120
	cfg.Agent.OfflineAlertAfterSeconds = 900

	insertMachineWithHeartbeat(t, db, "m-content", "online", 30*time.Minute)

	if err := scheduler.CheckHeartbeatTimeouts(ctx); err != nil {
		t.Fatalf("CheckHeartbeatTimeouts failed: %v", err)
	}

	var content string
	for _, a := range alerter.alerts {
		if a.AlertType == "agent_offline" && a.TargetID == "m-content" {
			content = a.Content
		}
	}
	if content == "" {
		t.Fatal("expected an agent_offline alert to be recorded")
	}
	if !strings.Contains(content, "900 seconds") {
		t.Errorf("expected the alert to quote the 900s alert threshold, got: %s", content)
	}
}

// newSchedulerWithRealAlertService builds a scheduler wired to the production AlertService
// over the given DB.
//
// The tests below turn on how repeat alerts are suppressed, which lives in AlertService and
// depends on real persisted alert rows and their status. The recording mock accepts every
// send unconditionally, so it cannot express that; driving the actual service keeps the
// assertions honest.
//
// No notification channel is created, so nothing is dispatched over the network. Alerts are
// still written to the alerts table, which is what the assertions count.
func newSchedulerWithRealAlertService(t *testing.T, db *sql.DB) *SchedulerService {
	t.Helper()

	schema := []string{
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
		`CREATE TABLE IF NOT EXISTS notification_channels (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL CHECK(type IN ('lark', 'telegram')),
			name TEXT NOT NULL,
			config_json TEXT NOT NULL DEFAULT '{}',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
	}
	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("failed to create alert tables: %v", err)
		}
	}

	alertSvc := NewAlertService(
		repository.NewAlertRepository(db),
		repository.NewNotificationChannelRepository(db),
	)

	machineRepo := repository.NewMachineRepository(db)
	certRepo := repository.NewCertificateRepository(db, t.TempDir())
	cfg := config.DefaultConfig()

	return NewSchedulerService(
		config.NewRuntimeConfig(cfg), certRepo, machineRepo,
		NewCertificateService(certRepo, db), nil, alertSvc, db,
	)
}

// countPersistedOfflineAlerts counts agent_offline rows actually written to the alerts table.
func countPersistedOfflineAlerts(t *testing.T, db *sql.DB, machineID string) int {
	t.Helper()

	var n int
	err := db.QueryRow(
		`SELECT COUNT(1) FROM alerts WHERE type = 'agent_offline' AND target_type = 'machine' AND target_id = ?`,
		machineID,
	).Scan(&n)
	if err != nil {
		t.Fatalf("failed to count alerts: %v", err)
	}
	return n
}

func TestCheckHeartbeatTimeouts_DoesNotRepeatAlertWhileMachineStaysOffline(t *testing.T) {
	// This runs every 30s and the alert query keeps matching a machine that stays down, so
	// repeat pushes are held off entirely by AlertService suppression: an unresolved alert
	// for the same machine and type short-circuits the send.
	db := setupTestDB(t)
	scheduler := newSchedulerWithRealAlertService(t, db)
	ctx := context.Background()

	cfg := scheduler.runtimeCfg.Get()
	cfg.Agent.HeartbeatTimeoutSeconds = 120
	cfg.Agent.OfflineAlertAfterSeconds = 600

	insertMachineWithHeartbeat(t, db, "m-stuck", "online", 20*time.Minute)

	for i := 0; i < 3; i++ {
		if err := scheduler.CheckHeartbeatTimeouts(ctx); err != nil {
			t.Fatalf("CheckHeartbeatTimeouts run %d failed: %v", i+1, err)
		}
	}

	if n := countPersistedOfflineAlerts(t, db, "m-stuck"); n != 1 {
		t.Errorf("expected exactly 1 alert across repeated ticks while offline, got %d", n)
	}
}

func TestCheckHeartbeatTimeouts_ReRaisesAlertResolvedWhileStillOffline(t *testing.T) {
	// Marking an alert resolved by hand while the machine is still silent does NOT count as
	// the problem being fixed, so the next tick raises it again. This is deliberate: the only
	// thing that legitimately clears an agent_offline alert is the agent reporting again
	// (see the AutoResolve pass), and honouring a manual mark over the actual state would
	// leave a live outage unreported.
	//
	// Note there is currently no UI for this — the web app never calls
	// POST /api/alerts/{id}/resolve — so in practice this path is only reachable by calling
	// the API directly.
	db := setupTestDB(t)
	scheduler := newSchedulerWithRealAlertService(t, db)
	ctx := context.Background()

	cfg := scheduler.runtimeCfg.Get()
	cfg.Agent.HeartbeatTimeoutSeconds = 120
	cfg.Agent.OfflineAlertAfterSeconds = 600

	insertMachineWithHeartbeat(t, db, "m-acked", "online", 20*time.Minute)

	if err := scheduler.CheckHeartbeatTimeouts(ctx); err != nil {
		t.Fatalf("first CheckHeartbeatTimeouts failed: %v", err)
	}
	if n := countPersistedOfflineAlerts(t, db, "m-acked"); n != 1 {
		t.Fatalf("expected 1 persisted alert, got %d", n)
	}

	// Operator marks it resolved while the machine is still down.
	if _, err := db.Exec(
		`UPDATE alerts SET status = 'resolved', resolved_at = ? WHERE target_id = ?`,
		time.Now().UTC().Format(time.RFC3339), "m-acked",
	); err != nil {
		t.Fatalf("failed to resolve alert: %v", err)
	}

	if err := scheduler.CheckHeartbeatTimeouts(ctx); err != nil {
		t.Fatalf("second CheckHeartbeatTimeouts failed: %v", err)
	}

	if n := countPersistedOfflineAlerts(t, db, "m-acked"); n != 2 {
		t.Errorf("expected the still-offline machine to be alerted again, got %d alerts", n)
	}
}

func TestCheckHeartbeatTimeouts_AlertsAgainAfterRecoveryAndSecondOutage(t *testing.T) {
	// Recovery clears the alert, so a later outage must be reported as a fresh one. Together
	// with the suppression test above this fixes the two ends of the behaviour: never repeat
	// while a single outage lasts, always report a new one.
	db := setupTestDB(t)
	scheduler := newSchedulerWithRealAlertService(t, db)
	ctx := context.Background()

	cfg := scheduler.runtimeCfg.Get()
	cfg.Agent.HeartbeatTimeoutSeconds = 120
	cfg.Agent.OfflineAlertAfterSeconds = 600

	// Outage 1: silent well past the alert threshold.
	insertMachineWithHeartbeat(t, db, "m-flaky", "online", 40*time.Minute)

	if err := scheduler.CheckHeartbeatTimeouts(ctx); err != nil {
		t.Fatalf("first CheckHeartbeatTimeouts failed: %v", err)
	}
	if n := countPersistedOfflineAlerts(t, db, "m-flaky"); n != 1 {
		t.Fatalf("expected 1 alert for the first outage, got %d", n)
	}

	// The agent reports again. This tick matters: with the machine online the AutoResolve
	// pass resolves the outstanding alert, which is what produces the [Recovered] message.
	// Without it the alert stays active and suppression would block the second outage.
	if _, err := db.Exec(
		`UPDATE machines SET status = 'online', last_heartbeat_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), "m-flaky",
	); err != nil {
		t.Fatalf("failed to simulate recovery: %v", err)
	}
	if err := scheduler.CheckHeartbeatTimeouts(ctx); err != nil {
		t.Fatalf("recovery CheckHeartbeatTimeouts failed: %v", err)
	}

	var status string
	if err := db.QueryRow(`SELECT status FROM alerts WHERE target_id = ?`, "m-flaky").Scan(&status); err != nil {
		t.Fatalf("failed to read alert status: %v", err)
	}
	if status != "resolved" {
		t.Fatalf("expected the first alert to be auto-resolved on recovery, got %q", status)
	}

	// Outage 2: silent again past the alert threshold (time compressed rather than waited on).
	if _, err := db.Exec(
		`UPDATE machines SET last_heartbeat_at = ? WHERE id = ?`,
		time.Now().UTC().Add(-20*time.Minute).Format(time.RFC3339), "m-flaky",
	); err != nil {
		t.Fatalf("failed to simulate the second outage: %v", err)
	}

	if err := scheduler.CheckHeartbeatTimeouts(ctx); err != nil {
		t.Fatalf("second CheckHeartbeatTimeouts failed: %v", err)
	}

	if n := countPersistedOfflineAlerts(t, db, "m-flaky"); n != 2 {
		t.Errorf("expected a second alert for the new outage, got %d alerts total", n)
	}
	if got := machineStatus(t, db, "m-flaky"); got != "offline" {
		t.Errorf("expected the machine to be offline again, got %q", got)
	}
}
