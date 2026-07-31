package service

import (
	"context"
	"testing"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// This file holds the scheduler-integration tests for the domain registration
// expiry monitoring feature (task 8.3). They cover the scheduler wiring added in
// tasks 8.1 / 8.2:
//
//   1. Disabled when interval <= 0 — the expiry refresh ticker is NOT created
//      (and an enabled ticker is torn down) when RefreshIntervalMinutes is
//      non-positive, mirroring the existing DNS-sync disable semantics
//      (requirement 6.5).
//   2. Interval change rebuilds — a changed RefreshIntervalMinutes enables,
//      disables or reschedules the ticker; an unchanged value is a no-op
//      (requirements 6.3 / 6.5).
//   3. DNS-sync cadence reconcile — reconcileRootDomainsFromCloudflare (invoked by
//      dnsWorker right after runDNSSyncAll) scans the enabled Cloudflare configs'
//      zones and hands them to ReconcileCloudflareZones, additively registering
//      them as source="cloudflare" root domains (requirement 2.4).
//
// The interval tests exercise checkExpiryRefreshIntervalChange directly — the
// extracted, observable function that owns the ticker lifecycle (the run() loop's
// startup init shares the exact same `interval > 0` conditional). This mirrors how
// the existing DNS scheduler tests drive runDNSSyncAll directly rather than
// spinning up the whole select loop. Shared helpers/mocks (setupRootDomainServiceDB,
// mockCloudflareClient, mockZoneScanner, mockWhoisClient, mockAlertSender,
// testRuntimeCfg, mustCountRows) are reused as-is.

// newExpiryIntervalScheduler builds a minimal SchedulerService carrying only a
// RuntimeConfig whose DomainExpiry.RefreshIntervalMinutes is set to interval.
// That is all checkExpiryRefreshIntervalChange reads, so no DB/deps are needed.
func newExpiryIntervalScheduler(t *testing.T, interval int) *SchedulerService {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.DomainExpiry.RefreshIntervalMinutes = interval
	return &SchedulerService{runtimeCfg: config.NewRuntimeConfig(cfg)}
}

// TestScheduler_ExpiryRefreshDisabledWhenIntervalNonPositive verifies that a
// non-positive RefreshIntervalMinutes disables the periodic expiry refresh: no
// ticker is created (when already disabled) and any running ticker is torn down
// (channel cleared), aligning with the existing DNS-sync disable behavior.
//
// Requirements: 6.5
func TestScheduler_ExpiryRefreshDisabledWhenIntervalNonPositive(t *testing.T) {
	// (a) Already disabled and staying disabled (0 -> 0): no ticker is created.
	t.Run("stays disabled when interval remains 0", func(t *testing.T) {
		s := newExpiryIntervalScheduler(t, 0)

		var ticker *time.Ticker
		var c <-chan time.Time
		current := 0

		s.checkExpiryRefreshIntervalChange(&ticker, &c, &current)

		if ticker != nil {
			ticker.Stop()
			t.Errorf("expected no ticker when interval stays 0, got a non-nil ticker")
		}
		if c != nil {
			t.Errorf("expected a nil channel when interval stays 0")
		}
		if current != 0 {
			t.Errorf("expected current interval to remain 0, got %d", current)
		}
	})

	// (b) Enabled -> disabled (positive -> 0): the running ticker is torn down.
	t.Run("tears down ticker when interval changes to 0", func(t *testing.T) {
		s := newExpiryIntervalScheduler(t, 0)

		ticker := time.NewTicker(time.Minute)
		var c <-chan time.Time = ticker.C
		current := 1440

		s.checkExpiryRefreshIntervalChange(&ticker, &c, &current)

		if ticker != nil {
			ticker.Stop()
			t.Errorf("expected ticker to be nil (disabled) after interval -> 0, got non-nil")
		}
		if c != nil {
			t.Errorf("expected channel to be nil (disabled) after interval -> 0")
		}
		if current != 0 {
			t.Errorf("expected current interval to become 0, got %d", current)
		}
	})

	// (c) Enabled -> disabled (positive -> negative): non-positive also disables.
	t.Run("tears down ticker when interval changes to negative", func(t *testing.T) {
		s := newExpiryIntervalScheduler(t, -5)

		ticker := time.NewTicker(time.Minute)
		var c <-chan time.Time = ticker.C
		current := 1440

		s.checkExpiryRefreshIntervalChange(&ticker, &c, &current)

		if ticker != nil {
			ticker.Stop()
			t.Errorf("expected ticker to be nil (disabled) after interval -> negative, got non-nil")
		}
		if c != nil {
			t.Errorf("expected channel to be nil (disabled) after interval -> negative")
		}
		if current != -5 {
			t.Errorf("expected current interval to become -5, got %d", current)
		}
	})
}

// TestScheduler_ExpiryRefreshIntervalChangeRebuildsTicker verifies that a changed
// global RefreshIntervalMinutes enables (0 -> positive) or reschedules
// (positive -> different positive) the ticker, while an unchanged value is a
// no-op that keeps the existing ticker.
//
// Requirements: 6.3, 6.5
func TestScheduler_ExpiryRefreshIntervalChangeRebuildsTicker(t *testing.T) {
	// (a) Disabled -> enabled (0 -> 60): a new ticker is created.
	t.Run("enables ticker when interval goes from 0 to positive", func(t *testing.T) {
		s := newExpiryIntervalScheduler(t, 60)

		var ticker *time.Ticker
		var c <-chan time.Time
		current := 0

		s.checkExpiryRefreshIntervalChange(&ticker, &c, &current)

		if ticker == nil {
			t.Fatalf("expected a ticker to be created when interval goes 0 -> 60")
		}
		defer ticker.Stop()
		if c == nil {
			t.Errorf("expected a non-nil channel after enabling the ticker")
		}
		if current != 60 {
			t.Errorf("expected current interval to become 60, got %d", current)
		}
	})

	// (b) Reschedule (30 -> 90): the ticker is rebuilt (a new instance).
	t.Run("rebuilds ticker when a positive interval changes", func(t *testing.T) {
		s := newExpiryIntervalScheduler(t, 90)

		old := time.NewTicker(30 * time.Minute)
		ticker := old
		var c <-chan time.Time = ticker.C
		current := 30

		s.checkExpiryRefreshIntervalChange(&ticker, &c, &current)

		if ticker == nil {
			t.Fatalf("expected a ticker after reschedule, got nil")
		}
		defer ticker.Stop()
		if ticker == old {
			t.Errorf("expected the ticker to be rebuilt (new instance) after interval change")
		}
		if c == nil {
			t.Errorf("expected a non-nil channel after reschedule")
		}
		if current != 90 {
			t.Errorf("expected current interval to become 90, got %d", current)
		}
	})

	// (c) No change (60 -> 60): the existing ticker is kept (no rebuild).
	t.Run("keeps ticker when interval is unchanged", func(t *testing.T) {
		s := newExpiryIntervalScheduler(t, 60)

		ticker := time.NewTicker(60 * time.Minute)
		defer ticker.Stop()
		old := ticker
		var c <-chan time.Time = ticker.C
		current := 60

		s.checkExpiryRefreshIntervalChange(&ticker, &c, &current)

		if ticker != old {
			t.Errorf("expected the ticker to be kept (no rebuild) when interval is unchanged")
		}
		if current != 60 {
			t.Errorf("expected current interval to remain 60, got %d", current)
		}
	})
}

// TestScheduler_ReconcileRootDomainsFromCloudflareOnDNSCadence verifies that the
// DNS-sync cadence step (reconcileRootDomainsFromCloudflare, invoked by dnsWorker
// after runDNSSyncAll) scans the enabled Cloudflare configs' zones and hands them
// to DomainExpiryService.ReconcileCloudflareZones, which additively registers each
// zone's registrable domain as a source="cloudflare" root domain.
//
// Requirements: 2.4
func TestScheduler_ReconcileRootDomainsFromCloudflareOnDNSCadence(t *testing.T) {
	ctx := context.Background()

	db := setupRootDomainServiceDB(t)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS thirdpart_dns (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		type TEXT NOT NULL DEFAULT 'cloudflare',
		api_token TEXT NOT NULL,
		config_json TEXT NOT NULL DEFAULT '{}',
		main_domains TEXT NOT NULL DEFAULT '[]',
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("failed to create thirdpart_dns table: %v", err)
	}

	dnsRepo := repository.NewThirdpartDNSRepository(db)
	domainRepo := repository.NewDomainRepository(db)
	rootRepo := repository.NewRootDomainRepository(db)

	// Mock Cloudflare returns two zones; ScanZones delegates to ListZones.
	cfClient := &mockCloudflareClient{
		zones: []Zone{
			{ID: "zone-1", Name: "example.com"},
			{ID: "zone-2", Name: "example.net"},
		},
	}
	dnsSvc := NewThirdpartDNSService(dnsRepo, domainRepo, cfClient, nil, testRuntimeCfg())

	expirySvc := NewDomainExpiryService(rootRepo, newMockZoneScanner(), &mockAlertSender{}, testRuntimeCfg())
	// ReconcileCloudflareZones never calls WHOIS, but inject a mock anyway so no
	// real network client is ever reachable from this service in tests.
	expirySvc.SetWhoisClient(newMockWhoisClient())

	// One enabled Cloudflare config so reconcile has something to scan.
	if err := dnsRepo.Create(ctx, &model.ThirdpartDNS{
		Name:        "cf",
		Type:        "cloudflare",
		APIToken:    "tok",
		ConfigJSON:  "{}",
		MainDomains: []string{"example.com"},
		Enabled:     true,
	}); err != nil {
		t.Fatalf("failed to create thirdpart_dns config: %v", err)
	}

	scheduler := &SchedulerService{}
	scheduler.SetThirdpartDNSService(dnsSvc, dnsRepo)
	scheduler.SetDomainExpiryService(expirySvc)

	// Act: exactly what dnsWorker runs on the DNS-sync cadence after runDNSSyncAll.
	scheduler.reconcileRootDomainsFromCloudflare(ctx)

	// Assert: both zones were reconciled into root_domains as source="cloudflare".
	if got := mustCountRows(t, db, "root_domains"); got != 2 {
		t.Fatalf("expected 2 root domains after reconcile, got %d", got)
	}
	for _, reg := range []string{"example.com", "example.net"} {
		rd, err := rootRepo.GetByRegistrableDomain(ctx, reg)
		if err != nil {
			t.Fatalf("expected %q to be reconciled, got error: %v", reg, err)
		}
		if rd == nil {
			t.Fatalf("expected %q to exist after reconcile", reg)
		}
		if rd.Source != "cloudflare" {
			t.Errorf("expected source %q for %q, got %q", "cloudflare", reg, rd.Source)
		}
	}
}

// TestScheduler_ReconcileRootDomainsFromCloudflareNilServiceNoop verifies the
// nil-guard: when the DomainExpiryService has not been wired onto the scheduler,
// the DNS-cadence reconcile step is a safe no-op (no panic, no writes), so the
// feature degrades gracefully before assembly in main.go.
//
// Requirements: 2.4
func TestScheduler_ReconcileRootDomainsFromCloudflareNilServiceNoop(t *testing.T) {
	ctx := context.Background()

	db := setupRootDomainServiceDB(t)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS thirdpart_dns (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		type TEXT NOT NULL DEFAULT 'cloudflare',
		api_token TEXT NOT NULL,
		config_json TEXT NOT NULL DEFAULT '{}',
		main_domains TEXT NOT NULL DEFAULT '[]',
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("failed to create thirdpart_dns table: %v", err)
	}

	dnsRepo := repository.NewThirdpartDNSRepository(db)
	domainRepo := repository.NewDomainRepository(db)

	cfClient := &mockCloudflareClient{zones: []Zone{{ID: "zone-1", Name: "example.com"}}}
	dnsSvc := NewThirdpartDNSService(dnsRepo, domainRepo, cfClient, nil, testRuntimeCfg())

	if err := dnsRepo.Create(ctx, &model.ThirdpartDNS{
		Name: "cf", Type: "cloudflare", APIToken: "tok", ConfigJSON: "{}", Enabled: true,
	}); err != nil {
		t.Fatalf("failed to create thirdpart_dns config: %v", err)
	}

	// DomainExpiryService intentionally NOT set on the scheduler.
	scheduler := &SchedulerService{}
	scheduler.SetThirdpartDNSService(dnsSvc, dnsRepo)

	// Must not panic and must not write anything.
	scheduler.reconcileRootDomainsFromCloudflare(ctx)

	if got := mustCountRows(t, db, "root_domains"); got != 0 {
		t.Errorf("expected 0 root domains when expiry service is nil, got %d", got)
	}
}

// TestScheduler_ExpiryRefreshOverflowIntervalTreatedAsDisabled verifies the overflow
// guard: a positive-but-huge RefreshIntervalMinutes whose (minutes * time.Minute)
// overflows int64 into a non-positive time.Duration is treated as disabled. No ticker
// is created and time.NewTicker is never called with a non-positive duration (which
// would panic and crash the scheduler). A normal positive interval still creates a
// ticker, proving the guard does not over-reach.
//
// The test passing (no panic) is the assertion — checkExpiryRefreshIntervalChange
// would panic here if the ticker were built from the raw overflowed duration.
//
// Requirements: 6.3, 6.5, 6.7
func TestScheduler_ExpiryRefreshOverflowIntervalTreatedAsDisabled(t *testing.T) {
	// 200000000 minutes * time.Minute (6e10 ns) == 1.2e19 ns, which overflows int64
	// (max ~9.22e18) and wraps to a negative time.Duration.
	//
	// NOTE: this is a runtime variable, not a const. Go rejects a *constant*
	// multiplication that overflows int64 at compile time, whereas the production
	// helper multiplies a runtime int (which wraps silently) — so the test must
	// mirror that by using a runtime value.
	overflowMinutes := 200000000

	// Precondition: confirm the raw multiplication really is non-positive so this
	// test actually exercises the overflow guard rather than a normal path.
	if d := time.Duration(overflowMinutes) * time.Minute; d > 0 {
		t.Fatalf("test precondition failed: expected overflow to non-positive duration, got %d", d)
	}

	// (a) Disabled -> overflow value: must NOT create a ticker and must NOT panic.
	t.Run("overflow interval does not create a ticker or panic", func(t *testing.T) {
		s := newExpiryIntervalScheduler(t, overflowMinutes)

		var ticker *time.Ticker
		var c <-chan time.Time
		current := 0

		// Would panic here (time.NewTicker with non-positive duration) if unguarded.
		s.checkExpiryRefreshIntervalChange(&ticker, &c, &current)

		if ticker != nil {
			ticker.Stop()
			t.Errorf("expected no ticker for overflow interval (treated as disabled), got non-nil")
		}
		if c != nil {
			t.Errorf("expected nil channel for overflow interval (treated as disabled)")
		}
		if current != overflowMinutes {
			t.Errorf("expected current interval to record %d, got %d", overflowMinutes, current)
		}
	})

	// (b) Enabled -> overflow value: the running ticker is torn down and NOT rebuilt.
	t.Run("tears down existing ticker when interval changes to overflow value", func(t *testing.T) {
		s := newExpiryIntervalScheduler(t, overflowMinutes)

		ticker := time.NewTicker(time.Minute)
		var c <-chan time.Time = ticker.C
		current := 1440

		s.checkExpiryRefreshIntervalChange(&ticker, &c, &current)

		if ticker != nil {
			ticker.Stop()
			t.Errorf("expected ticker to be torn down (nil) for overflow interval, got non-nil")
		}
		if c != nil {
			t.Errorf("expected channel to be nil for overflow interval")
		}
		if current != overflowMinutes {
			t.Errorf("expected current interval to record %d, got %d", overflowMinutes, current)
		}
	})

	// (c) Disabled -> normal positive interval: a ticker is still created (guard
	// does not over-reach into valid values).
	t.Run("normal positive interval still creates a ticker", func(t *testing.T) {
		s := newExpiryIntervalScheduler(t, 120)

		var ticker *time.Ticker
		var c <-chan time.Time
		current := 0

		s.checkExpiryRefreshIntervalChange(&ticker, &c, &current)

		if ticker == nil {
			t.Fatalf("expected a ticker for a normal positive interval")
		}
		defer ticker.Stop()
		if c == nil {
			t.Errorf("expected a non-nil channel for a normal positive interval")
		}
		if current != 120 {
			t.Errorf("expected current interval to become 120, got %d", current)
		}
	})
}
