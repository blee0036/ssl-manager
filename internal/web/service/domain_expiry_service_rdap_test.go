package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/model"
)

// This file holds the service-layer tests for the WHOIS -> RDAP fallback wired
// into DomainExpiryService.RefreshOne. It reuses the shared testDomainExpiryEnv
// (domain_expiry_testhelper_test.go), whose injected mock RDAP client defaults to
// FAILING, so these tests never touch the network. Each test seeds its root domain
// directly via the repository (bypassing svc.Create's best-effort RefreshOne) so
// the explicit RefreshOne below is the only lookup.

// seedRootDomainForRDAPTest inserts a single enabled manual root domain and
// returns it. Fresh in-memory DB per env makes the fixed registrable domain
// trivially unique.
func seedRootDomainForRDAPTest(t *testing.T, ctx context.Context, env *testDomainExpiryEnv, reg string) *model.RootDomain {
	t.Helper()
	rd := &model.RootDomain{
		Name:              reg,
		Source:            "manual",
		RegistrableDomain: reg,
		MonitorEnabled:    true,
	}
	if err := env.repo.Create(ctx, rd); err != nil {
		t.Fatalf("seed repo.Create(%q) error: %v", reg, err)
	}
	return rd
}

// TestRefreshOne_WhoisFailsRDAPSucceeds is the core .app-style regression guard:
// when the PRIMARY WHOIS lookup errors (as it does for a gTLD like .app with no
// legacy WHOIS server), the RDAP FALLBACK is consulted, and on RDAP success its
// expiry is recorded as a successful check.
func TestRefreshOne_WhoisFailsRDAPSucceeds(t *testing.T) {
	env := newTestDomainExpiryService(t)
	ctx := context.Background()

	const reg = "iterflor.app"
	rd := seedRootDomainForRDAPTest(t, ctx, env, reg)

	// WHOIS fails (no legacy server), RDAP succeeds with a far-future expiry (so
	// no alert noise).
	wantExpiry := time.Date(2030, 8, 1, 4, 0, 0, 0, time.UTC)
	env.whois.setError(reg, ErrWhoisQuery)
	env.rdap.setSuccess(reg, wantExpiry)

	refreshed, err := env.svc.RefreshOne(ctx, rd.ID)
	if err != nil {
		t.Fatalf("RefreshOne returned a Go error: %v", err)
	}

	// The RDAP fallback must have been consulted exactly once.
	if !env.rdap.wasQueried(reg) {
		t.Errorf("RDAP fallback was not queried; want it consulted after WHOIS failure")
	}

	// Assert both the returned record and the persisted read-back.
	persisted, gerr := env.repo.GetByID(ctx, rd.ID)
	if gerr != nil {
		t.Fatalf("GetByID read-back error: %v", gerr)
	}
	for _, c := range []struct {
		label string
		got   *model.RootDomain
	}{
		{"RefreshOne result", refreshed},
		{"GetByID read-back", persisted},
	} {
		g := c.got
		if g.LastStatus != "success" {
			t.Errorf("%s: last_status = %q; want \"success\"", c.label, g.LastStatus)
		}
		if g.LastError != "" {
			t.Errorf("%s: last_error = %q; want empty", c.label, g.LastError)
		}
		if g.ExpiryDate == nil {
			t.Errorf("%s: expiry_date is nil; want %s", c.label, wantExpiry)
			continue
		}
		if !g.ExpiryDate.Equal(wantExpiry) {
			t.Errorf("%s: expiry_date = %s; want instant-equal to %s", c.label, g.ExpiryDate, wantExpiry)
		}
		if g.ExpiryDate.Location().String() != "UTC" {
			t.Errorf("%s: expiry_date location = %s; want UTC", c.label, g.ExpiryDate.Location())
		}
	}
}

// TestRefreshOne_WhoisFailsRDAPSucceedsNormalizesToUTC confirms the fallback path
// normalizes a non-UTC RDAP expiry to UTC (matching the WHOIS-success behavior).
func TestRefreshOne_WhoisFailsRDAPSucceedsNormalizesToUTC(t *testing.T) {
	env := newTestDomainExpiryService(t)
	ctx := context.Background()

	const reg = "iterflor.app"
	rd := seedRootDomainForRDAPTest(t, ctx, env, reg)

	// RDAP returns an expiry displayed in a non-UTC zone; the service must store
	// the instant in UTC.
	loc := time.FixedZone("test+8", 8*3600)
	rdapExpiry := time.Date(2031, 1, 15, 0, 0, 0, 0, loc)
	env.whois.setError(reg, ErrWhoisQuery)
	env.rdap.setSuccess(reg, rdapExpiry)

	refreshed, err := env.svc.RefreshOne(ctx, rd.ID)
	if err != nil {
		t.Fatalf("RefreshOne returned a Go error: %v", err)
	}
	if refreshed.ExpiryDate == nil {
		t.Fatalf("expiry_date is nil; want the RDAP expiry recorded")
	}
	if !refreshed.ExpiryDate.Equal(rdapExpiry) {
		t.Errorf("expiry_date = %s; want instant-equal to %s", refreshed.ExpiryDate, rdapExpiry)
	}
	if refreshed.ExpiryDate.Location().String() != "UTC" {
		t.Errorf("expiry_date location = %s; want UTC", refreshed.ExpiryDate.Location())
	}
}

// TestRefreshOne_WhoisFailsRDAPFails verifies that when BOTH the WHOIS primary
// and the RDAP fallback fail, the check is recorded as failed with a combined
// error naming both layers, the previously known expiry is preserved, and no Go
// error bubbles up.
func TestRefreshOne_WhoisFailsRDAPFails(t *testing.T) {
	env := newTestDomainExpiryService(t)
	ctx := context.Background()

	const reg = "iterflor.app"
	rd := seedRootDomainForRDAPTest(t, ctx, env, reg)

	// Establish a KNOWN prior expiry via a simulated successful check.
	known := time.Date(2029, 5, 6, 7, 0, 0, 0, time.UTC)
	priorCheck := time.Now().UTC().Add(-24 * time.Hour)
	if err := env.repo.SaveExpiryResult(ctx, rd.ID, &known, priorCheck, "success", ""); err != nil {
		t.Fatalf("seed SaveExpiryResult error: %v", err)
	}

	// Both layers fail.
	env.whois.setError(reg, ErrWhoisQuery)
	env.rdap.setError(reg, ErrRDAPNoServer)

	refreshed, err := env.svc.RefreshOne(ctx, rd.ID)
	if err != nil {
		t.Fatalf("RefreshOne returned a Go error for a lookup failure: %v", err)
	}
	if !env.rdap.wasQueried(reg) {
		t.Errorf("RDAP fallback was not queried after WHOIS failure")
	}

	persisted, gerr := env.repo.GetByID(ctx, rd.ID)
	if gerr != nil {
		t.Fatalf("GetByID read-back error: %v", gerr)
	}
	for _, c := range []struct {
		label string
		got   *model.RootDomain
	}{
		{"RefreshOne result", refreshed},
		{"GetByID read-back", persisted},
	} {
		g := c.got
		if g.LastStatus != "failed" {
			t.Errorf("%s: last_status = %q; want \"failed\"", c.label, g.LastStatus)
		}
		// The combined error must name BOTH layers.
		if !strings.Contains(g.LastError, "whois") || !strings.Contains(g.LastError, "rdap") {
			t.Errorf("%s: last_error = %q; want it to mention both \"whois\" and \"rdap\"", c.label, g.LastError)
		}
		// The previously known expiry must be preserved unchanged.
		if g.ExpiryDate == nil {
			t.Errorf("%s: expiry_date is nil; want it preserved as %s", c.label, known)
			continue
		}
		if !g.ExpiryDate.Equal(known) {
			t.Errorf("%s: expiry_date = %s; want it preserved (instant-equal to %s)", c.label, g.ExpiryDate, known)
		}
	}
}

// TestRefreshOne_WhoisSucceedsRDAPNotConsulted verifies behavior is unchanged for
// working TLDs: when WHOIS succeeds, the RDAP fallback is never consulted.
func TestRefreshOne_WhoisSucceedsRDAPNotConsulted(t *testing.T) {
	env := newTestDomainExpiryService(t)
	ctx := context.Background()

	const reg = "example.com"
	rd := seedRootDomainForRDAPTest(t, ctx, env, reg)

	wantExpiry := time.Date(2030, 12, 31, 23, 59, 59, 0, time.UTC)
	env.whois.setSuccess(reg, wantExpiry)

	refreshed, err := env.svc.RefreshOne(ctx, rd.ID)
	if err != nil {
		t.Fatalf("RefreshOne returned a Go error: %v", err)
	}

	// The RDAP fallback must NOT have been consulted at all.
	if n := env.rdap.totalQueries(); n != 0 {
		t.Errorf("RDAP fallback queried %d time(s); want 0 when WHOIS succeeds", n)
	}
	if refreshed.LastStatus != "success" {
		t.Errorf("last_status = %q; want \"success\"", refreshed.LastStatus)
	}
	if refreshed.ExpiryDate == nil || !refreshed.ExpiryDate.Equal(wantExpiry) {
		t.Errorf("expiry_date = %v; want instant-equal to %s (from WHOIS)", refreshed.ExpiryDate, wantExpiry)
	}
}
