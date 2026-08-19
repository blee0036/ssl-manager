package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/model"
)

// This file holds the unit tests for the MANUAL EXPIRY OVERRIDE feature: an
// operator can set a root domain's registration expiry_date by hand (via
// Update) for registries that are structurally unqueryable via WHOIS/RDAP
// (e.g. some .eu / .uy / third-level ccTLD delegations — see requirements.md
// "已知限制与后续增强"). Once expiry_source is "manual":
//
//   - the manually-set expiry_date participates in the EXISTING threshold/alert
//     evaluation unchanged (no new alerting logic);
//   - RefreshOne / RefreshAll skip the WHOIS/RDAP query entirely for that
//     domain, so periodic refresh never overwrites the manual date and never
//     wastes a network round-trip that can only fail;
//   - clearing the override (expiry_date: "") switches expiry_source back to
//     "whois" and restores normal periodic querying.
//
// The shared helpers (newTestDomainExpiryService / mockWhoisClient /
// mockAlertSender) come from domain_expiry_testhelper_test.go and are reused
// as-is.

// seedManualOverrideDomain creates a root domain directly via the repository
// (no WHOIS) with the given registrable domain, monitoring enabled, and no
// expiry info yet. Returns the persisted record's ID.
func seedManualOverrideDomain(t *testing.T, ctx context.Context, env *testDomainExpiryEnv, reg string) string {
	t.Helper()
	rd := &model.RootDomain{
		Name:              reg,
		Source:            "manual",
		RegistrableDomain: reg,
		MonitorEnabled:    true,
	}
	if err := env.repo.Create(ctx, rd); err != nil {
		t.Fatalf("seed Create failed: %v", err)
	}
	return rd.ID
}

// TestUpdate_SetManualExpiryDate_SwitchesSourceAndStatus verifies that setting a
// non-empty expiry_date via Update parses it, persists it as expiry_date,
// switches expiry_source to "manual", sets last_status="manual", and clears
// last_error.
func TestUpdate_SetManualExpiryDate_SwitchesSourceAndStatus(t *testing.T) {
	env := newTestDomainExpiryService(t)
	ctx := context.Background()

	id := seedManualOverrideDomain(t, ctx, env, "manual-eu-example.eu")

	future := time.Now().UTC().Add(200 * 24 * time.Hour).Truncate(time.Second)
	expiryStr := future.Format(time.RFC3339)

	rd, err := env.svc.Update(ctx, id, model.UpdateRootDomainInput{ExpiryDate: &expiryStr})
	if err != nil {
		t.Fatalf("Update returned unexpected error: %v", err)
	}

	if rd.ExpirySource != "manual" {
		t.Errorf("expiry_source = %q, want %q", rd.ExpirySource, "manual")
	}
	if rd.LastStatus != "manual" {
		t.Errorf("last_status = %q, want %q", rd.LastStatus, "manual")
	}
	if rd.LastError != "" {
		t.Errorf("last_error = %q, want empty", rd.LastError)
	}
	if rd.ExpiryDate == nil || !rd.ExpiryDate.Equal(future) {
		t.Errorf("expiry_date = %v, want %v", rd.ExpiryDate, future)
	}

	// Re-read from the repository directly to confirm persistence (not just the
	// in-memory returned struct).
	reloaded, err := env.repo.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if reloaded.ExpirySource != "manual" || reloaded.LastStatus != "manual" {
		t.Errorf("persisted record not updated: expiry_source=%q last_status=%q", reloaded.ExpirySource, reloaded.LastStatus)
	}
}

// TestUpdate_InvalidManualExpiryDate_RejectedAsValidationError verifies that a
// non-empty, non-RFC3339 expiry_date string is rejected with ErrValidation and
// does not mutate the record.
func TestUpdate_InvalidManualExpiryDate_RejectedAsValidationError(t *testing.T) {
	env := newTestDomainExpiryService(t)
	ctx := context.Background()

	id := seedManualOverrideDomain(t, ctx, env, "bad-date-example.eu")

	bad := "not-a-date"
	_, err := env.svc.Update(ctx, id, model.UpdateRootDomainInput{ExpiryDate: &bad})
	if err == nil {
		t.Fatal("expected an error for an invalid expiry_date string, got nil")
	}
	if !errors.Is(err, ErrValidation) {
		t.Errorf("expected error to wrap ErrValidation, got %v", err)
	}

	// The record must be unchanged (still expiry_source="whois", no expiry_date).
	rd, err := env.repo.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if rd.ExpirySource != "whois" {
		t.Errorf("expiry_source should be unchanged ('whois'), got %q", rd.ExpirySource)
	}
	if rd.ExpiryDate != nil {
		t.Errorf("expiry_date should remain nil, got %v", rd.ExpiryDate)
	}
}

// TestUpdate_ClearManualExpiryDate_RestoresWhoisSource verifies that passing an
// empty string ("") for expiry_date clears the manual override: expiry_source
// reverts to "whois", last_status resets to "", and the previously-set
// expiry_date value itself is left untouched (only cleared by a future
// successful WHOIS/RDAP query).
func TestUpdate_ClearManualExpiryDate_RestoresWhoisSource(t *testing.T) {
	env := newTestDomainExpiryService(t)
	ctx := context.Background()

	id := seedManualOverrideDomain(t, ctx, env, "clear-override-example.eu")

	future := time.Now().UTC().Add(100 * 24 * time.Hour).Truncate(time.Second)
	expiryStr := future.Format(time.RFC3339)
	if _, err := env.svc.Update(ctx, id, model.UpdateRootDomainInput{ExpiryDate: &expiryStr}); err != nil {
		t.Fatalf("initial manual set failed: %v", err)
	}

	empty := ""
	rd, err := env.svc.Update(ctx, id, model.UpdateRootDomainInput{ExpiryDate: &empty})
	if err != nil {
		t.Fatalf("clearing the override returned an unexpected error: %v", err)
	}

	if rd.ExpirySource != "whois" {
		t.Errorf("expiry_source = %q, want %q after clearing", rd.ExpirySource, "whois")
	}
	if rd.LastStatus != "" {
		t.Errorf("last_status = %q, want empty after clearing", rd.LastStatus)
	}
	// expiry_date itself is left untouched by the clear operation.
	if rd.ExpiryDate == nil || !rd.ExpiryDate.Equal(future) {
		t.Errorf("expiry_date should remain %v after clearing override, got %v", future, rd.ExpiryDate)
	}
}

// TestRefreshOne_ManualSource_SkipsWhoisAndRDAP verifies that RefreshOne, when
// invoked on a record whose expiry_source is "manual", never queries the (mock)
// WHOIS or RDAP clients at all, and returns the record unchanged (aside from
// DaysRemaining being recomputed).
func TestRefreshOne_ManualSource_SkipsWhoisAndRDAP(t *testing.T) {
	env := newTestDomainExpiryService(t)
	ctx := context.Background()

	id := seedManualOverrideDomain(t, ctx, env, "skip-refresh-example.eu")

	future := time.Now().UTC().Add(5 * 24 * time.Hour).Truncate(time.Second) // within default threshold (14d)
	expiryStr := future.Format(time.RFC3339)
	if _, err := env.svc.Update(ctx, id, model.UpdateRootDomainInput{ExpiryDate: &expiryStr}); err != nil {
		t.Fatalf("manual set failed: %v", err)
	}

	// Reset query-tracking so the RefreshOne call below is the only thing counted.
	env.whois.reset()

	rd, err := env.svc.RefreshOne(ctx, id)
	if err != nil {
		t.Fatalf("RefreshOne returned an unexpected error: %v", err)
	}

	if queried := env.whois.queriedDomains(); len(queried) != 0 {
		t.Errorf("expected WHOIS to NOT be queried for a manual-source domain, but it was queried for: %v", queried)
	}
	if rd.ExpirySource != "manual" {
		t.Errorf("expiry_source changed unexpectedly to %q", rd.ExpirySource)
	}
	if rd.LastStatus != "manual" {
		t.Errorf("last_status changed unexpectedly to %q", rd.LastStatus)
	}
	if rd.ExpiryDate == nil || !rd.ExpiryDate.Equal(future) {
		t.Errorf("expiry_date changed unexpectedly: got %v, want %v", rd.ExpiryDate, future)
	}
	if rd.DaysRemaining == nil {
		t.Error("expected DaysRemaining to be populated (computed at read time) even for a manual-source record")
	}

	// The threshold-based warning alert must still fire (manual dates participate
	// in the existing alert evaluation unchanged).
	found := false
	for _, a := range env.alerter.alerts {
		if a.AlertType == AlertTypeDomainExpiring && a.TargetID == id {
			found = true
		}
	}
	if !found {
		t.Error("expected a domain_expiring alert to be evaluated for the manual-source record within threshold")
	}
}

// TestRefreshAll_SkipsManualSourceDomains verifies that RefreshAll (the periodic
// scheduler entry point) skips WHOIS/RDAP queries for manual-source domains while
// still refreshing whois-source domains normally.
func TestRefreshAll_SkipsManualSourceDomains(t *testing.T) {
	env := newTestDomainExpiryService(t)
	ctx := context.Background()

	manualID := seedManualOverrideDomain(t, ctx, env, "manual-refreshall-example.eu")
	future := time.Now().UTC().Add(50 * 24 * time.Hour).Truncate(time.Second)
	expiryStr := future.Format(time.RFC3339)
	if _, err := env.svc.Update(ctx, manualID, model.UpdateRootDomainInput{ExpiryDate: &expiryStr}); err != nil {
		t.Fatalf("manual set failed: %v", err)
	}

	// A normal whois-source domain, configured to succeed.
	whoisExpiry := time.Now().UTC().Add(365 * 24 * time.Hour)
	env.whois.setSuccess("whois-refreshall-example.com", whoisExpiry)
	whoisID := seedManualOverrideDomain(t, ctx, env, "whois-refreshall-example.com")

	// reset() clears the query log/counts but preserves the orchestrated results
	// configured above via setSuccess, so no reconfiguration is needed here.
	env.whois.reset()

	if err := env.svc.RefreshAll(ctx); err != nil {
		t.Fatalf("RefreshAll returned an unexpected error: %v", err)
	}

	queried := env.whois.queriedDomains()
	for _, d := range queried {
		if d == "manual-refreshall-example.eu" {
			t.Errorf("RefreshAll must not query WHOIS for the manual-source domain, but it did: %v", queried)
		}
	}
	foundWhoisQuery := false
	for _, d := range queried {
		if d == "whois-refreshall-example.com" {
			foundWhoisQuery = true
		}
	}
	if !foundWhoisQuery {
		t.Errorf("RefreshAll should still query WHOIS for the whois-source domain, queried=%v", queried)
	}

	// The manual domain's expiry_date/expiry_source must be unaffected.
	manualRd, err := env.repo.GetByID(ctx, manualID)
	if err != nil {
		t.Fatalf("GetByID(manual) failed: %v", err)
	}
	if manualRd.ExpirySource != "manual" || manualRd.ExpiryDate == nil || !manualRd.ExpiryDate.Equal(future) {
		t.Errorf("manual domain's expiry state changed unexpectedly: source=%q expiry=%v", manualRd.ExpirySource, manualRd.ExpiryDate)
	}

	// The whois domain must have picked up the queried expiry.
	whoisRd, err := env.repo.GetByID(ctx, whoisID)
	if err != nil {
		t.Fatalf("GetByID(whois) failed: %v", err)
	}
	if whoisRd.LastStatus != "success" {
		t.Errorf("whois-source domain last_status = %q, want %q", whoisRd.LastStatus, "success")
	}
}
