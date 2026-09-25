package service

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// TLSDialer is an interface for performing TLS connections, allowing mocking in tests.
type TLSDialer interface {
	DialTLS(ctx context.Context, addr string, config *tls.Config) (*tls.Conn, error)
}

// DNSResolver is an interface for DNS resolution, allowing mocking in tests.
type DNSResolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
}

// defaultTLSDialer implements TLSDialer using the standard library.
type defaultTLSDialer struct{}

func (d *defaultTLSDialer) DialTLS(ctx context.Context, addr string, config *tls.Config) (*tls.Conn, error) {
	dialer := &tls.Dialer{
		Config: config,
	}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	return conn.(*tls.Conn), nil
}

// defaultDNSResolver implements DNSResolver using the standard library.
type defaultDNSResolver struct{}

func (r *defaultDNSResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, host)
}

// DomainMonitorService handles domain monitoring business logic.
type DomainMonitorService struct {
	domainRepo *repository.DomainRepository
	certRepo   *repository.CertificateRepository
	tlsDialer  TLSDialer
	resolver   DNSResolver
	alerter    AlertSender
	runtimeCfg *config.RuntimeConfig
}

// NewDomainMonitorService creates a new DomainMonitorService.
func NewDomainMonitorService(
	domainRepo *repository.DomainRepository,
	certRepo *repository.CertificateRepository,
	alerter AlertSender,
	runtimeCfg *config.RuntimeConfig,
) *DomainMonitorService {
	return &DomainMonitorService{
		domainRepo: domainRepo,
		certRepo:   certRepo,
		tlsDialer:  &defaultTLSDialer{},
		resolver:   &defaultDNSResolver{},
		alerter:    alerter,
		runtimeCfg: runtimeCfg,
	}
}

// defaultPort returns the configured default monitor port.
func (s *DomainMonitorService) defaultPort() int {
	if s.runtimeCfg != nil {
		port := s.runtimeCfg.Get().DomainMonitor.DefaultPort
		if port > 0 {
			return port
		}
	}
	return 443
}

// monitorCfg returns the current domain monitor settings.
//
// The runtime config is re-read on every call so a change saved through
// PUT /api/system/config takes effect on the next probe without a restart. A nil
// runtimeCfg falls back to the built-in defaults, matching defaultPort() above — the
// damping is therefore on by default and cannot be silently lost by a wiring mistake.
func (s *DomainMonitorService) monitorCfg() config.DomainMonitorConfig {
	if s.runtimeCfg == nil {
		return config.DefaultConfig().DomainMonitor
	}
	return s.runtimeCfg.Get().DomainMonitor
}

// withRetry runs attempt up to 1+probe_retries times and returns the error of the last
// attempt if all of them fail.
//
// Each attempt gets its own timeout derived from domain_monitor.timeout_seconds, so a
// retry is a genuinely fresh chance rather than a share of one shrinking deadline. This is
// the first of the two damping layers: transient trouble (a lost SYN, a momentarily
// overloaded origin, the "context deadline exceeded" handshake timeouts that dominated the
// alert noise) is absorbed here and never even reaches the result table.
func (s *DomainMonitorService) withRetry(ctx context.Context, attempt func(context.Context) error) error {
	cfg := s.monitorCfg()
	timeout := cfg.EffectiveTimeout()
	retries := cfg.EffectiveProbeRetries()
	delay := cfg.EffectiveRetryDelay()

	var lastErr error
	for i := 0; i <= retries; i++ {
		if i > 0 && delay > 0 {
			// Wait before retrying, but give up immediately if the caller is shutting
			// down — ProbeAll runs on the scheduler goroutine.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		err := attempt(attemptCtx)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err

		// Once the parent context is done, later attempts cannot succeed either.
		if ctx.Err() != nil {
			return lastErr
		}
	}

	return lastErr
}

// resolveWithRetry resolves a domain name to IPs, retrying transient DNS failures.
//
// An empty answer with a nil error is treated as a failure rather than a success: the
// caller indexes ips[0], so letting it through would panic.
func (s *DomainMonitorService) resolveWithRetry(ctx context.Context, host string) ([]string, error) {
	var ips []string

	err := s.withRetry(ctx, func(attemptCtx context.Context) error {
		found, attemptErr := s.resolver.LookupHost(attemptCtx, host)
		if attemptErr != nil {
			return attemptErr
		}
		if len(found) == 0 {
			return fmt.Errorf("no addresses returned for %s", host)
		}
		ips = found
		return nil
	})
	if err != nil {
		return nil, err
	}

	return ips, nil
}

// dialWithRetry performs the TLS handshake, retrying transient failures.
//
// Only the successful attempt yields a connection; failed attempts return an error and no
// connection, so there is nothing to leak between iterations. Cancelling an attempt's
// context after a successful handshake is safe — the context only bounds dialling.
func (s *DomainMonitorService) dialWithRetry(ctx context.Context, addr string, tlsConfig *tls.Config) (*tls.Conn, error) {
	var conn *tls.Conn

	err := s.withRetry(ctx, func(attemptCtx context.Context) error {
		c, attemptErr := s.tlsDialer.DialTLS(attemptCtx, addr, tlsConfig)
		if attemptErr != nil {
			return attemptErr
		}
		conn = c
		return nil
	})
	if err != nil {
		return nil, err
	}

	return conn, nil
}

// maybeAlert sends a domain monitoring alert only once the failure has persisted across
// domain_monitor.alert_after_failures consecutive probe rounds.
//
// This is the second damping layer. Without it a single failed round produced a warning
// push followed by a [Recovered] push on the next round, so every transient blip cost two
// notifications and the alert was already stale by the time anyone read it.
//
// The streak is read back from domain_monitor_results, which means the caller must have
// saved the current probe's result before calling this.
func (s *DomainMonitorService) maybeAlert(ctx context.Context, domain *model.Domain, alertType, message string) {
	// Mirrors triggerAlert's guards, so an ignored domain costs no extra query.
	if domain.AlertIgnored || s.alerter == nil {
		return
	}

	threshold := s.monitorCfg().EffectiveAlertAfterFailures()
	if threshold > 1 {
		streak, err := s.domainRepo.CountConsecutiveMonitorFailures(ctx, domain.ID, threshold)
		switch {
		case err != nil:
			// Fail open: a bookkeeping failure must not swallow a real outage alert.
			log.Printf("[DomainMonitor] Failed to count consecutive failures for %s, alerting anyway: %v",
				domain.Name, err)
		case streak < threshold:
			log.Printf("[DomainMonitor] Holding %s alert for %s (%d/%d consecutive failed probes)",
				alertType, domain.Name, streak, threshold)
			return
		}
	}

	s.triggerAlert(ctx, domain, alertType, message)
}

// SetTLSDialer sets a custom TLS dialer (for testing).
func (s *DomainMonitorService) SetTLSDialer(dialer TLSDialer) {
	s.tlsDialer = dialer
}

// SetDNSResolver sets a custom DNS resolver (for testing).
func (s *DomainMonitorService) SetDNSResolver(resolver DNSResolver) {
	s.resolver = resolver
}

// GetByID retrieves a domain monitor by ID.
func (s *DomainMonitorService) GetByID(ctx context.Context, id string) (*model.Domain, error) {
	return s.domainRepo.GetByID(ctx, id)
}

// Create creates a new domain monitor.
func (s *DomainMonitorService) Create(ctx context.Context, input model.CreateDomainInput) (*model.Domain, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, fmt.Errorf("domain name cannot be empty")
	}

	domain := &model.Domain{
		Name:                       input.Name,
		Source:                     "manual",
		MonitorPort:                input.MonitorPort,
		LinkedMachineID:            input.LinkedMachineID,
		LinkedCertificateID:        input.LinkedCertificateID,
		LinkedMachineCertificateID: input.LinkedMachineCertificateID,
		MonitorEnabled:             true,
	}

	if domain.MonitorPort == 0 {
		domain.MonitorPort = s.defaultPort()
	}

	if err := s.domainRepo.Create(ctx, domain); err != nil {
		return nil, fmt.Errorf("failed to create domain: %w", err)
	}

	return domain, nil
}

// Update updates an existing domain monitor.
func (s *DomainMonitorService) Update(ctx context.Context, id string, input model.UpdateDomainInput) (*model.Domain, error) {
	updates := make(map[string]interface{})

	if input.MonitorPort != nil {
		updates["monitor_port"] = *input.MonitorPort
	}
	if input.LinkedMachineID != nil {
		updates["linked_machine_id"] = *input.LinkedMachineID
	}
	if input.LinkedCertificateID != nil {
		updates["linked_certificate_id"] = *input.LinkedCertificateID
	}
	if input.LinkedMachineCertificateID != nil {
		updates["linked_machine_certificate_id"] = *input.LinkedMachineCertificateID
	}
	if input.MonitorEnabled != nil {
		updates["monitor_enabled"] = *input.MonitorEnabled
	}
	if input.AlertIgnored != nil {
		updates["alert_ignored"] = *input.AlertIgnored
	}

	// If alert_ignored is being set to true, suppress active alerts first.
	// Order: suppress → update. If suppress fails, alert_ignored won't be persisted.
	if input.AlertIgnored != nil && *input.AlertIgnored {
		if s.alerter != nil {
			if err := s.alerter.SuppressActiveByTarget(ctx, "domain", id); err != nil {
				return nil, fmt.Errorf("failed to suppress active alerts: %w", err)
			}
		}
	}

	if err := s.domainRepo.Update(ctx, id, updates); err != nil {
		return nil, fmt.Errorf("failed to update domain: %w", err)
	}

	return s.domainRepo.GetByID(ctx, id)
}

// Delete deletes a domain monitor.
func (s *DomainMonitorService) Delete(ctx context.Context, id string) error {
	if err := s.domainRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("failed to delete domain: %w", err)
	}
	return nil
}

// List returns domain monitors with optional filtering.
func (s *DomainMonitorService) List(ctx context.Context, filter model.DomainFilter) ([]*model.Domain, error) {
	return s.domainRepo.List(ctx, filter)
}

// ListWithSort delegates to DomainRepository.ListWithSort for server-side sort/filter/pagination.
// Handler layer attaches latest monitor results separately.
func (s *DomainMonitorService) ListWithSort(ctx context.Context, params model.DomainListParams) ([]*model.Domain, int, error) {
	return s.domainRepo.ListWithSort(ctx, params)
}

// GetLatestMonitorResult retrieves the most recent monitor result for a domain.
func (s *DomainMonitorService) GetLatestMonitorResult(ctx context.Context, domainID string) (*model.DomainMonitorResult, error) {
	return s.domainRepo.GetLatestMonitorResult(ctx, domainID)
}

// GetLatestMonitorResultsBatch retrieves the latest monitor results for multiple domains in one query.
func (s *DomainMonitorService) GetLatestMonitorResultsBatch(ctx context.Context, domainIDs []string) (map[string]*model.DomainMonitorResult, error) {
	return s.domainRepo.GetLatestMonitorResultsBatch(ctx, domainIDs)
}

// Probe performs a TLS handshake probe for a specific domain.
func (s *DomainMonitorService) Probe(ctx context.Context, domainID string) (*model.DomainMonitorResult, error) {
	domain, err := s.domainRepo.GetByID(ctx, domainID)
	if err != nil {
		return nil, fmt.Errorf("failed to get domain: %w", err)
	}

	result := &model.DomainMonitorResult{
		DomainID:    domain.ID,
		CheckedPort: domain.MonitorPort,
		CheckedAt:   time.Now().UTC(),
	}

	// Step 1: DNS resolve (retried within this round, see withRetry)
	ips, err := s.resolveWithRetry(ctx, domain.Name)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("DNS resolution failed: %v", err)
		result.TLSSuccess = false

		// Save result
		if saveErr := s.domainRepo.SaveMonitorResult(ctx, result); saveErr != nil {
			return nil, fmt.Errorf("failed to save monitor result: %w", saveErr)
		}

		// Alert only once this failure has repeated enough times (see maybeAlert).
		// The result row above is part of that count, so it must be saved first.
		s.maybeAlert(ctx, domain, "dns_resolve_failed", result.ErrorMessage)

		return result, nil
	}
	result.ResolvedIPs = ips

	// Step 2: TLS dial with SNI
	addr := fmt.Sprintf("%s:%d", ips[0], domain.MonitorPort)
	tlsConfig := &tls.Config{
		ServerName:         domain.Name,
		InsecureSkipVerify: true, // We verify manually to get full cert info
	}

	conn, err := s.dialWithRetry(ctx, addr, tlsConfig)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("TLS handshake failed: %v", err)
		result.TLSSuccess = false

		// Save result
		if saveErr := s.domainRepo.SaveMonitorResult(ctx, result); saveErr != nil {
			return nil, fmt.Errorf("failed to save monitor result: %w", saveErr)
		}

		// Alert only once this failure has repeated enough times (see maybeAlert).
		s.maybeAlert(ctx, domain, "tls_handshake_failed", result.ErrorMessage)

		return result, nil
	}
	defer conn.Close()

	// Step 3: Extract certificate info
	result.TLSSuccess = true
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) > 0 {
		leaf := certs[0]

		// Fingerprint
		fingerprint := sha256.Sum256(leaf.Raw)
		result.CertificateFingerprintSHA256 = hex.EncodeToString(fingerprint[:])

		// Issuer
		result.Issuer = leaf.Issuer.CommonName

		// Expiry
		expireAt := leaf.NotAfter.UTC()
		result.ExpireAt = &expireAt
		daysRemaining := int(time.Until(expireAt).Hours() / 24)
		result.DaysRemaining = &daysRemaining

		// Domain match
		result.DomainMatched = verifyCertDomainMatch(leaf, domain.Name)

		// Chain validity
		result.ChainValid = verifyCertChain(certs)
	}

	// Step 4: Compare fingerprint with linked certificate in system.
	//
	// The alert is recorded as pending rather than sent here: maybeAlert counts the
	// current probe as part of the consecutive-failure streak, so it can only run after
	// step 5 has persisted this result. A brief mismatch is expected every time a
	// certificate is deployed (the live cert and the stored cert are out of sync for a
	// moment), which is exactly the kind of flapping the damping is meant to absorb.
	var pendingAlertType, pendingAlertMessage string
	if domain.LinkedCertificateID != "" && result.CertificateFingerprintSHA256 != "" {
		linkedCert, err := s.certRepo.GetByID(ctx, domain.LinkedCertificateID)
		if err == nil && linkedCert.FingerprintSHA256 != result.CertificateFingerprintSHA256 {
			// Fingerprint mismatch - mark as anomaly
			result.ErrorMessage = fmt.Sprintf("fingerprint mismatch: online=%s, system=%s",
				result.CertificateFingerprintSHA256, linkedCert.FingerprintSHA256)

			pendingAlertType = "fingerprint_mismatch"
			pendingAlertMessage = result.ErrorMessage
		}
	}

	// Step 5: Save result
	if err := s.domainRepo.SaveMonitorResult(ctx, result); err != nil {
		return nil, fmt.Errorf("failed to save monitor result: %w", err)
	}

	if pendingAlertType != "" {
		s.maybeAlert(ctx, domain, pendingAlertType, pendingAlertMessage)
	}

	// Auto-resolve alerts if probe was fully successful
	if result.TLSSuccess && result.DomainMatched && result.ErrorMessage == "" && s.alerter != nil {
		s.alerter.AutoResolve(ctx, "domain", domain.ID, "dns_resolve_failed")
		s.alerter.AutoResolve(ctx, "domain", domain.ID, "tls_handshake_failed")
		s.alerter.AutoResolve(ctx, "domain", domain.ID, "fingerprint_mismatch")
	}

	return result, nil
}

// ProbeAll probes all enabled domain monitors.
func (s *DomainMonitorService) ProbeAll(ctx context.Context) error {
	enabled := true
	domains, err := s.domainRepo.List(ctx, model.DomainFilter{MonitorEnabled: &enabled})
	if err != nil {
		return fmt.Errorf("failed to list enabled domains: %w", err)
	}

	for _, domain := range domains {
		// Probe each domain, continue on individual failures
		if _, err := s.Probe(ctx, domain.ID); err != nil {
			// Log error but continue probing other domains
			continue
		}
	}

	return nil
}

// triggerAlert sends an alert for domain monitoring issues.
func (s *DomainMonitorService) triggerAlert(ctx context.Context, domain *model.Domain, alertType, message string) {
	// Skip alert for ignored domains
	if domain.AlertIgnored {
		return
	}

	if s.alerter == nil {
		return
	}

	title := fmt.Sprintf("Domain monitor alert: %s", domain.Name)

	// Best effort - don't fail the probe if alert sending fails
	_ = s.alerter.SendAlert(ctx, "warning", alertType, title, message, "domain", domain.ID)
}

// verifyCertDomainMatch checks if the certificate covers the given domain name.
func verifyCertDomainMatch(cert *x509.Certificate, domain string) bool {
	// Check common name
	if matchDomain(cert.Subject.CommonName, domain) {
		return true
	}

	// Check SANs
	for _, san := range cert.DNSNames {
		if matchDomain(san, domain) {
			return true
		}
	}

	return false
}

// matchDomain checks if a certificate name pattern matches a domain.
// Supports wildcard matching (e.g., *.example.com matches sub.example.com).
func matchDomain(pattern, domain string) bool {
	pattern = strings.ToLower(pattern)
	domain = strings.ToLower(domain)

	if pattern == domain {
		return true
	}

	// Wildcard matching: *.example.com matches sub.example.com but NOT deep.sub.example.com
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[2:] // "example.com"
		// The domain must be exactly one level deeper than the suffix
		// e.g., "sub.example.com" has prefix "sub" before ".example.com"
		if strings.HasSuffix(domain, "."+suffix) {
			prefix := strings.TrimSuffix(domain, "."+suffix)
			// prefix must not contain dots (single level only)
			if !strings.Contains(prefix, ".") {
				return true
			}
		}
	}

	return false
}

// verifyCertChain verifies the certificate chain validity.
func verifyCertChain(certs []*x509.Certificate) bool {
	if len(certs) == 0 {
		return false
	}

	if len(certs) == 1 {
		// Single cert - check if self-signed (root CA) or missing chain
		return certs[0].IsCA
	}

	// Verify each cert in the chain is signed by the next
	for i := 0; i < len(certs)-1; i++ {
		err := certs[i].CheckSignatureFrom(certs[i+1])
		if err != nil {
			return false
		}
	}

	return true
}
