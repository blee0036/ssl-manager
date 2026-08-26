package service

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"testing"

	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// This file covers:
//
//  1. collectCloudflareZoneNames — multi-config collection & error isolation.
//     This helper survives the removal of the Cloudflare apex auto-sync
//     feature (cloudflare-domain-auto-sync spec): it is still used by
//     reconcileRootDomainsFromCloudflare / dnsWorker to drive
//     DomainExpiryService.ReconcileCloudflareZones (registration-expiry
//     monitoring, root_domains table).
//
//  2. cleanupZoneOnlyCloudflareDomainsOnce — the one-time bugfix cleanup that
//     removes leftover domains rows the (now-removed)
//     reconcileApexDomainMonitorsFromCloudflare previously created purely
//     because a Cloudflare Zone existed, regardless of whether that hostname
//     had any actual A/AAAA/CNAME record. TLS monitoring must be driven by a
//     real DNS record (ThirdpartDNSService.syncToLocalDomains), not by "being a
//     Zone/root domain" — that assumption was a design error.

// tokenAwareCloudflareClient is a CloudflareClient test double whose ListZones
// response depends on which API token was passed in. Unlike mockCloudflareClient
// (thirdpart_dns_service_test.go), which always returns the same fixed zones
// regardless of token, this lets a single test wire up multiple thirdpart_dns
// configs — each with a distinct token — that scan to different (or
// deliberately overlapping) zone sets, and lets one specific token's ScanZones
// call fail without affecting the others.
type tokenAwareCloudflareClient struct {
	zonesByToken map[string][]Zone
	errByToken   map[string]error
}

func (m *tokenAwareCloudflareClient) VerifyToken(ctx context.Context, token string) error {
	return nil
}

func (m *tokenAwareCloudflareClient) ListZones(ctx context.Context, token string) ([]Zone, error) {
	if err, ok := m.errByToken[token]; ok {
		return nil, err
	}
	return m.zonesByToken[token], nil
}

func (m *tokenAwareCloudflareClient) ListDNSRecords(ctx context.Context, token string, zoneID string, types []string) ([]DNSRecord, error) {
	return nil, nil
}

// setupCollectZoneNamesTestDB returns an in-memory SQLite DB with just the
// thirdpart_dns table (mirroring the schema used elsewhere in this package,
// e.g. scheduler_expiry_test.go), which is all collectCloudflareZoneNames'
// dnsRepo.List(ctx) call needs. The domains table is intentionally omitted:
// collectCloudflareZoneNames never touches it (it only lists configs and calls
// ScanZones), and repository.NewDomainRepository(db) itself runs no query at
// construction time, so the unused *repository.DomainRepository required by
// NewThirdpartDNSService's constructor signature is safe to build against this
// DB as-is.
func setupCollectZoneNamesTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := setupTestDB(t)

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

	return db
}

// countByName tallies occurrences of each name in names, so tests can assert
// both "which names are present" and "how many times" (duplicates preserved).
func countByName(names []string) map[string]int {
	counts := make(map[string]int, len(names))
	for _, n := range names {
		counts[n]++
	}
	return counts
}

// TestScheduler_CollectCloudflareZoneNames_UnionAcrossConfigsToleratesDuplicates
// verifies that collectCloudflareZoneNames, given multiple enabled thirdpart_dns
// configs whose ScanZones results overlap, returns every zone name contributed
// by every enabled config. A zone name shared by two configs appears twice in
// the result (the function does not de-duplicate); a disabled config's zones
// never appear at all.
func TestScheduler_CollectCloudflareZoneNames_UnionAcrossConfigsToleratesDuplicates(t *testing.T) {
	ctx := context.Background()
	db := setupCollectZoneNamesTestDB(t)
	dnsRepo := repository.NewThirdpartDNSRepository(db)
	domainRepo := repository.NewDomainRepository(db)

	cfClient := &tokenAwareCloudflareClient{
		zonesByToken: map[string][]Zone{
			"tok-a":        {{ID: "zone-a1", Name: "a.com"}, {ID: "zone-a2", Name: "b.com"}},
			"tok-b":        {{ID: "zone-b1", Name: "b.com"}, {ID: "zone-b2", Name: "c.com"}},
			"tok-disabled": {{ID: "zone-x", Name: "should-not-appear.com"}},
		},
	}
	dnsSvc := NewThirdpartDNSService(dnsRepo, domainRepo, cfClient, nil, testRuntimeCfg())

	for _, cfg := range []*model.ThirdpartDNS{
		{Name: "cf-a", Type: "cloudflare", APIToken: "tok-a", ConfigJSON: "{}", Enabled: true},
		{Name: "cf-b", Type: "cloudflare", APIToken: "tok-b", ConfigJSON: "{}", Enabled: true},
		{Name: "cf-disabled", Type: "cloudflare", APIToken: "tok-disabled", ConfigJSON: "{}", Enabled: false},
	} {
		if err := dnsRepo.Create(ctx, cfg); err != nil {
			t.Fatalf("failed to create thirdpart_dns config %q: %v", cfg.Name, err)
		}
	}

	scheduler := &SchedulerService{runtimeCfg: testRuntimeCfg()}
	scheduler.SetThirdpartDNSService(dnsSvc, dnsRepo)

	zoneNames := scheduler.collectCloudflareZoneNames(ctx)

	if len(zoneNames) != 4 {
		t.Fatalf("expected 4 zone names total (2 from each enabled config, duplicates preserved), got %d: %v", len(zoneNames), zoneNames)
	}

	counts := countByName(zoneNames)
	if counts["a.com"] != 1 {
		t.Errorf("expected a.com to appear once, got %d", counts["a.com"])
	}
	if counts["b.com"] != 2 {
		t.Errorf("expected b.com to appear twice (contributed by both configs, not de-duplicated), got %d", counts["b.com"])
	}
	if counts["c.com"] != 1 {
		t.Errorf("expected c.com to appear once, got %d", counts["c.com"])
	}
	if counts["should-not-appear.com"] != 0 {
		t.Errorf("expected the disabled config's zone to be excluded entirely, got count %d", counts["should-not-appear.com"])
	}

	// The de-duplicated union (set of unique names) must still be exactly the
	// three names contributed by the two enabled configs.
	uniqueNames := make([]string, 0, len(counts))
	for name := range counts {
		if name == "should-not-appear.com" {
			continue
		}
		uniqueNames = append(uniqueNames, name)
	}
	sort.Strings(uniqueNames)
	wantUnique := []string{"a.com", "b.com", "c.com"}
	if len(uniqueNames) != len(wantUnique) {
		t.Fatalf("expected unique zone set %v, got %v", wantUnique, uniqueNames)
	}
	for i, name := range wantUnique {
		if uniqueNames[i] != name {
			t.Errorf("expected unique zone set %v, got %v", wantUnique, uniqueNames)
			break
		}
	}
}

// TestScheduler_CollectCloudflareZoneNames_SingleConfigScanFailureIsolatesOthers
// verifies that when one enabled config's ScanZones call fails, the remaining
// enabled configs' zones are still collected — the failing config is skipped
// (logged and continue), and collection is not aborted for the rest.
func TestScheduler_CollectCloudflareZoneNames_SingleConfigScanFailureIsolatesOthers(t *testing.T) {
	ctx := context.Background()
	db := setupCollectZoneNamesTestDB(t)
	dnsRepo := repository.NewThirdpartDNSRepository(db)
	domainRepo := repository.NewDomainRepository(db)

	cfClient := &tokenAwareCloudflareClient{
		zonesByToken: map[string][]Zone{
			"tok-ok-1": {{ID: "zone-1", Name: "x.com"}},
			"tok-ok-2": {{ID: "zone-2", Name: "y.com"}, {ID: "zone-3", Name: "z.com"}},
		},
		errByToken: map[string]error{
			"tok-failing": errors.New("invalid token / cloudflare API error"),
		},
	}
	dnsSvc := NewThirdpartDNSService(dnsRepo, domainRepo, cfClient, nil, testRuntimeCfg())

	for _, cfg := range []*model.ThirdpartDNS{
		{Name: "cf-ok-1", Type: "cloudflare", APIToken: "tok-ok-1", ConfigJSON: "{}", Enabled: true},
		{Name: "cf-failing", Type: "cloudflare", APIToken: "tok-failing", ConfigJSON: "{}", Enabled: true},
		{Name: "cf-ok-2", Type: "cloudflare", APIToken: "tok-ok-2", ConfigJSON: "{}", Enabled: true},
	} {
		if err := dnsRepo.Create(ctx, cfg); err != nil {
			t.Fatalf("failed to create thirdpart_dns config %q: %v", cfg.Name, err)
		}
	}

	scheduler := &SchedulerService{runtimeCfg: testRuntimeCfg()}
	scheduler.SetThirdpartDNSService(dnsSvc, dnsRepo)

	// Must not panic despite one config's ScanZones failing.
	zoneNames := scheduler.collectCloudflareZoneNames(ctx)

	if len(zoneNames) != 3 {
		t.Fatalf("expected 3 zone names from the two surviving configs, got %d: %v", len(zoneNames), zoneNames)
	}

	counts := countByName(zoneNames)
	for _, want := range []string{"x.com", "y.com", "z.com"} {
		if counts[want] != 1 {
			t.Errorf("expected %q to be collected from a surviving config exactly once, got %d", want, counts[want])
		}
	}
}

// -----------------------------------------------------------------------------
// cleanupZoneOnlyCloudflareDomainsOnce — bugfix regression coverage
// -----------------------------------------------------------------------------

// setupCleanupTestDB returns an in-memory SQLite DB with the domains and
// domain_monitor_results tables (no unique index on name — that index was part
// of the removed apex auto-sync feature and no longer exists in migrate.go).
func setupCleanupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := setupTestDB(t)

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS domains (
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
	)`); err != nil {
		t.Fatalf("failed to create domains table: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS domain_monitor_results (
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
	)`); err != nil {
		t.Fatalf("failed to create domain_monitor_results table: %v", err)
	}

	return db
}

// TestScheduler_CleanupZoneOnlyCloudflareDomainsOnce_RemovesLeftoverZoneOnlyRecords
// verifies the bugfix: a domains row created by the removed apex auto-sync
// logic (source='cloudflare', thirdpart_dns_id=”, dns_record_id=” — i.e. it
// has no backing DNS record at all) is deleted, along with any of its
// domain_monitor_results, while a legitimate DNS-record-synced cloudflare
// domain, a manually-created domain, and legacy cloudflare data that simply
// hasn't been re-synced yet (thirdpart_dns_id set, dns_record_id empty) are all
// left untouched.
func TestScheduler_CleanupZoneOnlyCloudflareDomainsOnce_RemovesLeftoverZoneOnlyRecords(t *testing.T) {
	ctx := context.Background()
	db := setupCleanupTestDB(t)
	domainRepo := repository.NewDomainRepository(db)

	// Leftover zone-only record: no thirdpart_dns_id, no dns_record_id.
	if err := domainRepo.Create(ctx, &model.Domain{
		Name:           "8688.pp.ua",
		Source:         "cloudflare",
		MonitorPort:    443,
		MonitorEnabled: true,
	}); err != nil {
		t.Fatalf("failed to seed zone-only domain: %v", err)
	}
	// Fetch it back to get its generated ID for the monitor-result seed below.
	zoneOnly, err := domainRepo.List(ctx, model.DomainFilter{Name: "8688.pp.ua"})
	if err != nil || len(zoneOnly) != 1 {
		t.Fatalf("failed to fetch seeded zone-only domain: %v (rows=%v)", err, zoneOnly)
	}
	if err := domainRepo.SaveMonitorResult(ctx, &model.DomainMonitorResult{
		DomainID:    zoneOnly[0].ID,
		CheckedPort: 443,
		TLSSuccess:  false,
	}); err != nil {
		t.Fatalf("failed to seed monitor result for zone-only domain: %v", err)
	}

	// Legitimate DNS-record-synced cloudflare domain: has both thirdpart_dns_id
	// and dns_record_id.
	if err := domainRepo.Create(ctx, &model.Domain{
		Name:           "www.example.com",
		Source:         "cloudflare",
		ThirdpartDNSID: "dns-config-1",
		DNSRecordID:    "cf-rec-1",
		MonitorPort:    443,
		MonitorEnabled: true,
	}); err != nil {
		t.Fatalf("failed to seed legitimate cloudflare domain: %v", err)
	}

	// Legacy cloudflare data mid-migration: thirdpart_dns_id set, dns_record_id
	// not yet backfilled. Must NOT be treated as zone-only.
	if err := domainRepo.Create(ctx, &model.Domain{
		Name:           "legacy.example.com",
		Source:         "cloudflare",
		ThirdpartDNSID: "dns-config-1",
		MonitorPort:    443,
		MonitorEnabled: true,
	}); err != nil {
		t.Fatalf("failed to seed legacy cloudflare domain: %v", err)
	}

	// Manually-created domain: Source="manual", never matches the cleanup
	// signature regardless of its other fields.
	if err := domainRepo.Create(ctx, &model.Domain{
		Name:           "manual.example.com",
		Source:         "manual",
		MonitorPort:    443,
		MonitorEnabled: true,
	}); err != nil {
		t.Fatalf("failed to seed manual domain: %v", err)
	}

	scheduler := &SchedulerService{runtimeCfg: testRuntimeCfg()}
	scheduler.SetDomainRepo(domainRepo)

	scheduler.cleanupZoneOnlyCloudflareDomainsOnce(ctx)

	remaining, err := domainRepo.List(ctx, model.DomainFilter{})
	if err != nil {
		t.Fatalf("failed to list domains after cleanup: %v", err)
	}
	if len(remaining) != 3 {
		names := make([]string, len(remaining))
		for i, d := range remaining {
			names[i] = d.Name
		}
		t.Fatalf("expected 3 domains to remain after cleanup, got %d: %v", len(remaining), names)
	}
	for _, d := range remaining {
		if d.Name == "8688.pp.ua" {
			t.Fatalf("expected the zone-only leftover domain %q to be deleted, but it still exists", d.Name)
		}
	}

	// The zone-only domain's monitor result must also be gone (not left dangling).
	var monitorResultCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM domain_monitor_results WHERE domain_id = ?`, zoneOnly[0].ID).Scan(&monitorResultCount); err != nil {
		t.Fatalf("failed to count monitor results for the deleted zone-only domain: %v", err)
	}
	if monitorResultCount != 0 {
		t.Errorf("expected 0 monitor results for the deleted zone-only domain, got %d", monitorResultCount)
	}
}

// TestScheduler_CleanupZoneOnlyCloudflareDomainsOnce_RunsAtMostOnce verifies
// that the cleanup only actually queries/deletes on its first invocation
// (sync.Once) — calling it again within the same SchedulerService instance is
// a true no-op, not merely idempotent-by-accident.
func TestScheduler_CleanupZoneOnlyCloudflareDomainsOnce_RunsAtMostOnce(t *testing.T) {
	ctx := context.Background()
	db := setupCleanupTestDB(t)
	domainRepo := repository.NewDomainRepository(db)

	if err := domainRepo.Create(ctx, &model.Domain{
		Name:           "zone-only.example.com",
		Source:         "cloudflare",
		MonitorPort:    443,
		MonitorEnabled: true,
	}); err != nil {
		t.Fatalf("failed to seed zone-only domain: %v", err)
	}

	scheduler := &SchedulerService{runtimeCfg: testRuntimeCfg()}
	scheduler.SetDomainRepo(domainRepo)

	scheduler.cleanupZoneOnlyCloudflareDomainsOnce(ctx)

	if remaining, err := domainRepo.List(ctx, model.DomainFilter{}); err != nil || len(remaining) != 0 {
		t.Fatalf("expected 0 domains after first cleanup, got %d (err=%v)", len(remaining), err)
	}

	// Re-insert a new zone-only-looking row directly and call cleanup again:
	// because sync.Once already fired on this scheduler instance, this row must
	// NOT be removed — proving the cleanup genuinely runs at most once, rather
	// than happening to find nothing on a second real scan.
	if err := domainRepo.Create(ctx, &model.Domain{
		Name:           "another-zone-only.example.com",
		Source:         "cloudflare",
		MonitorPort:    443,
		MonitorEnabled: true,
	}); err != nil {
		t.Fatalf("failed to seed second zone-only domain: %v", err)
	}

	scheduler.cleanupZoneOnlyCloudflareDomainsOnce(ctx)

	remaining, err := domainRepo.List(ctx, model.DomainFilter{})
	if err != nil {
		t.Fatalf("failed to list domains after second cleanup call: %v", err)
	}
	if len(remaining) != 1 || remaining[0].Name != "another-zone-only.example.com" {
		t.Fatalf("expected the second cleanup call to be a true no-op (sync.Once already fired), got %d domains", len(remaining))
	}
}

// TestScheduler_CleanupZoneOnlyCloudflareDomainsOnce_NilDomainRepoNoopNoPanic
// verifies that when domainRepo has not been wired up (SetDomainRepo never
// called), the cleanup safely no-ops instead of panicking on the nil pointer.
func TestScheduler_CleanupZoneOnlyCloudflareDomainsOnce_NilDomainRepoNoopNoPanic(t *testing.T) {
	ctx := context.Background()

	// domainRepo intentionally left nil.
	scheduler := &SchedulerService{runtimeCfg: testRuntimeCfg()}

	// Must not panic.
	scheduler.cleanupZoneOnlyCloudflareDomainsOnce(ctx)
}
