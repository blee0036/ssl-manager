package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/ssl-manager/ssl-manager/internal/model"
)

// This file holds the example / unit tests for DomainExpiryService (task 6.15).
// They are deterministic (standard testing, NOT gopter) and complement the
// property tests in domain_expiry_service_property_test.go by pinning down three
// concrete guarantees:
//
//   1. Storage isolation      — creating a root domain writes ONLY root_domains,
//                               never the TLS-monitoring tables (requirements 1.2 / 1.3).
//   2. Alert types disjoint   — the new alert types / target type do not collide
//                               with any existing alert type / the TLS "domain"
//                               target (requirement 5.7, also 1.4).
//   3. Import failure no-op    — a failed Cloudflare scan leaves the root-domain
//                               set unchanged (requirement 2.3).
//
// The shared helpers (setupRootDomainServiceDB / newTestDomainExpiryService /
// mockZoneScanner / mockWhoisClient / mockAlertSender) come from
// domain_expiry_testhelper_test.go (task 6.5) and are reused as-is.

// mustCountRows returns the row count of the given table, failing the test on any
// query error. Table names passed in are hardcoded test literals, so the string
// concatenation is safe here.
func mustCountRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("failed to count rows in %q: %v", table, err)
	}
	return n
}

// TestDomainExpiryService_StorageIsolation verifies that creating a root domain
// persists ONLY to the independent root_domains table and never touches the
// existing TLS-monitoring tables (domains / domain_monitor_results), keeping the
// two monitoring concerns fully separate.
//
// Requirements: 1.2, 1.3
func TestDomainExpiryService_StorageIsolation(t *testing.T) {
	env := newTestDomainExpiryService(t)

	// The shared service setupTestDB does not create the TLS-monitoring tables,
	// so create minimal stand-ins here — a COUNT(*) is all these assertions need.
	// Using IF NOT EXISTS keeps this robust even if the shared helper ever starts
	// creating them.
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS domains (id TEXT PRIMARY KEY)`,
		`CREATE TABLE IF NOT EXISTS domain_monitor_results (id TEXT PRIMARY KEY)`,
	} {
		if _, err := env.db.Exec(stmt); err != nil {
			t.Fatalf("failed to create TLS-monitoring stand-in table: %v", err)
		}
	}

	// Create a root domain (mixed case + trailing dot also exercises normalization
	// to the registrable domain "example.com").
	if _, err := env.svc.Create(context.Background(), model.CreateRootDomainInput{Name: "Example.COM."}); err != nil {
		t.Fatalf("Create returned unexpected error: %v", err)
	}

	// The new record lives exclusively in root_domains.
	if got := mustCountRows(t, env.db, "root_domains"); got != 1 {
		t.Errorf("root_domains: expected 1 row after Create, got %d", got)
	}
	// The TLS-monitoring tables remain untouched (requirements 1.2 / 1.3).
	if got := mustCountRows(t, env.db, "domains"); got != 0 {
		t.Errorf("domains (TLS): expected 0 rows (storage isolation), got %d", got)
	}
	if got := mustCountRows(t, env.db, "domain_monitor_results"); got != 0 {
		t.Errorf("domain_monitor_results (TLS): expected 0 rows (storage isolation), got %d", got)
	}
}

// TestDomainExpiryService_AlertTypesDisjoint verifies that the root-domain
// registration-expiry alert types (domain_expiring / domain_expired) are distinct
// from every existing alert type in the codebase, that they differ from each
// other, and that the root_domain alert target type differs from the TLS "domain"
// target type. This keeps registration-expiry alerting from clashing with the
// existing TLS/cert/agent/dns alerting.
//
// Requirements: 5.7 (also 1.4)
func TestDomainExpiryService_AlertTypesDisjoint(t *testing.T) {
	// The set of alert types already in use across the codebase, confirmed by
	// grepping every SendAlert / AutoResolve call site plus the alert tests.
	existingAlertTypes := map[string]struct{}{
		"cert_expiring":                {},
		"cert_expired":                 {},
		"cert_renew_failed":            {},
		"cert_expiring_manual_dns":     {},
		"cert_upload_cannot_autorenew": {},
		"agent_offline":                {},
		"agent_token_revoked":          {},
		"revoked_token_used":           {},
		"deploy_failed":                {},
		"domain_probe_failed":          {},
		"domain_cert_mismatch":         {},
		"dns_resolve_failed":           {},
		"tls_handshake_failed":         {},
		"fingerprint_mismatch":         {},
		"dns_sync_failed":              {},
	}

	// The constants carry the documented string values.
	if AlertTypeDomainExpiring != "domain_expiring" {
		t.Errorf("AlertTypeDomainExpiring = %q, want %q", AlertTypeDomainExpiring, "domain_expiring")
	}
	if AlertTypeDomainExpired != "domain_expired" {
		t.Errorf("AlertTypeDomainExpired = %q, want %q", AlertTypeDomainExpired, "domain_expired")
	}

	// The two new types must not collide with any existing alert type.
	for _, newType := range []string{AlertTypeDomainExpiring, AlertTypeDomainExpired} {
		if _, clash := existingAlertTypes[newType]; clash {
			t.Errorf("new alert type %q collides with an existing alert type", newType)
		}
	}

	// The two new types must differ from each other.
	if AlertTypeDomainExpiring == AlertTypeDomainExpired {
		t.Errorf("expiring and expired alert types must differ, both are %q", AlertTypeDomainExpiring)
	}

	// The root-domain alert target type must be distinct from the TLS "domain"
	// target type used by DomainMonitorService.
	if alertTargetTypeRootDomain != "root_domain" {
		t.Errorf("alertTargetTypeRootDomain = %q, want %q", alertTargetTypeRootDomain, "root_domain")
	}
	if alertTargetTypeRootDomain == "domain" {
		t.Errorf(`root_domain target type must differ from the TLS "domain" target type`)
	}
}

// TestDomainExpiryService_ImportFailureLeavesSetUnchanged verifies that when the
// Cloudflare scan fails (invalid token / fetch error), ImportFromCloudflare
// returns a descriptive (wrapped) error and does NOT modify the root-domain set.
// A subsequent successful import is exercised to prove the set-unchanged result
// above is due to the failure path, not a broken import.
//
// Requirements: 2.3
func TestDomainExpiryService_ImportFailureLeavesSetUnchanged(t *testing.T) {
	env := newTestDomainExpiryService(t)
	ctx := context.Background()

	if got := mustCountRows(t, env.db, "root_domains"); got != 0 {
		t.Fatalf("precondition: expected 0 root_domains, got %d", got)
	}

	// Arrange: ScanZones fails.
	scanErr := errors.New("invalid cloudflare api token")
	env.scanner.setError(scanErr)

	result, err := env.svc.ImportFromCloudflare(ctx, "bad-token")
	if err == nil {
		t.Fatalf("expected an error when ScanZones fails, got nil (result=%+v)", result)
	}
	if !errors.Is(err, scanErr) {
		t.Errorf("expected the returned error to wrap the scan error, got %v", err)
	}
	if result != nil {
		t.Errorf("expected a nil result on scan failure, got %+v", result)
	}
	// Requirement 2.3: the set is unchanged after a failed import.
	if got := mustCountRows(t, env.db, "root_domains"); got != 0 {
		t.Errorf("root_domains must be unchanged after a failed import, got %d rows", got)
	}

	// Contrast: a subsequent successful import DOES add rows.
	env.scanner.setZoneNames("example.com", "example.org")
	okResult, err := env.svc.ImportFromCloudflare(ctx, "good-token")
	if err != nil {
		t.Fatalf("successful import returned an unexpected error: %v", err)
	}
	if okResult == nil || len(okResult.Imported) != 2 {
		t.Fatalf("expected 2 imported registrable domains, got %+v", okResult)
	}
	if got := mustCountRows(t, env.db, "root_domains"); got != 2 {
		t.Errorf("expected 2 root_domains after a successful import, got %d", got)
	}
}

// TestDomainExpiryService_ReconcileConflictThenContinue verifies finding #2 for
// ReconcileCloudflareZones: a pre-existing (duplicate) registrable domain sitting
// at the FRONT of the reconcile list must NOT abort the loop. Before the fix the
// duplicate tripped the UNIQUE index and returned a hard error, so every later
// zone silently stopped being registered. Now the conflict is an idempotent no-op
// (created=false) and the loop keeps processing, so the two new zones are still
// registered.
//
// Requirements: 2.2, 2.4, 2.5
func TestDomainExpiryService_ReconcileConflictThenContinue(t *testing.T) {
	env := newTestDomainExpiryService(t)
	ctx := context.Background()

	// Seed one pre-existing cloudflare root domain that also appears (first) in the
	// reconcile list below.
	const existing = "existing.com"
	if err := env.repo.Create(ctx, &model.RootDomain{
		Name:              existing,
		Source:            "cloudflare",
		RegistrableDomain: existing,
		MonitorEnabled:    true,
	}); err != nil {
		t.Fatalf("seed Create failed: %v", err)
	}

	const newA = "newa.com"
	const newB = "newb.com"

	// Reconcile with the pre-existing domain FIRST, then two new ones. A duplicate
	// must not be fatal, and the loop must continue past it.
	if err := env.svc.ReconcileCloudflareZones(ctx, []string{existing, newA, newB}); err != nil {
		t.Fatalf("ReconcileCloudflareZones returned an error; a duplicate must not abort the loop: %v", err)
	}

	// All three must be registered: the pre-existing one retained, both new ones added.
	for _, reg := range []string{existing, newA, newB} {
		rd, err := env.repo.GetByRegistrableDomain(ctx, reg)
		if err != nil || rd == nil {
			t.Errorf("expected %q to be registered after reconcile, got err=%v", reg, err)
		}
	}

	// Exactly three rows — the duplicate did not create a second row.
	if got := mustCountRows(t, env.db, "root_domains"); got != 3 {
		t.Errorf("expected exactly 3 root_domains after reconcile, got %d", got)
	}
}
