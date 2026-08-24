package service

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// This file holds the scheduler unit tests for the Cloudflare apex auto-sync
// feature (cloudflare-domain-auto-sync spec). Task 3.6 covers
// reconcileApexDomainMonitorsFromCloudflare's create/skip/retain semantics:
//
//   1. A new zone name gets a source="cloudflare" Domain record created, with
//      monitor_enabled=true (requirements 2.1, 2.2).
//   2. A zone name that already has a Domain record (regardless of Source —
//      "manual" or "cloudflare") is left untouched: no duplicate row, no
//      overwrite of existing fields (requirement 2.2, 2.4, 2.5).
//   3. An existing cloudflare-sourced record whose zone name is absent from the
//      current batch is retained as-is — not deleted, not disabled
//      (requirement 3.1, 3.2).
//   4. When domainRepo has not been wired up (nil, see SetDomainRepo), the
//      function is a safe no-op and must not panic (requirement 4.3).
//
// setupApexSyncTestDB is local to this file because it needs the domains table
// WITH the idx_domains_name_normalized UNIQUE index (mirroring
// internal/database/migrate.go) — CreateIfNotExists's
// `ON CONFLICT(LOWER(RTRIM(name, '.'))) DO NOTHING` clause requires that exact
// unique index to exist as its conflict target. Other domains-table test
// helpers in this package (e.g. in scheduler_test.go / thirdpart_dns_service_test.go)
// intentionally omit this index because their tests don't exercise
// CreateIfNotExists.

// setupApexSyncTestDB returns an in-memory SQLite DB with the domains table
// (all columns used by DomainRepository.Create/CreateIfNotExists) plus the
// idx_domains_name_normalized UNIQUE index that CreateIfNotExists's
// ON CONFLICT target requires.
func setupApexSyncTestDB(t *testing.T) *sql.DB {
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

	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_domains_name_normalized ON domains(LOWER(RTRIM(name, '.')))`); err != nil {
		t.Fatalf("failed to create idx_domains_name_normalized: %v", err)
	}

	return db
}

// apexDomainRow is the subset of domains-table columns the tests below inspect.
type apexDomainRow struct {
	source         string
	monitorPort    int
	monitorEnabled bool
	alertIgnored   bool
}

// queryDomainByNormalizedName looks up a single domains row by its normalized
// name (LOWER+RTRIM '.'), matching the same comparison CreateIfNotExists's
// unique index uses. found=false when no row matches.
func queryDomainByNormalizedName(t *testing.T, db *sql.DB, name string) (row apexDomainRow, found bool) {
	t.Helper()
	var monitorEnabled, alertIgnored int
	err := db.QueryRow(
		`SELECT source, monitor_port, monitor_enabled, alert_ignored FROM domains WHERE LOWER(RTRIM(name, '.')) = LOWER(RTRIM(?, '.'))`,
		name,
	).Scan(&row.source, &row.monitorPort, &monitorEnabled, &alertIgnored)
	if err == sql.ErrNoRows {
		return apexDomainRow{}, false
	}
	if err != nil {
		t.Fatalf("failed to query domain by normalized name %q: %v", name, err)
	}
	row.monitorEnabled = monitorEnabled != 0
	row.alertIgnored = alertIgnored != 0
	return row, true
}

// TestScheduler_ReconcileApexDomainMonitors_CreatesNewZone verifies that a brand
// new zone name gets a source="cloudflare" apex Domain monitor record created,
// with monitor_enabled=true.
//
// Requirements: 2.1, 2.2
func TestScheduler_ReconcileApexDomainMonitors_CreatesNewZone(t *testing.T) {
	ctx := context.Background()
	db := setupApexSyncTestDB(t)
	domainRepo := repository.NewDomainRepository(db)

	scheduler := &SchedulerService{runtimeCfg: testRuntimeCfg()}
	scheduler.SetDomainRepo(domainRepo)

	scheduler.reconcileApexDomainMonitorsFromCloudflare(ctx, []string{"example.com", "example.net"})

	if got := mustCountRows(t, db, "domains"); got != 2 {
		t.Fatalf("expected 2 domains created, got %d", got)
	}

	for _, name := range []string{"example.com", "example.net"} {
		row, found := queryDomainByNormalizedName(t, db, name)
		if !found {
			t.Fatalf("expected a domain record for %q to be created", name)
		}
		if row.source != "cloudflare" {
			t.Errorf("expected source %q for %q, got %q", "cloudflare", name, row.source)
		}
		if !row.monitorEnabled {
			t.Errorf("expected monitor_enabled=true for %q", name)
		}
		if row.monitorPort != 443 {
			t.Errorf("expected default monitor_port 443 for %q, got %d", name, row.monitorPort)
		}
		if row.alertIgnored {
			t.Errorf("expected alert_ignored=false for %q", name)
		}
	}
}

// TestScheduler_ReconcileApexDomainMonitors_SkipsExistingRecordAnySource verifies
// that a zone name which already has a Domain record — whether Source="manual"
// or Source="cloudflare" — is left completely untouched (no duplicate row
// created, no field overwritten), including when the incoming zone name only
// matches after case/trailing-dot normalization. A genuinely new zone name in
// the same batch is still created normally.
//
// Requirements: 2.2, 2.4, 2.5
func TestScheduler_ReconcileApexDomainMonitors_SkipsExistingRecordAnySource(t *testing.T) {
	ctx := context.Background()
	db := setupApexSyncTestDB(t)
	domainRepo := repository.NewDomainRepository(db)

	// Pre-existing manual record with distinctive field values so an overwrite
	// would be detectable.
	manualDomain := &model.Domain{
		Name:           "example.com",
		Source:         "manual",
		MonitorPort:    8443,
		MonitorEnabled: false,
		AlertIgnored:   true,
	}
	if err := domainRepo.Create(ctx, manualDomain); err != nil {
		t.Fatalf("failed to seed manual domain: %v", err)
	}

	// Pre-existing cloudflare record, also with distinctive values.
	cloudflareDomain := &model.Domain{
		Name:           "test.com",
		Source:         "cloudflare",
		MonitorPort:    9443,
		MonitorEnabled: false,
	}
	if err := domainRepo.Create(ctx, cloudflareDomain); err != nil {
		t.Fatalf("failed to seed cloudflare domain: %v", err)
	}

	scheduler := &SchedulerService{runtimeCfg: testRuntimeCfg()}
	scheduler.SetDomainRepo(domainRepo)

	// "Example.COM." normalizes to the same key as the pre-existing "example.com".
	scheduler.reconcileApexDomainMonitorsFromCloudflare(ctx, []string{"Example.COM.", "test.com", "new.com"})

	if got := mustCountRows(t, db, "domains"); got != 3 {
		t.Fatalf("expected 3 domains total (2 pre-existing + 1 new), got %d", got)
	}

	// Existing manual record must be untouched.
	row, found := queryDomainByNormalizedName(t, db, "example.com")
	if !found {
		t.Fatalf("expected pre-existing manual domain to still exist")
	}
	if row.source != "manual" {
		t.Errorf("expected existing record's source to remain %q, got %q (must not be overwritten)", "manual", row.source)
	}
	if row.monitorPort != 8443 {
		t.Errorf("expected existing record's monitor_port to remain 8443, got %d (must not be overwritten)", row.monitorPort)
	}
	if row.monitorEnabled {
		t.Errorf("expected existing record's monitor_enabled to remain false (must not be overwritten)")
	}
	if !row.alertIgnored {
		t.Errorf("expected existing record's alert_ignored to remain true (must not be overwritten)")
	}

	// Existing cloudflare record must also be untouched.
	row, found = queryDomainByNormalizedName(t, db, "test.com")
	if !found {
		t.Fatalf("expected pre-existing cloudflare domain to still exist")
	}
	if row.source != "cloudflare" {
		t.Errorf("expected existing record's source to remain %q, got %q", "cloudflare", row.source)
	}
	if row.monitorPort != 9443 {
		t.Errorf("expected existing record's monitor_port to remain 9443, got %d (must not be overwritten)", row.monitorPort)
	}
	if row.monitorEnabled {
		t.Errorf("expected existing record's monitor_enabled to remain false (must not be overwritten)")
	}

	// The genuinely new zone name must still be created normally.
	row, found = queryDomainByNormalizedName(t, db, "new.com")
	if !found {
		t.Fatalf("expected a new domain record for %q to be created", "new.com")
	}
	if row.source != "cloudflare" {
		t.Errorf("expected source %q for new record, got %q", "cloudflare", row.source)
	}
	if !row.monitorEnabled {
		t.Errorf("expected monitor_enabled=true for new record")
	}
}

// TestScheduler_ReconcileApexDomainMonitors_RetainsDisappearedCloudflareRecord
// verifies that an existing cloudflare-sourced apex Domain record whose zone
// name is absent from the current batch of zoneNames is retained by default:
// not deleted, not disabled.
//
// Requirements: 3.1, 3.2
func TestScheduler_ReconcileApexDomainMonitors_RetainsDisappearedCloudflareRecord(t *testing.T) {
	ctx := context.Background()
	db := setupApexSyncTestDB(t)
	domainRepo := repository.NewDomainRepository(db)

	oldDomain := &model.Domain{
		Name:           "old.com",
		Source:         "cloudflare",
		MonitorPort:    443,
		MonitorEnabled: true,
	}
	if err := domainRepo.Create(ctx, oldDomain); err != nil {
		t.Fatalf("failed to seed cloudflare domain: %v", err)
	}

	scheduler := &SchedulerService{runtimeCfg: testRuntimeCfg()}
	scheduler.SetDomainRepo(domainRepo)

	// "old.com" is NOT in this batch, simulating it disappearing from the latest
	// Cloudflare zone scan.
	scheduler.reconcileApexDomainMonitorsFromCloudflare(ctx, []string{"new.com"})

	if got := mustCountRows(t, db, "domains"); got != 2 {
		t.Fatalf("expected 2 domains (retained old.com + new new.com), got %d", got)
	}

	row, found := queryDomainByNormalizedName(t, db, "old.com")
	if !found {
		t.Fatalf("expected old.com to be retained, not deleted")
	}
	if row.source != "cloudflare" {
		t.Errorf("expected retained record's source to remain %q, got %q", "cloudflare", row.source)
	}
	if !row.monitorEnabled {
		t.Errorf("expected retained record's monitor_enabled to remain true (must not be auto-disabled)")
	}
}

// TestScheduler_ReconcileApexDomainMonitors_NilDomainRepoNoopNoPanic verifies
// that when domainRepo has not been wired up (SetDomainRepo never called), the
// function safely no-ops instead of panicking on the nil pointer.
//
// Requirements: 4.3
func TestScheduler_ReconcileApexDomainMonitors_NilDomainRepoNoopNoPanic(t *testing.T) {
	ctx := context.Background()

	// domainRepo intentionally left nil.
	scheduler := &SchedulerService{runtimeCfg: testRuntimeCfg()}

	// Must not panic.
	scheduler.reconcileApexDomainMonitorsFromCloudflare(ctx, []string{"example.com", "example.net"})
}

// -----------------------------------------------------------------------------
// Task 3.5: collectCloudflareZoneNames — multi-config collection & error isolation
// -----------------------------------------------------------------------------
//
// NOTE on a discrepancy between tasks.md and the actual implementation: the
// tasks.md description for 3.5 says collectCloudflareZoneNames should return
// "去重后的并集" (a de-duplicated union). Reading the real implementation and its
// doc comment in scheduler.go shows this is NOT the case — the function
// deliberately mirrors the original reconcileRootDomainsFromCloudflare
// collection loop exactly and does NOT de-duplicate:
//
//	"Names are NOT de-duplicated here — ... Callers (ReconcileCloudflareZones
//	and the apex auto-sync path's CreateIfNotExists) both tolerate duplicate
//	names safely via their own idempotent/atomic dedup primitives, so no
//	additional dedup is performed here."
//
// The tests below therefore assert the actual behavior: the returned slice
// contains every zone name from every enabled config's successful ScanZones
// call (the union of contributed names, WITH duplicates preserved when zones
// overlap across configs), rather than asserting a strictly de-duplicated
// result. Error isolation (a single config's ScanZones failure must not affect
// the others, and must not cause the function to panic or abort) is verified
// separately.

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
// the result (the function does not de-duplicate — see the discrepancy note
// above); a disabled config's zones never appear at all.
//
// Requirements: 1.3, 1.4
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
// (logged and continue), and collection is not aborted for the rest. This
// mirrors the error-isolation behavior of the original
// reconcileRootDomainsFromCloudflare collection loop (requirement 1.4).
// collectCloudflareZoneNames has no error return value at all (it always
// returns only []string), so "does not return an error" is structurally
// guaranteed by its signature; what this test actually verifies is that a
// single failure does not panic and does not prevent the other configs' zones
// from being collected.
//
// Requirements: 1.3, 1.4
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
// Task 3.7: dnsWorker — the two reconciles share the SAME zoneNames batch
// -----------------------------------------------------------------------------
//
// Neither of the two collaborators dnsWorker drives after runDNSSyncAll is an
// interface field on SchedulerService: domainExpiryService is a concrete
// *DomainExpiryService and reconcileApexDomainMonitorsFromCloudflare is a plain
// method on SchedulerService itself (not a pluggable dependency), so neither can
// be swapped out for a spy struct directly. What CAN be substituted is the
// CloudflareClient (an interface) that ultimately backs every ListZones call —
// both runDNSSyncAll's DNS-record fetch (via ThirdpartDNSService.fetchRecords)
// and collectCloudflareZoneNames's zone scan (via ThirdpartDNSService.ScanZones)
// funnel through the SAME injected CloudflareClient.ListZones.
//
// sequencedCloudflareClient below returns a DIFFERENT, call-index-specific zone
// set on each successive ListZones invocation. With exactly one enabled
// thirdpart_dns config, a single dnsWorker trigger makes exactly two ListZones
// calls in a fixed order:
//
//	call 1 -> runDNSSyncAll -> SyncRecords -> fetchRecords -> ListZones
//	call 2 -> collectCloudflareZoneNames -> ScanZones -> ListZones   (THE shared batch)
//
// collectCloudflareZoneNames is invoked exactly ONCE per trigger and its single
// result (call 2's zones) is handed to BOTH ReconcileCloudflareZones (writing
// root_domains) and reconcileApexDomainMonitorsFromCloudflare (writing domains).
// If the two reconciles ever regressed to each independently re-scanning zones
// (the pre-fix shape described in design.md, where reconcileRootDomainsFromCloudflare
// and a hypothetical independent apex scan each called ListZones on their own),
// a third ListZones call would occur and sequencedCloudflareClient would hand it
// a THIRD, deliberately different zone set ("unexpected-extra-call.com") that
// would show up in domains and NOT in root_domains (or vice versa), failing the
// "same batch" assertions below. Asserting the total call count is exactly 2 is
// the direct, positive proof that collectCloudflareZoneNames ran only once.
//
// Validates: Requirements 1.3

// sequencedCloudflareClient is a CloudflareClient test double whose ListZones
// response depends only on the ORDER of the call (1st, 2nd, 3rd, ...), not on
// the token. This lets a single test pin down exactly which zone set a given
// call site (DNS-record sync vs. zone-name collection) receives, and detect an
// unexpected extra call by having it return a distinct, easily identifiable
// zone set beyond the configured sequence.
type sequencedCloudflareClient struct {
	mu       sync.Mutex
	sequence [][]Zone
	calls    int
}

func (m *sequencedCloudflareClient) VerifyToken(ctx context.Context, token string) error {
	return nil
}

func (m *sequencedCloudflareClient) ListZones(ctx context.Context, token string) ([]Zone, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := m.calls
	m.calls++
	if idx < len(m.sequence) {
		return m.sequence[idx], nil
	}
	// Beyond the configured sequence: return a distinct, obviously-out-of-place
	// zone set so any unexpected extra ListZones call is trivially detectable in
	// assertions rather than silently reusing the last configured set.
	return []Zone{{ID: "unexpected", Name: "unexpected-extra-call.com"}}, nil
}

func (m *sequencedCloudflareClient) ListDNSRecords(ctx context.Context, token string, zoneID string, types []string) ([]DNSRecord, error) {
	// No DNS records for any zone: keeps runDNSSyncAll's DNS-record-sync path a
	// clean no-op (RecordsCount=0, no domains created from it), so the domains
	// table's only writers in this test are reconcileApexDomainMonitorsFromCloudflare.
	return nil, nil
}

// callCount returns how many times ListZones has been invoked so far.
func (m *sequencedCloudflareClient) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// setupDnsWorkerBatchTestDB returns a single in-memory SQLite DB carrying every
// table a full dnsWorker trigger cycle touches: the base tables from
// setupTestDB, domains (+ its unique index, required by
// DomainRepository.CreateIfNotExists' ON CONFLICT target), root_domains (+ its
// indexes), and thirdpart_dns / thirdpart_dns_sync_logs. Unlike
// setupApexSyncTestDB / setupRootDomainServiceDB / setupCollectZoneNamesTestDB
// (each of which calls setupTestDB — i.e. sql.Open — independently and so cannot
// be composed together), this test needs ONE shared *sql.DB so that
// runDNSSyncAll, ReconcileCloudflareZones and reconcileApexDomainMonitorsFromCloudflare
// all observe each other's writes within a single dnsWorker trigger.
//
// SetMaxOpenConns(1) pins the pool to a single underlying connection: an
// in-memory SQLite database (":memory:", no shared-cache DSN) is per-connection,
// so without this, the polling goroutine (main test) and the dnsWorker goroutine
// could each be handed a DIFFERENT, independently-empty in-memory database by the
// connection pool. This mirrors the same fix already used by
// TestRootDomainRepo_CreateIfNotExists_Concurrent (root_domain_repo_test.go) for
// the identical reason (real concurrent goroutines sharing one *sql.DB).
func setupDnsWorkerBatchTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := setupTestDB(t)

	stmts := []string{
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
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_domains_name_normalized ON domains(LOWER(RTRIM(name, '.')))`,
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
		`CREATE TABLE IF NOT EXISTS thirdpart_dns (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT 'cloudflare',
			api_token TEXT NOT NULL,
			config_json TEXT NOT NULL DEFAULT '{}',
			main_domains TEXT NOT NULL DEFAULT '[]',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS thirdpart_dns_sync_logs (
			id TEXT PRIMARY KEY,
			thirdpart_dns_id TEXT NOT NULL,
			records_count INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL CHECK(status IN ('success', 'failed')),
			error_message TEXT DEFAULT '',
			new_domains TEXT DEFAULT '[]',
			updated_domains TEXT DEFAULT '[]',
			removed_domains TEXT DEFAULT '[]',
			synced_at TEXT NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("failed to create schema: %v", err)
		}
	}

	// See doc comment above: pin to a single connection so the dnsWorker
	// goroutine and this test's polling goroutine observe the same database.
	db.SetMaxOpenConns(1)

	return db
}

// queryNames runs a query expected to return a single TEXT column and returns
// the sorted list of values. Used to compare the set of names written to
// root_domains vs. domains irrespective of insertion order.
func queryNames(t *testing.T, db *sql.DB, query string, args ...interface{}) []string {
	t.Helper()
	rows, err := db.Query(query, args...)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows iteration failed: %v", err)
	}
	sort.Strings(names)
	return names
}

// TestScheduler_DnsWorker_SharesSameZoneNamesBatchBetweenReconciles verifies
// that a single dnsWorker trigger collects the Cloudflare zone names exactly
// ONCE and hands that SAME batch to both DomainExpiryService.ReconcileCloudflareZones
// (root_domains) and reconcileApexDomainMonitorsFromCloudflare (domains) — they
// never end up reconciling against two different, independently-scanned zone
// sets.
//
// Validates: Requirements 1.3
func TestScheduler_DnsWorker_SharesSameZoneNamesBatchBetweenReconciles(t *testing.T) {
	ctx := context.Background()
	db := setupDnsWorkerBatchTestDB(t)

	dnsRepo := repository.NewThirdpartDNSRepository(db)
	domainRepo := repository.NewDomainRepository(db)
	rootRepo := repository.NewRootDomainRepository(db)

	// Call 1 (runDNSSyncAll -> fetchRecords -> ListZones): an unrelated zone with
	// no DNS records, so the DNS-record-sync path stays a clean no-op and never
	// writes to domains itself.
	// Call 2 (collectCloudflareZoneNames -> ScanZones -> ListZones): the batch
	// that BOTH reconciles below must receive identically.
	cfClient := &sequencedCloudflareClient{
		sequence: [][]Zone{
			{{ID: "z1", Name: "batch1.com"}},
			{{ID: "z2", Name: "example.com"}, {ID: "z3", Name: "example.net"}},
		},
	}
	dnsSvc := NewThirdpartDNSService(dnsRepo, domainRepo, cfClient, nil, testRuntimeCfg())
	expirySvc := NewDomainExpiryService(rootRepo, newMockZoneScanner(), &mockAlertSender{}, testRuntimeCfg())
	expirySvc.SetWhoisClient(newMockWhoisClient())

	// Exactly one enabled config, so collectCloudflareZoneNames makes exactly one
	// ScanZones/ListZones call per trigger — the second call in the sequence.
	if err := dnsRepo.Create(ctx, &model.ThirdpartDNS{
		Name:       "cf",
		Type:       "cloudflare",
		APIToken:   "tok",
		ConfigJSON: "{}",
		Enabled:    true,
	}); err != nil {
		t.Fatalf("failed to create thirdpart_dns config: %v", err)
	}

	scheduler := &SchedulerService{runtimeCfg: testRuntimeCfg()}
	scheduler.SetThirdpartDNSService(dnsSvc, dnsRepo)
	scheduler.SetDomainExpiryService(expirySvc)
	scheduler.SetDomainRepo(domainRepo)

	stopCh := make(chan struct{})
	trigger := make(chan struct{}, 1)
	scheduler.wg.Add(1)
	go scheduler.dnsWorker(ctx, stopCh, trigger)

	// Fire exactly one trigger, mirroring what the run() loop's dnsSyncC case
	// sends on the real DNS-sync cadence.
	trigger <- struct{}{}

	// Poll (no test-only hooks into production code) until BOTH tables reflect
	// the completed trigger cycle. reconcileApexDomainMonitorsFromCloudflare runs
	// AFTER ReconcileCloudflareZones inside the trigger case, so waiting on the
	// domains table's row count is what actually signals the whole cycle is done.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if mustCountRows(t, db, "root_domains") >= 2 && mustCountRows(t, db, "domains") >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for dnsWorker to finish reconciling (root_domains=%d, domains=%d)",
				mustCountRows(t, db, "root_domains"), mustCountRows(t, db, "domains"))
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Shut the worker down cleanly before asserting.
	close(stopCh)
	scheduler.wg.Wait()

	// Exactly 2 ListZones calls total: one from runDNSSyncAll's DNS-record fetch,
	// one from collectCloudflareZoneNames. If either reconcile had independently
	// re-scanned zones, this would be 3 (or more).
	if got := cfClient.callCount(); got != 2 {
		t.Fatalf("expected exactly 2 ListZones calls (1 DNS-record fetch + 1 shared zone-name collection), got %d", got)
	}

	wantBatch := []string{"example.com", "example.net"}

	rootDomainNames := queryNames(t, db, `SELECT registrable_domain FROM root_domains ORDER BY registrable_domain`)
	if len(rootDomainNames) != len(wantBatch) {
		t.Fatalf("expected root_domains to contain exactly %v, got %v", wantBatch, rootDomainNames)
	}
	for i, want := range wantBatch {
		if rootDomainNames[i] != want {
			t.Errorf("root_domains[%d]: expected %q, got %q (full: %v)", i, want, rootDomainNames[i], rootDomainNames)
		}
	}

	apexDomainNames := queryNames(t, db, `SELECT name FROM domains WHERE source = 'cloudflare' ORDER BY name`)
	if len(apexDomainNames) != len(wantBatch) {
		t.Fatalf("expected domains (source=cloudflare) to contain exactly %v, got %v", wantBatch, apexDomainNames)
	}
	for i, want := range wantBatch {
		if apexDomainNames[i] != want {
			t.Errorf("domains[%d]: expected %q, got %q (full: %v)", i, want, apexDomainNames[i], apexDomainNames)
		}
	}

	// The two reconciles' output sets must be IDENTICAL — the direct assertion
	// that they were driven by the same zoneNames slice, not two independently
	// scanned batches.
	if len(rootDomainNames) != len(apexDomainNames) {
		t.Fatalf("root_domains batch %v and domains batch %v differ in size — the two reconciles did not share the same zoneNames", rootDomainNames, apexDomainNames)
	}
	for i := range rootDomainNames {
		if rootDomainNames[i] != apexDomainNames[i] {
			t.Fatalf("root_domains batch %v and domains batch %v diverge at index %d — the two reconciles did not share the same zoneNames", rootDomainNames, apexDomainNames, i)
		}
	}

	// Neither the DNS-record-sync zone (batch1.com) nor the "extra call"
	// sentinel should ever appear as an apex domain monitor record.
	for _, unwanted := range []string{"batch1.com", "unexpected-extra-call.com"} {
		if _, found := queryDomainByNormalizedName(t, db, unwanted); found {
			t.Errorf("did not expect %q to be registered as an apex domain monitor", unwanted)
		}
	}
}
