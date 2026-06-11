package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// mockTLSDialer is a mock TLS dialer for testing.
type mockTLSDialer struct {
	conn *tls.Conn
	err  error
}

func (m *mockTLSDialer) DialTLS(ctx context.Context, addr string, config *tls.Config) (*tls.Conn, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.conn, nil
}

// mockDNSResolver is a mock DNS resolver for testing.
type mockDNSResolver struct {
	ips []string
	err error
}

func (m *mockDNSResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.ips, nil
}

// mockAlertSenderDomain is a mock alert sender for domain monitor testing.
// Uses the same interface as the scheduler's mockAlertSender but with a different name
// to avoid redeclaration since both are in the same test package.
// Note: We reuse the existing mockAlertSender from scheduler_test.go directly.

// setupDomainMonitorTestDB creates a test DB with required tables for domain monitoring.
func setupDomainMonitorTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db := setupTestDB(t)

	// Add domains table
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS domains (
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
	)`)
	if err != nil {
		t.Fatalf("failed to create domains table: %v", err)
	}

	// Add domain_monitor_results table
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

	return db
}

func TestDomainMonitorService_Create(t *testing.T) {
	db := setupDomainMonitorTestDB(t)
	domainRepo := repository.NewDomainRepository(db)
	svc := NewDomainMonitorService(domainRepo, nil, nil, nil)

	ctx := context.Background()

	domain, err := svc.Create(ctx, model.CreateDomainInput{
		Name:        "example.com",
		MonitorPort: 443,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if domain.ID == "" {
		t.Fatal("expected ID to be generated")
	}
	if domain.Name != "example.com" {
		t.Errorf("expected name 'example.com', got '%s'", domain.Name)
	}
	if domain.MonitorPort != 443 {
		t.Errorf("expected port 443, got %d", domain.MonitorPort)
	}
	if !domain.MonitorEnabled {
		t.Error("expected MonitorEnabled to be true by default")
	}
}

func TestDomainMonitorService_Create_DefaultPort(t *testing.T) {
	db := setupDomainMonitorTestDB(t)
	domainRepo := repository.NewDomainRepository(db)
	svc := NewDomainMonitorService(domainRepo, nil, nil, nil)

	ctx := context.Background()

	domain, err := svc.Create(ctx, model.CreateDomainInput{
		Name: "example.com",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if domain.MonitorPort != 443 {
		t.Errorf("expected default port 443, got %d", domain.MonitorPort)
	}
}

func TestDomainMonitorService_Create_EmptyName(t *testing.T) {
	db := setupDomainMonitorTestDB(t)
	domainRepo := repository.NewDomainRepository(db)
	svc := NewDomainMonitorService(domainRepo, nil, nil, nil)

	ctx := context.Background()

	_, err := svc.Create(ctx, model.CreateDomainInput{
		Name: "",
	})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestDomainMonitorService_Update(t *testing.T) {
	db := setupDomainMonitorTestDB(t)
	domainRepo := repository.NewDomainRepository(db)
	svc := NewDomainMonitorService(domainRepo, nil, nil, nil)

	ctx := context.Background()

	domain, err := svc.Create(ctx, model.CreateDomainInput{
		Name:        "example.com",
		MonitorPort: 443,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	newPort := 8443
	disabled := false
	updated, err := svc.Update(ctx, domain.ID, model.UpdateDomainInput{
		MonitorPort:    &newPort,
		MonitorEnabled: &disabled,
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if updated.MonitorPort != 8443 {
		t.Errorf("expected port 8443, got %d", updated.MonitorPort)
	}
	if updated.MonitorEnabled {
		t.Error("expected MonitorEnabled to be false")
	}
}

func TestDomainMonitorService_Delete(t *testing.T) {
	db := setupDomainMonitorTestDB(t)
	domainRepo := repository.NewDomainRepository(db)
	svc := NewDomainMonitorService(domainRepo, nil, nil, nil)

	ctx := context.Background()

	domain, err := svc.Create(ctx, model.CreateDomainInput{
		Name:        "example.com",
		MonitorPort: 443,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := svc.Delete(ctx, domain.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify it's gone
	domains, err := svc.List(ctx, model.DomainFilter{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(domains) != 0 {
		t.Errorf("expected 0 domains after delete, got %d", len(domains))
	}
}

func TestDomainMonitorService_List(t *testing.T) {
	db := setupDomainMonitorTestDB(t)
	domainRepo := repository.NewDomainRepository(db)
	svc := NewDomainMonitorService(domainRepo, nil, nil, nil)

	ctx := context.Background()

	// Create multiple domains
	for i := 0; i < 3; i++ {
		_, err := svc.Create(ctx, model.CreateDomainInput{
			Name:        fmt.Sprintf("domain%d.example.com", i),
			MonitorPort: 443,
		})
		if err != nil {
			t.Fatalf("Create %d failed: %v", i, err)
		}
	}

	domains, err := svc.List(ctx, model.DomainFilter{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(domains) != 3 {
		t.Errorf("expected 3 domains, got %d", len(domains))
	}
}

func TestDomainMonitorService_Probe_DNSFailure(t *testing.T) {
	db := setupDomainMonitorTestDB(t)
	domainRepo := repository.NewDomainRepository(db)
	alertSender := &mockAlertSender{}
	svc := NewDomainMonitorService(domainRepo, nil, alertSender, nil)

	// Set mock resolver that fails
	svc.SetDNSResolver(&mockDNSResolver{
		err: fmt.Errorf("no such host"),
	})

	ctx := context.Background()

	domain, err := svc.Create(ctx, model.CreateDomainInput{
		Name:        "nonexistent.example.com",
		MonitorPort: 443,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	result, err := svc.Probe(ctx, domain.ID)
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}

	if result.TLSSuccess {
		t.Error("expected TLSSuccess to be false on DNS failure")
	}
	if result.ErrorMessage == "" {
		t.Error("expected error message for DNS failure")
	}

	// Verify alert was triggered
	if len(alertSender.alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alertSender.alerts))
	}
	if alertSender.alerts[0].AlertType != "dns_resolve_failed" {
		t.Errorf("expected alert type 'dns_resolve_failed', got '%s'", alertSender.alerts[0].AlertType)
	}
}

func TestDomainMonitorService_Probe_TLSFailure(t *testing.T) {
	db := setupDomainMonitorTestDB(t)
	domainRepo := repository.NewDomainRepository(db)
	alertSender := &mockAlertSender{}
	svc := NewDomainMonitorService(domainRepo, nil, alertSender, nil)

	// Set mock resolver that succeeds
	svc.SetDNSResolver(&mockDNSResolver{
		ips: []string{"1.2.3.4"},
	})

	// Set mock TLS dialer that fails
	svc.SetTLSDialer(&mockTLSDialer{
		err: fmt.Errorf("connection refused"),
	})

	ctx := context.Background()

	domain, err := svc.Create(ctx, model.CreateDomainInput{
		Name:        "example.com",
		MonitorPort: 443,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	result, err := svc.Probe(ctx, domain.ID)
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}

	if result.TLSSuccess {
		t.Error("expected TLSSuccess to be false on TLS failure")
	}
	if len(result.ResolvedIPs) != 1 || result.ResolvedIPs[0] != "1.2.3.4" {
		t.Errorf("expected resolved IPs [1.2.3.4], got %v", result.ResolvedIPs)
	}
	if result.ErrorMessage == "" {
		t.Error("expected error message for TLS failure")
	}

	// Verify alert was triggered
	if len(alertSender.alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alertSender.alerts))
	}
	if alertSender.alerts[0].AlertType != "tls_handshake_failed" {
		t.Errorf("expected alert type 'tls_handshake_failed', got '%s'", alertSender.alerts[0].AlertType)
	}
}

func TestDomainMonitorService_Probe_Success(t *testing.T) {
	db := setupDomainMonitorTestDB(t)
	domainRepo := repository.NewDomainRepository(db)
	svc := NewDomainMonitorService(domainRepo, nil, nil, nil)

	// Create a real TLS server for testing
	cert, certPEM := generateTestCert(t, "example.com")

	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert},
	})
	if err != nil {
		t.Fatalf("failed to create TLS listener: %v", err)
	}
	defer listener.Close()

	// Accept connections in background - must complete TLS handshake before closing
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			// Perform TLS handshake by reading (this ensures the handshake completes)
			tlsConn := conn.(*tls.Conn)
			_ = tlsConn.Handshake()
			tlsConn.Close()
		}
	}()

	addr := listener.Addr().(*net.TCPAddr)

	// Use a custom resolver that returns localhost
	svc.SetDNSResolver(&mockDNSResolver{
		ips: []string{"127.0.0.1"},
	})

	ctx := context.Background()

	domain, err := svc.Create(ctx, model.CreateDomainInput{
		Name:        "example.com",
		MonitorPort: addr.Port,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	result, err := svc.Probe(ctx, domain.ID)
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}

	if !result.TLSSuccess {
		t.Errorf("expected TLSSuccess to be true, error: %s", result.ErrorMessage)
	}
	if result.CertificateFingerprintSHA256 == "" {
		t.Error("expected fingerprint to be set")
	}
	if result.Issuer == "" {
		t.Error("expected issuer to be set")
	}
	if result.ExpireAt == nil {
		t.Error("expected expire_at to be set")
	}
	if result.DaysRemaining == nil {
		t.Error("expected days_remaining to be set")
	}
	if !result.DomainMatched {
		t.Error("expected domain to match")
	}

	// Verify the fingerprint matches what we generated
	leafCert, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("failed to parse test cert: %v", err)
	}
	expectedFingerprint := sha256.Sum256(leafCert.Raw)
	expectedFP := hex.EncodeToString(expectedFingerprint[:])
	if result.CertificateFingerprintSHA256 != expectedFP {
		t.Errorf("fingerprint mismatch: got %s, expected %s", result.CertificateFingerprintSHA256, expectedFP)
	}

	_ = certPEM // suppress unused warning
}

func TestDomainMonitorService_Probe_FingerprintMismatch(t *testing.T) {
	db := setupDomainMonitorTestDB(t)

	// Also need certificates table for linked cert lookup
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS certificates (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		domains TEXT NOT NULL,
		source TEXT NOT NULL CHECK(source IN ('upload', 'certbot_cloudflare_dns', 'certbot_manual_dns')),
		expire_at TEXT NOT NULL,
		auto_renew INTEGER NOT NULL DEFAULT 0,
		issuer TEXT DEFAULT '',
		fingerprint_sha256 TEXT NOT NULL,
		chain_valid INTEGER NOT NULL DEFAULT 1,
		cert_dir_path TEXT NOT NULL,
		thirdpart_dns_id TEXT DEFAULT '',
		last_renew_at TEXT,
		renew_status TEXT DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("failed to create certificates table: %v", err)
	}

	tmpDir := t.TempDir()
	domainRepo := repository.NewDomainRepository(db)
	certRepo := repository.NewCertificateRepository(db, tmpDir)
	alertSender := &mockAlertSender{}
	svc := NewDomainMonitorService(domainRepo, certRepo, alertSender, nil)

	// Create a certificate in the system with a different fingerprint
	ctx := context.Background()
	sysCert := &model.Certificate{
		Name:              "example.com",
		Domains:           []string{"example.com"},
		Source:            "upload",
		ExpireAt:          time.Now().Add(90 * 24 * time.Hour),
		FingerprintSHA256: "system-fingerprint-different",
		ChainValid:        true,
	}
	if err := certRepo.Create(ctx, sysCert); err != nil {
		t.Fatalf("failed to create system cert: %v", err)
	}

	// Create a real TLS server
	cert, _ := generateTestCert(t, "example.com")
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert},
	})
	if err != nil {
		t.Fatalf("failed to create TLS listener: %v", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			tlsConn := conn.(*tls.Conn)
			_ = tlsConn.Handshake()
			tlsConn.Close()
		}
	}()

	addr := listener.Addr().(*net.TCPAddr)

	svc.SetDNSResolver(&mockDNSResolver{ips: []string{"127.0.0.1"}})

	// Create domain linked to the system certificate
	domain, err := svc.Create(ctx, model.CreateDomainInput{
		Name:                "example.com",
		MonitorPort:         addr.Port,
		LinkedCertificateID: sysCert.ID,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	result, err := svc.Probe(ctx, domain.ID)
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}

	// Should succeed TLS but report fingerprint mismatch
	if !result.TLSSuccess {
		t.Errorf("expected TLSSuccess to be true, error: %s", result.ErrorMessage)
	}

	// Should have triggered a fingerprint mismatch alert
	if len(alertSender.alerts) != 1 {
		t.Fatalf("expected 1 alert for fingerprint mismatch, got %d", len(alertSender.alerts))
	}
	if alertSender.alerts[0].AlertType != "fingerprint_mismatch" {
		t.Errorf("expected alert type 'fingerprint_mismatch', got '%s'", alertSender.alerts[0].AlertType)
	}

	// Error message should mention mismatch
	if result.ErrorMessage == "" {
		t.Error("expected error message for fingerprint mismatch")
	}
}

func TestDomainMonitorService_ProbeAll(t *testing.T) {
	db := setupDomainMonitorTestDB(t)
	domainRepo := repository.NewDomainRepository(db)
	svc := NewDomainMonitorService(domainRepo, nil, nil, nil)

	// Set mock resolver that fails (to keep test simple)
	svc.SetDNSResolver(&mockDNSResolver{
		err: fmt.Errorf("no such host"),
	})

	ctx := context.Background()

	// Create 2 enabled domains and 1 disabled
	for i := 0; i < 2; i++ {
		_, err := svc.Create(ctx, model.CreateDomainInput{
			Name:        fmt.Sprintf("domain%d.example.com", i),
			MonitorPort: 443,
		})
		if err != nil {
			t.Fatalf("Create %d failed: %v", i, err)
		}
	}

	// Create disabled domain
	domain3, err := svc.Create(ctx, model.CreateDomainInput{
		Name:        "disabled.example.com",
		MonitorPort: 443,
	})
	if err != nil {
		t.Fatalf("Create disabled domain failed: %v", err)
	}
	disabled := false
	_, err = svc.Update(ctx, domain3.ID, model.UpdateDomainInput{MonitorEnabled: &disabled})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// ProbeAll should not error even if individual probes fail
	err = svc.ProbeAll(ctx)
	if err != nil {
		t.Fatalf("ProbeAll failed: %v", err)
	}
}

func TestMatchDomain(t *testing.T) {
	tests := []struct {
		pattern string
		domain  string
		match   bool
	}{
		{"example.com", "example.com", true},
		{"example.com", "other.com", false},
		{"*.example.com", "sub.example.com", true},
		{"*.example.com", "example.com", false},
		{"*.example.com", "deep.sub.example.com", false},
		{"Example.COM", "example.com", true},
		{"*.Example.COM", "sub.example.com", true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_%s", tt.pattern, tt.domain), func(t *testing.T) {
			got := matchDomain(tt.pattern, tt.domain)
			if got != tt.match {
				t.Errorf("matchDomain(%q, %q) = %v, want %v", tt.pattern, tt.domain, got, tt.match)
			}
		})
	}
}

// generateTestCert creates a self-signed certificate for testing.
func generateTestCert(t *testing.T, domain string) (tls.Certificate, []byte) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:   domain,
			Organization: []string{"Test Org"},
		},
		DNSNames:              []string{domain},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(90 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	tlsCert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}

	return tlsCert, certDER
}
