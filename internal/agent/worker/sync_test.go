package worker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ssl-manager/ssl-manager/internal/agent/config"
)

func writeSyncTestCertificate(t *testing.T) (string, string) {
	t.Helper()

	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "cert.pem")
	certPEM, _ := generateTestCertAndKey(t)
	if err := os.WriteFile(certPath, certPEM, 0644); err != nil {
		t.Fatalf("failed to write test certificate: %v", err)
	}

	fingerprint, err := certificateFingerprint(certPath)
	if err != nil {
		t.Fatalf("failed to fingerprint test certificate: %v", err)
	}
	return certPath, fingerprint
}

func TestNeedsDeployment_NilLocalState(t *testing.T) {
	cfg := CertConfigResponse{
		MachineCertificateID: "mc-1",
		CertificateID:        "cert-1",
		FingerprintSHA256:    "abc123",
		CertPath:             "/etc/ssl/cert.pem",
		ConfigRevision:       1,
		LastDeployStatus:     "success",
	}

	if !NeedsDeployment(cfg, nil) {
		t.Error("expected NeedsDeployment to return true when localState is nil")
	}
}

func TestNeedsDeployment_LocalFileNotExist(t *testing.T) {
	cfg := CertConfigResponse{
		MachineCertificateID: "mc-1",
		CertificateID:        "cert-1",
		FingerprintSHA256:    "abc123",
		CertPath:             "/nonexistent/path/cert.pem",
		ConfigRevision:       1,
		LastDeployStatus:     "success",
	}

	localState := &config.MachineCertState{
		MachineCertificateID:  "mc-1",
		LastSyncedRevision:    1,
		LastSyncedFingerprint: "abc123",
		LastDeployStatus:      "success",
	}

	if !NeedsDeployment(cfg, localState) {
		t.Error("expected NeedsDeployment to return true when local cert file doesn't exist")
	}
}

func TestNeedsDeployment_FingerprintMismatch(t *testing.T) {
	// Create a temp file to simulate existing cert
	certPath, _ := writeSyncTestCertificate(t)

	cfg := CertConfigResponse{
		MachineCertificateID: "mc-1",
		CertificateID:        "cert-1",
		FingerprintSHA256:    "new-fingerprint",
		CertPath:             certPath,
		ConfigRevision:       1,
		LastDeployStatus:     "success",
	}

	localState := &config.MachineCertState{
		MachineCertificateID:  "mc-1",
		LastSyncedRevision:    1,
		LastSyncedFingerprint: "old-fingerprint",
		LastDeployStatus:      "success",
	}

	if !NeedsDeployment(cfg, localState) {
		t.Error("expected NeedsDeployment to return true when fingerprints don't match")
	}
}

func TestNeedsDeployment_ConfigRevisionDiffers(t *testing.T) {
	// Create a temp file to simulate existing cert
	certPath, fingerprint := writeSyncTestCertificate(t)

	cfg := CertConfigResponse{
		MachineCertificateID: "mc-1",
		CertificateID:        "cert-1",
		FingerprintSHA256:    fingerprint,
		CertPath:             certPath,
		ConfigRevision:       3,
		LastDeployStatus:     "success",
	}

	localState := &config.MachineCertState{
		MachineCertificateID:  "mc-1",
		LastSyncedRevision:    2,
		LastSyncedFingerprint: fingerprint,
		LastDeployStatus:      "success",
	}

	if !NeedsDeployment(cfg, localState) {
		t.Error("expected NeedsDeployment to return true when config_revision differs")
	}
}

func TestNeedsDeployment_StatusPending(t *testing.T) {
	// Create a temp file to simulate existing cert
	certPath, fingerprint := writeSyncTestCertificate(t)

	cfg := CertConfigResponse{
		MachineCertificateID: "mc-1",
		CertificateID:        "cert-1",
		FingerprintSHA256:    fingerprint,
		CertPath:             certPath,
		ConfigRevision:       1,
		LastDeployStatus:     "pending",
	}

	localState := &config.MachineCertState{
		MachineCertificateID:  "mc-1",
		LastSyncedRevision:    1,
		LastSyncedFingerprint: fingerprint,
		LastDeployStatus:      "success",
	}

	if !NeedsDeployment(cfg, localState) {
		t.Error("expected NeedsDeployment to return true when server status is pending")
	}
}

func TestNeedsDeployment_NoDeploymentNeeded(t *testing.T) {
	// Create a temp file to simulate existing cert
	certPath, fingerprint := writeSyncTestCertificate(t)

	cfg := CertConfigResponse{
		MachineCertificateID: "mc-1",
		CertificateID:        "cert-1",
		FingerprintSHA256:    fingerprint,
		CertPath:             certPath,
		ConfigRevision:       1,
		LastDeployStatus:     "success",
	}

	localState := &config.MachineCertState{
		MachineCertificateID:  "mc-1",
		LastSyncedRevision:    1,
		LastSyncedFingerprint: fingerprint,
		LastDeployStatus:      "success",
	}

	if NeedsDeployment(cfg, localState) {
		t.Error("expected NeedsDeployment to return false when no deployment is needed")
	}
}

func TestNeedsDeployment_EmptyLastDeployStatus(t *testing.T) {
	// Create a temp file to simulate existing cert
	certPath, fingerprint := writeSyncTestCertificate(t)

	cfg := CertConfigResponse{
		MachineCertificateID: "mc-1",
		CertificateID:        "cert-1",
		FingerprintSHA256:    fingerprint,
		CertPath:             certPath,
		ConfigRevision:       1,
		LastDeployStatus:     "", // empty status should not trigger deployment
	}

	localState := &config.MachineCertState{
		MachineCertificateID:  "mc-1",
		LastSyncedRevision:    1,
		LastSyncedFingerprint: fingerprint,
		LastDeployStatus:      "success",
	}

	if NeedsDeployment(cfg, localState) {
		t.Error("expected NeedsDeployment to return false when status is empty (not pending)")
	}
}

func TestNeedsDeployment_FailedStatusTriggersRetry(t *testing.T) {
	// Create a temp file to simulate existing cert
	certPath, fingerprint := writeSyncTestCertificate(t)

	cfg := CertConfigResponse{
		MachineCertificateID: "mc-1",
		CertificateID:        "cert-1",
		FingerprintSHA256:    fingerprint,
		CertPath:             certPath,
		ConfigRevision:       1,
		LastDeployStatus:     "failed", // failed status must trigger a retry
	}

	localState := &config.MachineCertState{
		MachineCertificateID:  "mc-1",
		LastSyncedRevision:    1,
		LastSyncedFingerprint: fingerprint,
		LastDeployStatus:      "failed",
	}

	if !NeedsDeployment(cfg, localState) {
		t.Error("expected NeedsDeployment to return true so failed deployment is retried")
	}
}

func TestNeedsDeployment_ActualCertificateFingerprintMismatch(t *testing.T) {
	certPath, actualFingerprint := writeSyncTestCertificate(t)
	const sourceFingerprint = "source-fingerprint"

	cfg := CertConfigResponse{
		MachineCertificateID: "mc-1",
		CertificateID:        "cert-1",
		FingerprintSHA256:    sourceFingerprint,
		CertPath:             certPath,
		ConfigRevision:       1,
		LastDeployStatus:     "success",
	}

	localState := &config.MachineCertState{
		MachineCertificateID:  "mc-1",
		LastSyncedRevision:    1,
		LastSyncedFingerprint: sourceFingerprint,
		LastDeployStatus:      "success",
	}

	if actualFingerprint == sourceFingerprint {
		t.Fatal("test setup requires different source and actual fingerprints")
	}
	if !NeedsDeployment(cfg, localState) {
		t.Error("expected actual certificate fingerprint mismatch to trigger deployment")
	}
}
