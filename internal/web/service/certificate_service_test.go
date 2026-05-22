package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// generateSelfSignedCert generates a self-signed certificate and private key for testing.
func generateSelfSignedCert(t *testing.T, domains []string, expireIn time.Duration) (certPEM, keyPEM []byte) {
	t.Helper()

	// Generate RSA key
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	// Create certificate template
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: domains[0],
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(expireIn),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              domains,
	}

	// Self-sign
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privKey.PublicKey, privKey)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	// Encode to PEM
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privKey)})

	return certPEM, keyPEM
}

// generateECDSACert generates an ECDSA self-signed certificate and private key for testing.
func generateECDSACert(t *testing.T, domains []string, expireIn time.Duration) (certPEM, keyPEM []byte) {
	t.Helper()

	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ECDSA key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: domains[0],
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(expireIn),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              domains,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privKey.PublicKey, privKey)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	ecKeyBytes, err := x509.MarshalECPrivateKey(privKey)
	if err != nil {
		t.Fatalf("failed to marshal EC private key: %v", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: ecKeyBytes})

	return certPEM, keyPEM
}

func TestParsePEM_ValidSelfSigned(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewCertificateRepository(db, t.TempDir())
	svc := NewCertificateService(repo, db)

	domains := []string{"example.com", "www.example.com"}
	expireIn := 365 * 24 * time.Hour
	certPEM, _ := generateSelfSignedCert(t, domains, expireIn)

	meta, err := svc.ParsePEM(certPEM)
	if err != nil {
		t.Fatalf("ParsePEM failed: %v", err)
	}

	// Verify domains are extracted
	if len(meta.Domains) == 0 {
		t.Fatal("expected domains to be extracted")
	}

	// Check that all expected domains are present
	domainMap := make(map[string]bool)
	for _, d := range meta.Domains {
		domainMap[d] = true
	}
	for _, expected := range domains {
		if !domainMap[expected] {
			t.Errorf("expected domain %q not found in parsed domains: %v", expected, meta.Domains)
		}
	}

	// Verify expiration is in the future
	if meta.ExpireAt.Before(time.Now()) {
		t.Error("expected expiration to be in the future")
	}

	// Verify fingerprint is non-empty
	if meta.FingerprintSHA256 == "" {
		t.Error("expected non-empty fingerprint")
	}

	// Verify fingerprint is a valid hex string (64 chars for SHA256)
	if len(meta.FingerprintSHA256) != 64 {
		t.Errorf("expected fingerprint length 64, got %d", len(meta.FingerprintSHA256))
	}

	// Verify issuer is set (self-signed, so issuer = subject CN)
	if meta.Issuer == "" {
		t.Error("expected non-empty issuer")
	}
}

func TestParsePEM_InvalidPEM(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewCertificateRepository(db, t.TempDir())
	svc := NewCertificateService(repo, db)

	_, err := svc.ParsePEM([]byte("not a valid PEM"))
	if err == nil {
		t.Fatal("expected error for invalid PEM, got nil")
	}
}

func TestValidateKeyPair_Matching_RSA(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewCertificateRepository(db, t.TempDir())
	svc := NewCertificateService(repo, db)

	certPEM, keyPEM := generateSelfSignedCert(t, []string{"test.com"}, 365*24*time.Hour)

	err := svc.ValidateKeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("expected matching key pair to validate, got error: %v", err)
	}
}

func TestValidateKeyPair_Matching_ECDSA(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewCertificateRepository(db, t.TempDir())
	svc := NewCertificateService(repo, db)

	certPEM, keyPEM := generateECDSACert(t, []string{"test.com"}, 365*24*time.Hour)

	err := svc.ValidateKeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("expected matching ECDSA key pair to validate, got error: %v", err)
	}
}

func TestValidateKeyPair_NonMatching(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewCertificateRepository(db, t.TempDir())
	svc := NewCertificateService(repo, db)

	// Generate two different key pairs
	certPEM, _ := generateSelfSignedCert(t, []string{"test.com"}, 365*24*time.Hour)
	_, otherKeyPEM := generateSelfSignedCert(t, []string{"other.com"}, 365*24*time.Hour)

	err := svc.ValidateKeyPair(certPEM, otherKeyPEM)
	if err == nil {
		t.Fatal("expected error for non-matching key pair, got nil")
	}
}

func TestValidateKeyPair_RSACertWithECDSAKey(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewCertificateRepository(db, t.TempDir())
	svc := NewCertificateService(repo, db)

	// RSA cert
	certPEM, _ := generateSelfSignedCert(t, []string{"test.com"}, 365*24*time.Hour)
	// ECDSA key
	_, ecKeyPEM := generateECDSACert(t, []string{"test.com"}, 365*24*time.Hour)

	err := svc.ValidateKeyPair(certPEM, ecKeyPEM)
	if err == nil {
		t.Fatal("expected error for RSA cert with ECDSA key, got nil")
	}
}

func TestCertCreate_AndGetByID(t *testing.T) {
	db := setupTestDB(t)
	dataDir := t.TempDir()
	repo := repository.NewCertificateRepository(db, dataDir)
	svc := NewCertificateService(repo, db)

	domains := []string{"mysite.com", "www.mysite.com"}
	certPEM, keyPEM := generateSelfSignedCert(t, domains, 365*24*time.Hour)

	input := model.CreateCertInput{
		Name:    "My Test Certificate",
		CertPEM: certPEM,
		KeyPEM:  keyPEM,
		// No chain PEM - chain_valid should be false
		AutoRenew: true,
	}

	ctx := context.Background()
	cert, err := svc.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify returned certificate
	if cert.ID == "" {
		t.Error("expected non-empty certificate ID")
	}
	if cert.Name != "My Test Certificate" {
		t.Errorf("expected name 'My Test Certificate', got %q", cert.Name)
	}
	if cert.Source != "upload" {
		t.Errorf("expected source 'upload', got %q", cert.Source)
	}
	if cert.AutoRenew != true {
		t.Error("expected auto_renew to be true")
	}
	if cert.ChainValid != false {
		t.Error("expected chain_valid to be false when no chain PEM provided")
	}
	if cert.FingerprintSHA256 == "" {
		t.Error("expected non-empty fingerprint")
	}

	// Verify domains
	domainMap := make(map[string]bool)
	for _, d := range cert.Domains {
		domainMap[d] = true
	}
	for _, expected := range domains {
		if !domainMap[expected] {
			t.Errorf("expected domain %q not found in cert domains: %v", expected, cert.Domains)
		}
	}

	// Verify GetByID
	retrieved, err := svc.GetByID(ctx, cert.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if retrieved.ID != cert.ID {
		t.Errorf("expected ID %q, got %q", cert.ID, retrieved.ID)
	}
	if retrieved.Name != cert.Name {
		t.Errorf("expected name %q, got %q", cert.Name, retrieved.Name)
	}
	if retrieved.FingerprintSHA256 != cert.FingerprintSHA256 {
		t.Errorf("expected fingerprint %q, got %q", cert.FingerprintSHA256, retrieved.FingerprintSHA256)
	}

	// Verify files were saved
	certDir := filepath.Join(dataDir, "certificates", cert.ID)
	if _, err := os.Stat(filepath.Join(certDir, "cert.pem")); os.IsNotExist(err) {
		t.Error("expected cert.pem to exist")
	}
	if _, err := os.Stat(filepath.Join(certDir, "fullchain.pem")); os.IsNotExist(err) {
		t.Error("expected fullchain.pem to exist")
	}
	if _, err := os.Stat(filepath.Join(certDir, "privkey.pem")); os.IsNotExist(err) {
		t.Error("expected privkey.pem to exist")
	}
}

func TestCertCreate_WithChainPEM(t *testing.T) {
	db := setupTestDB(t)
	dataDir := t.TempDir()
	repo := repository.NewCertificateRepository(db, dataDir)
	svc := NewCertificateService(repo, db)

	// Generate a CA and sign a leaf cert to simulate a chain
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate CA key: %v", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(100),
		Subject: pkix.Name{
			CommonName: "Test CA",
		},
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
	chainPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})

	// Generate leaf cert signed by CA
	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate leaf key: %v", err)
	}

	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(200),
		Subject: pkix.Name{
			CommonName: "leaf.example.com",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"leaf.example.com"},
	}

	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("failed to parse CA cert: %v", err)
	}

	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("failed to create leaf cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(leafKey)})

	input := model.CreateCertInput{
		Name:     "Chained Certificate",
		CertPEM:  certPEM,
		KeyPEM:   keyPEM,
		ChainPEM: chainPEM,
	}

	ctx := context.Background()
	cert, err := svc.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create with chain failed: %v", err)
	}

	if !cert.ChainValid {
		t.Error("expected chain_valid to be true when valid chain is provided")
	}

	// Verify chain.pem was saved
	certDir := filepath.Join(dataDir, "certificates", cert.ID)
	if _, err := os.Stat(filepath.Join(certDir, "chain.pem")); os.IsNotExist(err) {
		t.Error("expected chain.pem to exist")
	}
}

func TestCertCreate_InvalidKeyPair(t *testing.T) {
	db := setupTestDB(t)
	dataDir := t.TempDir()
	repo := repository.NewCertificateRepository(db, dataDir)
	svc := NewCertificateService(repo, db)

	certPEM, _ := generateSelfSignedCert(t, []string{"test.com"}, 365*24*time.Hour)
	_, wrongKeyPEM := generateSelfSignedCert(t, []string{"other.com"}, 365*24*time.Hour)

	input := model.CreateCertInput{
		Name:    "Bad Cert",
		CertPEM: certPEM,
		KeyPEM:  wrongKeyPEM,
	}

	ctx := context.Background()
	_, err := svc.Create(ctx, input)
	if err == nil {
		t.Fatal("expected error for mismatched key pair, got nil")
	}
}

func TestCertDelete(t *testing.T) {
	db := setupTestDB(t)
	dataDir := t.TempDir()
	repo := repository.NewCertificateRepository(db, dataDir)
	svc := NewCertificateService(repo, db)

	certPEM, keyPEM := generateSelfSignedCert(t, []string{"delete-me.com"}, 365*24*time.Hour)

	input := model.CreateCertInput{
		Name:    "To Delete",
		CertPEM: certPEM,
		KeyPEM:  keyPEM,
	}

	ctx := context.Background()
	cert, err := svc.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Delete
	err = svc.Delete(ctx, cert.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify it's gone
	_, err = svc.GetByID(ctx, cert.ID)
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}

	// Verify files are gone
	certDir := filepath.Join(dataDir, "certificates", cert.ID)
	if _, err := os.Stat(certDir); !os.IsNotExist(err) {
		t.Error("expected certificate directory to be removed")
	}
}

func TestMarkAssociatedPendingSync(t *testing.T) {
	db := setupTestDB(t)
	dataDir := t.TempDir()
	repo := repository.NewCertificateRepository(db, dataDir)
	svc := NewCertificateService(repo, db)

	// Create a certificate first
	certPEM, keyPEM := generateSelfSignedCert(t, []string{"sync-test.com"}, 365*24*time.Hour)
	input := model.CreateCertInput{
		Name:    "Sync Test",
		CertPEM: certPEM,
		KeyPEM:  keyPEM,
	}

	ctx := context.Background()
	cert, err := svc.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Insert some machine_certificates manually
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(ctx, `INSERT INTO machine_certificates (id, machine_id, certificate_id, cert_path, private_key_path, config_revision, last_deploy_status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"mc-1", "machine-1", cert.ID, "/etc/ssl/cert.pem", "/etc/ssl/key.pem", 1, "success", now, now)
	if err != nil {
		t.Fatalf("failed to insert machine_certificate: %v", err)
	}

	_, err = db.ExecContext(ctx, `INSERT INTO machine_certificates (id, machine_id, certificate_id, cert_path, private_key_path, config_revision, last_deploy_status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"mc-2", "machine-2", cert.ID, "/etc/ssl/cert.pem", "/etc/ssl/key.pem", 2, "success", now, now)
	if err != nil {
		t.Fatalf("failed to insert machine_certificate: %v", err)
	}

	// Mark as pending sync
	err = svc.MarkAssociatedPendingSync(ctx, cert.ID)
	if err != nil {
		t.Fatalf("MarkAssociatedPendingSync failed: %v", err)
	}

	// Verify both are now pending with incremented revision
	var status1, status2 string
	var rev1, rev2 int

	err = db.QueryRowContext(ctx, "SELECT last_deploy_status, config_revision FROM machine_certificates WHERE id = ?", "mc-1").Scan(&status1, &rev1)
	if err != nil {
		t.Fatalf("failed to query mc-1: %v", err)
	}
	err = db.QueryRowContext(ctx, "SELECT last_deploy_status, config_revision FROM machine_certificates WHERE id = ?", "mc-2").Scan(&status2, &rev2)
	if err != nil {
		t.Fatalf("failed to query mc-2: %v", err)
	}

	if status1 != "pending" {
		t.Errorf("expected mc-1 status 'pending', got %q", status1)
	}
	if status2 != "pending" {
		t.Errorf("expected mc-2 status 'pending', got %q", status2)
	}
	if rev1 != 2 {
		t.Errorf("expected mc-1 revision 2, got %d", rev1)
	}
	if rev2 != 3 {
		t.Errorf("expected mc-2 revision 3, got %d", rev2)
	}
}

func TestCertUpdate_WithNewPEM(t *testing.T) {
	db := setupTestDB(t)
	dataDir := t.TempDir()
	repo := repository.NewCertificateRepository(db, dataDir)
	svc := NewCertificateService(repo, db)

	// Create initial certificate
	certPEM, keyPEM := generateSelfSignedCert(t, []string{"update-test.com"}, 365*24*time.Hour)
	input := model.CreateCertInput{
		Name:    "Update Test",
		CertPEM: certPEM,
		KeyPEM:  keyPEM,
	}

	ctx := context.Background()
	cert, err := svc.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Insert a machine_certificate to verify pending sync
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(ctx, `INSERT INTO machine_certificates (id, machine_id, certificate_id, cert_path, private_key_path, config_revision, last_deploy_status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"mc-update-1", "machine-1", cert.ID, "/etc/ssl/cert.pem", "/etc/ssl/key.pem", 1, "success", now, now)
	if err != nil {
		t.Fatalf("failed to insert machine_certificate: %v", err)
	}

	// Generate new certificate
	newCertPEM, newKeyPEM := generateSelfSignedCert(t, []string{"updated.com", "www.updated.com"}, 730*24*time.Hour)

	updateInput := model.UpdateCertInput{
		CertPEM: newCertPEM,
		KeyPEM:  newKeyPEM,
	}

	updated, err := svc.Update(ctx, cert.ID, updateInput)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify metadata was updated
	if updated.FingerprintSHA256 == cert.FingerprintSHA256 {
		t.Error("expected fingerprint to change after update")
	}

	// Verify domains were updated
	domainMap := make(map[string]bool)
	for _, d := range updated.Domains {
		domainMap[d] = true
	}
	if !domainMap["updated.com"] {
		t.Error("expected 'updated.com' in updated domains")
	}

	// Verify machine_certificate was marked as pending
	var status string
	var rev int
	err = db.QueryRowContext(ctx, "SELECT last_deploy_status, config_revision FROM machine_certificates WHERE id = ?", "mc-update-1").Scan(&status, &rev)
	if err != nil {
		t.Fatalf("failed to query machine_certificate: %v", err)
	}
	if status != "pending" {
		t.Errorf("expected status 'pending', got %q", status)
	}
	if rev != 2 {
		t.Errorf("expected revision 2, got %d", rev)
	}
}

func TestCertList(t *testing.T) {
	db := setupTestDB(t)
	dataDir := t.TempDir()
	repo := repository.NewCertificateRepository(db, dataDir)
	svc := NewCertificateService(repo, db)

	ctx := context.Background()

	// Create two certificates
	certPEM1, keyPEM1 := generateSelfSignedCert(t, []string{"list1.com"}, 365*24*time.Hour)
	certPEM2, keyPEM2 := generateSelfSignedCert(t, []string{"list2.com"}, 365*24*time.Hour)

	_, err := svc.Create(ctx, model.CreateCertInput{Name: "Cert 1", CertPEM: certPEM1, KeyPEM: keyPEM1})
	if err != nil {
		t.Fatalf("Create cert 1 failed: %v", err)
	}
	_, err = svc.Create(ctx, model.CreateCertInput{Name: "Cert 2", CertPEM: certPEM2, KeyPEM: keyPEM2})
	if err != nil {
		t.Fatalf("Create cert 2 failed: %v", err)
	}

	// List all
	certs, err := svc.List(ctx, model.CertFilter{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(certs) != 2 {
		t.Errorf("expected 2 certificates, got %d", len(certs))
	}
}
