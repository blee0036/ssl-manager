package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"reflect"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// --- Generators ---

// genDomainList generates a list of 1-3 unique domain names using index-based naming to avoid discards.
func genDomainList() gopter.Gen {
	return gen.IntRange(1, 3).FlatMap(func(v interface{}) gopter.Gen {
		count := v.(int)
		return gen.Identifier().Map(func(base string) []string {
			domains := make([]string, count)
			for i := 0; i < count; i++ {
				if i == 0 {
					domains[i] = base + ".example.com"
				} else {
					domains[i] = fmt.Sprintf("%s%d.example.com", base, i)
				}
			}
			return domains
		})
	}, reflect.TypeOf([]string{}))
}

// genIssuerName generates a certificate issuer common name.
func genIssuerName() gopter.Gen {
	return gen.Identifier().Map(func(s string) string {
		return "CA-" + s
	})
}

// genMachineCertCount generates the number of associated machine certificates (1-10).
func genMachineCertCount() gopter.Gen {
	return gen.IntRange(1, 10)
}

// --- Helper functions for generating test certificates ---

// generateTestCertWithCA generates a leaf certificate signed by a CA.
// Returns certPEM, keyPEM, chainPEM (CA cert), and the raw leaf DER bytes.
func generateTestCertWithCA(t *testing.T, domains []string, issuerCN string, expireIn time.Duration) (certPEM, keyPEM, chainPEM []byte, leafDER []byte) {
	t.Helper()

	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate CA key: %v", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(100),
		Subject:               pkix.Name{CommonName: issuerCN},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("failed to create CA cert: %v", err)
	}
	chainPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})

	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("failed to parse CA cert: %v", err)
	}

	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate leaf key: %v", err)
	}

	leafTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(200),
		Subject:               pkix.Name{CommonName: domains[0]},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(expireIn),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              domains,
	}

	leafDER, err = x509.CreateCertificate(rand.Reader, leafTemplate, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("failed to create leaf cert: %v", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(leafKey)})

	return certPEM, keyPEM, chainPEM, leafDER
}

// generateTestSelfSignedCert generates a self-signed certificate (no chain).
func generateTestSelfSignedCert(t *testing.T, domains []string, expireIn time.Duration) (certPEM, keyPEM []byte, leafDER []byte) {
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
		BasicConstraintsValid: true,
		DNSNames:              domains,
	}

	leafDER, err = x509.CreateCertificate(rand.Reader, template, template, &privKey.PublicKey, privKey)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privKey)})

	return certPEM, keyPEM, leafDER
}

// TestProperty9_CertPEMParseCorrectness verifies that for any valid certificate PEM file,
// parsing should correctly extract domains, expiry time, issuer, and SHA256 fingerprint
// matching the actual certificate content.
//
// **Validates: Requirements 5.1**
func TestProperty9_CertPEMParseCorrectness(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 50
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	db := setupTestDB(t)
	dataDir := t.TempDir()
	repo := repository.NewCertificateRepository(db, dataDir)
	svc := NewCertificateService(repo, db)

	// Property: Parsed domains contain all SANs and fingerprint matches DER content
	properties.Property("parsed domains and fingerprint match certificate content", prop.ForAll(
		func(domains []string, expireDays int) bool {
			expireIn := time.Duration(expireDays) * 24 * time.Hour
			certPEM, _, leafDER := generateTestSelfSignedCert(t, domains, expireIn)

			meta, err := svc.ParsePEM(certPEM)
			if err != nil {
				t.Logf("ParsePEM failed: %v", err)
				return false
			}

			// All input domains should be present in parsed domains
			parsedDomainSet := make(map[string]bool)
			for _, d := range meta.Domains {
				parsedDomainSet[d] = true
			}
			for _, expected := range domains {
				if !parsedDomainSet[expected] {
					t.Logf("Expected domain %q not found in parsed domains: %v", expected, meta.Domains)
					return false
				}
			}

			// Verify SHA256 fingerprint matches the actual DER content
			expectedHash := sha256.Sum256(leafDER)
			expectedFingerprint := hex.EncodeToString(expectedHash[:])
			if meta.FingerprintSHA256 != expectedFingerprint {
				t.Logf("Fingerprint mismatch: got %q, expected %q", meta.FingerprintSHA256, expectedFingerprint)
				return false
			}

			return true
		},
		genDomainList(),
		gen.IntRange(30, 730),
	))

	// Property: Parsed expiry time matches the certificate's NotAfter
	properties.Property("parsed expiry time matches certificate NotAfter", prop.ForAll(
		func(domains []string, expireDays int) bool {
			expireIn := time.Duration(expireDays) * 24 * time.Hour
			certPEM, _, _ := generateTestSelfSignedCert(t, domains, expireIn)

			meta, err := svc.ParsePEM(certPEM)
			if err != nil {
				t.Logf("ParsePEM failed: %v", err)
				return false
			}

			block, _ := pem.Decode(certPEM)
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				t.Logf("ParseCertificate failed: %v", err)
				return false
			}

			if !meta.ExpireAt.Equal(cert.NotAfter) {
				t.Logf("ExpireAt mismatch: got %v, expected %v", meta.ExpireAt, cert.NotAfter)
				return false
			}

			return true
		},
		genDomainList(),
		gen.IntRange(30, 730),
	))

	// Property: Parsed issuer matches the certificate's Issuer CN
	properties.Property("parsed issuer matches certificate issuer CN", prop.ForAll(
		func(domains []string, issuerCN string) bool {
			expireIn := 365 * 24 * time.Hour
			certPEM, _, _, _ := generateTestCertWithCA(t, domains, issuerCN, expireIn)

			meta, err := svc.ParsePEM(certPEM)
			if err != nil {
				t.Logf("ParsePEM failed: %v", err)
				return false
			}

			if meta.Issuer != issuerCN {
				t.Logf("Issuer mismatch: got %q, expected %q", meta.Issuer, issuerCN)
				return false
			}

			return true
		},
		genDomainList(),
		genIssuerName(),
	))

	// Property: SHA256 fingerprint is always 64 hex characters
	properties.Property("fingerprint is always 64 hex characters", prop.ForAll(
		func(domains []string) bool {
			expireIn := 365 * 24 * time.Hour
			certPEM, _, _ := generateTestSelfSignedCert(t, domains, expireIn)

			meta, err := svc.ParsePEM(certPEM)
			if err != nil {
				t.Logf("ParsePEM failed: %v", err)
				return false
			}

			if len(meta.FingerprintSHA256) != 64 {
				t.Logf("Fingerprint length: got %d, expected 64", len(meta.FingerprintSHA256))
				return false
			}

			_, err = hex.DecodeString(meta.FingerprintSHA256)
			if err != nil {
				t.Logf("Fingerprint is not valid hex: %v", err)
				return false
			}

			return true
		},
		genDomainList(),
	))

	properties.TestingRun(t)
}

// TestProperty10_CertKeyMismatchRejection verifies that for any certificate PEM
// with a non-matching private key PEM, the validation function should return an error
// and the system should refuse to save.
//
// **Validates: Requirements 5.2**
func TestProperty10_CertKeyMismatchRejection(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 50
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	db := setupTestDB(t)
	dataDir := t.TempDir()
	repo := repository.NewCertificateRepository(db, dataDir)
	svc := NewCertificateService(repo, db)

	// Property: RSA cert with a different RSA key is always rejected
	properties.Property("RSA cert with different RSA key is rejected", prop.ForAll(
		func(domains1 []string, domains2 []string) bool {
			expireIn := 365 * 24 * time.Hour
			certPEM, _, _ := generateTestSelfSignedCert(t, domains1, expireIn)
			_, otherKeyPEM, _ := generateTestSelfSignedCert(t, domains2, expireIn)

			err := svc.ValidateKeyPair(certPEM, otherKeyPEM)
			if err == nil {
				t.Logf("Expected error for mismatched RSA keys, got nil")
				return false
			}
			return true
		},
		genDomainList(),
		genDomainList(),
	))

	// Property: RSA cert with ECDSA key is always rejected
	properties.Property("RSA cert with ECDSA key is rejected", prop.ForAll(
		func(domains []string) bool {
			expireIn := 365 * 24 * time.Hour
			certPEM, _, _ := generateTestSelfSignedCert(t, domains, expireIn)

			ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Logf("Failed to generate ECDSA key: %v", err)
				return false
			}
			ecKeyBytes, err := x509.MarshalECPrivateKey(ecKey)
			if err != nil {
				t.Logf("Failed to marshal ECDSA key: %v", err)
				return false
			}
			ecKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: ecKeyBytes})

			err = svc.ValidateKeyPair(certPEM, ecKeyPEM)
			if err == nil {
				t.Logf("Expected error for RSA cert with ECDSA key, got nil")
				return false
			}
			return true
		},
		genDomainList(),
	))

	// Property: ECDSA cert with RSA key is always rejected
	properties.Property("ECDSA cert with RSA key is rejected", prop.ForAll(
		func(domains []string) bool {
			expireIn := 365 * 24 * time.Hour

			ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Logf("Failed to generate ECDSA key: %v", err)
				return false
			}

			template := &x509.Certificate{
				SerialNumber: big.NewInt(1),
				Subject:      pkix.Name{CommonName: domains[0]},
				NotBefore:    time.Now().Add(-1 * time.Hour),
				NotAfter:     time.Now().Add(expireIn),
				KeyUsage:     x509.KeyUsageDigitalSignature,
				DNSNames:     domains,
			}

			certDER, err := x509.CreateCertificate(rand.Reader, template, template, &ecKey.PublicKey, ecKey)
			if err != nil {
				t.Logf("Failed to create ECDSA cert: %v", err)
				return false
			}
			certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

			rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
			if err != nil {
				t.Logf("Failed to generate RSA key: %v", err)
				return false
			}
			rsaKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rsaKey)})

			err = svc.ValidateKeyPair(certPEM, rsaKeyPEM)
			if err == nil {
				t.Logf("Expected error for ECDSA cert with RSA key, got nil")
				return false
			}
			return true
		},
		genDomainList(),
	))

	// Property: Mismatched key pair causes Create to fail (system refuses to save)
	properties.Property("Create refuses to save with mismatched key pair", prop.ForAll(
		func(domains1 []string, domains2 []string) bool {
			expireIn := 365 * 24 * time.Hour
			certPEM, _, _ := generateTestSelfSignedCert(t, domains1, expireIn)
			_, otherKeyPEM, _ := generateTestSelfSignedCert(t, domains2, expireIn)

			ctx := context.Background()
			input := model.CreateCertInput{
				Name:    "Should Fail",
				CertPEM: certPEM,
				KeyPEM:  otherKeyPEM,
			}

			_, err := svc.Create(ctx, input)
			if err == nil {
				t.Logf("Expected Create to fail with mismatched key pair, got nil")
				return false
			}
			return true
		},
		genDomainList(),
		genDomainList(),
	))

	properties.TestingRun(t)
}

// TestProperty11_CertUpdateTriggersPendingSyncMark verifies that for any certificate
// with N associated Machine_Certificates, when the certificate content is updated,
// all N Machine_Certificates should be marked as pending sync.
//
// **Validates: Requirements 5.3**
func TestProperty11_CertUpdateTriggersPendingSyncMark(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 30
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	// Property: All N associated machine_certificates are marked pending after cert update
	properties.Property("all associated machine_certificates marked pending after update", prop.ForAll(
		func(n int) bool {
			db := setupTestDB(t)
			dataDir := t.TempDir()
			repo := repository.NewCertificateRepository(db, dataDir)
			svc := NewCertificateService(repo, db)

			ctx := context.Background()

			// Create initial certificate
			domains := []string{"sync-prop.example.com"}
			certPEM, keyPEM, _ := generateTestSelfSignedCert(t, domains, 365*24*time.Hour)

			input := model.CreateCertInput{
				Name:    "Sync Property Test",
				CertPEM: certPEM,
				KeyPEM:  keyPEM,
			}

			cert, err := svc.Create(ctx, input)
			if err != nil {
				t.Logf("Create failed: %v", err)
				return false
			}

			// Insert N machine_certificates associated with this certificate
			now := time.Now().UTC().Format(time.RFC3339)
			for i := 0; i < n; i++ {
				mcID := fmt.Sprintf("mc-prop-%d-%s", i, cert.ID[:8])
				machineID := fmt.Sprintf("machine-%d", i)
				_, err := db.ExecContext(ctx,
					`INSERT INTO machine_certificates (id, machine_id, certificate_id, cert_path, private_key_path, config_revision, last_deploy_status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
					mcID, machineID, cert.ID, "/etc/ssl/cert.pem", "/etc/ssl/key.pem", 1, "success", now, now)
				if err != nil {
					t.Logf("Failed to insert machine_certificate %d: %v", i, err)
					return false
				}
			}

			// Generate new certificate content and update
			newDomains := []string{"updated-sync.example.com"}
			newCertPEM, newKeyPEM, _ := generateTestSelfSignedCert(t, newDomains, 730*24*time.Hour)

			updateInput := model.UpdateCertInput{
				CertPEM: newCertPEM,
				KeyPEM:  newKeyPEM,
			}

			_, err = svc.Update(ctx, cert.ID, updateInput)
			if err != nil {
				t.Logf("Update failed: %v", err)
				return false
			}

			// Verify ALL N machine_certificates are now marked as pending with incremented revision
			rows, err := db.QueryContext(ctx,
				`SELECT last_deploy_status, config_revision FROM machine_certificates WHERE certificate_id = ?`,
				cert.ID)
			if err != nil {
				t.Logf("Query failed: %v", err)
				return false
			}
			defer rows.Close()

			count := 0
			for rows.Next() {
				var status string
				var revision int
				if err := rows.Scan(&status, &revision); err != nil {
					t.Logf("Scan failed: %v", err)
					return false
				}

				if status != "pending" {
					t.Logf("Expected status 'pending', got %q", status)
					return false
				}

				if revision != 2 {
					t.Logf("Expected revision 2, got %d", revision)
					return false
				}

				count++
			}

			if count != n {
				t.Logf("Expected %d machine_certificates, found %d", n, count)
				return false
			}

			return true
		},
		genMachineCertCount(),
	))

	properties.TestingRun(t)
}

// TestProperty30_CertChainIntegrityRecord verifies that if the certificate chain
// is incomplete, the system should allow saving but mark chain_valid as false.
//
// **Validates: Requirements 5.7**
func TestProperty30_CertChainIntegrityRecord(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 30
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	// Property: Certificate without chain PEM is saved with chain_valid = false
	properties.Property("certificate without chain is saved with chain_valid=false", prop.ForAll(
		func(domains []string, expireDays int) bool {
			db := setupTestDB(t)
			dataDir := t.TempDir()
			repo := repository.NewCertificateRepository(db, dataDir)
			svc := NewCertificateService(repo, db)

			expireIn := time.Duration(expireDays) * 24 * time.Hour
			certPEM, keyPEM, _ := generateTestSelfSignedCert(t, domains, expireIn)

			ctx := context.Background()
			input := model.CreateCertInput{
				Name:    "No Chain Test",
				CertPEM: certPEM,
				KeyPEM:  keyPEM,
				// No ChainPEM provided - chain is incomplete
			}

			cert, err := svc.Create(ctx, input)
			if err != nil {
				t.Logf("Create failed: %v", err)
				return false
			}

			// Certificate should be saved (not rejected)
			if cert.ID == "" {
				t.Logf("Expected certificate to be saved with a valid ID")
				return false
			}

			// chain_valid should be false
			if cert.ChainValid {
				t.Logf("Expected chain_valid=false for cert without chain, got true")
				return false
			}

			// Verify it's persisted correctly in the database
			retrieved, err := svc.GetByID(ctx, cert.ID)
			if err != nil {
				t.Logf("GetByID failed: %v", err)
				return false
			}

			if retrieved.ChainValid {
				t.Logf("Expected persisted chain_valid=false, got true")
				return false
			}

			return true
		},
		genDomainList(),
		gen.IntRange(30, 730),
	))

	// Property: Certificate with valid chain PEM is saved with chain_valid = true
	properties.Property("certificate with valid chain is saved with chain_valid=true", prop.ForAll(
		func(domains []string, issuerCN string) bool {
			db := setupTestDB(t)
			dataDir := t.TempDir()
			repo := repository.NewCertificateRepository(db, dataDir)
			svc := NewCertificateService(repo, db)

			expireIn := 365 * 24 * time.Hour
			certPEM, keyPEM, chainPEM, _ := generateTestCertWithCA(t, domains, issuerCN, expireIn)

			ctx := context.Background()
			input := model.CreateCertInput{
				Name:     "With Chain Test",
				CertPEM:  certPEM,
				KeyPEM:   keyPEM,
				ChainPEM: chainPEM,
			}

			cert, err := svc.Create(ctx, input)
			if err != nil {
				t.Logf("Create failed: %v", err)
				return false
			}

			if cert.ID == "" {
				t.Logf("Expected certificate to be saved with a valid ID")
				return false
			}

			if !cert.ChainValid {
				t.Logf("Expected chain_valid=true for cert with valid chain, got false")
				return false
			}

			// Verify it's persisted correctly
			retrieved, err := svc.GetByID(ctx, cert.ID)
			if err != nil {
				t.Logf("GetByID failed: %v", err)
				return false
			}

			if !retrieved.ChainValid {
				t.Logf("Expected persisted chain_valid=true, got false")
				return false
			}

			return true
		},
		genDomainList(),
		genIssuerName(),
	))

	properties.TestingRun(t)
}
