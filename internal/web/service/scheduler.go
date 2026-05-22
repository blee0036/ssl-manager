package service

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/certbot"
	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// AlertSender is an interface for sending alert notifications.
type AlertSender interface {
	SendAlert(ctx context.Context, level, alertType, title, content, targetType, targetID string) error
	AutoResolve(ctx context.Context, targetType, targetID, alertType string)
}

// CertbotRenewer is an interface for issuing certificates via Certbot.
// This allows mocking in tests.
type CertbotRenewer interface {
	IssueCertCloudflare(ctx context.Context, domains []string, email string, cloudflareToken string) (*certbot.CertbotResult, error)
}

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

	// Scheduler control
	mu       sync.Mutex
	running  bool
	stopCh   chan struct{}
	wg       sync.WaitGroup

	// Retry configuration
	MaxRetries    int
	RetryInterval time.Duration
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

// Start begins the scheduler's periodic execution loop.
func (s *SchedulerService) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil // Already running, no-op
	}
	s.running = true
	s.stopCh = make(chan struct{})
	s.mu.Unlock()

	s.wg.Add(1)
	go s.run(ctx)

	log.Println("[Scheduler] Started")
	return nil
}

// Stop stops the scheduler and waits for the current cycle to finish.
func (s *SchedulerService) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil // Already stopped, no-op
	}
	s.running = false
	close(s.stopCh)
	s.mu.Unlock()

	s.wg.Wait()
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
func (s *SchedulerService) run(ctx context.Context) {
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

	for {
		select {
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		case <-renewalTicker.C:
			if err := s.CheckRenewals(ctx); err != nil {
				log.Printf("[Scheduler] CheckRenewals error: %v", err)
			}
		case <-heartbeatTicker.C:
			if err := s.CheckHeartbeatTimeouts(ctx); err != nil {
				log.Printf("[Scheduler] CheckHeartbeatTimeouts error: %v", err)
			}
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
		}
	}
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

	// Second pass: handle renewals for auto_renew certs only
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
			s.handleCloudflareRenewal(ctx, cert)
		case "certbot_manual_dns":
			s.handleManualDNSReminder(ctx, cert)
		case "upload":
			// Cannot auto-renew uploaded certificates - send a specific warning
			s.handleUploadAutoRenewWarning(ctx, cert)
		}
	}

	return nil
}

// handleCloudflareRenewal attempts to auto-renew a certificate using Certbot + Cloudflare DNS.
func (s *SchedulerService) handleCloudflareRenewal(ctx context.Context, cert *model.Certificate) {
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
			// Renewal succeeded
			s.updateRenewStatus(ctx, cert.ID, "success")
			log.Printf("[Scheduler] Successfully renewed certificate %s", cert.ID)
			// Auto-resolve any expiring/expired alerts for this certificate
			if s.alertSender != nil {
				s.alertSender.AutoResolve(ctx, "certificate", cert.ID, "cert_expiring")
				s.alertSender.AutoResolve(ctx, "certificate", cert.ID, "cert_expired")
				s.alertSender.AutoResolve(ctx, "certificate", cert.ID, "cert_renew_failed")
			}
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
		"expire_at":         meta.ExpireAt,
		"issuer":            meta.Issuer,
		"fingerprint_sha256": meta.FingerprintSHA256,
		"chain_valid":       meta.ChainValid,
		"last_renew_at":     now,
		"renew_status":      "success",
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
