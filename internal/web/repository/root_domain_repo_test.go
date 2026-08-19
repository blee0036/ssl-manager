package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/model"
)

// setupRootDomainTestDB builds an in-memory test DB (reusing the shared setupTestDB
// helper) and adds the root_domains table plus its indexes, including the UNIQUE
// index on registrable_domain that enforces dedup. It returns a ready-to-use
// RootDomainRepository. The schema mirrors internal/database/migrate.go.
//
// This is the canonical root-domain test-DB helper for the repository package: it
// is defined here (the unit-test file) and also consumed by the property-test file
// (root_domain_repo_property_test.go), so it must not be redeclared elsewhere.
func setupRootDomainTestDB(t *testing.T) *RootDomainRepository {
	t.Helper()
	db := setupTestDB(t)

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS root_domains (
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
	)`); err != nil {
		t.Fatalf("failed to create root_domains table: %v", err)
	}

	// Indexes mirror migrate.go; the UNIQUE index is required by the dedup test.
	indexes := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_root_domains_registrable ON root_domains(registrable_domain)`,
		`CREATE INDEX IF NOT EXISTS idx_root_domains_enabled ON root_domains(monitor_enabled)`,
		`CREATE INDEX IF NOT EXISTS idx_root_domains_expiry ON root_domains(expiry_date)`,
	}
	for _, stmt := range indexes {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("failed to create root_domains index: %v", err)
		}
	}

	return NewRootDomainRepository(db)
}

// createTestRootDomain inserts a root domain with the given attributes and fails
// the test on error. Returns the created record (with generated ID/timestamps).
func createTestRootDomain(t *testing.T, repo *RootDomainRepository, name, registrable, source string, monitorEnabled bool, expiry *time.Time) *model.RootDomain {
	t.Helper()
	rd := &model.RootDomain{
		Name:              name,
		RegistrableDomain: registrable,
		Source:            source,
		MonitorEnabled:    monitorEnabled,
		ExpiryDate:        expiry,
	}
	if err := repo.Create(context.Background(), rd); err != nil {
		t.Fatalf("failed to create test root domain %q: %v", registrable, err)
	}
	return rd
}

// assertRootDomainOrder asserts that the returned records appear in exactly the
// expected order (compared by Name).
func assertRootDomainOrder(t *testing.T, label string, got []*model.RootDomain, wantNames []string) {
	t.Helper()
	if len(got) != len(wantNames) {
		t.Fatalf("%s: expected %d results, got %d (%v)", label, len(wantNames), len(got), rootDomainNames(got))
	}
	for i, name := range wantNames {
		if got[i].Name != name {
			t.Fatalf("%s: order mismatch at index %d: want %v, got %v", label, i, wantNames, rootDomainNames(got))
		}
	}
}

func rootDomainNames(rds []*model.RootDomain) []string {
	names := make([]string, len(rds))
	for i, rd := range rds {
		names[i] = rd.Name
	}
	return names
}

// TestRootDomainRepo_Create_DuplicateRegistrableRejected verifies that the UNIQUE
// index on registrable_domain rejects a second insert with the same registrable
// domain, and that the original record is preserved (requirements 2.2 / 3.4).
func TestRootDomainRepo_Create_DuplicateRegistrableRejected(t *testing.T) {
	repo := setupRootDomainTestDB(t)
	ctx := context.Background()

	first := &model.RootDomain{
		Name:              "example.com",
		RegistrableDomain: "example.com",
		Source:            "manual",
		MonitorEnabled:    true,
	}
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("first Create failed: %v", err)
	}

	// A second Create with the SAME registrable_domain must be rejected by the
	// UNIQUE index idx_root_domains_registrable.
	dup := &model.RootDomain{
		Name:              "www.example.com",
		RegistrableDomain: "example.com",
		Source:            "cloudflare",
		MonitorEnabled:    true,
	}
	if err := repo.Create(ctx, dup); err == nil {
		t.Fatal("expected duplicate registrable_domain to be rejected by unique index, got nil error")
	}

	// The original record must be preserved unchanged.
	got, err := repo.GetByRegistrableDomain(ctx, "example.com")
	if err != nil {
		t.Fatalf("GetByRegistrableDomain failed: %v", err)
	}
	if got.ID != first.ID {
		t.Errorf("expected preserved record ID %q, got %q", first.ID, got.ID)
	}
	if got.Source != "manual" {
		t.Errorf("expected preserved source 'manual', got %q", got.Source)
	}

	// Confirm exactly one record exists overall.
	all, err := repo.List(ctx, model.RootDomainFilter{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("expected exactly 1 root domain after rejected duplicate, got %d", len(all))
	}
}

// TestRootDomainRepo_ListWithSort_SortByName verifies whitelist sorting by name
// in both directions, exercising the case-insensitive LOWER(RTRIM(name,'.')) expr.
func TestRootDomainRepo_ListWithSort_SortByName(t *testing.T) {
	repo := setupRootDomainTestDB(t)
	ctx := context.Background()

	// Mixed-case names to verify LOWER is applied (otherwise uppercase would sort
	// before lowercase in raw ASCII ordering).
	createTestRootDomain(t, repo, "Charlie.example", "charlie.example", "manual", true, nil)
	createTestRootDomain(t, repo, "alpha.example", "alpha.example", "manual", true, nil)
	createTestRootDomain(t, repo, "Bravo.example", "bravo.example", "manual", true, nil)

	asc, total, err := repo.ListWithSort(ctx, model.RootDomainListParams{
		SortBy: "name", SortOrder: "asc", Page: 1, PerPage: 100,
	}, 14)
	if err != nil {
		t.Fatalf("ListWithSort name asc failed: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected total 3, got %d", total)
	}
	assertRootDomainOrder(t, "name asc", asc, []string{"alpha.example", "Bravo.example", "Charlie.example"})

	desc, _, err := repo.ListWithSort(ctx, model.RootDomainListParams{
		SortBy: "name", SortOrder: "desc", Page: 1, PerPage: 100,
	}, 14)
	if err != nil {
		t.Fatalf("ListWithSort name desc failed: %v", err)
	}
	assertRootDomainOrder(t, "name desc", desc, []string{"Charlie.example", "Bravo.example", "alpha.example"})
}

// TestRootDomainRepo_ListWithSort_SortByExpiryDate verifies whitelist sorting by
// expiry_date, including the null-expiry case which sorts as 0 (earliest).
func TestRootDomainRepo_ListWithSort_SortByExpiryDate(t *testing.T) {
	repo := setupRootDomainTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	exp5 := now.Add(5 * 24 * time.Hour)
	exp10 := now.Add(10 * 24 * time.Hour)
	exp30 := now.Add(30 * 24 * time.Hour)

	createTestRootDomain(t, repo, "ten.example", "ten.example", "manual", true, &exp10)
	createTestRootDomain(t, repo, "thirty.example", "thirty.example", "manual", true, &exp30)
	createTestRootDomain(t, repo, "five.example", "five.example", "manual", true, &exp5)
	createTestRootDomain(t, repo, "unknown.example", "unknown.example", "manual", true, nil) // null expiry -> 0

	asc, total, err := repo.ListWithSort(ctx, model.RootDomainListParams{
		SortBy: "expiry_date", SortOrder: "asc", Page: 1, PerPage: 100,
	}, 14)
	if err != nil {
		t.Fatalf("ListWithSort expiry asc failed: %v", err)
	}
	if total != 4 {
		t.Fatalf("expected total 4, got %d", total)
	}
	// Ascending: null(0) first, then 5d, 10d, 30d.
	assertRootDomainOrder(t, "expiry asc", asc, []string{"unknown.example", "five.example", "ten.example", "thirty.example"})

	desc, _, err := repo.ListWithSort(ctx, model.RootDomainListParams{
		SortBy: "expiry_date", SortOrder: "desc", Page: 1, PerPage: 100,
	}, 14)
	if err != nil {
		t.Fatalf("ListWithSort expiry desc failed: %v", err)
	}
	// Descending: 30d, 10d, 5d, then null(0) last.
	assertRootDomainOrder(t, "expiry desc", desc, []string{"thirty.example", "ten.example", "five.example", "unknown.example"})
}

// TestRootDomainRepo_ListWithSort_Pagination verifies page/per_page slicing and
// that total reflects the full count (before pagination).
func TestRootDomainRepo_ListWithSort_Pagination(t *testing.T) {
	repo := setupRootDomainTestDB(t)
	ctx := context.Background()

	for _, n := range []string{"a.example", "b.example", "c.example", "d.example", "e.example"} {
		createTestRootDomain(t, repo, n, n, "manual", true, nil)
	}

	page1, total, err := repo.ListWithSort(ctx, model.RootDomainListParams{
		SortBy: "name", SortOrder: "asc", Page: 1, PerPage: 2,
	}, 14)
	if err != nil {
		t.Fatalf("ListWithSort page1 failed: %v", err)
	}
	if total != 5 {
		t.Errorf("page1: expected total 5, got %d", total)
	}
	assertRootDomainOrder(t, "page1", page1, []string{"a.example", "b.example"})

	page2, total, err := repo.ListWithSort(ctx, model.RootDomainListParams{
		SortBy: "name", SortOrder: "asc", Page: 2, PerPage: 2,
	}, 14)
	if err != nil {
		t.Fatalf("ListWithSort page2 failed: %v", err)
	}
	if total != 5 {
		t.Errorf("page2: expected total 5, got %d", total)
	}
	assertRootDomainOrder(t, "page2", page2, []string{"c.example", "d.example"})

	page3, total, err := repo.ListWithSort(ctx, model.RootDomainListParams{
		SortBy: "name", SortOrder: "asc", Page: 3, PerPage: 2,
	}, 14)
	if err != nil {
		t.Fatalf("ListWithSort page3 failed: %v", err)
	}
	if total != 5 {
		t.Errorf("page3: expected total 5, got %d", total)
	}
	assertRootDomainOrder(t, "page3", page3, []string{"e.example"})
}

// TestRootDomainRepo_ListWithSort_UnknownSortByFallsBackToDefault verifies that an
// unrecognized sort_by does not error and falls back to the default order,
// identical to passing an empty sort_by.
func TestRootDomainRepo_ListWithSort_UnknownSortByFallsBackToDefault(t *testing.T) {
	repo := setupRootDomainTestDB(t)
	ctx := context.Background()

	for _, n := range []string{"x.example", "y.example", "z.example"} {
		createTestRootDomain(t, repo, n, n, "manual", true, nil)
	}

	unknown, unknownTotal, err := repo.ListWithSort(ctx, model.RootDomainListParams{
		SortBy: "not_a_real_column", Page: 1, PerPage: 100,
	}, 14)
	if err != nil {
		t.Fatalf("ListWithSort with unknown sort_by failed: %v", err)
	}
	def, defTotal, err := repo.ListWithSort(ctx, model.RootDomainListParams{
		SortBy: "", Page: 1, PerPage: 100,
	}, 14)
	if err != nil {
		t.Fatalf("ListWithSort with empty sort_by failed: %v", err)
	}

	if unknownTotal != 3 || defTotal != 3 {
		t.Fatalf("expected total 3 for both, got unknown=%d default=%d", unknownTotal, defTotal)
	}
	if len(unknown) != len(def) {
		t.Fatalf("expected same length, got unknown=%d default=%d", len(unknown), len(def))
	}
	for i := range def {
		if unknown[i].ID != def[i].ID {
			t.Fatalf("unknown sort_by did not fall back to default order at index %d: unknown=%s default=%s",
				i, unknown[i].ID, def[i].ID)
		}
	}
}

// TestRootDomainRepo_ListEnabled_OnlyEnabled verifies ListEnabled returns only
// records with monitor_enabled = 1 (requirement 6.6).
func TestRootDomainRepo_ListEnabled_OnlyEnabled(t *testing.T) {
	repo := setupRootDomainTestDB(t)
	ctx := context.Background()

	createTestRootDomain(t, repo, "enabled-1.example", "enabled-1.example", "manual", true, nil)
	createTestRootDomain(t, repo, "disabled-1.example", "disabled-1.example", "manual", false, nil)
	createTestRootDomain(t, repo, "enabled-2.example", "enabled-2.example", "cloudflare", true, nil)

	enabled, err := repo.ListEnabled(ctx)
	if err != nil {
		t.Fatalf("ListEnabled failed: %v", err)
	}

	if len(enabled) != 2 {
		t.Fatalf("expected 2 enabled root domains, got %d (%v)", len(enabled), rootDomainNames(enabled))
	}
	for _, rd := range enabled {
		if !rd.MonitorEnabled {
			t.Errorf("ListEnabled returned a disabled root domain: %s", rd.RegistrableDomain)
		}
		if rd.RegistrableDomain == "disabled-1.example" {
			t.Errorf("ListEnabled must not return the disabled record %q", rd.RegistrableDomain)
		}
	}
}

// TestRootDomainRepo_CreateIfNotExists_Idempotent verifies the atomic dedup
// primitive: the first insert reports created=true, and a second insert with the
// SAME registrable_domain is a no-op — it reports created=false with a nil error
// (a conflict is NOT an error), does NOT insert a duplicate row, and does NOT
// mutate the existing row (findings #2; requirements 2.2 / 3.4).
func TestRootDomainRepo_CreateIfNotExists_Idempotent(t *testing.T) {
	repo := setupRootDomainTestDB(t)
	ctx := context.Background()

	first := &model.RootDomain{
		Name:              "example.com",
		RegistrableDomain: "example.com",
		Source:            "manual",
		MonitorEnabled:    true,
	}
	created, err := repo.CreateIfNotExists(ctx, first)
	if err != nil {
		t.Fatalf("first CreateIfNotExists failed: %v", err)
	}
	if !created {
		t.Fatal("expected first CreateIfNotExists to report created=true")
	}

	// Second call with the SAME registrable_domain but DIFFERENT other fields: it
	// must be skipped by ON CONFLICT(registrable_domain) DO NOTHING.
	dup := &model.RootDomain{
		Name:              "www.example.com",
		RegistrableDomain: "example.com",
		Source:            "cloudflare",
		MonitorEnabled:    false,
	}
	created, err = repo.CreateIfNotExists(ctx, dup)
	if err != nil {
		t.Fatalf("second CreateIfNotExists returned an error for a conflict; want nil: %v", err)
	}
	if created {
		t.Fatal("expected second CreateIfNotExists to report created=false on conflict")
	}

	// Exactly one row exists (no duplicate inserted).
	all, err := repo.List(ctx, model.RootDomainFilter{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected exactly 1 row after a conflicting CreateIfNotExists, got %d", len(all))
	}

	// The existing row is preserved unchanged — the second call's values (different
	// name/source/monitor flag) must NOT have overwritten it.
	got, err := repo.GetByRegistrableDomain(ctx, "example.com")
	if err != nil {
		t.Fatalf("GetByRegistrableDomain failed: %v", err)
	}
	if got.ID != first.ID {
		t.Errorf("existing row ID changed: want %q, got %q", first.ID, got.ID)
	}
	if got.Name != "example.com" {
		t.Errorf("existing row name mutated: want %q, got %q", "example.com", got.Name)
	}
	if got.Source != "manual" {
		t.Errorf("existing row source mutated: want %q, got %q", "manual", got.Source)
	}
	if !got.MonitorEnabled {
		t.Error("existing row monitor_enabled mutated: want true, got false")
	}
}

// TestRootDomainRepo_CreateIfNotExists_Concurrent verifies the race-safety of the
// atomic dedup primitive (finding #2): N goroutines racing to insert the SAME
// registrable_domain all succeed without error, exactly one reports created=true,
// and exactly one row ends up in the table (the UNIQUE-index conflict is absorbed
// by ON CONFLICT DO NOTHING rather than surfacing as an error).
func TestRootDomainRepo_CreateIfNotExists_Concurrent(t *testing.T) {
	repo := setupRootDomainTestDB(t)
	ctx := context.Background()

	// An in-memory SQLite database is per-connection: each pooled connection would
	// otherwise see its OWN empty database. Pin the pool to a single shared
	// connection so all goroutines hit the same table. Application-level racing
	// (many goroutines inserting the same registrable domain at once) is still fully
	// exercised; the point of the test is that the atomic insert never errors and
	// never creates a duplicate.
	repo.db.SetMaxOpenConns(1)

	const goroutines = 16
	const reg = "race.example"

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, goroutines)
	createdCh := make(chan bool, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // release all goroutines together to maximize contention
			ok, err := repo.CreateIfNotExists(ctx, &model.RootDomain{
				Name:              reg,
				RegistrableDomain: reg,
				Source:            "manual",
				MonitorEnabled:    true,
			})
			if err != nil {
				errs <- err
				return
			}
			createdCh <- ok
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	close(createdCh)

	for err := range errs {
		t.Fatalf("CreateIfNotExists returned an error under concurrency: %v", err)
	}

	createdCount := 0
	for ok := range createdCh {
		if ok {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Errorf("expected exactly 1 goroutine to report created=true, got %d", createdCount)
	}

	all, err := repo.List(ctx, model.RootDomainFilter{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected exactly 1 row after concurrent CreateIfNotExists, got %d", len(all))
	}
	if all[0].RegistrableDomain != reg {
		t.Errorf("expected registrable domain %q, got %q", reg, all[0].RegistrableDomain)
	}
}

// tierForDays maps a truncated-toward-zero days_remaining value to the list
// filter_status tier, mirroring the service's alert/UI classification
// (days<=0 -> expired; 0<days<=threshold -> expiring; days>threshold -> ok). Used
// to cross-check that the SQL predicates agree with the days_remaining contract.
func tierForDays(days, threshold int) string {
	switch {
	case days <= 0:
		return "expired"
	case days <= threshold:
		return "expiring"
	default:
		return "ok"
	}
}

// TestRootDomainRepo_StatusFilter_MatchesDaysRemainingTiers verifies finding #4:
// the expired/expiring/ok filter_status predicates classify rows by the SAME
// truncated-toward-zero days_remaining = int((expiry-now)/24h) contract the
// service, alerts and frontend use — NOT by a raw timestamp comparison against
// "now". Rows are seeded at offsets that land just off the day boundaries where
// the two definitions used to disagree.
func TestRootDomainRepo_StatusFilter_MatchesDaysRemainingTiers(t *testing.T) {
	repo := setupRootDomainTestDB(t)
	ctx := context.Background()

	const threshold = 14
	now := time.Now().UTC()

	cases := []struct {
		name      string
		reg       string
		offset    time.Duration
		nilExpiry bool
		wantDays  int    // expected int((expiry-now)/24h); ignored when nilExpiry
		wantTier  string // "expired" | "expiring" | "ok" | "unknown"
	}{
		// +12h => days_remaining 0 => expired (NOT expiring). The old raw-timestamp
		// predicate wrongly put this in "expiring" (expiry > now).
		{name: "12h out -> days 0 -> expired", reg: "d12h.example", offset: 12 * time.Hour, wantDays: 0, wantTier: "expired"},
		// +36h => days_remaining 1 => expiring.
		{name: "36h out -> days 1 -> expiring", reg: "d36h.example", offset: 36 * time.Hour, wantDays: 1, wantTier: "expiring"},
		// +14d12h => days_remaining 14 => expiring (NOT ok) at threshold 14.
		{name: "14d12h out -> days 14 -> expiring", reg: "d14d12h.example", offset: 14*24*time.Hour + 12*time.Hour, wantDays: 14, wantTier: "expiring"},
		// +15d12h => days_remaining 15 => ok. The old predicate wrongly put a
		// threshold+12h row in "ok" while the UI still said "expiring"; here at
		// 15d12h both agree on "ok", pinning the corrected boundary.
		{name: "15d12h out -> days 15 -> ok", reg: "d15d12h.example", offset: 15*24*time.Hour + 12*time.Hour, wantDays: 15, wantTier: "ok"},
		// Already past => expired.
		{name: "2d past -> days -2 -> expired", reg: "past.example", offset: -2 * 24 * time.Hour, wantDays: -2, wantTier: "expired"},
		// Null expiry => unknown, never in expired/expiring/ok.
		{name: "null expiry -> unknown", reg: "unknown.example", nilExpiry: true, wantTier: "unknown"},
	}

	regToTier := make(map[string]string, len(cases))
	for _, c := range cases {
		var expiry *time.Time
		if !c.nilExpiry {
			e := now.Add(c.offset)
			expiry = &e
			// Cross-check: the hand-picked tier is consistent with the truncated
			// day count (the service's computeDaysRemaining-equivalent). This ties
			// the SQL assertions below directly to the days_remaining contract.
			gotDays := int(e.Sub(now).Hours() / 24)
			if gotDays != c.wantDays {
				t.Fatalf("%s: precondition day-count mismatch: computed %d, want %d", c.name, gotDays, c.wantDays)
			}
			if tierForDays(gotDays, threshold) != c.wantTier {
				t.Fatalf("%s: intended tier %q disagrees with days_remaining=%d tiering", c.name, c.wantTier, gotDays)
			}
		}
		createTestRootDomain(t, repo, c.reg, c.reg, "manual", true, expiry)
		regToTier[c.reg] = c.wantTier
	}

	// For each tier, ListWithSort(filter_status=tier) must return EXACTLY the seeded
	// rows whose intended tier equals it — no misses, no cross-tier bleed.
	for _, status := range []string{"expired", "expiring", "ok", "unknown"} {
		items, _, err := repo.ListWithSort(ctx, model.RootDomainListParams{
			FilterStatus: status, Page: 1, PerPage: 100,
		}, threshold)
		if err != nil {
			t.Fatalf("ListWithSort filter_status=%q failed: %v", status, err)
		}
		gotRegs := make(map[string]struct{}, len(items))
		for _, rd := range items {
			gotRegs[rd.RegistrableDomain] = struct{}{}
		}
		for reg, tier := range regToTier {
			_, present := gotRegs[reg]
			if tier == status && !present {
				t.Errorf("filter_status=%q: expected %q to be returned (its tier is %q)", status, reg, tier)
			}
			if tier != status && present {
				t.Errorf("filter_status=%q: %q should NOT be returned (its tier is %q)", status, reg, tier)
			}
		}
	}
}
