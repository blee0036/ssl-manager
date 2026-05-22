package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/certbot"
	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// --- Mock implementations ---

type mockAlertSender struct {
	alerts []sentAlert
}

type sentAlert struct {
	Level      string
	AlertType  string
	Title      string
	Content    string
	TargetType string
	TargetID   string
}

func (m *mockAlertSender) SendAlert(_ context.Context, level, alertType, title, content, targetType, targetID string) error {
	m.alerts = append(m.alerts, sentAlert{
		Level:      level,
		AlertType:  alertType,
		Title:      title,
		Content:    content,
		TargetType: targetType,
		TargetID:   targetID,
	})
	return nil
}

func (m *mockAlertSender) AutoResolve(_ context.Context, _, _, _ string) {}

type mockCertbotRenewer struct {
	result *certbot.CertbotResult
	err    error
	calls  int
}

func (m *mockCertbotRenewer) IssueCertCloudflare(_ context.Context, _ []string, _ string, _ string) (*certbot.CertbotResult, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

// --- Test helpers ---

func generateSchedulerTestCert(t *testing.T, domains []string, expireIn time.Duration) (certPEM, keyPEM []byte) {
	t.Helper()
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: domains[0]},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(expireIn),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              domains,
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privKey.PublicKey, privKey)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privKey)})
	return certPEM, keyPEM
}

func setupSchedulerTest(t *testing.T) (*SchedulerService, *sql.DB, *repository.CertificateRepository, *CertificateService, *mockAlertSender, *mockCertbotRenewer) {
	t.Helper()
	db := setupTestDB(t)

	// Add thirdpart_dns table needed by scheduler
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS thirdpart_dns (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		type TEXT NOT NULL DEFAULT 'cloudflare',
		api_token TEXT NOT NULL,
		config_json TEXT NOT NULL DEFAULT '{}',
		main_domains TEXT NOT NULL DEFAULT '[]',
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("failed to create thirdpart_dns table: %v", err)
	}

	dataDir := t.TempDir()
	certRepo := repository.NewCertificateRepository(db, dataDir)
	certService := NewCertificateService(certRepo, db)

	cfg := config.DefaultConfig()
	alertSender := &mockAlertSender{}
	renewer := &mockCertbotRenewer{}

	machineRepo := repository.NewMachineRepository(db)
	scheduler := NewSchedulerService(config.NewRuntimeConfig(cfg), certRepo, machineRepo, certService, renewer, alertSender, db)
	scheduler.RetryInterval = 0 // No delay in tests

	return scheduler, db, certRepo, certService, alertSender, renewer
}

// insertCertDirectly inserts a certificate record directly into the DB for testing.
func insertCertDirectly(t *testing.T, db *sql.DB, id, name, source string, domains []string, expireAt time.Time, autoRenew bool, thirdpartDNSID string) {
	t.Helper()
	domainsJSON, _ := json.Marshal(domains)
	now := time.Now().UTC().Format(time.RFC3339)
	autoRenewInt := 0
	if autoRenew {
		autoRenewInt = 1
	}
	_, err := db.Exec(`INSERT INTO certificates (id, name, domains, source, expire_at, auto_renew, issuer, fingerprint_sha256, chain_valid, cert_dir_path, thirdpart_dns_id, renew_status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, name, string(domainsJSON), source, expireAt.UTC().Format(time.RFC3339),
		autoRenewInt, "Test Issuer", "abc123fingerprint", 1, "certificates/"+id,
		thirdpartDNSID, "", now, now,
	)
	if err != nil {
		t.Fatalf("failed to insert test certificate: %v", err)
	}
}

// --- Tests ---

func TestCheckRenewals_SkipsNonAutoRenewCerts(t *testing.T) {
	scheduler, db, _, _, alertSender, renewer := setupSchedulerTest(t)

	// Insert a cert expiring in 5 days but auto_renew=false
	insertCertDirectly(t, db, "cert-no-renew", "No Renew Cert", "certbot_cloudflare_dns",
		[]string{"example.com"}, time.Now().Add(5*24*time.Hour), false, "dns-1")

	ctx := context.Background()
	err := scheduler.CheckRenewals(ctx)
	if err != nil {
		t.Fatalf("CheckRenewals failed: %v", err)
	}

	// Should not have called certbot (no renewal for non-auto-renew)
	if renewer.calls != 0 {
		t.Errorf("expected 0 certbot calls, got %d", renewer.calls)
	}
	// Should have sent 1 expiry alert for the non-auto-renew cert
	if len(alertSender.alerts) != 1 {
		t.Errorf("expected 1 expiry alert, got %d", len(alertSender.alerts))
	}
	if len(alertSender.alerts) > 0 && alertSender.alerts[0].AlertType != "cert_expiring" {
		t.Errorf("expected alert type 'cert_expiring', got '%s'", alertSender.alerts[0].AlertType)
	}
}

func TestCheckRenewals_SkipsUploadCerts(t *testing.T) {
	scheduler, db, _, _, alertSender, renewer := setupSchedulerTest(t)

	// Insert an upload cert expiring soon with auto_renew=true
	insertCertDirectly(t, db, "cert-upload", "Upload Cert", "upload",
		[]string{"upload.com"}, time.Now().Add(5*24*time.Hour), true, "")

	ctx := context.Background()
	err := scheduler.CheckRenewals(ctx)
	if err != nil {
		t.Fatalf("CheckRenewals failed: %v", err)
	}

	if renewer.calls != 0 {
		t.Errorf("expected 0 certbot calls for upload cert, got %d", renewer.calls)
	}
	// Should have 2 alerts: cert_expiring (first pass) + cert_upload_cannot_autorenew (second pass)
	if len(alertSender.alerts) != 2 {
		t.Fatalf("expected 2 alerts for upload cert with auto_renew=true, got %d", len(alertSender.alerts))
	}
	// First alert: expiry warning
	if alertSender.alerts[0].AlertType != "cert_expiring" {
		t.Errorf("expected first alert type 'cert_expiring', got '%s'", alertSender.alerts[0].AlertType)
	}
	// Second alert: cannot auto-renew warning
	if alertSender.alerts[1].AlertType != "cert_upload_cannot_autorenew" {
		t.Errorf("expected second alert type 'cert_upload_cannot_autorenew', got '%s'", alertSender.alerts[1].AlertType)
	}
}

func TestCheckRenewals_SkipsCertsNotExpiringSoon(t *testing.T) {
	scheduler, db, _, _, alertSender, renewer := setupSchedulerTest(t)

	// Insert a cert expiring in 30 days (default threshold is 15)
	insertCertDirectly(t, db, "cert-far", "Far Cert", "certbot_cloudflare_dns",
		[]string{"far.com"}, time.Now().Add(30*24*time.Hour), true, "dns-1")

	ctx := context.Background()
	err := scheduler.CheckRenewals(ctx)
	if err != nil {
		t.Fatalf("CheckRenewals failed: %v", err)
	}

	if renewer.calls != 0 {
		t.Errorf("expected 0 certbot calls, got %d", renewer.calls)
	}
	if len(alertSender.alerts) != 0 {
		t.Errorf("expected 0 alerts, got %d", len(alertSender.alerts))
	}
}

func TestCheckRenewals_ManualDNS_SendsReminder(t *testing.T) {
	scheduler, db, _, _, alertSender, _ := setupSchedulerTest(t)

	// Insert a manual DNS cert expiring in 10 days with auto_renew=true
	insertCertDirectly(t, db, "cert-manual", "Manual DNS Cert", "certbot_manual_dns",
		[]string{"manual.com"}, time.Now().Add(10*24*time.Hour), true, "")

	ctx := context.Background()
	err := scheduler.CheckRenewals(ctx)
	if err != nil {
		t.Fatalf("CheckRenewals failed: %v", err)
	}

	// Should have sent 2 alerts: cert_expiring (first pass) + cert_expiring_manual_dns (second pass)
	if len(alertSender.alerts) != 2 {
		t.Fatalf("expected 2 alerts, got %d", len(alertSender.alerts))
	}
	// First alert: expiry warning
	if alertSender.alerts[0].AlertType != "cert_expiring" {
		t.Errorf("expected first alert type 'cert_expiring', got %q", alertSender.alerts[0].AlertType)
	}
	// Second alert: manual DNS reminder
	alert := alertSender.alerts[1]
	if alert.Level != "warning" {
		t.Errorf("expected alert level 'warning', got %q", alert.Level)
	}
	if alert.AlertType != "cert_expiring_manual_dns" {
		t.Errorf("expected alert type 'cert_expiring_manual_dns', got %q", alert.AlertType)
	}
	if alert.TargetID != "cert-manual" {
		t.Errorf("expected target ID 'cert-manual', got %q", alert.TargetID)
	}
}

func TestCheckRenewals_CloudflareDNS_SuccessfulRenewal(t *testing.T) {
	scheduler, db, certRepo, _, alertSender, renewer := setupSchedulerTest(t)

	// Insert thirdpart_dns config
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`INSERT INTO thirdpart_dns (id, name, type, api_token, config_json, main_domains, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"dns-1", "Cloudflare", "cloudflare", "cf-api-token-123", "{}", "[]", 1, now, now)
	if err != nil {
		t.Fatalf("failed to insert thirdpart_dns: %v", err)
	}

	// Insert a cloudflare DNS cert expiring in 10 days
	insertCertDirectly(t, db, "cert-cf", "CF Cert", "certbot_cloudflare_dns",
		[]string{"cf.example.com"}, time.Now().Add(10*24*time.Hour), true, "dns-1")

	// Create cert directory so SaveCertFiles works
	certRepo.CertDirPath("cert-cf")

	// Generate a new cert to return from mock certbot
	newCertPEM, newKeyPEM := generateSchedulerTestCert(t, []string{"cf.example.com"}, 90*24*time.Hour)
	renewer.result = &certbot.CertbotResult{
		OutputDir: "/tmp/certbot/live/cf.example.com",
		CertFiles: &certbot.CertFiles{
			CertPEM:       newCertPEM,
			ChainPEM:      newCertPEM, // Use same for simplicity
			FullchainPEM:  newCertPEM,
			PrivateKeyPEM: newKeyPEM,
		},
	}

	ctx := context.Background()
	err = scheduler.CheckRenewals(ctx)
	if err != nil {
		t.Fatalf("CheckRenewals failed: %v", err)
	}

	// Certbot should have been called once
	if renewer.calls != 1 {
		t.Errorf("expected 1 certbot call, got %d", renewer.calls)
	}

	// Should have 1 expiry alert (first pass) but no failure alerts
	if len(alertSender.alerts) != 1 {
		t.Errorf("expected 1 alert (expiry warning), got %d", len(alertSender.alerts))
	}
	if len(alertSender.alerts) > 0 && alertSender.alerts[0].AlertType != "cert_expiring" {
		t.Errorf("expected alert type 'cert_expiring', got '%s'", alertSender.alerts[0].AlertType)
	}

	// Verify renew_status was updated to success
	var renewStatus string
	err = db.QueryRow("SELECT renew_status FROM certificates WHERE id = ?", "cert-cf").Scan(&renewStatus)
	if err != nil {
		t.Fatalf("failed to query renew_status: %v", err)
	}
	if renewStatus != "success" {
		t.Errorf("expected renew_status 'success', got %q", renewStatus)
	}
}

func TestCheckRenewals_CloudflareDNS_FailureWithRetry(t *testing.T) {
	scheduler, db, _, _, alertSender, renewer := setupSchedulerTest(t)
	scheduler.MaxRetries = 2 // Total 3 attempts (initial + 2 retries)

	// Insert thirdpart_dns config
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`INSERT INTO thirdpart_dns (id, name, type, api_token, config_json, main_domains, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"dns-fail", "Cloudflare", "cloudflare", "cf-token", "{}", "[]", 1, now, now)
	if err != nil {
		t.Fatalf("failed to insert thirdpart_dns: %v", err)
	}

	// Insert a cloudflare DNS cert expiring in 5 days
	insertCertDirectly(t, db, "cert-fail", "Fail Cert", "certbot_cloudflare_dns",
		[]string{"fail.example.com"}, time.Now().Add(5*24*time.Hour), true, "dns-fail")

	// Mock certbot to always fail
	renewer.err = fmt.Errorf("certbot connection timeout")

	ctx := context.Background()
	err = scheduler.CheckRenewals(ctx)
	if err != nil {
		t.Fatalf("CheckRenewals failed: %v", err)
	}

	// Should have retried: initial + MaxRetries = 3 calls
	if renewer.calls != 3 {
		t.Errorf("expected 3 certbot calls (with retries), got %d", renewer.calls)
	}

	// Should have sent 2 alerts: cert_expiring (first pass) + cert_renew_failed (after retries)
	if len(alertSender.alerts) != 2 {
		t.Fatalf("expected 2 alerts, got %d", len(alertSender.alerts))
	}
	// First alert: expiry warning
	if alertSender.alerts[0].AlertType != "cert_expiring" {
		t.Errorf("expected first alert type 'cert_expiring', got %q", alertSender.alerts[0].AlertType)
	}
	// Second alert: renewal failure
	alert := alertSender.alerts[1]
	if alert.Level != "critical" {
		t.Errorf("expected alert level 'critical', got %q", alert.Level)
	}
	if alert.AlertType != "cert_renew_failed" {
		t.Errorf("expected alert type 'cert_renew_failed', got %q", alert.AlertType)
	}

	// Verify renew_status contains failure info
	var renewStatus string
	err = db.QueryRow("SELECT renew_status FROM certificates WHERE id = ?", "cert-fail").Scan(&renewStatus)
	if err != nil {
		t.Fatalf("failed to query renew_status: %v", err)
	}
	if renewStatus == "" || renewStatus == "success" {
		t.Errorf("expected renew_status to contain failure info, got %q", renewStatus)
	}
}

func TestCheckRenewals_CloudflareDNS_MarksPendingSync(t *testing.T) {
	scheduler, db, certRepo, _, _, renewer := setupSchedulerTest(t)

	// Insert thirdpart_dns config
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`INSERT INTO thirdpart_dns (id, name, type, api_token, config_json, main_domains, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"dns-sync", "Cloudflare", "cloudflare", "cf-token-sync", "{}", "[]", 1, now, now)
	if err != nil {
		t.Fatalf("failed to insert thirdpart_dns: %v", err)
	}

	// Insert cert
	insertCertDirectly(t, db, "cert-sync", "Sync Cert", "certbot_cloudflare_dns",
		[]string{"sync.example.com"}, time.Now().Add(10*24*time.Hour), true, "dns-sync")

	// Create cert directory
	certRepo.CertDirPath("cert-sync")

	// Insert machine_certificates associated with this cert
	_, err = db.Exec(`INSERT INTO machine_certificates (id, machine_id, certificate_id, cert_path, private_key_path, config_revision, last_deploy_status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"mc-1", "machine-1", "cert-sync", "/etc/ssl/cert.pem", "/etc/ssl/key.pem", 1, "success", now, now)
	if err != nil {
		t.Fatalf("failed to insert machine_certificate: %v", err)
	}

	// Mock successful renewal
	newCertPEM, newKeyPEM := generateSchedulerTestCert(t, []string{"sync.example.com"}, 90*24*time.Hour)
	renewer.result = &certbot.CertbotResult{
		OutputDir: "/tmp/certbot/live/sync.example.com",
		CertFiles: &certbot.CertFiles{
			CertPEM:       newCertPEM,
			ChainPEM:      newCertPEM,
			FullchainPEM:  newCertPEM,
			PrivateKeyPEM: newKeyPEM,
		},
	}

	ctx := context.Background()
	err = scheduler.CheckRenewals(ctx)
	if err != nil {
		t.Fatalf("CheckRenewals failed: %v", err)
	}

	// Verify machine_certificate was marked as pending with incremented revision
	var status string
	var rev int
	err = db.QueryRow("SELECT last_deploy_status, config_revision FROM machine_certificates WHERE id = ?", "mc-1").Scan(&status, &rev)
	if err != nil {
		t.Fatalf("failed to query machine_certificate: %v", err)
	}
	if status != "pending" {
		t.Errorf("expected status 'pending', got %q", status)
	}
	if rev != 2 {
		t.Errorf("expected config_revision 2, got %d", rev)
	}
}

func TestCheckRenewals_MissingThirdpartDNS(t *testing.T) {
	scheduler, db, _, _, alertSender, renewer := setupSchedulerTest(t)

	// Insert cert with non-existent thirdpart_dns_id
	insertCertDirectly(t, db, "cert-no-dns", "No DNS Cert", "certbot_cloudflare_dns",
		[]string{"nodns.example.com"}, time.Now().Add(5*24*time.Hour), true, "nonexistent-dns")

	ctx := context.Background()
	err := scheduler.CheckRenewals(ctx)
	if err != nil {
		t.Fatalf("CheckRenewals failed: %v", err)
	}

	// Should have tried and failed, sending an alert
	// The renewer should have been called 0 times (fails before reaching certbot)
	if renewer.calls != 0 {
		t.Errorf("expected 0 certbot calls (should fail at token lookup), got %d", renewer.calls)
	}

	// Should have sent 2 alerts: cert_expiring (first pass) + cert_renew_failed (after retries)
	if len(alertSender.alerts) != 2 {
		t.Fatalf("expected 2 alerts, got %d", len(alertSender.alerts))
	}
	if alertSender.alerts[0].AlertType != "cert_expiring" {
		t.Errorf("expected first alert type 'cert_expiring', got %q", alertSender.alerts[0].AlertType)
	}
	if alertSender.alerts[1].AlertType != "cert_renew_failed" {
		t.Errorf("expected second alert type 'cert_renew_failed', got %q", alertSender.alerts[1].AlertType)
	}
}

func TestScheduler_StartStop(t *testing.T) {
	scheduler, _, _, _, _, _ := setupSchedulerTest(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start scheduler
	err := scheduler.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !scheduler.IsRunning() {
		t.Error("expected scheduler to be running")
	}

	// Starting again should be a no-op (no error)
	err = scheduler.Start(ctx)
	if err != nil {
		t.Fatalf("second Start should not error: %v", err)
	}

	// Stop scheduler
	err = scheduler.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if scheduler.IsRunning() {
		t.Error("expected scheduler to be stopped")
	}

	// Stopping again should be a no-op (no error)
	err = scheduler.Stop()
	if err != nil {
		t.Fatalf("second Stop should not error: %v", err)
	}
}

func TestCheckHeartbeatTimeouts(t *testing.T) {
	scheduler, db, _, _, _, _ := setupSchedulerTest(t)

	now := time.Now().UTC()
	// Machine that is online but heartbeat is old (should go offline)
	oldHeartbeat := now.Add(-200 * time.Second).Format(time.RFC3339)
	_, err := db.Exec(`INSERT INTO machines (id, name, ip, status, agent_token_hash, last_heartbeat_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"m-old", "Old Machine", "1.2.3.4", "online", "hash1", oldHeartbeat, now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		t.Fatalf("failed to insert machine: %v", err)
	}

	// Machine that is online with recent heartbeat (should stay online)
	recentHeartbeat := now.Add(-30 * time.Second).Format(time.RFC3339)
	_, err = db.Exec(`INSERT INTO machines (id, name, ip, status, agent_token_hash, last_heartbeat_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"m-recent", "Recent Machine", "5.6.7.8", "online", "hash2", recentHeartbeat, now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		t.Fatalf("failed to insert machine: %v", err)
	}

	ctx := context.Background()
	err = scheduler.CheckHeartbeatTimeouts(ctx)
	if err != nil {
		t.Fatalf("CheckHeartbeatTimeouts failed: %v", err)
	}

	// Verify old machine is now offline
	var status1 string
	err = db.QueryRow("SELECT status FROM machines WHERE id = ?", "m-old").Scan(&status1)
	if err != nil {
		t.Fatalf("failed to query machine status: %v", err)
	}
	if status1 != "offline" {
		t.Errorf("expected old machine status 'offline', got %q", status1)
	}

	// Verify recent machine is still online
	var status2 string
	err = db.QueryRow("SELECT status FROM machines WHERE id = ?", "m-recent").Scan(&status2)
	if err != nil {
		t.Fatalf("failed to query machine status: %v", err)
	}
	if status2 != "online" {
		t.Errorf("expected recent machine status 'online', got %q", status2)
	}
}

// --- Domain Monitor Scheduler Integration Tests ---

func TestRunDomainMonitor_NilService(t *testing.T) {
	scheduler, _, _, _, _, _ := setupSchedulerTest(t)

	// Without setting a DomainMonitorService, RunDomainMonitor should be a no-op
	ctx := context.Background()
	err := scheduler.RunDomainMonitor(ctx)
	if err != nil {
		t.Fatalf("RunDomainMonitor with nil service should not error, got: %v", err)
	}
}

func TestRunDomainMonitor_CallsProbeAll(t *testing.T) {
	scheduler, db, _, _, _, _ := setupSchedulerTest(t)

	// Create domains table for domain monitor
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS domains (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		source TEXT DEFAULT 'manual' CHECK(source IN ('manual', 'certificate', 'cloudflare')),
		thirdpart_dns_id TEXT DEFAULT '',
		dns_record_type TEXT DEFAULT '',
		dns_record_value TEXT DEFAULT '',
		monitor_port INTEGER NOT NULL DEFAULT 443,
		linked_machine_id TEXT,
		linked_certificate_id TEXT,
		linked_machine_certificate_id TEXT,
		monitor_enabled INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("failed to create domains table: %v", err)
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS domain_monitor_results (
		id TEXT PRIMARY KEY,
		domain_id TEXT NOT NULL,
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
	)`)
	if err != nil {
		t.Fatalf("failed to create domain_monitor_results table: %v", err)
	}

	// Set up domain monitor service
	domainRepo := repository.NewDomainRepository(db)
	certRepo := repository.NewCertificateRepository(db, t.TempDir())
	alertSender := &mockAlertSender{}
	domainMonitorSvc := NewDomainMonitorService(domainRepo, certRepo, alertSender, nil)

	// Use a mock DNS resolver that fails (to keep test simple and fast)
	domainMonitorSvc.SetDNSResolver(&mockDNSResolver{
		err: fmt.Errorf("no such host"),
	})

	// Set the domain monitor service on the scheduler
	scheduler.SetDomainMonitorService(domainMonitorSvc)

	ctx := context.Background()

	// Insert some enabled domains
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(`INSERT INTO domains (id, name, monitor_port, monitor_enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		"dom-1", "example.com", 443, 1, now, now)
	if err != nil {
		t.Fatalf("failed to insert domain: %v", err)
	}
	_, err = db.Exec(`INSERT INTO domains (id, name, monitor_port, monitor_enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		"dom-2", "test.com", 443, 1, now, now)
	if err != nil {
		t.Fatalf("failed to insert domain: %v", err)
	}

	// Insert a disabled domain (should not be probed)
	_, err = db.Exec(`INSERT INTO domains (id, name, monitor_port, monitor_enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		"dom-3", "disabled.com", 443, 0, now, now)
	if err != nil {
		t.Fatalf("failed to insert domain: %v", err)
	}

	// Run domain monitor
	err = scheduler.RunDomainMonitor(ctx)
	if err != nil {
		t.Fatalf("RunDomainMonitor failed: %v", err)
	}

	// Verify that monitor results were saved for enabled domains
	var resultCount int
	err = db.QueryRow("SELECT COUNT(*) FROM domain_monitor_results").Scan(&resultCount)
	if err != nil {
		t.Fatalf("failed to count results: %v", err)
	}
	// Should have 2 results (only enabled domains)
	if resultCount != 2 {
		t.Errorf("expected 2 monitor results (enabled domains only), got %d", resultCount)
	}

	// Verify alerts were triggered for DNS failures
	if len(alertSender.alerts) != 2 {
		t.Errorf("expected 2 alerts (DNS failure for each enabled domain), got %d", len(alertSender.alerts))
	}
}

func TestRunDomainMonitor_UsesConfigInterval(t *testing.T) {
	scheduler, _, _, _, _, _ := setupSchedulerTest(t)

	// Verify the config interval is used (default is 60 minutes)
	if scheduler.runtimeCfg.Get().DomainMonitor.IntervalMinutes != 60 {
		t.Errorf("expected default interval 60 minutes, got %d", scheduler.runtimeCfg.Get().DomainMonitor.IntervalMinutes)
	}

	// Change interval and verify it's accessible
	scheduler.runtimeCfg.Get().DomainMonitor.IntervalMinutes = 30
	if scheduler.runtimeCfg.Get().DomainMonitor.IntervalMinutes != 30 {
		t.Errorf("expected interval 30 minutes after update, got %d", scheduler.runtimeCfg.Get().DomainMonitor.IntervalMinutes)
	}
}
