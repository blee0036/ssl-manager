package repository

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/model"
)

func newTestCertificate() *model.Certificate {
	return &model.Certificate{
		Name:              "test-cert",
		Domains:           []string{"example.com", "*.example.com"},
		Source:            "upload",
		ExpireAt:          time.Now().Add(90 * 24 * time.Hour).UTC().Truncate(time.Second),
		AutoRenew:         true,
		Issuer:            "Let's Encrypt",
		FingerprintSHA256: "abc123def456",
		ChainValid:        true,
		ThirdpartDNSID:    "",
		RenewStatus:       "",
	}
}

func TestCertRepository_Create(t *testing.T) {
	db := setupTestDB(t)
	tmpDir := t.TempDir()
	repo := NewCertificateRepository(db, tmpDir)

	ctx := context.Background()
	cert := newTestCertificate()

	err := repo.Create(ctx, cert)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify ID was generated
	if cert.ID == "" {
		t.Fatal("expected ID to be generated")
	}

	// Verify directory was created
	dirPath := repo.CertDirPath(cert.ID)
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		t.Fatalf("expected certificate directory to be created at %s", dirPath)
	}

	// Verify timestamps were set
	if cert.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be set")
	}
	if cert.UpdatedAt.IsZero() {
		t.Fatal("expected UpdatedAt to be set")
	}
}

func TestCertRepository_GetByID(t *testing.T) {
	db := setupTestDB(t)
	tmpDir := t.TempDir()
	repo := NewCertificateRepository(db, tmpDir)

	ctx := context.Background()
	cert := newTestCertificate()

	err := repo.Create(ctx, cert)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Retrieve by ID
	got, err := repo.GetByID(ctx, cert.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if got.ID != cert.ID {
		t.Errorf("expected ID %s, got %s", cert.ID, got.ID)
	}
	if got.Name != cert.Name {
		t.Errorf("expected Name %s, got %s", cert.Name, got.Name)
	}
	if len(got.Domains) != 2 {
		t.Errorf("expected 2 domains, got %d", len(got.Domains))
	}
	if got.Domains[0] != "example.com" {
		t.Errorf("expected first domain example.com, got %s", got.Domains[0])
	}
	if got.Source != "upload" {
		t.Errorf("expected source upload, got %s", got.Source)
	}
	if got.AutoRenew != true {
		t.Error("expected AutoRenew to be true")
	}
	if got.ChainValid != true {
		t.Error("expected ChainValid to be true")
	}
}

func TestCertRepository_GetByID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	tmpDir := t.TempDir()
	repo := NewCertificateRepository(db, tmpDir)

	ctx := context.Background()
	_, err := repo.GetByID(ctx, "nonexistent-id")
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestCertRepository_List(t *testing.T) {
	db := setupTestDB(t)
	tmpDir := t.TempDir()
	repo := NewCertificateRepository(db, tmpDir)

	ctx := context.Background()

	// Create multiple certificates
	cert1 := newTestCertificate()
	cert1.Name = "cert-1"
	cert1.Source = "upload"
	cert1.AutoRenew = true

	cert2 := newTestCertificate()
	cert2.Name = "cert-2"
	cert2.Source = "certbot_cloudflare_dns"
	cert2.AutoRenew = false

	if err := repo.Create(ctx, cert1); err != nil {
		t.Fatalf("Create cert1 failed: %v", err)
	}
	if err := repo.Create(ctx, cert2); err != nil {
		t.Fatalf("Create cert2 failed: %v", err)
	}

	// List all
	certs, err := repo.List(ctx, model.CertFilter{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(certs) != 2 {
		t.Fatalf("expected 2 certificates, got %d", len(certs))
	}

	// Filter by source
	certs, err = repo.List(ctx, model.CertFilter{Source: "upload"})
	if err != nil {
		t.Fatalf("List with source filter failed: %v", err)
	}
	if len(certs) != 1 {
		t.Fatalf("expected 1 certificate with source=upload, got %d", len(certs))
	}
	if certs[0].Name != "cert-1" {
		t.Errorf("expected cert-1, got %s", certs[0].Name)
	}

	// Filter by auto_renew
	autoRenewTrue := true
	certs, err = repo.List(ctx, model.CertFilter{AutoRenew: &autoRenewTrue})
	if err != nil {
		t.Fatalf("List with auto_renew filter failed: %v", err)
	}
	if len(certs) != 1 {
		t.Fatalf("expected 1 certificate with auto_renew=true, got %d", len(certs))
	}
}

func TestCertRepository_Update(t *testing.T) {
	db := setupTestDB(t)
	tmpDir := t.TempDir()
	repo := NewCertificateRepository(db, tmpDir)

	ctx := context.Background()
	cert := newTestCertificate()

	if err := repo.Create(ctx, cert); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Update name and auto_renew
	updates := map[string]interface{}{
		"name":       "updated-cert",
		"auto_renew": false,
	}

	if err := repo.Update(ctx, cert.ID, updates); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify update
	got, err := repo.GetByID(ctx, cert.ID)
	if err != nil {
		t.Fatalf("GetByID after update failed: %v", err)
	}
	if got.Name != "updated-cert" {
		t.Errorf("expected name updated-cert, got %s", got.Name)
	}
	if got.AutoRenew != false {
		t.Error("expected AutoRenew to be false after update")
	}
}

func TestCertRepository_Update_NotFound(t *testing.T) {
	db := setupTestDB(t)
	tmpDir := t.TempDir()
	repo := NewCertificateRepository(db, tmpDir)

	ctx := context.Background()
	err := repo.Update(ctx, "nonexistent-id", map[string]interface{}{"name": "test"})
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestCertRepository_Delete(t *testing.T) {
	db := setupTestDB(t)
	tmpDir := t.TempDir()
	repo := NewCertificateRepository(db, tmpDir)

	ctx := context.Background()
	cert := newTestCertificate()

	if err := repo.Create(ctx, cert); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify directory exists
	dirPath := repo.CertDirPath(cert.ID)
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		t.Fatal("expected directory to exist before delete")
	}

	// Delete
	if err := repo.Delete(ctx, cert.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify record is gone
	_, err := repo.GetByID(ctx, cert.ID)
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows after delete, got %v", err)
	}

	// Verify directory is removed
	if _, err := os.Stat(dirPath); !os.IsNotExist(err) {
		t.Fatal("expected directory to be removed after delete")
	}
}

func TestCertRepository_SaveAndReadCertFiles(t *testing.T) {
	db := setupTestDB(t)
	tmpDir := t.TempDir()
	repo := NewCertificateRepository(db, tmpDir)

	ctx := context.Background()
	cert := newTestCertificate()

	if err := repo.Create(ctx, cert); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Save cert files
	certPEM := []byte("-----BEGIN CERTIFICATE-----\ntest cert\n-----END CERTIFICATE-----")
	chainPEM := []byte("-----BEGIN CERTIFICATE-----\ntest chain\n-----END CERTIFICATE-----")
	fullchainPEM := []byte("-----BEGIN CERTIFICATE-----\ntest fullchain\n-----END CERTIFICATE-----")
	privkeyPEM := []byte("-----BEGIN PRIVATE KEY-----\ntest key\n-----END PRIVATE KEY-----")

	err := repo.SaveCertFiles(cert.ID, certPEM, chainPEM, fullchainPEM, privkeyPEM)
	if err != nil {
		t.Fatalf("SaveCertFiles failed: %v", err)
	}

	// Verify file permissions (only on non-Windows platforms)
	dirPath := repo.CertDirPath(cert.ID)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dirPath, "privkey.pem"))
		if err != nil {
			t.Fatalf("failed to stat privkey.pem: %v", err)
		}
		if info.Mode().Perm() != 0600 {
			t.Errorf("expected privkey.pem permissions 0600, got %o", info.Mode().Perm())
		}

		info, err = os.Stat(filepath.Join(dirPath, "cert.pem"))
		if err != nil {
			t.Fatalf("failed to stat cert.pem: %v", err)
		}
		if info.Mode().Perm() != 0644 {
			t.Errorf("expected cert.pem permissions 0644, got %o", info.Mode().Perm())
		}
	}

	// Read cert files
	gotCert, gotChain, gotFullchain, gotPrivkey, err := repo.ReadCertFiles(cert.ID)
	if err != nil {
		t.Fatalf("ReadCertFiles failed: %v", err)
	}

	if string(gotCert) != string(certPEM) {
		t.Errorf("cert.pem content mismatch")
	}
	if string(gotChain) != string(chainPEM) {
		t.Errorf("chain.pem content mismatch")
	}
	if string(gotFullchain) != string(fullchainPEM) {
		t.Errorf("fullchain.pem content mismatch")
	}
	if string(gotPrivkey) != string(privkeyPEM) {
		t.Errorf("privkey.pem content mismatch")
	}
}

func TestCertRepository_DeleteCertFiles(t *testing.T) {
	db := setupTestDB(t)
	tmpDir := t.TempDir()
	repo := NewCertificateRepository(db, tmpDir)

	ctx := context.Background()
	cert := newTestCertificate()

	if err := repo.Create(ctx, cert); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Save some files
	err := repo.SaveCertFiles(cert.ID, []byte("cert"), []byte("chain"), []byte("fullchain"), []byte("key"))
	if err != nil {
		t.Fatalf("SaveCertFiles failed: %v", err)
	}

	// Delete cert files
	if err := repo.DeleteCertFiles(cert.ID); err != nil {
		t.Fatalf("DeleteCertFiles failed: %v", err)
	}

	// Verify directory is gone
	dirPath := repo.CertDirPath(cert.ID)
	if _, err := os.Stat(dirPath); !os.IsNotExist(err) {
		t.Fatal("expected directory to be removed")
	}
}

func TestCertRepository_ListExpiringSoon(t *testing.T) {
	db := setupTestDB(t)
	tmpDir := t.TempDir()
	repo := NewCertificateRepository(db, tmpDir)

	ctx := context.Background()

	// Create a certificate expiring in 10 days
	cert1 := newTestCertificate()
	cert1.Name = "expiring-soon"
	cert1.ExpireAt = time.Now().Add(10 * 24 * time.Hour).UTC().Truncate(time.Second)

	// Create a certificate expiring in 60 days
	cert2 := newTestCertificate()
	cert2.Name = "not-expiring-soon"
	cert2.ExpireAt = time.Now().Add(60 * 24 * time.Hour).UTC().Truncate(time.Second)

	if err := repo.Create(ctx, cert1); err != nil {
		t.Fatalf("Create cert1 failed: %v", err)
	}
	if err := repo.Create(ctx, cert2); err != nil {
		t.Fatalf("Create cert2 failed: %v", err)
	}

	// List expiring within 15 days
	certs, err := repo.ListExpiringSoon(ctx, 15)
	if err != nil {
		t.Fatalf("ListExpiringSoon failed: %v", err)
	}
	if len(certs) != 1 {
		t.Fatalf("expected 1 expiring certificate, got %d", len(certs))
	}
	if certs[0].Name != "expiring-soon" {
		t.Errorf("expected expiring-soon, got %s", certs[0].Name)
	}

	// List expiring within 90 days (should include both)
	certs, err = repo.ListExpiringSoon(ctx, 90)
	if err != nil {
		t.Fatalf("ListExpiringSoon failed: %v", err)
	}
	if len(certs) != 2 {
		t.Fatalf("expected 2 expiring certificates, got %d", len(certs))
	}
}

func TestCertRepository_CertDirPath(t *testing.T) {
	repo := NewCertificateRepository(nil, "/data")
	path := repo.CertDirPath("test-id-123")
	expected := filepath.Join("/data", "certificates", "test-id-123")
	if path != expected {
		t.Errorf("expected %s, got %s", expected, path)
	}
}
