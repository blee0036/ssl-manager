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
	"encoding/hex"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// TestProperty19_DomainFingerprintMismatchMarksAnomaly verifies that when the online
// certificate fingerprint doesn't match the system certificate fingerprint, the domain
// should be marked as anomalous (error message about mismatch and alert triggered).
// When fingerprints match, no mismatch error or alert should occur.
//
// **Validates: Requirements 10.4**
func TestProperty19_DomainFingerprintMismatchMarksAnomaly(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 50
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	// Property: When online fingerprint differs from system fingerprint, probe result
	// contains a mismatch error message and an alert is triggered.
	properties.Property("fingerprint mismatch triggers anomaly and alert", prop.ForAll(
		func(systemFPSeed int64) bool {
			db := setupDomainMonitorTestDB(t)
			tmpDir := t.TempDir()
			domainRepo := repository.NewDomainRepository(db)
			certRepo := repository.NewCertificateRepository(db, tmpDir)
			alertSender := &mockAlertSender{}
			svc := NewDomainMonitorService(domainRepo, certRepo, alertSender, nil)

			ctx := context.Background()

			// Generate a TLS certificate for the "online" server
			tlsCert, leafCert := generatePropertyTestCert(t, "test.example.com")

			// Compute the online fingerprint
			onlineFingerprint := sha256.Sum256(leafCert.Raw)
			onlineFP := hex.EncodeToString(onlineFingerprint[:])

			// Generate a different system fingerprint using the seed
			// We create a deterministic but different fingerprint by hashing the seed
			systemFPBytes := sha256.Sum256([]byte(strings.Repeat("x", int(systemFPSeed%64)+1)))
			systemFP := hex.EncodeToString(systemFPBytes[:])

			// Ensure system fingerprint is actually different from online
			if systemFP == onlineFP {
				// Extremely unlikely but handle it - modify to ensure difference
				systemFPBytes[0] ^= 0xFF
				systemFP = hex.EncodeToString(systemFPBytes[:])
			}

			// Create a certificate in the system with the different fingerprint
			sysCert := &model.Certificate{
				Name:              "test.example.com",
				Domains:           []string{"test.example.com"},
				Source:            "upload",
				ExpireAt:          time.Now().Add(90 * 24 * time.Hour),
				FingerprintSHA256: systemFP,
				ChainValid:        true,
			}
			if err := certRepo.Create(ctx, sysCert); err != nil {
				t.Logf("Failed to create system cert: %v", err)
				return false
			}

			// Start a TLS server with the online certificate
			listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
				Certificates: []tls.Certificate{tlsCert},
			})
			if err != nil {
				t.Logf("Failed to create TLS listener: %v", err)
				return false
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

			// Mock DNS to resolve to localhost
			svc.SetDNSResolver(&mockDNSResolver{ips: []string{"127.0.0.1"}})

			// Create domain linked to the system certificate
			domain, err := svc.Create(ctx, model.CreateDomainInput{
				Name:                "test.example.com",
				MonitorPort:         addr.Port,
				LinkedCertificateID: sysCert.ID,
			})
			if err != nil {
				t.Logf("Failed to create domain: %v", err)
				return false
			}

			// Probe the domain
			result, err := svc.Probe(ctx, domain.ID)
			if err != nil {
				t.Logf("Probe failed: %v", err)
				return false
			}

			// Verify: TLS should succeed
			if !result.TLSSuccess {
				t.Logf("Expected TLS success, got error: %s", result.ErrorMessage)
				return false
			}

			// Verify: Error message should mention fingerprint mismatch
			if !strings.Contains(result.ErrorMessage, "fingerprint mismatch") {
				t.Logf("Expected error message to contain 'fingerprint mismatch', got: %q", result.ErrorMessage)
				return false
			}

			// Verify: Alert should be triggered with type "fingerprint_mismatch"
			if len(alertSender.alerts) != 1 {
				t.Logf("Expected 1 alert, got %d", len(alertSender.alerts))
				return false
			}
			if alertSender.alerts[0].AlertType != "fingerprint_mismatch" {
				t.Logf("Expected alert type 'fingerprint_mismatch', got %q", alertSender.alerts[0].AlertType)
				return false
			}

			// Verify: Error message contains both fingerprints
			if !strings.Contains(result.ErrorMessage, onlineFP) {
				t.Logf("Expected error message to contain online fingerprint %q", onlineFP)
				return false
			}
			if !strings.Contains(result.ErrorMessage, systemFP) {
				t.Logf("Expected error message to contain system fingerprint %q", systemFP)
				return false
			}

			return true
		},
		gen.Int64Range(1, 10000), // seed for generating different system fingerprints
	))

	// Property: When online fingerprint matches system fingerprint, no mismatch error or alert.
	properties.Property("matching fingerprints produce no mismatch error or alert", prop.ForAll(
		func(domainName string) bool {
			db := setupDomainMonitorTestDB(t)
			tmpDir := t.TempDir()
			domainRepo := repository.NewDomainRepository(db)
			certRepo := repository.NewCertificateRepository(db, tmpDir)
			alertSender := &mockAlertSender{}
			svc := NewDomainMonitorService(domainRepo, certRepo, alertSender, nil)

			ctx := context.Background()

			// Generate a TLS certificate
			tlsCert, leafCert := generatePropertyTestCert(t, domainName)

			// Compute the online fingerprint - this will also be the system fingerprint
			fingerprint := sha256.Sum256(leafCert.Raw)
			fp := hex.EncodeToString(fingerprint[:])

			// Create a certificate in the system with the SAME fingerprint as online
			sysCert := &model.Certificate{
				Name:              domainName,
				Domains:           []string{domainName},
				Source:            "upload",
				ExpireAt:          time.Now().Add(90 * 24 * time.Hour),
				FingerprintSHA256: fp,
				ChainValid:        true,
			}
			if err := certRepo.Create(ctx, sysCert); err != nil {
				t.Logf("Failed to create system cert: %v", err)
				return false
			}

			// Start a TLS server with the same certificate
			listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
				Certificates: []tls.Certificate{tlsCert},
			})
			if err != nil {
				t.Logf("Failed to create TLS listener: %v", err)
				return false
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

			// Mock DNS to resolve to localhost
			svc.SetDNSResolver(&mockDNSResolver{ips: []string{"127.0.0.1"}})

			// Create domain linked to the system certificate
			domain, err := svc.Create(ctx, model.CreateDomainInput{
				Name:                domainName,
				MonitorPort:         addr.Port,
				LinkedCertificateID: sysCert.ID,
			})
			if err != nil {
				t.Logf("Failed to create domain: %v", err)
				return false
			}

			// Probe the domain
			result, err := svc.Probe(ctx, domain.ID)
			if err != nil {
				t.Logf("Probe failed: %v", err)
				return false
			}

			// Verify: TLS should succeed
			if !result.TLSSuccess {
				t.Logf("Expected TLS success, got error: %s", result.ErrorMessage)
				return false
			}

			// Verify: No fingerprint mismatch error
			if strings.Contains(result.ErrorMessage, "fingerprint mismatch") {
				t.Logf("Expected no fingerprint mismatch error, got: %q", result.ErrorMessage)
				return false
			}

			// Verify: No alerts triggered
			if len(alertSender.alerts) != 0 {
				t.Logf("Expected 0 alerts, got %d (types: %v)", len(alertSender.alerts), alertSender.alerts)
				return false
			}

			return true
		},
		gen.Identifier().Map(func(s string) string {
			// Ensure we have a valid domain name for cert generation
			if len(s) > 20 {
				s = s[:20]
			}
			return strings.ToLower(s) + ".example.com"
		}),
	))

	properties.TestingRun(t)
}

// generatePropertyTestCert creates a self-signed TLS certificate for property testing.
// Returns both the tls.Certificate (for the server) and the parsed x509.Certificate (for fingerprint computation).
func generatePropertyTestCert(t *testing.T, domain string) (tls.Certificate, *x509.Certificate) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			CommonName:   domain,
			Organization: []string{"Property Test Org"},
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

	leafCert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}

	return tlsCert, leafCert
}
