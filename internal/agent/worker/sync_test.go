package worker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ssl-manager/ssl-manager/internal/agent/config"
)

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
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "cert.pem")
	if err := os.WriteFile(certPath, []byte("cert content"), 0644); err != nil {
		t.Fatal(err)
	}

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
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "cert.pem")
	if err := os.WriteFile(certPath, []byte("cert content"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := CertConfigResponse{
		MachineCertificateID: "mc-1",
		CertificateID:        "cert-1",
		FingerprintSHA256:    "abc123",
		CertPath:             certPath,
		ConfigRevision:       3,
		LastDeployStatus:     "success",
	}

	localState := &config.MachineCertState{
		MachineCertificateID:  "mc-1",
		LastSyncedRevision:    2,
		LastSyncedFingerprint: "abc123",
		LastDeployStatus:      "success",
	}

	if !NeedsDeployment(cfg, localState) {
		t.Error("expected NeedsDeployment to return true when config_revision differs")
	}
}

func TestNeedsDeployment_StatusPending(t *testing.T) {
	// Create a temp file to simulate existing cert
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "cert.pem")
	if err := os.WriteFile(certPath, []byte("cert content"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := CertConfigResponse{
		MachineCertificateID: "mc-1",
		CertificateID:        "cert-1",
		FingerprintSHA256:    "abc123",
		CertPath:             certPath,
		ConfigRevision:       1,
		LastDeployStatus:     "pending",
	}

	localState := &config.MachineCertState{
		MachineCertificateID:  "mc-1",
		LastSyncedRevision:    1,
		LastSyncedFingerprint: "abc123",
		LastDeployStatus:      "success",
	}

	if !NeedsDeployment(cfg, localState) {
		t.Error("expected NeedsDeployment to return true when server status is pending")
	}
}

func TestNeedsDeployment_NoDeploymentNeeded(t *testing.T) {
	// Create a temp file to simulate existing cert
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "cert.pem")
	if err := os.WriteFile(certPath, []byte("cert content"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := CertConfigResponse{
		MachineCertificateID: "mc-1",
		CertificateID:        "cert-1",
		FingerprintSHA256:    "abc123",
		CertPath:             certPath,
		ConfigRevision:       1,
		LastDeployStatus:     "success",
	}

	localState := &config.MachineCertState{
		MachineCertificateID:  "mc-1",
		LastSyncedRevision:    1,
		LastSyncedFingerprint: "abc123",
		LastDeployStatus:      "success",
	}

	if NeedsDeployment(cfg, localState) {
		t.Error("expected NeedsDeployment to return false when no deployment is needed")
	}
}

func TestNeedsDeployment_EmptyLastDeployStatus(t *testing.T) {
	// Create a temp file to simulate existing cert
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "cert.pem")
	if err := os.WriteFile(certPath, []byte("cert content"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := CertConfigResponse{
		MachineCertificateID: "mc-1",
		CertificateID:        "cert-1",
		FingerprintSHA256:    "abc123",
		CertPath:             certPath,
		ConfigRevision:       1,
		LastDeployStatus:     "", // empty status should not trigger deployment
	}

	localState := &config.MachineCertState{
		MachineCertificateID:  "mc-1",
		LastSyncedRevision:    1,
		LastSyncedFingerprint: "abc123",
		LastDeployStatus:      "success",
	}

	if NeedsDeployment(cfg, localState) {
		t.Error("expected NeedsDeployment to return false when status is empty (not pending)")
	}
}

func TestNeedsDeployment_FailedStatusNoOtherChanges(t *testing.T) {
	// Create a temp file to simulate existing cert
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "cert.pem")
	if err := os.WriteFile(certPath, []byte("cert content"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := CertConfigResponse{
		MachineCertificateID: "mc-1",
		CertificateID:        "cert-1",
		FingerprintSHA256:    "abc123",
		CertPath:             certPath,
		ConfigRevision:       1,
		LastDeployStatus:     "failed", // failed status alone should not trigger re-deployment
	}

	localState := &config.MachineCertState{
		MachineCertificateID:  "mc-1",
		LastSyncedRevision:    1,
		LastSyncedFingerprint: "abc123",
		LastDeployStatus:      "failed",
	}

	if NeedsDeployment(cfg, localState) {
		t.Error("expected NeedsDeployment to return false when status is failed but nothing else changed")
	}
}
