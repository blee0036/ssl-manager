package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// Alert type / target constants for root-domain registration-expiry alerting.
// These are intentionally distinct from the existing TLS certificate alert types
// (cert_expiring / cert_expired / tls_handshake_failed / ...) and from the TLS
// "domain" target type, keeping the two monitoring concerns fully separate
// (requirements 1.4 / 5.7).
const (
	// AlertTypeDomainExpiring is the warning-level alert type raised when a root
	// domain's registration will expire within the configured threshold
	// (requirement 5.3).
	AlertTypeDomainExpiring = "domain_expiring"
	// AlertTypeDomainExpired is the critical-level alert type raised when a root
	// domain's registration has already expired (requirement 5.4).
	AlertTypeDomainExpired = "domain_expired"

	// alertTargetTypeRootDomain is the alert target type for root-domain
	// registration-expiry alerts, distinct from the TLS "domain" target type.
	alertTargetTypeRootDomain = "root_domain"

	// alertLevelWarning / alertLevelCritical mirror the plain string levels used
	// across the existing alert subsystem (there are no model-level constants to
	// reuse; e.g. domain_monitor_service.go passes "warning" directly).
	alertLevelWarning  = "warning"
	alertLevelCritical = "critical"

	// defaultExpiryThresholdDays is the fallback expiry threshold (in days) used
	// when the global config value is non-positive (requirement 5.1).
	defaultExpiryThresholdDays = 14
)

// whoisRefreshBackoff is the polite delay inserted between successive per-domain
// WHOIS queries during RefreshAll, to avoid hammering rate-limited WHOIS servers
// (requirement 6.4 handles per-domain failures; this keeps the batch polite).
//
// It is a package-level var (rather than a const) so tests can override it to 0
// and keep batch/property tests fast (e.g. Property 11 over many domains). This
// is a deliberate, documented tradeoff: production uses the 1s default; tests set
// `whoisRefreshBackoff = 0`.
var whoisRefreshBackoff = 1 * time.Second

// Service-level sentinel errors for handler-layer mapping via errors.Is:
//   - ErrValidation -> HTTP 400 (empty/whitespace name, or invalid domain syntax
//     / public suffix). The invalid-domain case also wraps ErrInvalidDomain, so
//     callers may match either sentinel.
//   - ErrDuplicate  -> HTTP 409 (the registrable domain is already monitored).
var (
	// ErrValidation indicates caller-supplied input failed validation: an empty
	// or whitespace-only name, or a value that is not a valid registrable domain
	// (syntactically invalid, or a public suffix such as com.cn). Handlers map it
	// to HTTP 400 (requirements 3.2 / 3.3 / 4.2).
	ErrValidation = errors.New("validation error")
	// ErrDuplicate indicates the registrable domain (eTLD+1) already exists in the
	// root-domain set; the existing record is kept. Handlers map it to HTTP 409
	// (requirement 3.4).
	ErrDuplicate = errors.New("root domain already exists")
)

// ZoneScanner is the minimal interface for scanning Cloudflare zones. It is
// satisfied by *ThirdpartDNSService (which exposes ScanZones), keeping the
// dependency decoupled and easy to mock in tests.
type ZoneScanner interface {
	ScanZones(ctx context.Context, token string) ([]Zone, error)
}

// DomainExpiryService orchestrates root-domain (WHOIS registration expiry)
// monitoring: manual add, Cloudflare import/reconcile, WHOIS refresh, alert
// evaluation, and CRUD. It is fully independent of DomainMonitorService (TLS
// certificate monitoring).
//
// NOTE: This file currently provides the service skeleton, dependency injection,
// alert evaluation, and the additive write paths (Create / ImportFromCloudflare /
// ReconcileCloudflareZones). The Refresh / Get / List / Update / Delete methods
// are added in subsequent tasks (6.3 / 6.4).
type DomainExpiryService struct {
	repo        *repository.RootDomainRepository
	whois       WhoisClient
	rdap        RDAPClient // universal fallback consulted only when a WHOIS lookup errors
	alerter     AlertSender
	runtimeCfg  *config.RuntimeConfig
	zoneScanner ZoneScanner // reuses ThirdpartDNSService.ScanZones
	refreshing  sync.Map    // registrableDomain -> struct{}, serializes per-domain refresh
}

// NewDomainExpiryService creates a DomainExpiryService. The default WhoisClient
// (PRIMARY) and RDAPClient (FALLBACK) both read the CURRENT runtime config's
// WhoisTimeoutSeconds on every query (via NewWhoisClientFunc / NewRDAPClient
// sharing one timeout closure), so a change to whois_timeout_seconds through
// /api/system/config takes effect on the next query without restarting the
// process or reconstructing the service. A non-positive / unset value falls back
// to each client's own default (see their resolveTimeout). Tests may override the
// clients via SetWhoisClient / SetRDAPClient.
func NewDomainExpiryService(
	repo *repository.RootDomainRepository,
	zoneScanner ZoneScanner,
	alerter AlertSender,
	runtimeCfg *config.RuntimeConfig,
) *DomainExpiryService {
	// Shared dynamic-timeout closure: reused by BOTH the WHOIS and RDAP clients so
	// they honor the same runtime whois_timeout_seconds without duplication. A nil
	// runtimeCfg (or non-positive value) yields 0, letting each client fall back to
	// its own default.
	timeoutFn := func() time.Duration {
		if runtimeCfg != nil {
			return time.Duration(runtimeCfg.Get().DomainExpiry.WhoisTimeoutSeconds) * time.Second
		}
		return 0
	}
	return &DomainExpiryService{
		repo:        repo,
		whois:       NewWhoisClientFunc(timeoutFn),
		rdap:        NewRDAPClient(timeoutFn),
		alerter:     alerter,
		runtimeCfg:  runtimeCfg,
		zoneScanner: zoneScanner,
	}
}

// SetWhoisClient overrides the WHOIS client. Intended for tests (inject a mock
// so no real network requests are made).
func (s *DomainExpiryService) SetWhoisClient(c WhoisClient) {
	s.whois = c
}

// SetRDAPClient overrides the RDAP fallback client. Intended for tests (inject a
// mock so no real network requests are made). Mirrors SetWhoisClient.
func (s *DomainExpiryService) SetRDAPClient(c RDAPClient) {
	s.rdap = c
}

// expiryThresholdDays returns the effective global expiry threshold in days,
// falling back to defaultExpiryThresholdDays when unset or non-positive
// (requirements 5.1 / 5.2 / 5.8).
func (s *DomainExpiryService) expiryThresholdDays() int {
	if s.runtimeCfg != nil {
		if t := s.runtimeCfg.Get().DomainExpiry.ExpiryThresholdDays; t > 0 {
			return t
		}
	}
	return defaultExpiryThresholdDays
}

// daysRemaining computes the whole number of days between now and expiry,
// truncated toward zero (positive = future, negative = already expired). This
// matches DomainMonitorService's convention (requirement 8.2).
func daysRemaining(expiry, now time.Time) int {
	return int(expiry.Sub(now).Hours() / 24)
}

// evaluateAlerts sends or auto-resolves registration-expiry alerts for a single
// root domain based on its remaining days against the global expiry threshold.
//
//   - alert_ignored is true            -> no evaluation, no send (requirement 5.6)
//   - expiry unknown (ExpiryDate nil)  -> not evaluated             (requirement 8.3)
//   - days <= 0                        -> critical domain_expired   (requirement 5.4)
//   - 0 < days <= threshold            -> warning  domain_expiring  (requirement 5.3)
//   - days > threshold                 -> auto-resolve both alert types (requirement 5.5)
//
// SendAlert carries its own duplicate suppression (AlertService.ShouldSuppress),
// so no extra dedup is needed here.
func (s *DomainExpiryService) evaluateAlerts(ctx context.Context, rd *model.RootDomain) {
	if s.alerter == nil || rd == nil {
		return
	}

	// Requirement 5.6: ignored domains are never evaluated or alerted.
	if rd.AlertIgnored {
		return
	}

	// Requirement 8.3: unknown expiry (never successfully checked) is not evaluated.
	if rd.ExpiryDate == nil {
		return
	}

	threshold := s.expiryThresholdDays()
	now := time.Now().UTC()
	expiry := rd.ExpiryDate.UTC()
	days := daysRemaining(expiry, now)
	expiryStr := expiry.Format(time.RFC3339)

	switch {
	case days <= 0:
		// Requirement 5.4: already expired -> critical.
		title := fmt.Sprintf("Domain registration expired: %s", rd.RegistrableDomain)
		content := fmt.Sprintf(
			"The registration for root domain %s has expired (expiry date: %s).",
			rd.RegistrableDomain, expiryStr,
		)
		_ = s.alerter.SendAlert(ctx, alertLevelCritical, AlertTypeDomainExpired, title, content, alertTargetTypeRootDomain, rd.ID)
	case days <= threshold:
		// Requirement 5.3: within the threshold window -> warning.
		title := fmt.Sprintf("Domain registration expiring: %s", rd.RegistrableDomain)
		content := fmt.Sprintf(
			"The registration for root domain %s will expire in %d day(s) (expiry date: %s).",
			rd.RegistrableDomain, days, expiryStr,
		)
		_ = s.alerter.SendAlert(ctx, alertLevelWarning, AlertTypeDomainExpiring, title, content, alertTargetTypeRootDomain, rd.ID)
	default:
		// Requirement 5.5: days > threshold (e.g. after renewal) -> auto-resolve
		// any previously raised expiring/expired alerts.
		s.alerter.AutoResolve(ctx, alertTargetTypeRootDomain, rd.ID, AlertTypeDomainExpiring)
		s.alerter.AutoResolve(ctx, alertTargetTypeRootDomain, rd.ID, AlertTypeDomainExpired)
	}
}

// Create manually adds a root domain (source="manual"). It validates the input,
// computes the registrable domain (eTLD+1), rejects duplicates, and persists a
// new record with monitoring enabled.
//
//   - empty / whitespace-only name       -> ErrValidation                        (requirement 3.2)
//   - invalid syntax / public suffix     -> ErrValidation wrapping ErrInvalidDomain (requirements 3.3 / 4.2)
//   - registrable domain already present -> ErrDuplicate, existing record kept    (requirement 3.4)
//
// After the record is persisted, Create makes a BEST-EFFORT attempt to populate
// its expiry date via a single RefreshOne call (aligning with the existing
// "create-then-probe" UX). This is best-effort only: any refresh error never
// fails Create — the freshly created record (expiry unknown) is returned instead,
// and the periodic / manual refresh will fill in the expiry later.
func (s *DomainExpiryService) Create(ctx context.Context, in model.CreateRootDomainInput) (*model.RootDomain, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		// Requirement 3.2: empty / whitespace-only names are rejected.
		return nil, fmt.Errorf("%w: name must not be empty", ErrValidation)
	}

	reg, err := RegistrableDomain(name)
	if err != nil {
		// Requirements 3.3 / 4.2: syntactically invalid input or a public suffix
		// itself. Wrap both ErrValidation (for handler 400 mapping) and the
		// underlying ErrInvalidDomain so callers can errors.Is either sentinel.
		return nil, fmt.Errorf("%w: %w", ErrValidation, err)
	}

	rd := &model.RootDomain{
		Name:              normalizeDomain(name),
		Source:            "manual",
		RegistrableDomain: reg,
		MonitorEnabled:    true,
	}

	// Requirement 3.4: dedup by registrable domain via a single atomic, race-safe
	// insert (INSERT ... ON CONFLICT DO NOTHING). Unlike a "check then create"
	// sequence, this closes the race where two concurrent adds (or a manual add
	// racing the DNS-cadence reconcile) both miss the pre-check and the loser trips
	// the UNIQUE index. A conflict is reported as created=false (NOT an error),
	// which we map to ErrDuplicate (HTTP 409); the existing record is kept.
	created, err := s.repo.CreateIfNotExists(ctx, rd)
	if err != nil {
		return nil, fmt.Errorf("failed to create root domain: %w", err)
	}
	if !created {
		return nil, fmt.Errorf("%w: %s", ErrDuplicate, reg)
	}

	// Best-effort single WHOIS refresh so the new record has an expiry date right
	// away. RefreshOne only returns a Go error on infrastructure failures (DB /
	// missing id); WHOIS-layer failures are folded into the returned record. On
	// any error we fall through and return the created record unchanged, so a
	// refresh failure never fails Create.
	if refreshed, rerr := s.RefreshOne(ctx, rd.ID); rerr == nil && refreshed != nil {
		return refreshed, nil
	}

	return rd, nil
}

// ImportFromCloudflare scans Cloudflare zones with the given API token and
// registers each zone's root domain as a source="cloudflare" RootDomain.
//
//   - ScanZones failure                -> descriptive error, set unchanged     (requirement 2.3)
//   - zone already present (by eTLD+1)  -> recorded in Skipped                  (requirement 2.2)
//   - new zone                          -> created, recorded in Imported        (requirement 2.1)
//
// Imported domains are intentionally NOT queried via WHOIS here (to stay polite
// to rate-limited WHOIS servers); the periodic refresh scheduler fills in expiry
// dates later. A zone whose name is not a valid registrable domain is recorded
// in Skipped (rather than aborting the whole import) so the result stays
// informative.
func (s *DomainExpiryService) ImportFromCloudflare(ctx context.Context, token string) (*model.RootDomainImportResult, error) {
	zones, err := s.zoneScanner.ScanZones(ctx, token)
	if err != nil {
		// Requirement 2.3: on scan failure (invalid token / fetch error), return a
		// descriptive error and do NOT modify the root-domain set.
		return nil, fmt.Errorf("failed to scan cloudflare zones: %w", err)
	}

	result := model.NewRootDomainImportResult()
	result.Total = len(zones)

	for _, zone := range zones {
		reg, err := RegistrableDomain(zone.Name)
		if err != nil {
			// Unusual: a Cloudflare zone name that is not a valid registrable
			// domain. Skip it (record for visibility) rather than failing import.
			result.Skipped = append(result.Skipped, normalizeDomain(zone.Name))
			continue
		}

		rd := &model.RootDomain{
			Name:              normalizeDomain(zone.Name),
			Source:            "cloudflare",
			RegistrableDomain: reg,
			MonitorEnabled:    true,
		}
		// Atomic dedup: created=true -> newly imported; created=false -> already
		// present (requirement 2.2), recorded as skipped. A conflict is NOT an
		// error, so a concurrent import/reconcile racing on the same zone can never
		// abort the loop; only a genuine DB failure returns an error.
		created, err := s.repo.CreateIfNotExists(ctx, rd)
		if err != nil {
			return nil, fmt.Errorf("failed to create root domain %q: %w", reg, err)
		}
		if created {
			result.Imported = append(result.Imported, reg)
		} else {
			result.Skipped = append(result.Skipped, reg)
		}
	}

	return result, nil
}

// ReconcileCloudflareZones performs an additive reconcile of the root-domain set
// against the current Cloudflare zone names: new registrable domains are added as
// source="cloudflare"; already-present ones are kept (dedup semantics identical
// to ImportFromCloudflare, requirement 2.4).
//
// This method is purely additive: existing cloudflare root domains that are NOT
// in zoneNames are never touched, so they are retained by default (not deleted,
// not disabled), which naturally satisfies requirement 2.5. It is intended to be
// driven by the scheduler on the existing DNS-sync cadence.
func (s *DomainExpiryService) ReconcileCloudflareZones(ctx context.Context, zoneNames []string) error {
	for _, name := range zoneNames {
		reg, err := RegistrableDomain(name)
		if err != nil {
			// Skip names that are not valid registrable domains.
			continue
		}

		rd := &model.RootDomain{
			Name:              normalizeDomain(name),
			Source:            "cloudflare",
			RegistrableDomain: reg,
			MonitorEnabled:    true,
		}
		// Atomic, additive dedup (requirement 2.2 / 2.4): an already-present
		// registrable domain surfaces as created=false, which is NOT an error, so a
		// pre-existing (or concurrently inserted) domain never aborts the loop and
		// every later zone is still reconciled. Only a genuine DB failure returns an
		// error. Vanished domains are never touched here, satisfying default-retain
		// (requirement 2.5).
		if _, err := s.repo.CreateIfNotExists(ctx, rd); err != nil {
			return fmt.Errorf("failed to create root domain %q: %w", reg, err)
		}
	}
	return nil
}

// RefreshOne performs a single WHOIS refresh for the root domain identified by
// id and returns the updated record (with DaysRemaining populated). It backs
// requirement 7 (manual refresh) and is reused by RefreshAll (requirement 6).
//
// Concurrency: refreshes are serialized per registrable_domain via the
// `refreshing` sync.Map, so a manual refresh and the periodic refresh never issue
// duplicate concurrent WHOIS queries for the same domain, while different domains
// still proceed in parallel. We store a *sync.Mutex per registrable domain and
// hold it for the duration of the refresh. The map entry is intentionally left in
// place afterwards (a bounded "leak": there is at most one entry per monitored
// registrable domain), which keeps the serialization scheme simple and correct;
// a delete-on-unlock scheme would reintroduce a store/lock race.
//
// Lookup strategy — WHOIS PRIMARY, RDAP FALLBACK:
//   - WHOIS is queried first. On success its expiry is used.
//   - On ANY WHOIS error (and when an RDAP client is configured) the RDAP
//     fallback is queried. RDAP is the ICANN-mandated structured successor to
//     WHOIS and covers newer gTLDs (e.g. .app / .dev) whose registries expose no
//     legacy WHOIS server. On RDAP success its expiry is used and the WHOIS error
//     is cleared; on RDAP failure the recorded error combines both
//     ("whois: ...; rdap: ...").
//
// Error semantics (design "WHOIS 层失败不冒泡 Go error"):
//   - lookup success (WHOIS or RDAP) -> SaveExpiryResult(&expiry, now, "success",
//     ""), then the record is re-read and alerts are evaluated (requirements
//     4.3 / 4.4 / 7.1).
//   - lookup failure (BOTH WHOIS and RDAP) -> SaveExpiryResult(nil, now, "failed",
//     err): the previously known expiry_date is preserved, last_status/last_error
//     record the (combined) failure, and NO Go error is returned — the re-read
//     record (last_status="failed") is returned with a nil error (requirements
//     4.5 / 4.6 / 7.2). Alerts are not evaluated on failure (expiry unchanged).
//
// Only genuine infrastructure errors bubble up as a non-nil Go error: the id not
// existing (GetByID -> sql.ErrNoRows, wrapped so handlers map it to 404) and DB
// failures from GetByID / SaveExpiryResult.
//
// Manual override short-circuit: when the record's ExpirySource is "manual" (set
// via Update), RefreshOne skips the WHOIS/RDAP query entirely, re-evaluates
// alerts against the manually-set expiry_date, and returns immediately. This
// covers registries that are structurally unqueryable via WHOIS/RDAP (see
// requirements.md "已知限制与后续增强").
func (s *DomainExpiryService) RefreshOne(ctx context.Context, id string) (*model.RootDomain, error) {
	rd, err := s.repo.GetByID(ctx, id)
	if err != nil {
		// Wraps sql.ErrNoRows on not-found so the handler can map it to 404.
		return nil, fmt.Errorf("failed to get root domain: %w", err)
	}

	// Manual expiry override (see "已知限制与后续增强" in requirements.md): this
	// domain's registrar is structurally unqueryable via WHOIS/RDAP (e.g. some
	// .eu / .uy / third-level ccTLD delegations), so an operator has set
	// expiry_date by hand via Update. Skip the WHOIS/RDAP query entirely — both
	// to avoid a pointless network round-trip that can only fail, and to avoid
	// clobbering last_status="manual" with a failed check. The expiry date is
	// still re-evaluated against the current time/threshold on every call (via
	// evaluateAlerts below), so alerts stay correct as the countdown progresses.
	if rd.ExpirySource == "manual" {
		computeDaysRemaining(rd)
		s.evaluateAlerts(ctx, rd)
		return rd, nil
	}

	// Serialize refreshes of the same registrable domain (see doc above).
	actual, _ := s.refreshing.LoadOrStore(rd.RegistrableDomain, &sync.Mutex{})
	mu := actual.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	now := time.Now().UTC()
	expiry, werr := s.whois.LookupExpiry(ctx, rd.RegistrableDomain)
	if werr != nil && s.rdap != nil {
		// WHOIS (PRIMARY) failed: fall back to RDAP (the structured successor that
		// covers TLDs with no legacy WHOIS server, e.g. .app / .dev). On RDAP
		// success adopt its expiry and clear the error; on RDAP failure record a
		// combined error naming both layers.
		rexpiry, rerr := s.rdap.LookupExpiry(ctx, rd.RegistrableDomain)
		if rerr == nil {
			expiry, werr = rexpiry, nil
		} else {
			werr = fmt.Errorf("whois: %v; rdap: %v", werr, rerr)
		}
	}
	if werr != nil {
		// WHOIS-layer failure: preserve the old expiry_date (nil expiry), record
		// the failure. Do NOT bubble as a Go error — only DB errors bubble.
		if serr := s.repo.SaveExpiryResult(ctx, id, nil, now, "failed", werr.Error()); serr != nil {
			return nil, fmt.Errorf("failed to save expiry result: %w", serr)
		}
	} else {
		if serr := s.repo.SaveExpiryResult(ctx, id, &expiry, now, "success", ""); serr != nil {
			return nil, fmt.Errorf("failed to save expiry result: %w", serr)
		}
	}

	// Re-read to reflect the persisted changes for the returned record.
	refreshed, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to reload root domain: %w", err)
	}

	// Evaluate alerts only on success: the expiry date may have changed. On
	// failure the expiry is unchanged, so re-evaluation would be redundant.
	if werr == nil {
		s.evaluateAlerts(ctx, refreshed)
	}

	computeDaysRemaining(refreshed)
	return refreshed, nil
}

// RefreshAll refreshes every enabled root domain's registration expiry, backing
// the periodic Expiry_Refresh_Scheduler (requirements 6.1 / 6.4 / 6.6).
//
//   - ListEnabled returns only monitor_enabled=1 domains, so disabled domains are
//     skipped (requirement 6.6).
//   - Domains are processed serially. A single RefreshOne failure is logged and
//     iteration continues with the remaining domains (requirement 6.4). Note that
//     WHOIS-layer failures do NOT surface here (RefreshOne folds them into the
//     record and returns nil error); the errors handled here are infrastructure
//     errors (DB / missing id).
//   - A small backoff (whoisRefreshBackoff) is inserted between successive queries
//     to stay polite to rate-limited WHOIS servers. It is a package-level var so
//     tests can set it to 0 for speed.
//
// A non-nil error is only returned when the initial ListEnabled fails.
func (s *DomainExpiryService) RefreshAll(ctx context.Context) error {
	rds, err := s.repo.ListEnabled(ctx)
	if err != nil {
		return fmt.Errorf("failed to list enabled root domains: %w", err)
	}

	for i, rd := range rds {
		// Polite backoff between queries (not before the first, and skipped
		// entirely when whoisRefreshBackoff is 0, e.g. in tests).
		if i > 0 && whoisRefreshBackoff > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(whoisRefreshBackoff):
			}
		}

		if _, err := s.RefreshOne(ctx, rd.ID); err != nil {
			// Requirement 6.4: log and continue on individual failures.
			log.Printf("[DomainExpiry] RefreshAll: refresh of %s (%s) failed: %v", rd.ID, rd.RegistrableDomain, err)
			continue
		}
	}

	return nil
}

// GetByID retrieves a single root domain by id and populates its (non-persistent)
// DaysRemaining field before returning (requirements 8.2 / 8.3). A missing id
// surfaces as a wrapped sql.ErrNoRows so the handler can map it to HTTP 404
// (requirement 8).
func (s *DomainExpiryService) GetByID(ctx context.Context, id string) (*model.RootDomain, error) {
	rd, err := s.repo.GetByID(ctx, id)
	if err != nil {
		// Wraps sql.ErrNoRows on not-found so the handler can map it to 404.
		return nil, fmt.Errorf("failed to get root domain: %w", err)
	}
	// Populate the read-time DaysRemaining / unknown state (requirements 8.2 / 8.3).
	computeDaysRemaining(rd)
	return rd, nil
}

// ListWithSort returns root domains with server-side filtering, sorting and
// pagination, plus the total count (before pagination), backing the list view
// (requirement 8.1). The global expiry threshold is read from the runtime config
// and passed to the repository so the expiring/ok filter_status predicates use
// the effective threshold. Each returned record has its DaysRemaining populated
// (requirements 8.2 / 8.3).
func (s *DomainExpiryService) ListWithSort(ctx context.Context, params model.RootDomainListParams) ([]*model.RootDomain, int, error) {
	threshold := s.expiryThresholdDays()
	items, total, err := s.repo.ListWithSort(ctx, params, threshold)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list root domains: %w", err)
	}
	for _, rd := range items {
		computeDaysRemaining(rd)
	}
	return items, total, nil
}

// Update applies a partial update (monitor_enabled / alert_ignored) to a root
// domain and returns the refreshed record (requirement 5.6).
//
// Suppress-before-persist ordering (mirrors DomainMonitorService.Update): when
// alert_ignored is being set to true, active alerts for this root domain are
// suppressed FIRST via AlertSender.SuppressActiveByTarget. If suppression fails,
// the error is returned and the update is NOT persisted (so alert_ignored stays
// as it was — the change is rolled back by never being written). Guarded on a
// non-nil alerter.
//
// A missing id surfaces as a wrapped sql.ErrNoRows for HTTP 404 mapping: when
// there are updates, repo.Update reports it; when there are none, the trailing
// GetByID re-read reports it. The record is always re-read via GetByID so the
// returned record has DaysRemaining populated.
func (s *DomainExpiryService) Update(ctx context.Context, id string, in model.UpdateRootDomainInput) (*model.RootDomain, error) {
	updates := make(map[string]interface{})
	if in.MonitorEnabled != nil {
		updates["monitor_enabled"] = *in.MonitorEnabled
	}
	if in.AlertIgnored != nil {
		updates["alert_ignored"] = *in.AlertIgnored
	}

	// Manual expiry override (see requirements.md "已知限制与后续增强"): lets an
	// operator set/clear expiry_date by hand for domains whose registry is
	// structurally unqueryable via WHOIS/RDAP.
	//   - non-empty string: parse as the new expiry_date, switch expiry_source to
	//     "manual", set last_status="manual" and clear last_error. The periodic
	//     refresh (RefreshAll -> RefreshOne) will then skip the WHOIS/RDAP query
	//     for this domain entirely.
	//   - empty string (""): clear the override, switching expiry_source back to
	//     "whois" and resetting last_status to "" (pending re-check); expiry_date
	//     itself is left untouched until the next successful WHOIS/RDAP query
	//     overwrites it. Restores normal periodic WHOIS querying.
	if in.ExpiryDate != nil {
		if strings.TrimSpace(*in.ExpiryDate) == "" {
			updates["expiry_source"] = "whois"
			updates["last_status"] = ""
		} else {
			parsed, perr := time.Parse(time.RFC3339, strings.TrimSpace(*in.ExpiryDate))
			if perr != nil {
				return nil, fmt.Errorf("%w: invalid expiry_date: %v", ErrValidation, perr)
			}
			updates["expiry_date"] = parsed.UTC().Format(time.RFC3339)
			updates["expiry_source"] = "manual"
			updates["last_status"] = "manual"
			updates["last_error"] = ""
		}
	}

	// Requirement 5.6: when alert_ignored is set to true, suppress active alerts
	// first. Order: suppress → persist. If suppress fails, alert_ignored is not
	// persisted (mirrors DomainMonitorService.Update).
	if in.AlertIgnored != nil && *in.AlertIgnored {
		if s.alerter != nil {
			if err := s.alerter.SuppressActiveByTarget(ctx, alertTargetTypeRootDomain, id); err != nil {
				return nil, fmt.Errorf("failed to suppress active alerts: %w", err)
			}
		}
	}

	if err := s.repo.Update(ctx, id, updates); err != nil {
		// Wraps sql.ErrNoRows on not-found so the handler can map it to 404.
		// (repo.Update is a no-op returning nil when updates is empty; in that
		// case the GetByID below still surfaces a missing id as 404.)
		return nil, fmt.Errorf("failed to update root domain: %w", err)
	}

	// Re-read via GetByID (which sets DaysRemaining) and return.
	rd, err := s.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// A manual expiry_date was just set: re-evaluate alerts immediately so the
	// new date's threshold/critical classification takes effect right away,
	// mirroring RefreshOne's on-success evaluation (requirement 5).
	if in.ExpiryDate != nil && strings.TrimSpace(*in.ExpiryDate) != "" {
		s.evaluateAlerts(ctx, rd)
	}

	return rd, nil
}

// Delete removes a root domain and its inlined registration-expiry data
// (requirement 8.4). A missing id surfaces as a wrapped sql.ErrNoRows so the
// handler can map it to HTTP 404.
func (s *DomainExpiryService) Delete(ctx context.Context, id string) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		// Wraps sql.ErrNoRows on not-found so the handler can map it to 404.
		return fmt.Errorf("failed to delete root domain: %w", err)
	}
	return nil
}

// computeDaysRemaining sets rd.DaysRemaining from rd.ExpiryDate: the whole number
// of days between now (UTC) and the expiry date (truncated toward zero), or nil
// when the expiry date is unknown (requirements 8.2 / 8.3). Shared helper reused
// by RefreshOne (and later by the list/get read paths in task 6.4).
func computeDaysRemaining(rd *model.RootDomain) {
	if rd == nil {
		return
	}
	if rd.ExpiryDate == nil {
		rd.DaysRemaining = nil
		return
	}
	d := daysRemaining(rd.ExpiryDate.UTC(), time.Now().UTC())
	rd.DaysRemaining = &d
}
