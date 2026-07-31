package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// This file holds the DomainExpiryService service-layer property tests. It is
// started by task 6.5 (Property 2) and APPENDED to by tasks 6.6–6.14. Shared
// fixtures live in domain_expiry_testhelper_test.go; the small generic helpers at
// the bottom of this file (rawNameFromSeed, sameSet, currentRegistrableSet) are
// intended for reuse by the later appended properties.

// ---------------------------------------------------------------------------
// Property 2: 规范化与去重幂等 (normalization & dedup idempotence)
// ---------------------------------------------------------------------------

// TestProperty2_NormalizationAndDedupIdempotent verifies Property 2.
//
// For any set of raw names (mixed case, trailing dots, a mix of manual and
// cloudflare sources, and duplicates), after routing them through manual add
// (Create), Cloudflare import (ImportFromCloudflare) and reconcile
// (ReconcileCloudflareZones):
//
//   - dedup:       each distinct registrable domain (eTLD+1) has EXACTLY one row
//     in root_domains (never two rows for names that normalize to the
//     same eTLD+1, e.g. "EXAMPLE.COM", "example.com.", "www.example.com").
//   - coverage:    every distinct valid eTLD+1 across the inputs is present.
//   - idempotence: repeating the whole manual/import/reconcile round leaves the
//     set of persisted registrable domains unchanged (no new rows).
//
// The oracle for "which eTLD+1s should exist" is the same pure RegistrableDomain
// function the service uses, so the property pins the service's persisted state to
// the normalization spec while also asserting dedup and idempotence.
//
// WHOIS and Cloudflare are mocked (no real network). gopter ≥100 iterations.
//
// Feature: domain-expiry-monitor, Property 2: 规范化与去重幂等
//
// **Validates: Requirements 2.1, 2.2, 2.4, 3.1, 3.4**
func TestProperty2_NormalizationAndDedupIdempotent(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(2) // deterministic

	properties := gopter.NewProperties(parameters)

	properties.Property("normalization + dedup is idempotent across manual add / import / reconcile", prop.ForAll(
		func(seeds []int) bool {
			env := newTestDomainExpiryService(t)
			ctx := context.Background()

			// Decode each seed into a raw name (mixed case, optional trailing dot,
			// optional subdomain prefix, single- and multi-level public suffixes).
			rawNames := make([]string, len(seeds))
			for i, s := range seeds {
				rawNames[i] = rawNameFromSeed(s)
			}

			// Partition into manual vs cloudflare inputs by index parity so both
			// code paths (and cross-source dedup) are exercised in one run.
			var manualNames, cfNames []string
			for i, n := range rawNames {
				if i%2 == 0 {
					manualNames = append(manualNames, n)
				} else {
					cfNames = append(cfNames, n)
				}
			}

			// Oracle: the distinct valid eTLD+1s across ALL inputs, regardless of
			// source. This is exactly the set of rows that must end up persisted.
			expected := make(map[string]struct{})
			for _, n := range rawNames {
				if reg, err := RegistrableDomain(n); err == nil {
					expected[reg] = struct{}{}
				}
			}

			// apply runs one full round of manual add + import + reconcile over the
			// same inputs. It returns false only on an UNEXPECTED error (duplicate
			// and validation rejections are the normal dedup/normalize outcomes).
			apply := func() bool {
				for _, n := range manualNames {
					if _, err := env.svc.Create(ctx, model.CreateRootDomainInput{Name: n}); err != nil {
						if !errors.Is(err, ErrDuplicate) && !errors.Is(err, ErrValidation) {
							t.Logf("unexpected Create error for %q: %v", n, err)
							return false
						}
					}
				}
				env.scanner.setZoneNames(cfNames...)
				if _, err := env.svc.ImportFromCloudflare(ctx, "token"); err != nil {
					t.Logf("unexpected ImportFromCloudflare error: %v", err)
					return false
				}
				if err := env.svc.ReconcileCloudflareZones(ctx, cfNames); err != nil {
					t.Logf("unexpected ReconcileCloudflareZones error: %v", err)
					return false
				}
				return true
			}

			// First round: assert dedup + coverage against the oracle.
			if !apply() {
				return false
			}
			set1, count1, ok := currentRegistrableSet(t, ctx, env.repo)
			if !ok {
				return false
			}
			// "At most one record per eTLD+1": row count equals the number of
			// distinct registrable domains (no duplicate rows).
			if count1 != len(set1) {
				t.Logf("duplicate rows detected: %d rows for %d distinct registrable domains", count1, len(set1))
				return false
			}
			// Coverage + dedup: persisted set equals the oracle set exactly.
			if !sameSet(set1, expected) {
				t.Logf("after first round: persisted=%v expected=%v", keysOf(set1), keysOf(expected))
				return false
			}

			// Second round with identical inputs: idempotent — no new rows, same set.
			if !apply() {
				return false
			}
			set2, count2, ok := currentRegistrableSet(t, ctx, env.repo)
			if !ok {
				return false
			}
			if count2 != count1 {
				t.Logf("not idempotent: row count changed %d -> %d", count1, count2)
				return false
			}
			if !sameSet(set1, set2) {
				t.Logf("not idempotent: set changed %v -> %v", keysOf(set1), keysOf(set2))
				return false
			}

			return true
		},
		// Bounded seed range so distinct raw names naturally collide (duplicates)
		// and many distinct seeds normalize to the same eTLD+1 (dedup pressure).
		gen.SliceOf(gen.IntRange(0, rdNameCombos-1)),
	))

	properties.TestingRun(t)
}

// ---------------------------------------------------------------------------
// Shared generators / helpers for the service-layer property tests.
// (Reusable by the properties appended in tasks 6.6–6.14.)
// ---------------------------------------------------------------------------

// Name-generation pools. Combined they yield rdNameCombos distinct raw names that
// deliberately stress normalization/dedup: mixed-case labels and suffixes, an
// optional trailing dot, optional subdomain prefixes, and both single-level
// (com/net) and multi-level (co.uk/com.cn) public suffixes. Many distinct combos
// normalize to the same registrable domain (e.g. "EXAMPLE.COM", "example.com.",
// "www.example.com" all -> "example.com").
var (
	rdSubPool    = []string{"", "www.", "WWW.", "a.b."}
	rdLabelPool  = []string{"example", "Example", "EXAMPLE", "test", "foo"}
	rdSuffixPool = []string{"com", "COM", "net", "co.uk", "com.cn"}
	rdDotPool    = []string{"", "."}
)

// rdNameCombos is the number of distinct raw names rawNameFromSeed can produce;
// the seed generator ranges over [0, rdNameCombos) so seeds map bijectively to
// combos while slice-level repetition still produces duplicate names.
const rdNameCombos = 4 * 5 * 5 * 2 // len(sub)*len(label)*len(suffix)*len(dot) = 200

// rawNameFromSeed deterministically decodes a non-negative seed into a raw domain
// name of the form [sub]label.suffix[.] drawn from the pools above.
func rawNameFromSeed(seed int) string {
	if seed < 0 {
		seed = -seed
	}
	sub := rdSubPool[seed%len(rdSubPool)]
	seed /= len(rdSubPool)
	label := rdLabelPool[seed%len(rdLabelPool)]
	seed /= len(rdLabelPool)
	suffix := rdSuffixPool[seed%len(rdSuffixPool)]
	seed /= len(rdSuffixPool)
	dot := rdDotPool[seed%len(rdDotPool)]
	return sub + label + "." + suffix + dot
}

// currentRegistrableSet lists every persisted root domain and returns the set of
// their registrable domains plus the total row count (so callers can detect
// duplicate rows via count != len(set)). Returns ok=false on a query error.
func currentRegistrableSet(t *testing.T, ctx context.Context, repo *repository.RootDomainRepository) (map[string]struct{}, int, bool) {
	t.Helper()
	rows, err := repo.List(ctx, model.RootDomainFilter{})
	if err != nil {
		t.Logf("repo.List error: %v", err)
		return nil, 0, false
	}
	set := make(map[string]struct{}, len(rows))
	for _, rd := range rows {
		set[rd.RegistrableDomain] = struct{}{}
	}
	return set, len(rows), true
}

// sameSet reports whether two string sets contain exactly the same keys.
func sameSet(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

// keysOf returns the keys of a set as a slice (for readable failure logs).
func keysOf(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ---------------------------------------------------------------------------
// Property 3: reconcile 保留已消失的 Cloudflare 根域名
//            (reconcile retains vanished Cloudflare root domains)
// ---------------------------------------------------------------------------

// property3LabelPool provides distinct DNS labels for building seeded root
// domains. The per-index label keeps every seeded registrable domain distinct
// regardless of the suffix chosen, and the pool is deliberately large enough to
// cover the bounded seed count used below without needing fmt/strconv (so this
// property can be pure-appended without touching the file's import block).
var property3LabelPool = []string{
	"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel",
	"india", "juliet", "kilo", "lima", "mike", "november", "oscar", "papa",
}

// property3SuffixPool cycles single-level (com/net/org) and multi-level
// (co.uk/com.cn) public suffixes so the property exercises eTLD+1 computation for
// both kinds. Combined with the distinct labels above, every seeded name maps to
// a distinct registrable domain that equals the constructed name.
var property3SuffixPool = []string{"com", "net", "co.uk", "com.cn", "org"}

// property3SeedName builds the i-th seeded root domain, e.g. "alpha.com",
// "bravo.net", "charlie.co.uk", "delta.com.cn". Callers must keep i within
// [0, len(property3LabelPool)).
func property3SeedName(i int) string {
	return property3LabelPool[i] + "." + property3SuffixPool[i%len(property3SuffixPool)]
}

// TestProperty3_ReconcileRetainsVanishedCloudflareDomains verifies Property 3.
//
// For any previously registered set of source="cloudflare" root domains, running
// ReconcileCloudflareZones with a PROPER SUBSET of that set must retain every
// pre-existing root domain — including the ones that no longer appear in the
// subset ("vanished" zones). Requirement 2.5 mandates default-retain: a cloudflare
// root domain that disappears from the latest zones is neither deleted nor
// disabled.
//
// Setup per iteration (fresh in-memory DB via newTestDomainExpiryService, so no
// cross-iteration interference):
//   - Seed N distinct cloudflare root domains d0..d(N-1) via ImportFromCloudflare.
//   - Build the reconcile subset so it ALWAYS excludes d0 (guaranteeing a proper
//     subset — at least one vanished domain every iteration) and includes each of
//     the remaining domains iff its random keep flag is set. Domains left out are
//     the "vanished" ones.
//   - Run ReconcileCloudflareZones(ctx, subset).
//
// Assertions after reconcile:
//   - retain:   every seeded registrable domain still exists (GetByRegistrableDomain
//     succeeds), including d0 and every other vanished domain.
//   - no-op:    the total row count is unchanged — reconcile with a subset of the
//     existing set adds nothing and removes nothing.
//   - unchanged: each retained record keeps MonitorEnabled == true (not disabled)
//     and Source == "cloudflare" (not mutated).
//
// WHOIS and Cloudflare are mocked (no real network; reconcile itself issues no
// WHOIS). gopter ≥100 iterations.
//
// Feature: domain-expiry-monitor, Property 3: reconcile 保留已消失的 Cloudflare 根域名
//
// **Validates: Requirements 2.5**
func TestProperty3_ReconcileRetainsVanishedCloudflareDomains(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(3) // deterministic

	properties := gopter.NewProperties(parameters)

	properties.Property("reconcile with a proper subset retains vanished cloudflare root domains", prop.ForAll(
		func(keepRest []bool) bool {
			// Bound N to the label pool (and keep each of 100+ fresh-DB iterations
			// fast). keepRest drives domains d1..d(N-1); d0 is always seeded and
			// always vanished, so N = len(keepRest) + 1.
			if maxRest := len(property3LabelPool) - 1; len(keepRest) > maxRest {
				keepRest = keepRest[:maxRest]
			}
			n := len(keepRest) + 1

			env := newTestDomainExpiryService(t)
			ctx := context.Background()

			// Seed all N domains as source="cloudflare" via the canonical import path.
			allNames := make([]string, n)
			for i := 0; i < n; i++ {
				allNames[i] = property3SeedName(i)
			}
			env.scanner.setZoneNames(allNames...)
			if _, err := env.svc.ImportFromCloudflare(ctx, "token"); err != nil {
				t.Logf("seed ImportFromCloudflare error: %v", err)
				return false
			}
			// Sanity: all N distinct registrable domains were seeded.
			if _, count, ok := currentRegistrableSet(t, ctx, env.repo); !ok || count != n {
				t.Logf("seed count mismatch: got %d want %d", count, n)
				return false
			}

			// Build the reconcile subset: ALWAYS exclude d0 (proper subset), include
			// d(i+1) iff keepRest[i]. Track the registrable domains that "vanished"
			// (are not in the subset) — the crux of requirement 2.5.
			var subset []string
			vanished := make(map[string]struct{})
			vanished[property3SeedName(0)] = struct{}{} // d0 is always vanished
			for i, keep := range keepRest {
				name := property3SeedName(i + 1)
				if keep {
					subset = append(subset, name)
				} else {
					vanished[name] = struct{}{}
				}
			}

			// Reconcile with the proper subset of the seeded set.
			if err := env.svc.ReconcileCloudflareZones(ctx, subset); err != nil {
				t.Logf("ReconcileCloudflareZones error: %v", err)
				return false
			}

			// Assertion "retain" + "unchanged": every seeded domain — kept OR
			// vanished — still exists, stays enabled, and keeps its cloudflare source.
			for i := 0; i < n; i++ {
				reg := property3SeedName(i)
				rd, err := env.repo.GetByRegistrableDomain(ctx, reg)
				if err != nil || rd == nil {
					t.Logf("seeded domain %q missing after reconcile (err=%v)", reg, err)
					return false
				}
				if !rd.MonitorEnabled {
					t.Logf("seeded domain %q was disabled after reconcile", reg)
					return false
				}
				if rd.Source != "cloudflare" {
					t.Logf("seeded domain %q source changed to %q after reconcile", reg, rd.Source)
					return false
				}
			}

			// Assertion "no-op": reconcile with a subset of the existing set neither
			// adds nor removes rows — the persisted set is exactly the seeded set.
			set, count, ok := currentRegistrableSet(t, ctx, env.repo)
			if !ok {
				return false
			}
			if count != n {
				t.Logf("row count changed after reconcile: got %d want %d", count, n)
				return false
			}
			if len(set) != n {
				t.Logf("distinct registrable set size changed: got %d want %d", len(set), n)
				return false
			}
			// Explicitly confirm each vanished domain was retained (default-retain).
			for reg := range vanished {
				if _, present := set[reg]; !present {
					t.Logf("vanished domain %q was not retained after reconcile", reg)
					return false
				}
			}

			return true
		},
		gen.SliceOf(gen.Bool()),
	))

	properties.TestingRun(t)
}

// ---------------------------------------------------------------------------
// Property 4: 拒绝空白与非法输入 (reject blank & invalid input)
// ---------------------------------------------------------------------------

// property4PublicSuffixes are public suffixes themselves (an eTLD, not an
// eTLD+1). RegistrableDomain rejects each because there is no registrable label
// sitting above the suffix (publicsuffix reports "cannot derive eTLD+1"), so
// Create must reject them with ErrValidation (requirements 3.3 / 4.2). Every
// entry here has been verified to fail RegistrableDomain.
var property4PublicSuffixes = []interface{}{"com", "co.uk", "com.cn", "net", "org"}

// property4SyntacticallyInvalid are non-whitespace strings that violate domain
// syntax and therefore make RegistrableDomain fail: a bare single label with no
// dot ("localhost"), a leading dot (".com" → empty label), a double dot
// ("a..b" → empty label), an embedded space ("a b"), a trailing-only label
// ("a." → normalizes to "a", a bare label) and a lone-dot string (".."). Every
// entry here has been verified to be rejected by Create with ErrValidation
// (requirement 3.3).
var property4SyntacticallyInvalid = []interface{}{"localhost", ".com", "a..b", "a b", "a.", ".."}

// property4WhitespaceChars are the runes used to synthesize pure-whitespace
// inputs. Any string composed solely of these trims to "" under
// strings.TrimSpace, which Create rejects as an empty name (requirement 3.2). A
// length of zero yields the empty string, also covered by requirement 3.2.
var property4WhitespaceChars = []byte{' ', '\t', '\n', '\r', '\v', '\f'}

// TestProperty4_RejectBlankAndInvalidInput verifies Property 4.
//
// For ANY input drawn from three categories that are ALL invalid — pure
// whitespace (including the empty string), a public suffix by itself (e.g.
// "com" / "co.uk" / "com.cn"), or a syntactically invalid string (bare label,
// leading/double dot, embedded space) — manual add (svc.Create) MUST:
//
//   - reject the request with a non-nil error that satisfies
//     errors.Is(err, ErrValidation) (requirements 3.2 empty/whitespace, 3.3
//     invalid syntax / public suffix), and
//   - leave the root_domains set unchanged (no new row is inserted): the row
//     count after the rejected Create equals the row count before it.
//
// The generator is built from a variable-length whitespace mapper plus two
// OneConstOf pools of verified-invalid constants, combined with OneGenOf so each
// iteration exercises one of the three categories at random. Every value the
// generator can produce has been confirmed to be genuinely invalid, so Create
// must reject it unconditionally — no input in the space is ever accepted.
//
// WHOIS and Cloudflare are mocked (Create rejects during validation, before any
// WHOIS/DB write, so neither is exercised here). gopter ≥100 iterations.
//
// Feature: domain-expiry-monitor, Property 4: 拒绝空白与非法输入
//
// **Validates: Requirements 3.2, 3.3**
func TestProperty4_RejectBlankAndInvalidInput(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(4) // deterministic

	properties := gopter.NewProperties(parameters)

	// whitespaceGen yields a string of length 0..6 composed solely of whitespace
	// runes, so it always trims to "" (an empty name, requirement 3.2).
	whitespaceGen := gen.IntRange(0, 6).Map(func(n int) string {
		out := make([]byte, n)
		for i := 0; i < n; i++ {
			out[i] = property4WhitespaceChars[i%len(property4WhitespaceChars)]
		}
		return string(out)
	})

	// invalidInputGen picks one of the three invalid categories per iteration.
	invalidInputGen := gen.OneGenOf(
		whitespaceGen,
		gen.OneConstOf(property4PublicSuffixes...),
		gen.OneConstOf(property4SyntacticallyInvalid...),
	)

	properties.Property("Create rejects blank/invalid input with ErrValidation and leaves the set unchanged", prop.ForAll(
		func(input string) bool {
			env := newTestDomainExpiryService(t)
			ctx := context.Background()

			// Record the root_domains row count before the rejected Create.
			_, countBefore, ok := currentRegistrableSet(t, ctx, env.repo)
			if !ok {
				return false
			}

			_, err := env.svc.Create(ctx, model.CreateRootDomainInput{Name: input})

			// Requirements 3.2 / 3.3: the request must be rejected as a validation
			// error (Create wraps ErrValidation for both empty/whitespace names and
			// invalid-domain / public-suffix inputs).
			if err == nil {
				t.Logf("Create(%q) unexpectedly succeeded; expected ErrValidation", input)
				return false
			}
			if !errors.Is(err, ErrValidation) {
				t.Logf("Create(%q) error = %v; want errors.Is(_, ErrValidation)", input, err)
				return false
			}

			// The root-domain set must be unchanged (no new row inserted).
			_, countAfter, ok := currentRegistrableSet(t, ctx, env.repo)
			if !ok {
				return false
			}
			if countAfter != countBefore {
				t.Logf("row count changed after rejected Create(%q): %d -> %d", input, countBefore, countAfter)
				return false
			}

			return true
		},
		invalidInputGen,
	))

	properties.TestingRun(t)
}

// ---------------------------------------------------------------------------
// Property 6: 刷新成功记录到期日与检查状态
//            (a successful refresh records the expiry date and check status)
// ---------------------------------------------------------------------------

// prop6MaxExpirySeconds is the upper bound for generated expiry instants
// (2099-12-31T23:59:59Z), keeping every generated time within [1970-01-01, 2100)
// at second precision. RFC3339 storage is second-precision, so generating
// whole-second instants (time.Unix(sec, 0)) guarantees the persisted value
// round-trips to the exact same instant under time.Time.Equal.
const prop6MaxExpirySeconds int64 = 4102444799 // 2099-12-31T23:59:59Z

// TestProperty6_RefreshSuccessRecordsExpiryAndStatus verifies Property 6.
//
// For ANY expiry instant that WHOIS returns successfully — supplied to the mock
// in an ARBITRARY time zone (verbatim, not pre-normalized) — RefreshOne must,
// after a successful lookup, record:
//
//   - expiry (UTC): the persisted expiry_date is non-nil, equals the
//     WHOIS-returned instant via time.Time.Equal (location-agnostic instant
//     equality), and is stored/returned in the UTC location (requirement 4.3
//     UTC normalization, 4.4 record the expiry).
//   - status:       last_status == "success" (requirements 4.4 / 7.1).
//   - checked-at:   last_checked_at is non-nil, i.e. this check's timestamp was
//     set (requirements 4.4 / 7.1).
//   - days known:   days_remaining is non-nil (bonus — a known expiry always
//     yields a computed remaining-days value, never "unknown").
//
// Setup per iteration uses a FRESH env (fresh in-memory DB) and seeds the root
// domain directly via repo.Create — bypassing svc.Create's own best-effort
// RefreshOne — so the explicit RefreshOne below is the only WHOIS query and its
// outcome is fully controlled by the mock. A fresh DB per iteration makes a fixed
// registrable domain trivially unique. The mock returns the expiry VERBATIM (in
// the generated zone), so the assertions genuinely exercise the service/repo UTC
// normalization rather than a pre-normalized input.
//
// Both the record returned by RefreshOne and a fresh repo.GetByID read-back are
// asserted, pinning both the in-memory result and the persisted state. (The
// repository read-back does not compute days_remaining — that is the service's
// job — so the days_remaining check applies only to the RefreshOne result.)
//
// WHOIS is mocked (no real network). gopter ≥100 iterations.
//
// Feature: domain-expiry-monitor, Property 6: 刷新成功记录到期日与检查状态
//
// **Validates: Requirements 4.4, 7.1**
func TestProperty6_RefreshSuccessRecordsExpiryAndStatus(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(6) // deterministic

	properties := gopter.NewProperties(parameters)

	properties.Property("a successful WHOIS refresh records the (UTC) expiry, success status and checked-at", prop.ForAll(
		func(sec int64, offsetHours int) bool {
			env := newTestDomainExpiryService(t)
			ctx := context.Background()

			// Seed a single root domain directly via the repository so the only
			// WHOIS query is the explicit RefreshOne below (svc.Create would fire
			// its own best-effort refresh, muddying the controlled outcome).
			const reg = "example.com"
			rd := &model.RootDomain{
				Name:              reg,
				Source:            "manual",
				RegistrableDomain: reg,
				MonitorEnabled:    true,
			}
			if err := env.repo.Create(ctx, rd); err != nil {
				t.Logf("seed repo.Create error: %v", err)
				return false
			}

			// The WHOIS-returned expiry: a whole-second instant displayed in an
			// arbitrary (possibly non-UTC) zone. The mock returns it verbatim, so
			// RefreshOne/the repo must be the ones to normalize it to UTC en route
			// to storage.
			loc := time.FixedZone("prop6zone", offsetHours*3600)
			expiry := time.Unix(sec, 0).In(loc)
			env.whois.setSuccess(reg, expiry)

			refreshed, err := env.svc.RefreshOne(ctx, rd.ID)
			if err != nil {
				t.Logf("RefreshOne error: %v", err)
				return false
			}

			// Assert both the returned record and a fresh read-back from the DB.
			persisted, err := env.repo.GetByID(ctx, rd.ID)
			if err != nil {
				t.Logf("GetByID read-back error: %v", err)
				return false
			}

			for _, c := range []struct {
				label string
				got   *model.RootDomain
			}{
				{"RefreshOne result", refreshed},
				{"GetByID read-back", persisted},
			} {
				g := c.got
				if g == nil {
					t.Logf("%s: record is nil", c.label)
					return false
				}
				// expiry recorded, instant-equal to the WHOIS value, in UTC.
				if g.ExpiryDate == nil {
					t.Logf("%s: expiry_date is nil; want instant %s", c.label, expiry)
					return false
				}
				if !g.ExpiryDate.Equal(expiry) {
					t.Logf("%s: expiry_date = %s; want instant-equal to %s", c.label, g.ExpiryDate, expiry)
					return false
				}
				if g.ExpiryDate.Location().String() != "UTC" {
					t.Logf("%s: expiry_date location = %s; want UTC", c.label, g.ExpiryDate.Location())
					return false
				}
				// status success.
				if g.LastStatus != "success" {
					t.Logf("%s: last_status = %q; want \"success\"", c.label, g.LastStatus)
					return false
				}
				// checked-at set (this check's timestamp).
				if g.LastCheckedAt == nil {
					t.Logf("%s: last_checked_at is nil; want it set", c.label)
					return false
				}
			}

			// Bonus: a known expiry always yields a computed days_remaining (never
			// "unknown"). Only the RefreshOne result carries it — repo.GetByID does
			// not compute days_remaining (that is the service GetByID's job, covered
			// by Property 12).
			if refreshed.DaysRemaining == nil {
				t.Logf("RefreshOne result: days_remaining is nil; want a computed value for known expiry %s", expiry)
				return false
			}

			return true
		},
		gen.Int64Range(0, prop6MaxExpirySeconds),
		gen.IntRange(-12, 14),
	))

	properties.TestingRun(t)
}

// ---------------------------------------------------------------------------
// Property 7: 刷新失败保留既有到期日
//            (a failed refresh preserves the previously known expiry date)
// ---------------------------------------------------------------------------

// property7WhoisFailures enumerates ALL WHOIS failure classes a refresh can hit:
// network error / no available server and rate-limit (requirement 4.5), plus
// unparsable response and missing-expiry (requirement 4.6). The property below
// selects one per iteration so every failure type is exercised and asserted to
// behave identically — the previously known expiry is preserved, last_status
// becomes "failed", and last_error is non-empty. These sentinels are defined in
// whois_client.go (same package); the mock returns them verbatim, so
// RefreshOne records werr.Error() (always non-empty) as last_error.
var property7WhoisFailures = []error{
	ErrWhoisQuery,     // network error / no available WHOIS server (req 4.5)
	ErrWhoisRateLimit, // rate limited (req 4.5)
	ErrWhoisParse,     // response could not be parsed into an expiry (req 4.6)
	ErrWhoisNoExpiry,  // parsed successfully but no expiry field (req 4.6)
}

// TestProperty7_RefreshFailurePreservesExistingExpiry verifies Property 7.
//
// For ANY previously known expiry date and ANY class of WHOIS failure (network /
// no-server / rate-limit / parse / no-expiry), a subsequent RefreshOne must NOT
// clobber the known expiry: after the failed refresh the persisted expiry_date is
// unchanged, last_status is "failed", and last_error is non-empty (requirements
// 4.5 / 4.6 / 7.2). A WHOIS-layer failure must also NOT surface as a Go error
// (design: "WHOIS 层失败不冒泡 Go error") — RefreshOne returns the preserved
// record with a nil error.
//
// Setup per iteration (FRESH env / in-memory DB, so a fixed registrable domain is
// trivially unique and there is no cross-iteration interference):
//   - Seed one root domain via repo.Create (bypassing svc.Create's best-effort
//     RefreshOne, so the only refresh is the explicit failing one below).
//   - Establish a KNOWN expiry via repo.SaveExpiryResult(&known, ..., "success",
//     ""), i.e. simulate a prior SUCCESSFUL check. The known instant is supplied
//     in an arbitrary time zone so the preserved value is compared as an instant
//     (time.Time.Equal), not by wall-clock/zone. Whole-second instants round-trip
//     exactly through the repo's RFC3339 (second-precision) storage.
//   - Orchestrate the WHOIS mock to FAIL for this domain with the selected failure
//     class (env.whois.setError), overriding the env's permissive default success.
//
// Assertions (on BOTH the record returned by RefreshOne and a fresh repo.GetByID
// read-back, pinning the in-memory result and the persisted state):
//   - no Go error from RefreshOne (the WHOIS failure is folded into the record).
//   - expiry preserved: expiry_date is non-nil and instant-equal to the seeded
//     known expiry (unchanged by the failure).
//   - status: last_status == "failed".
//   - error recorded: last_error is non-empty.
//
// WHOIS is mocked (no real network). gopter ≥100 iterations.
//
// Feature: domain-expiry-monitor, Property 7: 刷新失败保留既有到期日
//
// **Validates: Requirements 4.5, 4.6, 7.2**
func TestProperty7_RefreshFailurePreservesExistingExpiry(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(7) // deterministic

	properties := gopter.NewProperties(parameters)

	properties.Property("a failed WHOIS refresh preserves the known expiry, sets failed status and a non-empty error", prop.ForAll(
		func(sec int64, offsetHours int, failIdx int) bool {
			env := newTestDomainExpiryService(t)
			ctx := context.Background()

			// Seed a single root domain directly via the repository so the only
			// WHOIS query is the explicit failing RefreshOne below (svc.Create would
			// fire its own best-effort refresh, muddying the controlled outcome).
			const reg = "example.com"
			rd := &model.RootDomain{
				Name:              reg,
				Source:            "manual",
				RegistrableDomain: reg,
				MonitorEnabled:    true,
			}
			if err := env.repo.Create(ctx, rd); err != nil {
				t.Logf("seed repo.Create error: %v", err)
				return false
			}

			// Establish a KNOWN expiry via a simulated prior SUCCESSFUL check. The
			// instant is displayed in an arbitrary (possibly non-UTC) zone; the repo
			// stores it as UTC RFC3339 (second precision), so a whole-second instant
			// round-trips exactly and compares instant-equal via time.Time.Equal.
			loc := time.FixedZone("prop7zone", offsetHours*3600)
			known := time.Unix(sec, 0).In(loc)
			priorCheck := time.Now().UTC().Add(-24 * time.Hour)
			if err := env.repo.SaveExpiryResult(ctx, rd.ID, &known, priorCheck, "success", ""); err != nil {
				t.Logf("seed SaveExpiryResult error: %v", err)
				return false
			}

			// Arrange the selected WHOIS failure class for this domain, overriding
			// the env's permissive default success. Covering all four sentinels
			// across iterations exercises requirements 4.5 and 4.6.
			failErr := property7WhoisFailures[failIdx]
			env.whois.setError(reg, failErr)

			// A WHOIS-layer failure must NOT bubble up as a Go error (it is folded
			// into the returned record).
			refreshed, err := env.svc.RefreshOne(ctx, rd.ID)
			if err != nil {
				t.Logf("RefreshOne returned a Go error for WHOIS failure %v: %v", failErr, err)
				return false
			}

			// Read back the persisted state as well, to pin both surfaces.
			persisted, gerr := env.repo.GetByID(ctx, rd.ID)
			if gerr != nil {
				t.Logf("GetByID read-back error: %v", gerr)
				return false
			}

			for _, c := range []struct {
				label string
				got   *model.RootDomain
			}{
				{"RefreshOne result", refreshed},
				{"GetByID read-back", persisted},
			} {
				g := c.got
				if g == nil {
					t.Logf("%s: record is nil", c.label)
					return false
				}
				// expiry preserved: unchanged and instant-equal to the seeded known value.
				if g.ExpiryDate == nil {
					t.Logf("%s: expiry_date is nil after failure %v; want it preserved as %s", c.label, failErr, known)
					return false
				}
				if !g.ExpiryDate.Equal(known) {
					t.Logf("%s: expiry_date = %s after failure %v; want it unchanged (instant-equal to %s)", c.label, g.ExpiryDate, failErr, known)
					return false
				}
				// status failed (requirements 4.5 / 4.6 / 7.2).
				if g.LastStatus != "failed" {
					t.Logf("%s: last_status = %q after failure %v; want \"failed\"", c.label, g.LastStatus, failErr)
					return false
				}
				// error recorded (non-empty descriptive failure reason).
				if g.LastError == "" {
					t.Logf("%s: last_error is empty after failure %v; want a non-empty description", c.label, failErr)
					return false
				}
			}

			return true
		},
		gen.Int64Range(0, prop6MaxExpirySeconds),
		gen.IntRange(-12, 14),
		gen.IntRange(0, len(property7WhoisFailures)-1),
	))

	properties.TestingRun(t)
}

// ---------------------------------------------------------------------------
// Property 8: 告警级别匹配到期阈值分级
//            (alert level matches the expiry-threshold tiering)
// ---------------------------------------------------------------------------

// prop8DaysToExpiry converts a target remaining-days value into an expiry instant
// (UTC) that RefreshOne/evaluateAlerts will classify as EXACTLY `days` remaining.
//
// daysRemaining truncates (expiry-now)/24h toward zero, so an expiry sitting on a
// whole-day boundary is fragile: the sub-second drift between "now" captured here
// and "now" re-captured inside evaluateAlerts (and the repo's RFC3339 second
// truncation on store) could tip an integer boundary. To make the target robust
// we offset by a HALF DAY away from the boundary in the sign direction of `days`:
//
//   - days >= 0: expiry = now + days*24h + 12h  -> (expiry-now)/24h ≈ days + 0.5
//   - days <  0: expiry = now + days*24h - 12h  -> (expiry-now)/24h ≈ days - 0.5
//
// In both cases int() truncates the .5 fraction back to exactly `days`, and no
// sub-second drift can ever cross the integer boundary (12h dwarfs it). This lets
// the test pick a precise tier (expired / expiring / healthy) deterministically.
func prop8DaysToExpiry(days int) time.Time {
	now := time.Now().UTC()
	base := time.Duration(days) * 24 * time.Hour
	if days >= 0 {
		return now.Add(base + 12*time.Hour)
	}
	return now.Add(base - 12*time.Hour)
}

// prop8AlertMatches reports whether a single recorded alert has the expected
// level, type, root-domain target type and target id, logging a descriptive
// failure (including the threshold/days context) otherwise. Shared by the
// critical and warning tiers below.
func prop8AlertMatches(t *testing.T, a sentAlert, wantLevel, wantType, wantID string, threshold, days int) bool {
	if a.Level != wantLevel {
		t.Logf("threshold=%d days=%d: alert level = %q; want %q", threshold, days, a.Level, wantLevel)
		return false
	}
	if a.AlertType != wantType {
		t.Logf("threshold=%d days=%d: alert type = %q; want %q", threshold, days, a.AlertType, wantType)
		return false
	}
	if a.TargetType != "root_domain" {
		t.Logf("threshold=%d days=%d: alert target_type = %q; want \"root_domain\"", threshold, days, a.TargetType)
		return false
	}
	if a.TargetID != wantID {
		t.Logf("threshold=%d days=%d: alert target_id = %q; want %q", threshold, days, a.TargetID, wantID)
		return false
	}
	return true
}

// TestProperty8_AlertLevelMatchesExpiryThresholdTiering verifies Property 8.
//
// For ANY global Expiry Threshold and ANY remaining-days value `days`, a
// successful WHOIS refresh must raise exactly the alert dictated by the tier the
// remaining days fall into:
//
//   - days <= 0             -> a critical `domain_expired` alert    (requirement 5.4)
//   - 0 < days <= threshold -> a warning  `domain_expiring` alert   (requirements 5.2 / 5.3)
//   - days > threshold      -> NO new registration-expiry alert     (renewed / healthy)
//
// The switch in evaluateAlerts tests `days <= 0` before `days <= threshold`, so
// the expired tier takes precedence at the 0 boundary; this test mirrors that
// ordering exactly.
//
// Setup per iteration (FRESH env / in-memory DB, so a fixed registrable domain is
// trivially unique and the mockAlertSender's recordings never leak across
// iterations):
//   - Set the global ExpiryThresholdDays on the runtime config to the generated
//     threshold. All generated thresholds are > 0, so evaluateAlerts uses the
//     value verbatim (no fallback to the default 14).
//   - Seed one root domain via repo.Create (bypassing svc.Create's best-effort
//     RefreshOne, so the explicit RefreshOne below is the ONLY alert-evaluating
//     query — exactly one evaluateAlerts call, over a clean alert log).
//   - Orchestrate the WHOIS mock to succeed for this domain with an expiry that
//     realizes EXACTLY `days` remaining (prop8DaysToExpiry). RefreshOne persists
//     the expiry and then evaluates alerts.
//
// Assertions (against the mockAlertSender's recorded SendAlert calls; its
// AutoResolve is a no-op that records nothing, so the days>threshold tier leaves
// the alert log empty — i.e. no NEW expiry alert is sent):
//   - critical tier: exactly one recorded alert, level "critical", type
//     "domain_expired", target_type "root_domain", target_id = the seeded id.
//   - warning tier:  exactly one recorded alert, level "warning", type
//     "domain_expiring", target_type "root_domain", target_id = the seeded id.
//   - healthy tier:  zero recorded alerts.
//
// The generated ranges (threshold in [1,60], days in [-60,120]) span all three
// tiers with ample coverage across the 100+ iterations.
//
// WHOIS and Cloudflare are mocked (no real network). gopter ≥100 iterations.
//
// Feature: domain-expiry-monitor, Property 8: 告警级别匹配到期阈值分级
//
// **Validates: Requirements 5.2, 5.3, 5.4**
func TestProperty8_AlertLevelMatchesExpiryThresholdTiering(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(8) // deterministic

	properties := gopter.NewProperties(parameters)

	properties.Property("a successful refresh raises the alert level matching the remaining-days tier", prop.ForAll(
		func(threshold, days int) bool {
			env := newTestDomainExpiryService(t)
			ctx := context.Background()

			// Set the global expiry threshold. All generated thresholds are > 0, so
			// evaluateAlerts uses this value verbatim (no fallback to 14). Get()
			// returns the live config pointer; mutate it and Update to be explicit.
			cfg := env.cfg.Get()
			cfg.DomainExpiry.ExpiryThresholdDays = threshold
			env.cfg.Update(cfg)

			// Seed a single root domain directly via the repository so the only
			// alert-evaluating query is the explicit RefreshOne below.
			const reg = "example.com"
			rd := &model.RootDomain{
				Name:              reg,
				Source:            "manual",
				RegistrableDomain: reg,
				MonitorEnabled:    true,
			}
			if err := env.repo.Create(ctx, rd); err != nil {
				t.Logf("seed repo.Create error: %v", err)
				return false
			}

			// Orchestrate a successful WHOIS lookup returning an expiry that yields
			// EXACTLY `days` remaining, then refresh (persists the expiry and fires
			// evaluateAlerts).
			env.whois.setSuccess(reg, prop8DaysToExpiry(days))
			if _, err := env.svc.RefreshOne(ctx, rd.ID); err != nil {
				t.Logf("threshold=%d days=%d: RefreshOne error: %v", threshold, days, err)
				return false
			}

			alerts := env.alerter.alerts
			switch {
			case days <= 0:
				// Requirement 5.4: already expired -> exactly one critical domain_expired.
				if len(alerts) != 1 {
					t.Logf("threshold=%d days=%d: got %d alerts; want exactly 1 (critical domain_expired)", threshold, days, len(alerts))
					return false
				}
				return prop8AlertMatches(t, alerts[0], "critical", "domain_expired", rd.ID, threshold, days)
			case days <= threshold:
				// Requirements 5.2 / 5.3: within the threshold -> exactly one warning domain_expiring.
				if len(alerts) != 1 {
					t.Logf("threshold=%d days=%d: got %d alerts; want exactly 1 (warning domain_expiring)", threshold, days, len(alerts))
					return false
				}
				return prop8AlertMatches(t, alerts[0], "warning", "domain_expiring", rd.ID, threshold, days)
			default:
				// days > threshold: no NEW registration-expiry alert is sent (the
				// service auto-resolves instead, which the mock records as a no-op).
				if len(alerts) != 0 {
					t.Logf("threshold=%d days=%d: got %d alerts; want 0 (days > threshold sends no new expiry alert)", threshold, days, len(alerts))
					return false
				}
				return true
			}
		},
		gen.IntRange(1, 60),    // global expiry threshold (days), always > 0
		gen.IntRange(-60, 120), // remaining days, spanning all three tiers
	))

	properties.TestingRun(t)
}

// ---------------------------------------------------------------------------
// Property 9: 续费后自动消解告警
//            (renewal auto-resolves the previously raised expiry alerts)
// ---------------------------------------------------------------------------

// TestProperty9_RenewalAutoResolvesExpiryAlerts verifies Property 9.
//
// For ANY root domain that was PREVIOUSLY in an expiry-alerting state (either
// already-expired -> domain_expired, or within-threshold -> domain_expiring),
// once a fresh WHOIS lookup reports a new expiry date whose remaining days become
// GREATER than the global Expiry Threshold (e.g. after a renewal), a subsequent
// RefreshOne must auto-resolve BOTH registration-expiry alert types
// (domain_expiring AND domain_expired) for this root domain's target
// (target_type "root_domain", the seeded id). This is requirement 5.5: when
// Days Remaining rises back above the threshold, the previously raised expiry
// alerts are automatically resolved.
//
// Setup per iteration (FRESH env / in-memory DB, so a fixed registrable domain is
// trivially unique and the mockAlertSender's recordings never leak across
// iterations):
//   - Set the global ExpiryThresholdDays on the runtime config to the generated
//     threshold (always > 0, so evaluateAlerts uses it verbatim — no fallback to 14).
//   - Seed one root domain via repo.Create (bypassing svc.Create's best-effort
//     RefreshOne, so the two explicit RefreshOne calls below are the only
//     alert-evaluating queries).
//   - First refresh: orchestrate the WHOIS mock to succeed with an expiry that
//     yields EXACTLY `initialDays` remaining, where initialDays = threshold -
//     belowThreshold <= threshold. This lands in an ALERTING tier (expired when
//     initialDays <= 0, expiring when 0 < initialDays <= threshold), driving the
//     domain into the "previously alerting" precondition. A sanity check (reusing
//     prop8AlertMatches) asserts exactly the expected alert was recorded.
//   - Renewal refresh: re-orchestrate the WHOIS mock to succeed with an expiry
//     yielding renewedDays = threshold + aboveThreshold > threshold remaining
//     (aboveThreshold >= 1), then RefreshOne again. days > threshold takes the
//     evaluateAlerts default branch, which auto-resolves both expiry alert types.
//
// Assertions (against the mockAlertSender's recorded AutoResolve calls, kept in
// .resolved separately from SendAlert's .alerts):
//   - both domain_expiring AND domain_expired were auto-resolved for
//     (target_type "root_domain", target_id = the seeded id).
//
// prop8DaysToExpiry is reused to convert a target remaining-days value into a
// robust expiry instant (offset half a day off the integer boundary so
// truncation is drift-proof). WHOIS and Cloudflare are mocked (no real network).
// gopter >=100 iterations.
//
// Feature: domain-expiry-monitor, Property 9: 续费后自动消解告警
//
// **Validates: Requirements 5.5**
func TestProperty9_RenewalAutoResolvesExpiryAlerts(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(9) // deterministic

	properties := gopter.NewProperties(parameters)

	properties.Property("a renewal that lifts days above the threshold auto-resolves both expiry alert types", prop.ForAll(
		func(threshold, belowThreshold, aboveThreshold int) bool {
			env := newTestDomainExpiryService(t)
			ctx := context.Background()

			// Set the global expiry threshold (> 0, used verbatim by evaluateAlerts).
			cfg := env.cfg.Get()
			cfg.DomainExpiry.ExpiryThresholdDays = threshold
			env.cfg.Update(cfg)

			// Derive the two remaining-days targets from the threshold:
			//   initialDays = threshold - belowThreshold  (<= threshold -> alerting tier)
			//   renewedDays = threshold + aboveThreshold   (>  threshold -> healthy/renewed)
			initialDays := threshold - belowThreshold
			renewedDays := threshold + aboveThreshold

			// Seed a single root domain directly via the repository so the only
			// alert-evaluating queries are the two explicit RefreshOne calls below.
			const reg = "example.com"
			rd := &model.RootDomain{
				Name:              reg,
				Source:            "manual",
				RegistrableDomain: reg,
				MonitorEnabled:    true,
			}
			if err := env.repo.Create(ctx, rd); err != nil {
				t.Logf("seed repo.Create error: %v", err)
				return false
			}

			// --- First refresh: drive the domain into an alerting state. ---
			env.whois.setSuccess(reg, prop8DaysToExpiry(initialDays))
			if _, err := env.svc.RefreshOne(ctx, rd.ID); err != nil {
				t.Logf("threshold=%d initialDays=%d: first RefreshOne error: %v", threshold, initialDays, err)
				return false
			}

			// Sanity-check the precondition: the first refresh raised EXACTLY the
			// expected expiry alert (mirrors Property 8's tiering). This confirms the
			// domain was "previously in an expiry-alerting state" before renewal.
			if len(env.alerter.alerts) != 1 {
				t.Logf("threshold=%d initialDays=%d: got %d alerts after first refresh; want exactly 1 (alerting precondition)", threshold, initialDays, len(env.alerter.alerts))
				return false
			}
			if initialDays <= 0 {
				if !prop8AlertMatches(t, env.alerter.alerts[0], "critical", "domain_expired", rd.ID, threshold, initialDays) {
					return false
				}
			} else {
				if !prop8AlertMatches(t, env.alerter.alerts[0], "warning", "domain_expiring", rd.ID, threshold, initialDays) {
					return false
				}
			}

			// --- Renewal refresh: new expiry lifts days back above the threshold. ---
			env.whois.setSuccess(reg, prop8DaysToExpiry(renewedDays))
			if _, err := env.svc.RefreshOne(ctx, rd.ID); err != nil {
				t.Logf("threshold=%d renewedDays=%d: renewal RefreshOne error: %v", threshold, renewedDays, err)
				return false
			}

			// Both expiry alert types must have been auto-resolved for this root
			// domain's target (requirement 5.5). AutoResolve recordings live in
			// .resolved; the first (alerting) refresh records none, so both entries
			// come from the renewal refresh's default branch.
			resolvedExpiring := false
			resolvedExpired := false
			for _, r := range env.alerter.resolved {
				if r.TargetType != alertTargetTypeRootDomain || r.TargetID != rd.ID {
					continue
				}
				switch r.AlertType {
				case AlertTypeDomainExpiring:
					resolvedExpiring = true
				case AlertTypeDomainExpired:
					resolvedExpired = true
				}
			}
			if !resolvedExpiring {
				t.Logf("threshold=%d initialDays=%d renewedDays=%d: %q was NOT auto-resolved for (root_domain, %s); resolved=%+v",
					threshold, initialDays, renewedDays, AlertTypeDomainExpiring, rd.ID, env.alerter.resolved)
				return false
			}
			if !resolvedExpired {
				t.Logf("threshold=%d initialDays=%d renewedDays=%d: %q was NOT auto-resolved for (root_domain, %s); resolved=%+v",
					threshold, initialDays, renewedDays, AlertTypeDomainExpired, rd.ID, env.alerter.resolved)
				return false
			}

			return true
		},
		gen.IntRange(1, 60), // global expiry threshold (days), always > 0
		gen.IntRange(0, 80), // belowThreshold: initialDays = threshold - it (<= threshold, spans expiring & expired)
		gen.IntRange(1, 60), // aboveThreshold: renewedDays = threshold + it (> threshold, renewed/healthy)
	))

	properties.TestingRun(t)
}

// ---------------------------------------------------------------------------
// Property 10: 忽略告警抑制发送
//            (alert_ignored suppresses registration-expiry alert sending)
// ---------------------------------------------------------------------------

// TestProperty10_AlertIgnoredSuppressesExpiryAlerts verifies Property 10.
//
// For ANY expiry status — already expired, within the threshold (expiring), or
// healthy/renewed — when a root domain's alert_ignored flag is true, a successful
// WHOIS refresh must NOT emit ANY registration-expiry alert (neither the critical
// domain_expired nor the warning domain_expiring) for that root domain
// (requirement 5.6). evaluateAlerts short-circuits on rd.AlertIgnored BEFORE any
// tiering, so the suppression holds across every tier — including the expired and
// expiring tiers that would otherwise send an alert (cf. Property 8).
//
// Setup per iteration (FRESH env / in-memory DB, so a fixed registrable domain is
// trivially unique and the mockAlertSender's recordings never leak across
// iterations):
//   - Seed one root domain via repo.Create with AlertIgnored=true and
//     MonitorEnabled=true, bypassing svc.Create's best-effort RefreshOne so the
//     explicit RefreshOne below is the ONLY alert-evaluating query, over a clean
//     alert log. The repository persists alert_ignored (boolToInt) and reads it
//     back, so the re-read record evaluateAlerts sees genuinely carries
//     AlertIgnored=true.
//   - Orchestrate the WHOIS mock to succeed with an expiry that realizes EXACTLY
//     `days` remaining (prop8DaysToExpiry, offset half a day off the integer
//     boundary so truncation is drift-proof). The generated `days` span all three
//     tiers (expired <= 0, expiring 1..14, healthy > 14) via gen.IntRange(-60, 120)
//     against the default global threshold of 14, so the property is exercised in
//     every tier.
//   - Call RefreshOne (persists the expiry, then evaluates alerts — which must
//     short-circuit on alert_ignored and send nothing).
//
// Assertion (against the mockAlertSender's recorded SendAlert calls in .alerts):
//   - ZERO registration-expiry alerts were sent: no recorded alert has AlertType
//     domain_expiring or domain_expired. Because evaluateAlerts returns before any
//     tiering when alert_ignored is true, no SendAlert is ever invoked for the
//     ignored domain in any tier.
//
// WHOIS and Cloudflare are mocked (no real network). gopter ≥100 iterations.
//
// Feature: domain-expiry-monitor, Property 10: 忽略告警抑制发送
//
// **Validates: Requirements 5.6**
func TestProperty10_AlertIgnoredSuppressesExpiryAlerts(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(10) // deterministic

	properties := gopter.NewProperties(parameters)

	properties.Property("an alert_ignored root domain emits no registration-expiry alert in any tier", prop.ForAll(
		func(days int) bool {
			env := newTestDomainExpiryService(t)
			ctx := context.Background()

			// Seed a single root domain with alert_ignored=true directly via the
			// repository so the only alert-evaluating query is the explicit
			// RefreshOne below. The default config threshold is 14, so `days` spans
			// expired (<=0), expiring (1..14) and healthy (>14) tiers.
			const reg = "example.com"
			rd := &model.RootDomain{
				Name:              reg,
				Source:            "manual",
				RegistrableDomain: reg,
				MonitorEnabled:    true,
				AlertIgnored:      true,
			}
			if err := env.repo.Create(ctx, rd); err != nil {
				t.Logf("seed repo.Create error: %v", err)
				return false
			}

			// Orchestrate a successful WHOIS lookup returning an expiry that yields
			// EXACTLY `days` remaining, then refresh (persists the expiry and fires
			// evaluateAlerts — which must short-circuit on alert_ignored).
			env.whois.setSuccess(reg, prop8DaysToExpiry(days))
			if _, err := env.svc.RefreshOne(ctx, rd.ID); err != nil {
				t.Logf("days=%d: RefreshOne error: %v", days, err)
				return false
			}

			// Requirement 5.6: no registration-expiry alert may be sent for an
			// ignored root domain, regardless of tier.
			for _, a := range env.alerter.alerts {
				if a.AlertType == AlertTypeDomainExpiring || a.AlertType == AlertTypeDomainExpired {
					t.Logf("days=%d: registration-expiry alert emitted for ignored domain: level=%q type=%q target=(%s,%s); want none",
						days, a.Level, a.AlertType, a.TargetType, a.TargetID)
					return false
				}
			}

			return true
		},
		gen.IntRange(-60, 120), // remaining days, spanning expired / expiring / healthy tiers
	))

	properties.TestingRun(t)
}

// ---------------------------------------------------------------------------
// Property 11: 周期刷新遍历启用项、跳过禁用项并在失败时继续
//            (periodic refresh traverses enabled items, skips disabled ones,
//             and continues after individual failures)
// ---------------------------------------------------------------------------

// prop11KnownExpiry is a fixed, whole-second UTC instant used to pre-seed an
// ENABLED root domain's previously-known expiry (via SaveExpiryResult with a
// "success" status) BEFORE RefreshAll runs. It is far in the future (well beyond
// any test threshold) so a successful re-evaluation never sends an alert, and its
// whole-second value round-trips exactly through the repo's RFC3339 storage — so
// the "failure preserves the known expiry" assertion can compare instants.
var prop11KnownExpiry = time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)

// prop11FreshExpiry is the fixed, whole-second UTC instant a SUCCESSFUL WHOIS
// lookup returns during RefreshAll. It differs from prop11KnownExpiry so that an
// enabled+success domain's expiry is observably UPDATED (to this value), while an
// enabled+failure domain's expiry is observably PRESERVED (at prop11KnownExpiry).
// It is likewise far in the future, so no alert fires during evaluation.
var prop11FreshExpiry = time.Date(2040, 6, 15, 0, 0, 0, 0, time.UTC)

// TestProperty11_RefreshAllTraversesEnabledSkipsDisabledContinuesOnFailure
// verifies Property 11.
//
// For ANY set of root domains with an arbitrary enabled/disabled flag per domain
// AND an arbitrary per-item success/failure WHOIS outcome, one periodic refresh
// (RefreshAll) must:
//
//   - query EXACTLY the enabled domains — every enabled domain is queried once,
//     and no disabled domain is ever queried (requirements 6.1 / 6.4 / 6.6);
//   - update every enabled+success domain (expiry set to the WHOIS value,
//     last_status "success") and mark every enabled+failure domain
//     (last_status "failed", non-empty last_error, previously-known expiry
//     preserved) (requirement 6.4);
//   - NOT abort early on an individual failure — every enabled domain, including
//     those ordered AFTER a failing one, is still processed to completion
//     (requirement 6.4);
//   - leave disabled domains untouched — never queried, last_status stays "",
//     expiry/last_checked_at stay nil (requirement 6.6).
//
// Per-domain wiring is encoded in a single generated int per domain (2 bits):
// bit 0 = enabled, bit 1 = success. The four codes cover enabled+success (3),
// enabled+failure (1) and disabled (0, 2 — the success bit is irrelevant when
// disabled). Over 100+ iterations the random slices interleave failures and
// successes in every order, so the "continue after failure" guarantee is
// exercised with failing domains sitting before later enabled domains.
//
// Setup per iteration (FRESH env / in-memory DB, so a fixed set of registrable
// domains is trivially unique and the mock's query log never leaks across
// iterations):
//   - Distinct registrable domains are built with property3SeedName(i) (reused
//     from Property 3), so each seeded record's registrable_domain — the WHOIS
//     query key and the unique index — is distinct. N is capped to the label
//     pool so every index yields a distinct label.
//   - Each domain is seeded via repo.Create with MonitorEnabled per its flag,
//     bypassing svc.Create's best-effort RefreshOne so RefreshAll is the ONLY
//     source of WHOIS queries and the mock's query log reflects it exactly.
//   - ENABLED domains additionally get a KNOWN prior expiry (prop11KnownExpiry)
//     via SaveExpiryResult(..., "success", ...) so the failure-preservation
//     assertion is meaningful; DISABLED domains are left fresh (last_status "").
//   - The WHOIS mock is orchestrated per domain: enabled+success -> setSuccess
//     with prop11FreshExpiry (distinct from the seeded known expiry, so "updated"
//     is observable); enabled+failure -> setError with a failure class cycled
//     from property7WhoisFailures (reused). Disabled domains are also armed with
//     an error so that IF one were wrongly queried its last_status would flip to
//     "failed" — belt-and-suspenders on top of the direct wasQueried check.
//
// Assertions: a global check that the total WHOIS query count equals the number
// of enabled domains and that every queried domain is in the enabled set (exactly
// the enabled items, nothing else), plus per-domain checks (against a fresh
// repo.GetByID read-back) for each of the three cases above.
//
// The polite inter-query backoff is disabled package-wide (whoisRefreshBackoff=0,
// set in domain_expiry_testhelper_test.go's init) so this multi-domain property
// runs fast. WHOIS and Cloudflare are mocked (no real network). gopter ≥100
// iterations.
//
// Feature: domain-expiry-monitor, Property 11: 周期刷新遍历启用项、跳过禁用项并在失败时继续
//
// **Validates: Requirements 6.1, 6.4, 6.6**
func TestProperty11_RefreshAllTraversesEnabledSkipsDisabledContinuesOnFailure(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(11) // deterministic

	properties := gopter.NewProperties(parameters)

	properties.Property("RefreshAll queries exactly the enabled domains, updates successes, marks failures, skips disabled, and never aborts early", prop.ForAll(
		func(codes []int) bool {
			// Bound N to the label pool so property3SeedName yields a distinct
			// registrable domain per index. Keeps each of the 100+ fresh-DB
			// iterations fast.
			if maxN := len(property3LabelPool); len(codes) > maxN {
				codes = codes[:maxN]
			}

			env := newTestDomainExpiryService(t)
			ctx := context.Background()

			// Per-domain plan so assertions can consult the intended wiring by id
			// and registrable domain.
			type domainPlan struct {
				id      string
				reg     string
				enabled bool
				success bool
			}
			plans := make([]domainPlan, 0, len(codes))
			enabledRegs := make(map[string]struct{})

			for i, code := range codes {
				reg := property3SeedName(i)
				enabled := code&1 != 0
				success := code&2 != 0

				// Seed the record with MonitorEnabled per the enabled flag.
				rd := &model.RootDomain{
					Name:              reg,
					Source:            "manual",
					RegistrableDomain: reg,
					MonitorEnabled:    enabled,
				}
				if err := env.repo.Create(ctx, rd); err != nil {
					t.Logf("seed repo.Create(%q) error: %v", reg, err)
					return false
				}

				if enabled {
					enabledRegs[reg] = struct{}{}

					// Establish a KNOWN prior expiry (simulated prior success) so the
					// failure-preservation assertion is meaningful. Disabled domains
					// are deliberately left fresh (last_status ""), matching
					// requirement 6.6's "skipped -> untouched".
					prior := prop11KnownExpiry
					priorCheck := time.Now().UTC().Add(-24 * time.Hour)
					if err := env.repo.SaveExpiryResult(ctx, rd.ID, &prior, priorCheck, "success", ""); err != nil {
						t.Logf("seed SaveExpiryResult(%q) error: %v", reg, err)
						return false
					}

					// Orchestrate the WHOIS outcome for this enabled domain.
					if success {
						env.whois.setSuccess(reg, prop11FreshExpiry)
					} else {
						// Vary the failure class across failing domains for coverage.
						env.whois.setError(reg, property7WhoisFailures[i%len(property7WhoisFailures)])
					}
				} else {
					// Disabled: arm an error so that IF RefreshAll wrongly queried it
					// its last_status would flip to "failed" (caught by the untouched
					// assertions below) — on top of the direct wasQueried check.
					env.whois.setError(reg, ErrWhoisQuery)
				}

				plans = append(plans, domainPlan{id: rd.ID, reg: reg, enabled: enabled, success: success})
			}

			// Run one periodic refresh over all enabled domains. RefreshAll only
			// returns a Go error if the initial ListEnabled fails; per-domain WHOIS
			// failures are folded into records and never bubble up.
			if err := env.svc.RefreshAll(ctx); err != nil {
				t.Logf("RefreshAll returned an error: %v", err)
				return false
			}

			// Global: WHOIS was queried for EXACTLY the enabled set — none for any
			// disabled domain, and no domain queried more than once. The count
			// equality plus the membership check together pin "对且仅对启用项发起查询"
			// (requirements 6.1 / 6.4 / 6.6).
			if got, want := env.whois.totalQueries(), len(enabledRegs); got != want {
				t.Logf("total WHOIS queries = %d; want %d (exactly the enabled domains)", got, want)
				return false
			}
			for _, q := range env.whois.queriedDomains() {
				if _, ok := enabledRegs[q]; !ok {
					t.Logf("WHOIS queried a non-enabled domain %q", q)
					return false
				}
			}

			// Per-domain assertions against the persisted state (fresh read-back).
			for _, p := range plans {
				rd, err := env.repo.GetByID(ctx, p.id)
				if err != nil || rd == nil {
					t.Logf("GetByID(%q) error: %v", p.reg, err)
					return false
				}

				if !p.enabled {
					// Requirement 6.6: disabled domains are skipped entirely — never
					// queried and left untouched (fresh: no status/expiry/check time).
					if env.whois.wasQueried(p.reg) {
						t.Logf("disabled domain %q was queried; want skipped", p.reg)
						return false
					}
					if rd.LastStatus != "" {
						t.Logf("disabled domain %q last_status = %q; want \"\" (untouched)", p.reg, rd.LastStatus)
						return false
					}
					if rd.ExpiryDate != nil {
						t.Logf("disabled domain %q expiry_date = %v; want nil (untouched)", p.reg, rd.ExpiryDate)
						return false
					}
					if rd.LastCheckedAt != nil {
						t.Logf("disabled domain %q last_checked_at = %v; want nil (untouched)", p.reg, rd.LastCheckedAt)
						return false
					}
					continue
				}

				// Enabled: queried EXACTLY once (requirements 6.1 / 6.4) and its
				// check timestamp was set.
				if c := env.whois.queryCount(p.reg); c != 1 {
					t.Logf("enabled domain %q queried %d times; want exactly 1", p.reg, c)
					return false
				}
				if rd.LastCheckedAt == nil {
					t.Logf("enabled domain %q last_checked_at is nil; want it set after refresh", p.reg)
					return false
				}

				if p.success {
					// enabled+success: expiry UPDATED to the WHOIS value, status
					// "success", no error (requirement 6.4).
					if rd.LastStatus != "success" {
						t.Logf("enabled+success domain %q last_status = %q; want \"success\"", p.reg, rd.LastStatus)
						return false
					}
					if rd.ExpiryDate == nil || !rd.ExpiryDate.Equal(prop11FreshExpiry) {
						t.Logf("enabled+success domain %q expiry_date = %v; want updated to %s", p.reg, rd.ExpiryDate, prop11FreshExpiry)
						return false
					}
					if rd.LastError != "" {
						t.Logf("enabled+success domain %q last_error = %q; want empty", p.reg, rd.LastError)
						return false
					}
				} else {
					// enabled+failure: status "failed", error recorded, previously
					// known expiry PRESERVED. That this domain was processed at all
					// (regardless of where a failing domain sits in the batch) is the
					// "did not abort early" guarantee (requirement 6.4).
					if rd.LastStatus != "failed" {
						t.Logf("enabled+failure domain %q last_status = %q; want \"failed\"", p.reg, rd.LastStatus)
						return false
					}
					if rd.LastError == "" {
						t.Logf("enabled+failure domain %q last_error is empty; want a non-empty description", p.reg)
						return false
					}
					if rd.ExpiryDate == nil || !rd.ExpiryDate.Equal(prop11KnownExpiry) {
						t.Logf("enabled+failure domain %q expiry_date = %v; want preserved as %s", p.reg, rd.ExpiryDate, prop11KnownExpiry)
						return false
					}
				}
			}

			return true
		},
		// Each int encodes one domain's wiring (bit 0 = enabled, bit 1 = success).
		gen.SliceOf(gen.IntRange(0, 3)),
	))

	properties.TestingRun(t)
}

// ---------------------------------------------------------------------------
// Property 12: 剩余天数计算与未知态
//            (days-remaining computation & unknown state)
// ---------------------------------------------------------------------------

// prop12MaxDays bounds the generated target remaining-day counts to +/-4000 days
// (~11 years), spanning the already-expired (negative), boundary (zero) and
// far-future (positive) tiers while staying comfortably inside time.Duration's
// ~292-year range (so expiry.Sub(now) never overflows). Combined with
// prop8DaysToExpiry's half-day boundary offset, every generated day count yields
// a drift-proof, deterministic expected days_remaining.
const prop12MaxDays = 4000

// TestProperty12_DaysRemainingComputationAndUnknownState verifies Property 12.
//
// Two complementary sub-properties, each exercised through BOTH service read
// paths that compute the non-persistent days_remaining field — svc.GetByID and
// svc.ListWithSort (both call the shared computeDaysRemaining):
//
//	known expiry (requirement 8.2): for ANY expiry date, days_remaining equals the
//	    whole number of days between the expiry and the current time, truncated
//	    toward zero. The expiry is built from a target integer day-count via
//	    prop8DaysToExpiry (reused from Property 8), which offsets the instant half
//	    a day off the nearest whole-day boundary in the sign direction of the
//	    target; int() truncation then deterministically yields exactly that target
//	    regardless of the sub-second clock drift between the test's "now" and the
//	    service's "now" (and the repo's RFC3339 second-precision truncation on
//	    store — both utterly dwarfed by the 12h offset). The expiry is supplied in
//	    an arbitrary time zone to confirm the day count depends only on the instant,
//	    not on the display zone (the repo stores it as UTC either way).
//
//	unknown state (requirement 8.3): for a root domain with NO successful WHOIS
//	    result — whether never checked, or checked but FAILED (which preserves a
//	    nil expiry) — both expiry_date and days_remaining read back as nil
//	    ("unknown").
//
// Setup per iteration uses a FRESH env (fresh in-memory DB), and seeds the root
// domain directly via the repository (repo.Create [+ SaveExpiryResult]) so no
// WHOIS query is ever issued — the read-time days_remaining computation is
// exercised in isolation. A fresh DB per iteration makes a fixed registrable
// domain trivially unique and keeps the single-row ListWithSort assertion exact.
//
// WHOIS and Cloudflare are mocked (and here never even invoked — no real
// network). gopter >=100 iterations per sub-property.
//
// Feature: domain-expiry-monitor, Property 12: 剩余天数计算与未知态
//
// **Validates: Requirements 8.2, 8.3**
func TestProperty12_DaysRemainingComputationAndUnknownState(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(12) // deterministic

	properties := gopter.NewProperties(parameters)

	// --- Sub-property A: known expiry -> truncated-toward-zero day count (req 8.2) ---
	properties.Property("days_remaining equals the truncated-toward-zero day count for a known expiry", prop.ForAll(
		func(days, offsetHours int) bool {
			env := newTestDomainExpiryService(t)
			ctx := context.Background()

			// Seed a single root domain directly via the repository (no svc.Create,
			// so its best-effort RefreshOne never fires and no WHOIS query is made).
			const reg = "example.com"
			rd := &model.RootDomain{
				Name:              reg,
				Source:            "manual",
				RegistrableDomain: reg,
				MonitorEnabled:    true,
			}
			if err := env.repo.Create(ctx, rd); err != nil {
				t.Logf("seed repo.Create error: %v", err)
				return false
			}

			// Build an expiry that yields EXACTLY `days` remaining (half-day offset
			// off the whole-day boundary => truncation is drift-proof), displayed in
			// an arbitrary zone (the instant, and hence the day count, is
			// zone-independent; the repo stores it as UTC regardless). Persist it as
			// a simulated successful check so expiry_date is populated.
			loc := time.FixedZone("prop12zone", offsetHours*3600)
			expiry := prop8DaysToExpiry(days).In(loc)
			checkedAt := time.Now().UTC()
			if err := env.repo.SaveExpiryResult(ctx, rd.ID, &expiry, checkedAt, "success", ""); err != nil {
				t.Logf("seed SaveExpiryResult error: %v", err)
				return false
			}

			// Read back through BOTH service read paths that compute days_remaining.
			got, err := env.svc.GetByID(ctx, rd.ID)
			if err != nil {
				t.Logf("GetByID error: %v", err)
				return false
			}
			items, total, err := env.svc.ListWithSort(ctx, model.RootDomainListParams{})
			if err != nil {
				t.Logf("ListWithSort error: %v", err)
				return false
			}
			// Fresh single-row DB: the list must contain exactly the one seeded row.
			if total != 1 || len(items) != 1 {
				t.Logf("ListWithSort returned total=%d len=%d; want exactly 1", total, len(items))
				return false
			}

			for _, c := range []struct {
				label string
				got   *model.RootDomain
			}{
				{"GetByID", got},
				{"ListWithSort item", items[0]},
			} {
				g := c.got
				// A known expiry must yield a non-nil expiry and a non-nil computed
				// days_remaining equal to the intended truncated-toward-zero count.
				if g.ExpiryDate == nil {
					t.Logf("%s: expiry_date is nil; want a known expiry (days=%d)", c.label, days)
					return false
				}
				if g.DaysRemaining == nil {
					t.Logf("%s: days_remaining is nil; want %d for a known expiry", c.label, days)
					return false
				}
				if *g.DaysRemaining != days {
					t.Logf("%s: days_remaining = %d; want %d (expiry=%s)", c.label, *g.DaysRemaining, days, g.ExpiryDate)
					return false
				}
			}

			return true
		},
		gen.IntRange(-prop12MaxDays, prop12MaxDays), // target remaining days: expired / boundary / future
		gen.IntRange(-12, 14),                       // arbitrary display-zone offset (hours)
	))

	// --- Sub-property B: unknown expiry -> days_remaining nil (req 8.3) ---
	properties.Property("days_remaining is nil (unknown) whenever the expiry date is unknown", prop.ForAll(
		func(failedCheck bool) bool {
			env := newTestDomainExpiryService(t)
			ctx := context.Background()

			// Seed a single root domain with NO successful WHOIS result: either
			// never checked, or checked but FAILED (which preserves a nil expiry).
			const reg = "example.com"
			rd := &model.RootDomain{
				Name:              reg,
				Source:            "manual",
				RegistrableDomain: reg,
				MonitorEnabled:    true,
			}
			if err := env.repo.Create(ctx, rd); err != nil {
				t.Logf("seed repo.Create error: %v", err)
				return false
			}
			if failedCheck {
				// A failed check writes last_status/last_error but leaves expiry_date
				// NULL (nil expiry arg), so the expiry stays unknown (req 4.5/4.6/7.2).
				if err := env.repo.SaveExpiryResult(ctx, rd.ID, nil, time.Now().UTC(), "failed", "whois failed"); err != nil {
					t.Logf("seed SaveExpiryResult(failed) error: %v", err)
					return false
				}
			}

			// Read back through BOTH service read paths that compute days_remaining.
			got, err := env.svc.GetByID(ctx, rd.ID)
			if err != nil {
				t.Logf("GetByID error: %v", err)
				return false
			}
			items, total, err := env.svc.ListWithSort(ctx, model.RootDomainListParams{})
			if err != nil {
				t.Logf("ListWithSort error: %v", err)
				return false
			}
			// Fresh single-row DB: the list must contain exactly the one seeded row.
			if total != 1 || len(items) != 1 {
				t.Logf("ListWithSort returned total=%d len=%d; want exactly 1", total, len(items))
				return false
			}

			for _, c := range []struct {
				label string
				got   *model.RootDomain
			}{
				{"GetByID", got},
				{"ListWithSort item", items[0]},
			} {
				g := c.got
				// Requirement 8.3: unknown expiry -> both expiry_date and
				// days_remaining are nil ("unknown").
				if g.ExpiryDate != nil {
					t.Logf("%s: expiry_date = %s; want nil (unknown, failedCheck=%v)", c.label, g.ExpiryDate, failedCheck)
					return false
				}
				if g.DaysRemaining != nil {
					t.Logf("%s: days_remaining = %d; want nil (unknown, failedCheck=%v)", c.label, *g.DaysRemaining, failedCheck)
					return false
				}
			}

			return true
		},
		gen.Bool(),
	))

	properties.TestingRun(t)
}
