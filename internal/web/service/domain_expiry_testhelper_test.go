package service

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// This file holds the SHARED test helpers for the DomainExpiryService service-layer
// property/unit tests (tasks 6.5–6.15). It is owned by task 6.5 (Property 2) and is
// intentionally kept free of any test cases so downstream tasks can reuse:
//
//   - mockZoneScanner          — a test double for the ZoneScanner interface.
//   - setupRootDomainServiceDB — an in-memory DB with the root_domains schema.
//   - testDomainExpiryEnv      — a bundle of a wired DomainExpiryService + deps.
//   - newTestDomainExpiryService — the service builder (with functional options).
//
// The mockWhoisClient (task 5.2, whois_client_mock_test.go) and mockAlertSender
// (scheduler_test.go) are reused as-is and are NOT redefined here.

// init makes RefreshAll-based tests fast by removing the polite inter-query
// backoff (production default is 1s). whoisRefreshBackoff is a package-level var
// declared in domain_expiry_service.go precisely so tests can zero it. Setting it
// once here covers every test in the package.
func init() {
	whoisRefreshBackoff = 0
}

// mockZoneScanner is a concurrency-safe test double for the ZoneScanner interface
// (service.ZoneScanner, satisfied in production by *ThirdpartDNSService). It is
// injected into DomainExpiryService so ImportFromCloudflare / reconcile tests can
// orchestrate the zones returned by ScanZones — or simulate a scan failure (task
// 6.15) — without touching the Cloudflare API.
//
// Orchestration:
//   - setZones / setZoneNames configure the zones returned on success.
//   - setError configures ScanZones to fail (clears any configured zones intent).
//
// Observability: every call is recorded (count + token) so tests can assert that
// ScanZones was (or was not) invoked.
type mockZoneScanner struct {
	mu     sync.Mutex
	zones  []Zone
	err    error
	calls  int
	tokens []string
}

// compile-time assertion that *mockZoneScanner satisfies ZoneScanner.
var _ ZoneScanner = (*mockZoneScanner)(nil)

// newMockZoneScanner creates an empty scanner that returns zero zones and no
// error until configured.
func newMockZoneScanner() *mockZoneScanner {
	return &mockZoneScanner{}
}

// setZones configures the zones returned by ScanZones and clears any error.
// Returns the receiver for fluent configuration.
func (m *mockZoneScanner) setZones(zones []Zone) *mockZoneScanner {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.zones = zones
	m.err = nil
	return m
}

// setZoneNames is a convenience that builds zones from the given names (each name
// becomes a Zone with a synthetic ID) and clears any error. Returns the receiver
// for fluent configuration.
func (m *mockZoneScanner) setZoneNames(names ...string) *mockZoneScanner {
	zones := make([]Zone, 0, len(names))
	for i, n := range names {
		zones = append(zones, Zone{ID: fmt.Sprintf("zone-%d", i), Name: n})
	}
	return m.setZones(zones)
}

// setError configures ScanZones to return err (simulating an invalid token /
// fetch failure, requirement 2.3). Returns the receiver for fluent configuration.
func (m *mockZoneScanner) setError(err error) *mockZoneScanner {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
	return m
}

// ScanZones implements ZoneScanner. It records the call (count + token) and
// returns either the configured error or a copy of the configured zones.
func (m *mockZoneScanner) ScanZones(_ context.Context, token string) ([]Zone, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.tokens = append(m.tokens, token)
	if m.err != nil {
		return nil, m.err
	}
	out := make([]Zone, len(m.zones))
	copy(out, m.zones)
	return out, nil
}

// callCount returns how many times ScanZones has been invoked.
func (m *mockZoneScanner) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// setupRootDomainServiceDB returns an in-memory SQLite DB that has every table
// created by the shared service setupTestDB PLUS the root_domains table and its
// three indexes. The schema mirrors internal/database/migrate.go exactly (source
// CHECK manual|cloudflare, nullable expiry_date/last_checked_at, INTEGER booleans,
// TEXT timestamps) so repository behavior in tests matches production.
func setupRootDomainServiceDB(t *testing.T) *sql.DB {
	t.Helper()

	db := setupTestDB(t)

	stmts := []string{
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
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_root_domains_registrable ON root_domains(registrable_domain)`,
		`CREATE INDEX IF NOT EXISTS idx_root_domains_enabled ON root_domains(monitor_enabled)`,
		`CREATE INDEX IF NOT EXISTS idx_root_domains_expiry ON root_domains(expiry_date)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("failed to create root_domains schema: %v", err)
		}
	}

	return db
}

// testDomainExpiryEnv bundles a DomainExpiryService with its wired dependencies
// and the underlying DB, so service-layer property/unit tests (tasks 6.5–6.15)
// can orchestrate inputs (WHOIS outcomes, Cloudflare zones), inspect side effects
// (alerts sent), tune the global config (threshold/interval), and query persisted
// state directly.
type testDomainExpiryEnv struct {
	svc     *DomainExpiryService
	repo    *repository.RootDomainRepository
	whois   *mockWhoisClient
	rdap    *mockRDAPClient
	scanner *mockZoneScanner
	alerter *mockAlertSender
	cfg     *config.RuntimeConfig
	db      *sql.DB
}

// testEnvConfig holds the resolved options for newTestDomainExpiryService.
type testEnvConfig struct {
	cfg          *config.Config
	whoisDefault mockWhoisResult
}

// testEnvOption customizes newTestDomainExpiryService.
type testEnvOption func(*testEnvConfig)

// withConfig overrides the default (config.DefaultConfig()) configuration.
func withConfig(cfg *config.Config) testEnvOption {
	return func(o *testEnvConfig) { o.cfg = cfg }
}

// withWhoisDefaultSuccess sets the injected mock WHOIS client's default outcome
// (for domains not explicitly orchestrated) to a successful expiry.
func withWhoisDefaultSuccess(expiry time.Time) testEnvOption {
	return func(o *testEnvConfig) { o.whoisDefault = mockWhoisResult{expiry: expiry} }
}

// withWhoisDefaultError sets the injected mock WHOIS client's default outcome
// (for domains not explicitly orchestrated) to a failure (e.g. ErrWhoisNoExpiry).
func withWhoisDefaultError(err error) testEnvOption {
	return func(o *testEnvConfig) { o.whoisDefault = mockWhoisResult{err: err} }
}

// newTestDomainExpiryService builds a DomainExpiryService wired with a real
// *repository.RootDomainRepository (over a fresh in-memory DB), an injected
// *mockWhoisClient (no real WHOIS network calls), a *mockZoneScanner, a
// *mockAlertSender, and a *config.RuntimeConfig, and calls SetWhoisClient. It
// returns a testDomainExpiryEnv bundling all of them for orchestration and
// assertions.
//
// Defaults (override via options or by mutating the returned env):
//   - config: config.DefaultConfig() — ExpiryThresholdDays=14, RefreshIntervalMinutes=1440,
//     WhoisTimeoutSeconds=10, so expiryThresholdDays() works out of the box.
//   - WHOIS default outcome: success far in the future. This keeps Create's
//     best-effort RefreshOne benign (it records a healthy expiry and evaluates no
//     alert), so callers that don't care about WHOIS get a clean baseline. Callers
//     that do care orchestrate per-domain outcomes on env.whois, which take
//     precedence over this default.
func newTestDomainExpiryService(t *testing.T, opts ...testEnvOption) *testDomainExpiryEnv {
	t.Helper()

	resolved := &testEnvConfig{
		cfg:          config.DefaultConfig(),
		whoisDefault: mockWhoisResult{expiry: time.Now().UTC().Add(3650 * 24 * time.Hour)},
	}
	for _, opt := range opts {
		opt(resolved)
	}

	db := setupRootDomainServiceDB(t)
	repo := repository.NewRootDomainRepository(db)

	whois := newMockWhoisClient()
	if resolved.whoisDefault.err != nil {
		whois.setDefaultError(resolved.whoisDefault.err)
	} else {
		whois.setDefaultSuccess(resolved.whoisDefault.expiry)
	}

	// Inject a default-FAILING mock RDAP client (default outcome ErrRDAPNoServer)
	// so every existing test stays offline and preserves its expectations: a WHOIS
	// success means RDAP is never consulted, while a WHOIS failure now also sees
	// the RDAP fallback fail (no real network) and still records "failed" — exactly
	// as before this fallback existed (crucial for Properties 7 and 11). Tests that
	// exercise the fallback orchestrate per-domain outcomes on env.rdap.
	rdap := newMockRDAPClient()

	scanner := newMockZoneScanner()
	alerter := &mockAlertSender{}
	runtimeCfg := config.NewRuntimeConfig(resolved.cfg)

	svc := NewDomainExpiryService(repo, scanner, alerter, runtimeCfg)
	svc.SetWhoisClient(whois)
	svc.SetRDAPClient(rdap)

	return &testDomainExpiryEnv{
		svc:     svc,
		repo:    repo,
		whois:   whois,
		rdap:    rdap,
		scanner: scanner,
		alerter: alerter,
		cfg:     runtimeCfg,
		db:      db,
	}
}
