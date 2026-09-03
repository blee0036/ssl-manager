package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/certbot"
	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// expiryTickerDuration converts a configured RefreshIntervalMinutes value into the
// time.Duration used to build the periodic WHOIS expiry-refresh ticker.
//
// It returns ok=false (meaning "disabled — do not create a ticker") in two cases:
//   - minutes <= 0: the documented "disabled" state.
//   - the computed time.Duration(minutes) * time.Minute is <= 0: on 64-bit systems a
//     large positive minutes value (roughly > 153,722,867) overflows int64
//     nanoseconds and wraps to a non-positive duration. Passing such a value to
//     time.NewTicker panics, so we guard against it here and treat it as disabled.
//
// Config validation (config.MaxRefreshIntervalMinutes) already rejects out-of-range
// positive values before they reach here; this helper is defense-in-depth so the
// scheduler can never call time.NewTicker with a non-positive duration.
func expiryTickerDuration(minutes int) (time.Duration, bool) {
	if minutes <= 0 {
		return 0, false
	}
	d := time.Duration(minutes) * time.Minute
	if d <= 0 {
		return 0, false
	}
	return d, true
}

// AlertSender is an interface for sending alert notifications.
type AlertSender interface {
	SendAlert(ctx context.Context, level, alertType, title, content, targetType, targetID string) error
	AutoResolve(ctx context.Context, targetType, targetID, alertType string)
	SuppressActiveByTarget(ctx context.Context, targetType, targetID string) error
}

// CertbotRenewer is an interface for issuing certificates via Certbot.
// This allows mocking in tests.
type CertbotRenewer interface {
	IssueCertCloudflare(ctx context.Context, domains []string, email string, cloudflareToken string) (*certbot.CertbotResult, error)
}

var (
	// ErrCertificateRenewalInProgress prevents concurrent scheduler and manual
	// renewal attempts from writing the same certificate files at once.
	ErrCertificateRenewalInProgress = errors.New("certificate renewal is already in progress")
	// ErrManualRenewalUnsupported is returned when a certificate cannot be
	// renewed directly through the Cloudflare DNS Certbot flow.
	ErrManualRenewalUnsupported = errors.New("manual renewal is only supported for Cloudflare DNS certificates")
)

// SchedulerService handles periodic tasks: certificate renewal checks,
// heartbeat timeout detection, and domain monitoring.
type SchedulerService struct {
	runtimeCfg     *config.RuntimeConfig
	certRepo       *repository.CertificateRepository
	machineRepo    *repository.MachineRepository
	certService    *CertificateService
	certbotRenewer CertbotRenewer
	alertSender    AlertSender
	db             *sql.DB

	// Domain monitoring
	domainMonitorService *DomainMonitorService

	// DNS sync dependencies (NEW)
	thirdpartDNSService *ThirdpartDNSService
	dnsRepo             *repository.ThirdpartDNSRepository

	// Domain expiry monitoring (WHOIS registration expiry)
	domainExpiryService *DomainExpiryService

	// Cloudflare apex auto-sync one-time cleanup (see
	// cleanupZoneOnlyCloudflareDomainsOnce): removes leftover domains rows
	// created by the removed reconcileApexDomainMonitorsFromCloudflare logic.
	// Direct access to the domains table, distinct from domainMonitorService.
	domainRepo          *repository.DomainRepository
	zoneOnlyCleanupOnce sync.Once

	// Scheduler control
	mu       sync.Mutex
	running  bool
	stopping bool // prevents Start during Stop/Wait overlap
	stopCh   chan struct{}
	wg       sync.WaitGroup

	// Child context for graceful cancellation of long-running tasks (DNS sync)
	childCtx    context.Context
	childCancel context.CancelFunc

	// DNS sync trigger channel: capacity 1, coalesces multiple ticks
	dnsSyncTrigger chan struct{}

	// Retry configuration
	MaxRetries    int
	RetryInterval time.Duration

	renewalMu sync.Mutex
	renewing  map[string]struct{}
}

// NewSchedulerService creates a new SchedulerService.
func NewSchedulerService(
	runtimeCfg *config.RuntimeConfig,
	certRepo *repository.CertificateRepository,
	machineRepo *repository.MachineRepository,
	certService *CertificateService,
	certbotRenewer CertbotRenewer,
	alertSender AlertSender,
	db *sql.DB,
) *SchedulerService {
	return &SchedulerService{
		runtimeCfg:     runtimeCfg,
		certRepo:       certRepo,
		machineRepo:    machineRepo,
		certService:    certService,
		certbotRenewer: certbotRenewer,
		alertSender:    alertSender,
		db:             db,
		MaxRetries:     3,
		RetryInterval:  5 * time.Minute,
	}
}

// SetDomainMonitorService sets the domain monitor service for periodic probing.
func (s *SchedulerService) SetDomainMonitorService(svc *DomainMonitorService) {
	s.domainMonitorService = svc
}

// SetThirdpartDNSService sets the thirdpart DNS service and repo for periodic sync.
func (s *SchedulerService) SetThirdpartDNSService(svc *ThirdpartDNSService, repo *repository.ThirdpartDNSRepository) {
	s.thirdpartDNSService = svc
	s.dnsRepo = repo
}

// SetDomainExpiryService sets the domain expiry service for periodic WHOIS refresh.
func (s *SchedulerService) SetDomainExpiryService(svc *DomainExpiryService) {
	s.domainExpiryService = svc
}

// SetDomainRepo wires the domain repository so the scheduler can run the
// one-time cleanup of leftover "zone-only" Cloudflare apex domain monitor rows
// (see cleanupZoneOnlyCloudflareDomainsOnce). Not calling this (leaving
// domainRepo nil) safely disables that cleanup (nil-guarded).
func (s *SchedulerService) SetDomainRepo(repo *repository.DomainRepository) {
	s.domainRepo = repo
}

// Start begins the scheduler's periodic execution loop.
func (s *SchedulerService) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running || s.stopping {
		s.mu.Unlock()
		return nil // Already running or previous Stop still in progress
	}
	s.running = true

	// Create immutable channels for this run cycle
	stopCh := make(chan struct{})
	trigger := make(chan struct{}, 1)
	s.stopCh = stopCh
	s.dnsSyncTrigger = trigger

	// Create child context for cancellation of long-running sub-tasks
	s.childCtx, s.childCancel = context.WithCancel(ctx)

	// wg.Add(2): main run loop + DNS worker goroutine
	s.wg.Add(2)
	s.mu.Unlock()

	// Pass channels as immutable parameters so goroutines don't read mutable struct fields
	go s.run(s.childCtx, stopCh, trigger)
	go s.dnsWorker(s.childCtx, stopCh, trigger)

	log.Println("[Scheduler] Started")
	return nil
}

// Stop stops the scheduler and waits for all goroutines to finish.
func (s *SchedulerService) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil // Already stopped, no-op
	}
	s.running = false
	s.stopping = true

	// 1. Cancel child context to signal long-running tasks (DNS sync) to abort
	if s.childCancel != nil {
		s.childCancel()
	}
	// 2. Close stopCh to exit select loops
	close(s.stopCh)
	s.mu.Unlock()

	// 3. Wait for both goroutines (main run loop + DNS worker) to exit
	s.wg.Wait()

	// 4. Clear stopping flag
	s.mu.Lock()
	s.stopping = false
	s.mu.Unlock()

	log.Println("[Scheduler] Stopped")
	return nil
}

// IsRunning returns whether the scheduler is currently running.
func (s *SchedulerService) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// run is the main scheduler loop that periodically checks renewals,
// heartbeat timeouts, and domain monitoring.
// Channels are passed as immutable parameters to avoid reading mutable struct fields.
func (s *SchedulerService) run(ctx context.Context, stopCh <-chan struct{}, dnsSyncTrigger chan<- struct{}) {
	defer s.wg.Done()

	// Run immediately on start
	if err := s.CheckRenewals(ctx); err != nil {
		log.Printf("[Scheduler] CheckRenewals error: %v", err)
	}
	if err := s.CheckHeartbeatTimeouts(ctx); err != nil {
		log.Printf("[Scheduler] CheckHeartbeatTimeouts error: %v", err)
	}
	if err := s.RunDomainMonitor(ctx); err != nil {
		log.Printf("[Scheduler] RunDomainMonitor error: %v", err)
	}
	// Run cleanup on start (non-critical)
	if err := s.RunCleanup(ctx); err != nil {
		log.Printf("[Scheduler] RunCleanup error: %v", err)
	}

	// Default check interval: every hour for renewals and heartbeat
	renewalTicker := time.NewTicker(1 * time.Hour)
	defer renewalTicker.Stop()

	// Heartbeat timeout check: every 30 seconds
	heartbeatTicker := time.NewTicker(30 * time.Second)
	defer heartbeatTicker.Stop()

	// Domain monitor interval from config
	domainMonitorInterval := time.Duration(s.runtimeCfg.Get().DomainMonitor.IntervalMinutes) * time.Minute
	if domainMonitorInterval <= 0 {
		domainMonitorInterval = 60 * time.Minute
	}
	currentDomainInterval := s.runtimeCfg.Get().DomainMonitor.IntervalMinutes
	if currentDomainInterval <= 0 {
		currentDomainInterval = 60
	}
	domainMonitorTicker := time.NewTicker(domainMonitorInterval)
	defer domainMonitorTicker.Stop()

	// DNS sync ticker — nil means disabled
	var currentDNSTicker *time.Ticker
	var dnsSyncC <-chan time.Time
	currentDNSInterval := s.runtimeCfg.Get().ThirdpartDNS.SyncIntervalMinutes
	if currentDNSInterval > 0 {
		currentDNSTicker = time.NewTicker(time.Duration(currentDNSInterval) * time.Minute)
		dnsSyncC = currentDNSTicker.C
	}
	defer func() {
		if currentDNSTicker != nil {
			currentDNSTicker.Stop()
		}
	}()

	// Expiry refresh ticker — nil means disabled (interval <= 0).
	// NOTE: unlike CheckRenewals / RunDomainMonitor above, we deliberately do NOT
	// run RefreshAll on startup. WHOIS servers are rate-limit sensitive, so running
	// a full refresh on every restart could cause bursty queries and trigger limits.
	// The first periodic refresh therefore happens after one full interval.
	var expiryTicker *time.Ticker
	var expiryC <-chan time.Time
	currentExpiryInterval := s.runtimeCfg.Get().DomainExpiry.RefreshIntervalMinutes
	if d, ok := expiryTickerDuration(currentExpiryInterval); ok {
		expiryTicker = time.NewTicker(d)
		expiryC = expiryTicker.C
	}
	defer func() {
		if expiryTicker != nil {
			expiryTicker.Stop()
		}
	}()

	for {
		select {
		case <-stopCh:
			return
		case <-ctx.Done():
			return
		case <-renewalTicker.C:
			if err := s.CheckRenewals(ctx); err != nil {
				log.Printf("[Scheduler] CheckRenewals error: %v", err)
			}
			// Periodic cleanup piggybacks on the hourly renewal check
			if err := s.RunCleanup(ctx); err != nil {
				log.Printf("[Scheduler] RunCleanup error: %v", err)
			}
		case <-heartbeatTicker.C:
			if err := s.CheckHeartbeatTimeouts(ctx); err != nil {
				log.Printf("[Scheduler] CheckHeartbeatTimeouts error: %v", err)
			}
			// Piggyback on heartbeat tick (30s) to check DNS interval config changes
			s.checkDNSIntervalChange(&currentDNSTicker, &dnsSyncC, &currentDNSInterval)
			// Piggyback on heartbeat tick (30s) to check expiry refresh interval config changes
			s.checkExpiryRefreshIntervalChange(&expiryTicker, &expiryC, &currentExpiryInterval)
		case <-domainMonitorTicker.C:
			if err := s.RunDomainMonitor(ctx); err != nil {
				log.Printf("[Scheduler] RunDomainMonitor error: %v", err)
			}
			// Check if the domain monitor interval has changed in config
			newInterval := s.runtimeCfg.Get().DomainMonitor.IntervalMinutes
			if newInterval != currentDomainInterval && newInterval > 0 {
				domainMonitorTicker.Stop()
				currentDomainInterval = newInterval
				domainMonitorTicker = time.NewTicker(time.Duration(currentDomainInterval) * time.Minute)
			}
		case <-dnsSyncC:
			// Non-blocking send to trigger channel; if worker is busy, coalesce
			select {
			case dnsSyncTrigger <- struct{}{}:
			default:
				log.Println("[Scheduler] DNS sync trigger coalesced (worker busy)")
			}
		case <-expiryC:
			// Periodic WHOIS registration-expiry refresh (disabled when expiryC is nil).
			if s.domainExpiryService != nil {
				if err := s.domainExpiryService.RefreshAll(ctx); err != nil {
					log.Printf("[Scheduler] RefreshAll(expiry) error: %v", err)
				}
			}
		}
	}
}

// checkDNSIntervalChange checks if the DNS sync interval config has changed.
// Handles 0→positive (enable), positive→0 (disable), and positive→positive (reschedule).
func (s *SchedulerService) checkDNSIntervalChange(
	currentDNSTicker **time.Ticker,
	dnsSyncC *<-chan time.Time,
	currentDNSInterval *int,
) {
	newInterval := s.runtimeCfg.Get().ThirdpartDNS.SyncIntervalMinutes
	if newInterval == *currentDNSInterval {
		return // no change
	}

	// Stop old ticker first
	if *currentDNSTicker != nil {
		(*currentDNSTicker).Stop()
		*currentDNSTicker = nil
		*dnsSyncC = nil
	}

	*currentDNSInterval = newInterval

	if newInterval > 0 {
		// Create new ticker
		*currentDNSTicker = time.NewTicker(time.Duration(newInterval) * time.Minute)
		*dnsSyncC = (*currentDNSTicker).C
		log.Printf("[Scheduler] DNS sync interval changed to %d minutes", newInterval)
	} else {
		log.Println("[Scheduler] DNS sync disabled (interval <= 0)")
	}
}

// checkExpiryRefreshIntervalChange checks if the domain expiry refresh interval config has changed.
// Handles 0→positive (enable), positive→0 (disable), and positive→positive (reschedule).
// Faithful mirror of checkDNSIntervalChange for the WHOIS expiry refresh ticker.
func (s *SchedulerService) checkExpiryRefreshIntervalChange(
	expiryTicker **time.Ticker,
	expiryC *<-chan time.Time,
	currentExpiryInterval *int,
) {
	newInterval := s.runtimeCfg.Get().DomainExpiry.RefreshIntervalMinutes
	if newInterval == *currentExpiryInterval {
		return // no change
	}

	// Stop old ticker first
	if *expiryTicker != nil {
		(*expiryTicker).Stop()
		*expiryTicker = nil
		*expiryC = nil
	}

	*currentExpiryInterval = newInterval

	if d, ok := expiryTickerDuration(newInterval); ok {
		// Create new ticker only when the duration is safely positive.
		*expiryTicker = time.NewTicker(d)
		*expiryC = (*expiryTicker).C
		log.Printf("[Scheduler] Expiry refresh interval changed to %d minutes", newInterval)
	} else {
		// Disabled: interval <= 0, or a positive value that overflows time.Duration
		// (guarded so we never call time.NewTicker with a non-positive duration).
		log.Println("[Scheduler] Expiry refresh disabled (interval <= 0 or out of range)")
	}
}

// dnsWorker is a long-running goroutine that listens on the trigger channel.
// Each trigger causes a serial execution of runDNSSyncAll.
// Exits on ctx.Done() or stopCh.
func (s *SchedulerService) dnsWorker(ctx context.Context, stopCh <-chan struct{}, trigger <-chan struct{}) {
	defer s.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-stopCh:
			return
		case <-trigger:
			s.runDNSSyncAll(ctx)
			// Reconcile cloudflare-sourced root domains (registration-expiry
			// monitoring, root_domains table) on the same DNS-sync cadence
			// (requirement 2.4). This is the ONLY reconcile driven by
			// collectCloudflareZoneNames now — TLS/SSL apex auto-sync into
			// domains was removed (see cleanupZoneOnlyCloudflareDomainsOnce):
			// creating a TLS monitor for every Cloudflare Zone regardless of
			// whether it has any actual A/AAAA/CNAME record was a design
			// error. TLS monitoring for a hostname must be driven by that
			// hostname actually having a DNS record, which is exactly what
			// runDNSSyncAll (via ThirdpartDNSService.syncToLocalDomains)
			// already does.
			if s.domainExpiryService != nil {
				zoneNames := s.collectCloudflareZoneNames(ctx)
				if err := s.domainExpiryService.ReconcileCloudflareZones(ctx, zoneNames); err != nil {
					log.Printf("[Scheduler] reconcile root domains error: %v", err)
				}
			}
			// One-time best-effort cleanup of any leftover rows the removed
			// apex auto-sync logic previously created. Runs at most once per
			// process lifetime (sync.Once) rather than every trigger, since
			// after the first successful cleanup there is nothing left to do.
			s.cleanupZoneOnlyCloudflareDomainsOnce(ctx)
		}
	}
}

// runDNSSyncAll syncs all enabled thirdpart DNS configs.
// Accepts ctx to support cancellation. Processes configs serially.
func (s *SchedulerService) runDNSSyncAll(ctx context.Context) {
	if s.thirdpartDNSService == nil || s.dnsRepo == nil {
		return
	}
	configs, err := s.dnsRepo.List(ctx)
	if err != nil {
		log.Printf("[Scheduler] Failed to list DNS configs for sync: %v", err)
		return
	}
	for _, cfg := range configs {
		// Check if context is cancelled (graceful shutdown)
		select {
		case <-ctx.Done():
			log.Println("[Scheduler] DNS sync cancelled by context")
			return
		default:
		}
		if !cfg.Enabled {
			continue
		}
		if _, err := s.thirdpartDNSService.SyncRecords(ctx, cfg.ID); err != nil {
			if errors.Is(err, ErrSyncInProgress) {
				log.Printf("[Scheduler] DNS sync for config %s (%s): skipped (already in progress)", cfg.Name, cfg.ID)
			} else {
				log.Printf("[Scheduler] DNS sync for config %s (%s): %v", cfg.Name, cfg.ID, err)
			}
		}
	}
}

// collectCloudflareZoneNames scans zones for every enabled thirdpart DNS config
// and returns the zone names collected across all configs, in config-then-zone
// iteration order. Names are NOT de-duplicated here — this mirrors the original
// reconcileRootDomainsFromCloudflare collection loop exactly: if two enabled
// configs share a zone, or ScanZones itself returns duplicate entries, the
// returned slice may contain the same name more than once. Callers
// (ReconcileCloudflareZones and the apex auto-sync path's CreateIfNotExists) both
// tolerate duplicate names safely via their own idempotent/atomic dedup
// primitives, so no additional dedup is performed here.
//
// A single config's ScanZones failure is logged and skipped (continue), so it
// does not abort collection for the remaining configs (requirement 1.4: error
// isolation). Required dependencies (thirdpartDNSService, dnsRepo) are
// nil-guarded up front; returns nil if the DNS integration is not wired up.
func (s *SchedulerService) collectCloudflareZoneNames(ctx context.Context) []string {
	if s.thirdpartDNSService == nil || s.dnsRepo == nil {
		return nil
	}
	configs, err := s.dnsRepo.List(ctx)
	if err != nil {
		log.Printf("[Scheduler] Failed to list DNS configs for root-domain reconcile: %v", err)
		return nil
	}
	var zoneNames []string
	for _, cfg := range configs {
		if !cfg.Enabled {
			continue
		}
		zones, err := s.thirdpartDNSService.ScanZones(ctx, cfg.APIToken)
		if err != nil {
			// Single config failure must not stop the others.
			log.Printf("[Scheduler] ScanZones for config %s (%s) failed during root-domain reconcile: %v", cfg.Name, cfg.ID, err)
			continue
		}
		for _, z := range zones {
			zoneNames = append(zoneNames, z.Name)
		}
	}
	return zoneNames
}

// reconcileRootDomainsFromCloudflare reconciles the root-domain set against the
// current Cloudflare zones, driven by the existing third-party DNS sync cadence
// (requirement 2.4). It runs inside the single-threaded dnsWorker right after
// runDNSSyncAll, so it executes serially and is safe.
//
// It delegates zone collection to collectCloudflareZoneNames (names only, no
// WHOIS) and hands the collected zone names to ReconcileCloudflareZones, which
// additively registers newly appearing root domains as source="cloudflare" and
// keeps the existing ones (requirement 2.5: cloudflare root domains that no
// longer appear are retained by default — never deleted or disabled here).
//
// Robustness: collectCloudflareZoneNames nil-guards thirdpartDNSService/dnsRepo
// and isolates a single config's ScanZones failure (continue) so it does not
// stop the others; this function only additionally guards domainExpiryService,
// preserving the original combined nil-guard behavior.
func (s *SchedulerService) reconcileRootDomainsFromCloudflare(ctx context.Context) {
	if s.domainExpiryService == nil {
		return
	}
	zoneNames := s.collectCloudflareZoneNames(ctx)
	if err := s.domainExpiryService.ReconcileCloudflareZones(ctx, zoneNames); err != nil {
		log.Printf("[Scheduler] reconcile root domains error: %v", err)
	}
}

// cleanupZoneOnlyCloudflareDomainsOnce removes leftover TLS/SSL monitor rows
// that were previously created by a now-removed feature
// (reconcileApexDomainMonitorsFromCloudflare): it added a domains row for
// EVERY Cloudflare Zone's root domain purely because the Zone existed in
// Cloudflare, without checking whether that hostname actually resolves via any
// A/AAAA/CNAME record. That was a design error — TLS monitoring only makes
// sense for a hostname that has a real DNS record backing it, which is exactly
// what runDNSSyncAll (via ThirdpartDNSService.syncToLocalDomains) already
// handles; "being a Zone/root domain" is an unrelated concept that belongs to
// registration-expiry monitoring (root_domains), not TLS monitoring (domains).
//
// This cleanup is idempotent and runs via sync.Once — at most once per process
// lifetime — because DomainRepository.DeleteZoneOnlyCloudflareRecords itself
// deletes everything matching the leftover signature in one call; there is
// nothing left to clean up on later dnsWorker triggers within the same run.
// Nil-guarded: a no-op if domainRepo has not been wired up (see SetDomainRepo).
func (s *SchedulerService) cleanupZoneOnlyCloudflareDomainsOnce(ctx context.Context) {
	if s.domainRepo == nil {
		return
	}
	s.zoneOnlyCleanupOnce.Do(func() {
		removed, err := s.domainRepo.DeleteZoneOnlyCloudflareRecords(ctx)
		if err != nil {
			log.Printf("[Scheduler] cleanup of zone-only cloudflare domain monitors failed: %v", err)
			return
		}
		if removed > 0 {
			log.Printf("[Scheduler] removed %d leftover zone-only cloudflare domain monitor(s) (no backing DNS record)", removed)
		}
	})
}

// RunCleanup removes old records from alerts, sync_logs, audit_logs, and domain_monitor_results.
// Logic: delete records older than RetentionDays, but always keep at least MinKeepCount per table.
// If RetentionDays <= 0, cleanup is disabled.
func (s *SchedulerService) RunCleanup(ctx context.Context) error {
	cfg := s.runtimeCfg.Get().Cleanup
	if cfg.RetentionDays <= 0 {
		return nil // cleanup disabled
	}

	cutoff := time.Now().UTC().Add(-time.Duration(cfg.RetentionDays) * 24 * time.Hour).Format(time.RFC3339)
	minKeep := cfg.MinKeepCount
	if minKeep <= 0 {
		minKeep = 1000
	}

	// Each table: delete WHERE created_at < cutoff AND id NOT IN (top N newest)
	tables := []struct {
		name      string
		timeCol   string
		parentCol string // for domain_monitor_results we use domain_id grouping? No, just global retention.
	}{
		{name: "alerts", timeCol: "created_at"},
		{name: "thirdpart_dns_sync_logs", timeCol: "synced_at"},
		{name: "audit_logs", timeCol: "created_at"},
		{name: "domain_monitor_results", timeCol: "checked_at"},
	}

	for _, t := range tables {
		// Count total records
		var total int
		if err := s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", t.name)).Scan(&total); err != nil {
			log.Printf("[Cleanup] Failed to count %s: %v", t.name, err)
			continue
		}

		// If total <= minKeep, nothing to delete regardless of age
		if total <= minKeep {
			continue
		}

		// Delete old records but preserve at least minKeep newest ones.
		// Strategy: DELETE WHERE timeCol < cutoff AND id NOT IN (SELECT id ORDER BY timeCol DESC LIMIT minKeep)
		query := fmt.Sprintf(
			`DELETE FROM %s WHERE %s < ? AND id NOT IN (SELECT id FROM %s ORDER BY %s DESC LIMIT ?)`,
			t.name, t.timeCol, t.name, t.timeCol,
		)
		result, err := s.db.ExecContext(ctx, query, cutoff, minKeep)
		if err != nil {
			log.Printf("[Cleanup] Failed to clean %s: %v", t.name, err)
			continue
		}
		if affected, _ := result.RowsAffected(); affected > 0 {
			log.Printf("[Cleanup] Deleted %d old records from %s", affected, t.name)
		}
	}

	return nil
}

// CheckRenewals checks all certificates with auto_renew=true and triggers
// renewal for those expiring within default_before_days.
// Also sends expiry alerts for ALL certificates that are expiring soon.
func (s *SchedulerService) CheckRenewals(ctx context.Context) error {
	beforeDays := s.runtimeCfg.Get().Alert.DefaultBeforeDays
	if beforeDays <= 0 {
		beforeDays = 15
	}

	// Get all certificates expiring within the threshold
	certs, err := s.certRepo.ListExpiringSoon(ctx, beforeDays)
	if err != nil {
		return fmt.Errorf("failed to list expiring certificates: %w", err)
	}

	// First pass: send expiry alerts for ALL expiring certificates (regardless of auto_renew or source)
	if s.alertSender != nil {
		for _, cert := range certs {
			daysRemaining := int(time.Until(cert.ExpireAt).Hours() / 24)

			// Distinguish between already expired and expiring soon
			if daysRemaining <= 0 {
				// Already expired - send critical alert
				alertContent := fmt.Sprintf(
					"Certificate %s (%s) has EXPIRED (expired %d days ago). Immediate action required.",
					cert.Name, cert.ID, -daysRemaining,
				)
				if err := s.alertSender.SendAlert(
					ctx, "critical", "cert_expired",
					"Certificate Expired",
					alertContent, "certificate", cert.ID,
				); err != nil {
					log.Printf("[Scheduler] Failed to send expired alert for cert %s: %v", cert.ID, err)
				}
			} else {
				// Expiring soon - send warning alert
				var alertContent string
				if cert.AutoRenew {
					alertContent = fmt.Sprintf(
						"Certificate %s (%s) expires in %d days. Auto-renewal is enabled.",
						cert.Name, cert.ID, daysRemaining,
					)
				} else {
					alertContent = fmt.Sprintf(
						"Certificate %s (%s) expires in %d days. Auto-renew is disabled - manual action required.",
						cert.Name, cert.ID, daysRemaining,
					)
				}
				if err := s.alertSender.SendAlert(
					ctx, "warning", "cert_expiring",
					"Certificate Expiring Soon",
					alertContent, "certificate", cert.ID,
				); err != nil {
					log.Printf("[Scheduler] Failed to send expiry alert for cert %s: %v", cert.ID, err)
				}
			}
		}
	}

	// Second pass: handle expiring certificates that require an action.
	renewalCandidates := make([]*model.Certificate, 0)
	renewalCandidateIDs := make(map[string]struct{})
	for _, cert := range certs {
		if !cert.AutoRenew {
			continue
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		switch cert.Source {
		case "certbot_cloudflare_dns":
			if isFailedRenewalStatus(cert.RenewStatus) {
				// Failed renewals use the persisted once-per-day retry path below,
				// even when the certificate is inside the normal expiry window.
				continue
			}
			renewalCandidates = append(renewalCandidates, cert)
			renewalCandidateIDs[cert.ID] = struct{}{}
		case "certbot_manual_dns":
			s.handleManualDNSReminder(ctx, cert)
		case "upload":
			// Cannot auto-renew uploaded certificates - send a specific warning
			s.handleUploadAutoRenewWarning(ctx, cert)
		}
	}

	// Retry a failed Cloudflare renewal only after a full day. updateRenewStatus
	// persists the failure time in updated_at, so this survives process restarts
	// without adding an in-memory timer or a new database column.
	retryBefore := time.Now().UTC().Add(-24 * time.Hour)
	failedCerts, err := s.certRepo.ListFailedAutoRenewal(ctx, retryBefore)
	if err != nil {
		return fmt.Errorf("failed to list failed automatic renewals: %w", err)
	}
	for _, cert := range failedCerts {
		if _, exists := renewalCandidateIDs[cert.ID]; exists {
			continue
		}
		renewalCandidates = append(renewalCandidates, cert)
		renewalCandidateIDs[cert.ID] = struct{}{}
	}

	for _, cert := range renewalCandidates {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		s.handleCloudflareRenewal(ctx, cert)
	}

	return nil
}

// handleCloudflareRenewal attempts to auto-renew a certificate using Certbot + Cloudflare DNS.
func (s *SchedulerService) handleCloudflareRenewal(ctx context.Context, cert *model.Certificate) {
	if !s.beginRenewal(cert.ID) {
		log.Printf("[Scheduler] Skipping renewal for certificate %s because another renewal is in progress", cert.ID)
		return
	}
	defer s.finishRenewal(cert.ID)

	s.updateRenewStatus(ctx, cert.ID, "renewing")

	var lastErr error

	for attempt := 0; attempt <= s.MaxRetries; attempt++ {
		if attempt > 0 {
			// Wait before retry
			select {
			case <-ctx.Done():
				return
			case <-time.After(s.RetryInterval):
			}
			log.Printf("[Scheduler] Retrying renewal for certificate %s (attempt %d/%d)", cert.ID, attempt+1, s.MaxRetries)
		}

		err := s.renewCloudflare(ctx, cert)
		if err == nil {
			s.recordRenewalSuccess(ctx, cert, "automatically")
			return
		}

		lastErr = err
		log.Printf("[Scheduler] Renewal attempt %d failed for certificate %s: %v", attempt+1, cert.ID, err)
	}

	// All retries exhausted - record error and send alert
	errMsg := fmt.Sprintf("renewal failed after %d attempts: %v", s.MaxRetries+1, lastErr)
	s.updateRenewStatus(ctx, cert.ID, "failed: "+errMsg)

	if s.alertSender != nil {
		alertContent := fmt.Sprintf("Certificate %s (%s) auto-renewal failed: %s", cert.Name, cert.ID, lastErr)
		if err := s.alertSender.SendAlert(ctx, "critical", "cert_renew_failed", "Certificate Renewal Failed", alertContent, "certificate", cert.ID); err != nil {
			log.Printf("[Scheduler] Failed to send renewal failure alert: %v", err)
		}
	}
}

// RenewCertificate manually renews one Cloudflare DNS certificate immediately.
// It performs one Certbot attempt so the HTTP request does not block through
// the scheduler's delayed retry loop; a failure is persisted and retried by
// the scheduler no sooner than 24 hours later.
func (s *SchedulerService) RenewCertificate(ctx context.Context, certID string) (*model.Certificate, error) {
	cert, err := s.certRepo.GetByID(ctx, certID)
	if err != nil {
		return nil, err
	}
	if cert.Source != "certbot_cloudflare_dns" {
		return nil, ErrManualRenewalUnsupported
	}
	if !s.beginRenewal(cert.ID) {
		return nil, ErrCertificateRenewalInProgress
	}
	defer s.finishRenewal(cert.ID)

	s.updateRenewStatus(ctx, cert.ID, "renewing")
	if err := s.renewCloudflare(ctx, cert); err != nil {
		s.updateRenewStatus(ctx, cert.ID, "failed: manual renewal failed: "+err.Error())
		return nil, fmt.Errorf("manual renewal failed: %w", err)
	}

	s.recordRenewalSuccess(ctx, cert, "manually")
	renewedCert, err := s.certRepo.GetByID(ctx, cert.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get renewed certificate: %w", err)
	}
	return renewedCert, nil
}

func (s *SchedulerService) beginRenewal(certID string) bool {
	s.renewalMu.Lock()
	defer s.renewalMu.Unlock()

	if s.renewing == nil {
		s.renewing = make(map[string]struct{})
	}
	if _, exists := s.renewing[certID]; exists {
		return false
	}
	s.renewing[certID] = struct{}{}
	return true
}

func (s *SchedulerService) finishRenewal(certID string) {
	s.renewalMu.Lock()
	defer s.renewalMu.Unlock()
	delete(s.renewing, certID)
}

func (s *SchedulerService) recordRenewalSuccess(ctx context.Context, cert *model.Certificate, mode string) {
	s.updateRenewStatus(ctx, cert.ID, "success")
	log.Printf("[Scheduler] Successfully %s renewed certificate %s", mode, cert.ID)
	if s.alertSender == nil {
		return
	}
	s.alertSender.AutoResolve(ctx, "certificate", cert.ID, "cert_expiring")
	s.alertSender.AutoResolve(ctx, "certificate", cert.ID, "cert_expired")
	s.alertSender.AutoResolve(ctx, "certificate", cert.ID, "cert_renew_failed")
}

func isFailedRenewalStatus(status string) bool {
	return status == "failed" || strings.HasPrefix(status, "failed:")
}

// renewCloudflare performs the actual Certbot Cloudflare DNS renewal.
func (s *SchedulerService) renewCloudflare(ctx context.Context, cert *model.Certificate) error {
	if s.certbotRenewer == nil {
		return fmt.Errorf("certbot renewer is not configured")
	}

	// Get the Cloudflare API token from the associated thirdpart_dns config
	cloudflareToken, err := s.getCloudflareToken(ctx, cert.ThirdpartDNSID)
	if err != nil {
		return fmt.Errorf("failed to get cloudflare token: %w", err)
	}

	// Issue new certificate via Certbot
	result, err := s.certbotRenewer.IssueCertCloudflare(ctx, cert.Domains, s.runtimeCfg.Get().Certbot.Email, cloudflareToken)
	if err != nil {
		return fmt.Errorf("certbot issuance failed: %w", err)
	}

	// Overwrite certificate files
	if err := s.certRepo.SaveCertFiles(
		cert.ID,
		result.CertFiles.CertPEM,
		result.CertFiles.ChainPEM,
		result.CertFiles.FullchainPEM,
		result.CertFiles.PrivateKeyPEM,
	); err != nil {
		return fmt.Errorf("failed to save renewed certificate files: %w", err)
	}

	// Parse new certificate metadata
	meta, err := s.certService.ParsePEM(result.CertFiles.FullchainPEM)
	if err != nil {
		return fmt.Errorf("failed to parse renewed certificate: %w", err)
	}

	// Update certificate metadata in database
	now := time.Now().UTC()
	updates := map[string]interface{}{
		"domains":            meta.Domains,
		"expire_at":          meta.ExpireAt,
		"issuer":             meta.Issuer,
		"fingerprint_sha256": meta.FingerprintSHA256,
		"chain_valid":        meta.ChainValid,
		"last_renew_at":      now,
		"renew_status":       "success",
	}

	if err := s.certRepo.Update(ctx, cert.ID, updates); err != nil {
		return fmt.Errorf("failed to update certificate metadata: %w", err)
	}

	// Mark all associated machine_certificates as pending sync
	if err := s.certService.MarkAssociatedPendingSync(ctx, cert.ID); err != nil {
		return fmt.Errorf("failed to mark associated machine certificates as pending: %w", err)
	}

	return nil
}

// handleManualDNSReminder sends a reminder notification for manual DNS certificates
// that are expiring soon but cannot be auto-renewed.
func (s *SchedulerService) handleManualDNSReminder(ctx context.Context, cert *model.Certificate) {
	if s.alertSender == nil {
		return
	}

	daysRemaining := int(time.Until(cert.ExpireAt).Hours() / 24)
	alertContent := fmt.Sprintf(
		"Certificate %s (%s) expires in %d days. Source is certbot_manual_dns - manual renewal required.",
		cert.Name, cert.ID, daysRemaining,
	)

	if err := s.alertSender.SendAlert(
		ctx, "warning", "cert_expiring_manual_dns",
		"Certificate Expiring - Manual Renewal Required",
		alertContent, "certificate", cert.ID,
	); err != nil {
		log.Printf("[Scheduler] Failed to send manual DNS reminder: %v", err)
	}
}

// handleUploadAutoRenewWarning sends a warning for uploaded certificates that have
// auto_renew=true but cannot actually be auto-renewed (since they were manually uploaded).
func (s *SchedulerService) handleUploadAutoRenewWarning(ctx context.Context, cert *model.Certificate) {
	if s.alertSender == nil {
		return
	}

	daysRemaining := int(time.Until(cert.ExpireAt).Hours() / 24)
	alertContent := fmt.Sprintf(
		"Certificate %s (%s) expires in %d days. Auto-renew is enabled but this is an uploaded certificate that cannot be auto-renewed. Please renew manually and re-upload.",
		cert.Name, cert.ID, daysRemaining,
	)

	if err := s.alertSender.SendAlert(
		ctx, "warning", "cert_upload_cannot_autorenew",
		"Cannot Auto-Renew Uploaded Certificate",
		alertContent, "certificate", cert.ID,
	); err != nil {
		log.Printf("[Scheduler] Failed to send upload auto-renew warning: %v", err)
	}
}

// getCloudflareToken retrieves the Cloudflare API token from the thirdpart_dns table.
func (s *SchedulerService) getCloudflareToken(ctx context.Context, thirdpartDNSID string) (string, error) {
	if thirdpartDNSID == "" {
		return "", fmt.Errorf("certificate has no associated thirdpart_dns_id")
	}

	var apiToken string
	err := s.db.QueryRowContext(ctx,
		"SELECT api_token FROM thirdpart_dns WHERE id = ? AND enabled = 1",
		thirdpartDNSID,
	).Scan(&apiToken)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("thirdpart_dns config %s not found or disabled", thirdpartDNSID)
		}
		return "", fmt.Errorf("failed to query thirdpart_dns: %w", err)
	}

	return apiToken, nil
}

// updateRenewStatus updates the renew_status field of a certificate.
func (s *SchedulerService) updateRenewStatus(ctx context.Context, certID, status string) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		"UPDATE certificates SET renew_status = ?, updated_at = ? WHERE id = ?",
		status, now, certID,
	)
	if err != nil {
		log.Printf("[Scheduler] Failed to update renew_status for %s: %v", certID, err)
	}
}

// CheckHeartbeatTimeouts checks all machines for heartbeat timeouts.
// Machines that haven't sent a heartbeat within heartbeat_timeout_seconds are marked offline.
// Sends an alert for each machine that transitions to offline.
func (s *SchedulerService) CheckHeartbeatTimeouts(ctx context.Context) error {
	timeoutSeconds := s.runtimeCfg.Get().Agent.HeartbeatTimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 120
	}

	cutoff := time.Now().UTC().Add(-time.Duration(timeoutSeconds) * time.Second)
	now := time.Now().UTC().Format(time.RFC3339)

	// First, find machines that will go offline (for alerting)
	if s.machineRepo != nil && s.alertSender != nil {
		offlineMachines, err := s.machineRepo.ListByHeartbeatBefore(ctx, cutoff)
		if err != nil {
			log.Printf("[Scheduler] Failed to list machines going offline: %v", err)
		} else {
			for _, machine := range offlineMachines {
				alertContent := fmt.Sprintf(
					"Machine %s (%s, IP: %s) has gone offline. Last heartbeat was more than %d seconds ago.",
					machine.Name, machine.ID, machine.IP, timeoutSeconds,
				)
				if err := s.alertSender.SendAlert(
					ctx, "warning", "agent_offline",
					"Agent Offline",
					alertContent, "machine", machine.ID,
				); err != nil {
					log.Printf("[Scheduler] Failed to send agent offline alert for machine %s: %v", machine.ID, err)
				}
			}
		}
	}

	// Mark machines as offline if their last heartbeat is before the cutoff
	_, err := s.db.ExecContext(ctx,
		`UPDATE machines SET status = 'offline', updated_at = ? 
		 WHERE status = 'online' AND last_heartbeat_at IS NOT NULL AND last_heartbeat_at < ?`,
		now, cutoff.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("failed to update offline machines: %w", err)
	}

	// Auto-resolve agent_offline alerts for machines that are currently online
	if s.alertSender != nil && s.machineRepo != nil {
		onlineMachines, err := s.machineRepo.ListByStatus(ctx, "online")
		if err == nil {
			for _, m := range onlineMachines {
				s.alertSender.AutoResolve(ctx, "machine", m.ID, "agent_offline")
			}
		}
	}

	return nil
}

// RunDomainMonitor probes all enabled domain monitors.
func (s *SchedulerService) RunDomainMonitor(ctx context.Context) error {
	if s.domainMonitorService == nil {
		return nil
	}
	return s.domainMonitorService.ProbeAll(ctx)
}
