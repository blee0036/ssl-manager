package worker

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/agent/config"
	"github.com/ssl-manager/ssl-manager/internal/model"
)

// generateTestCertAndKey generates a self-signed certificate and matching private key for testing.
func generateTestCertAndKey(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test.example.com"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privKey.PublicKey, privKey)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER := x509.MarshalPKCS1PrivateKey(privKey)
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM
}

// generateMismatchedKey generates a different RSA private key (not matching any cert).
func generateMismatchedKey(t *testing.T) []byte {
	t.Helper()

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	keyDER := x509.MarshalPKCS1PrivateKey(privKey)
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyDER})
}

func TestValidateCertKeyPair_Matching(t *testing.T) {
	certPEM, keyPEM := generateTestCertAndKey(t)

	err := ValidateCertKeyPair(certPEM, keyPEM)
	if err != nil {
		t.Errorf("expected no error for matching cert/key pair, got: %v", err)
	}
}

func TestValidateCertKeyPair_Mismatched(t *testing.T) {
	certPEM, _ := generateTestCertAndKey(t)
	mismatchedKey := generateMismatchedKey(t)

	err := ValidateCertKeyPair(certPEM, mismatchedKey)
	if err == nil {
		t.Error("expected error for mismatched cert/key pair, got nil")
	}
}

func TestValidateCertKeyPair_InvalidCertPEM(t *testing.T) {
	_, keyPEM := generateTestCertAndKey(t)

	err := ValidateCertKeyPair([]byte("not a valid PEM"), keyPEM)
	if err == nil {
		t.Error("expected error for invalid cert PEM, got nil")
	}
}

func TestValidateCertKeyPair_InvalidKeyPEM(t *testing.T) {
	certPEM, _ := generateTestCertAndKey(t)

	err := ValidateCertKeyPair(certPEM, []byte("not a valid PEM"))
	if err == nil {
		t.Error("expected error for invalid key PEM, got nil")
	}
}

func TestValidateCertKeyPair_ECDSAMatching(t *testing.T) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ECDSA key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "ecdsa.example.com"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privKey.PublicKey, privKey)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyDER, err := x509.MarshalECPrivateKey(privKey)
	if err != nil {
		t.Fatalf("failed to marshal EC key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	if err := ValidateCertKeyPair(certPEM, keyPEM); err != nil {
		t.Errorf("expected no error for matching ECDSA cert/key pair, got: %v", err)
	}
}

func TestWriteFilesAtomically_Success(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "certs", "cert.pem")
	keyPath := filepath.Join(tmpDir, "certs", "privkey.pem")

	certData := []byte("-----BEGIN CERTIFICATE-----\ntest cert\n-----END CERTIFICATE-----\n")
	keyData := []byte("-----BEGIN RSA PRIVATE KEY-----\ntest key\n-----END RSA PRIVATE KEY-----\n")

	w := &DeployWorker{}

	err := w.WriteFilesAtomically(certPath, keyPath, certData, keyData)
	if err != nil {
		t.Fatalf("WriteFilesAtomically failed: %v", err)
	}

	// Verify cert file exists and has correct content
	gotCert, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("failed to read cert file: %v", err)
	}
	if string(gotCert) != string(certData) {
		t.Errorf("cert content mismatch: got %q, want %q", gotCert, certData)
	}

	// Verify key file exists and has correct content
	gotKey, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("failed to read key file: %v", err)
	}
	if string(gotKey) != string(keyData) {
		t.Errorf("key content mismatch: got %q, want %q", gotKey, keyData)
	}

	// Verify permissions (only on Linux/Unix)
	if runtime.GOOS != "windows" {
		certInfo, _ := os.Stat(certPath)
		if certInfo.Mode().Perm() != 0644 {
			t.Errorf("cert file permissions: got %o, want 0644", certInfo.Mode().Perm())
		}

		keyInfo, _ := os.Stat(keyPath)
		if keyInfo.Mode().Perm() != 0600 {
			t.Errorf("key file permissions: got %o, want 0600", keyInfo.Mode().Perm())
		}
	}
}

func TestWriteFilesAtomically_CreatesDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "deep", "nested", "dir", "cert.pem")
	keyPath := filepath.Join(tmpDir, "another", "dir", "privkey.pem")

	certData := []byte("cert data")
	keyData := []byte("key data")

	w := &DeployWorker{}

	err := w.WriteFilesAtomically(certPath, keyPath, certData, keyData)
	if err != nil {
		t.Fatalf("WriteFilesAtomically failed: %v", err)
	}

	// Verify directories were created
	certDirInfo, err := os.Stat(filepath.Dir(certPath))
	if err != nil {
		t.Fatalf("cert directory not created: %v", err)
	}
	if !certDirInfo.IsDir() {
		t.Error("cert path parent is not a directory")
	}

	keyDirInfo, err := os.Stat(filepath.Dir(keyPath))
	if err != nil {
		t.Fatalf("key directory not created: %v", err)
	}
	if !keyDirInfo.IsDir() {
		t.Error("key path parent is not a directory")
	}
}

func TestWriteFilesAtomically_OverwritesExisting(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "cert.pem")
	keyPath := filepath.Join(tmpDir, "privkey.pem")

	// Write initial files
	os.WriteFile(certPath, []byte("old cert"), 0644)
	os.WriteFile(keyPath, []byte("old key"), 0600)

	newCert := []byte("new cert data")
	newKey := []byte("new key data")

	w := &DeployWorker{}

	err := w.WriteFilesAtomically(certPath, keyPath, newCert, newKey)
	if err != nil {
		t.Fatalf("WriteFilesAtomically failed: %v", err)
	}

	gotCert, _ := os.ReadFile(certPath)
	if string(gotCert) != string(newCert) {
		t.Errorf("cert not overwritten: got %q, want %q", gotCert, newCert)
	}

	gotKey, _ := os.ReadFile(keyPath)
	if string(gotKey) != string(newKey) {
		t.Errorf("key not overwritten: got %q, want %q", gotKey, newKey)
	}
}

func TestExecuteCommands_Success(t *testing.T) {
	w := &DeployWorker{
		CommandTimeout: 60 * time.Second,
	}

	commands := []string{"echo hello", "echo world"}
	outputs, err := w.ExecuteCommands(context.Background(), commands)
	if err != nil {
		t.Fatalf("ExecuteCommands failed: %v", err)
	}

	if len(outputs) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(outputs))
	}

	if outputs[0].ExitCode != 0 {
		t.Errorf("first command exit code: got %d, want 0. stderr: %s", outputs[0].ExitCode, outputs[0].Stderr)
	}

	if outputs[1].ExitCode != 0 {
		t.Errorf("second command exit code: got %d, want 0", outputs[1].ExitCode)
	}
}

func TestExecuteCommands_FailureStopsExecution(t *testing.T) {
	w := &DeployWorker{
		CommandTimeout: 60 * time.Second,
	}

	var commands []string
	if runtime.GOOS == "windows" {
		commands = []string{"echo first", "cmd /C exit 1", "echo should_not_run"}
	} else {
		commands = []string{"echo first", "exit 1", "echo should_not_run"}
	}

	outputs, err := w.ExecuteCommands(context.Background(), commands)
	if err == nil {
		t.Fatal("expected error when command fails, got nil")
	}

	// Should have 2 outputs: the successful first command and the failed second command
	if len(outputs) != 2 {
		t.Fatalf("expected 2 outputs (stopped at failure), got %d", len(outputs))
	}

	if outputs[0].ExitCode != 0 {
		t.Errorf("first command should succeed, got exit code %d", outputs[0].ExitCode)
	}

	if outputs[1].ExitCode == 0 {
		t.Error("second command should have non-zero exit code")
	}
}

func TestExecuteCommands_Timeout(t *testing.T) {
	w := &DeployWorker{
		CommandTimeout: 1 * time.Second,
	}

	var commands []string
	if runtime.GOOS == "windows" {
		commands = []string{"ping -n 11 127.0.0.1"}
	} else {
		commands = []string{"sleep 10"}
	}

	outputs, err := w.ExecuteCommands(context.Background(), commands)
	if err == nil {
		t.Fatal("expected error for timed-out command, got nil")
	}

	if len(outputs) != 1 {
		t.Fatalf("expected 1 output, got %d", len(outputs))
	}

	if outputs[0].ExitCode == 0 {
		t.Error("timed-out command should have non-zero exit code")
	}
}

func TestExecuteCommands_EmptyCommands(t *testing.T) {
	w := &DeployWorker{
		CommandTimeout: 60 * time.Second,
	}

	commands := []string{"", "  ", ""}
	outputs, err := w.ExecuteCommands(context.Background(), commands)
	if err != nil {
		t.Fatalf("ExecuteCommands failed for empty commands: %v", err)
	}

	if len(outputs) != 0 {
		t.Errorf("expected 0 outputs for empty commands, got %d", len(outputs))
	}
}

func TestParseCommands(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "single command",
			input:    "nginx -s reload",
			expected: []string{"nginx -s reload"},
		},
		{
			name:     "multiple commands",
			input:    "systemctl reload nginx\nsystemctl reload apache2",
			expected: []string{"systemctl reload nginx", "systemctl reload apache2"},
		},
		{
			name:     "with empty lines",
			input:    "echo hello\n\necho world\n",
			expected: []string{"echo hello", "echo world"},
		},
		{
			name:     "with whitespace",
			input:    "  echo hello  \n  echo world  ",
			expected: []string{"echo hello", "echo world"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseCommands(tt.input)
			if len(result) != len(tt.expected) {
				t.Fatalf("got %d commands, want %d", len(result), len(tt.expected))
			}
			for i, cmd := range result {
				if cmd != tt.expected[i] {
					t.Errorf("command[%d]: got %q, want %q", i, cmd, tt.expected[i])
				}
			}
		})
	}
}

func TestDownloadCertificate_Success(t *testing.T) {
	certPEM, keyPEM := generateTestCertAndKey(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/agent/machine-certificates/mc-123/download" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}

		resp := struct {
			Code    int                  `json:"code"`
			Message string               `json:"message"`
			Data    CertDownloadResponse `json:"data"`
		}{
			Code:    200,
			Message: "success",
			Data: CertDownloadResponse{
				CertificateID:     "cert-1",
				FingerprintSHA256: "abc123",
				FullchainPEM:      string(certPEM),
				PrivateKeyPEM:     string(keyPEM),
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	agentCfg := &config.AgentConfig{
		ServerURL:  server.URL,
		MachineID:  "machine-1",
		AgentToken: "test-token",
	}

	dw := NewDeployWorker(agentCfg, config.NewAgentLocalState(), "")

	result, err := dw.DownloadCertificate(context.Background(), "mc-123")
	if err != nil {
		t.Fatalf("DownloadCertificate failed: %v", err)
	}

	if result.CertificateID != "cert-1" {
		t.Errorf("CertificateID: got %q, want %q", result.CertificateID, "cert-1")
	}
	if result.FingerprintSHA256 != "abc123" {
		t.Errorf("FingerprintSHA256: got %q, want %q", result.FingerprintSHA256, "abc123")
	}
	if result.FullchainPEM != string(certPEM) {
		t.Error("FullchainPEM content mismatch")
	}
	if result.PrivateKeyPEM != string(keyPEM) {
		t.Error("PrivateKeyPEM content mismatch")
	}
}

func TestDownloadCertificate_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	agentCfg := &config.AgentConfig{
		ServerURL:  server.URL,
		MachineID:  "machine-1",
		AgentToken: "bad-token",
	}

	dw := NewDeployWorker(agentCfg, config.NewAgentLocalState(), "")

	_, err := dw.DownloadCertificate(context.Background(), "mc-123")
	if err == nil {
		t.Fatal("expected error for unauthorized request, got nil")
	}
}

func TestDownloadCertificate_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	agentCfg := &config.AgentConfig{
		ServerURL:  server.URL,
		MachineID:  "machine-1",
		AgentToken: "test-token",
	}

	dw := NewDeployWorker(agentCfg, config.NewAgentLocalState(), "")

	_, err := dw.DownloadCertificate(context.Background(), "mc-123")
	if err == nil {
		t.Fatal("expected error for server error, got nil")
	}
}

func TestDeploy_FullSuccess(t *testing.T) {
	certPEM, keyPEM := generateTestCertAndKey(t)
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "ssl", "cert.pem")
	keyPath := filepath.Join(tmpDir, "ssl", "privkey.pem")
	statePath := filepath.Join(tmpDir, "state.json")

	var deployLogReceived bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/agent/machine-certificates/mc-1/download" && r.Method == http.MethodGet:
			resp := struct {
				Code    int                  `json:"code"`
				Message string               `json:"message"`
				Data    CertDownloadResponse `json:"data"`
			}{
				Code:    200,
				Message: "success",
				Data: CertDownloadResponse{
					CertificateID:     "cert-1",
					FingerprintSHA256: "fingerprint-abc",
					FullchainPEM:      string(certPEM),
					PrivateKeyPEM:     string(keyPEM),
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)

		case r.URL.Path == "/api/agent/deployment-logs" && r.Method == http.MethodPost:
			deployLogReceived = true
			var logEntry struct {
				Status string `json:"status"`
			}
			json.NewDecoder(r.Body).Decode(&logEntry)
			if logEntry.Status != "success" {
				t.Errorf("expected deployment log status 'success', got %q", logEntry.Status)
			}
			w.WriteHeader(http.StatusCreated)

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	agentCfg := &config.AgentConfig{
		ServerURL:           server.URL,
		MachineID:           "machine-1",
		AgentToken:          "test-token",
		PollIntervalSeconds: 60,
	}

	state := config.NewAgentLocalState()
	dw := NewDeployWorker(agentCfg, state, statePath)

	certCfg := CertConfigResponse{
		MachineCertificateID: "mc-1",
		CertificateID:        "cert-1",
		FingerprintSHA256:    "fingerprint-abc",
		CertPath:             certPath,
		PrivateKeyPath:       keyPath,
		PostDeployCommands:   "echo deployed",
		ConfigRevision:       2,
	}

	result, err := dw.Deploy(context.Background(), certCfg)
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}

	if result.Status != "success" {
		t.Errorf("expected status 'success', got %q (error: %s)", result.Status, result.ErrorMessage)
	}

	// Verify files were written
	if _, err := os.Stat(certPath); err != nil {
		t.Errorf("cert file not created: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Errorf("key file not created: %v", err)
	}

	// Verify deployment log was reported
	if !deployLogReceived {
		t.Error("deployment log was not reported to server")
	}

	// Verify local state was updated
	certState := state.GetCertState("mc-1")
	if certState == nil {
		t.Fatal("local state not updated")
	}
	if certState.LastSyncedRevision != 2 {
		t.Errorf("LastSyncedRevision: got %d, want 2", certState.LastSyncedRevision)
	}
	if certState.LastSyncedFingerprint != "fingerprint-abc" {
		t.Errorf("LastSyncedFingerprint: got %q, want %q", certState.LastSyncedFingerprint, "fingerprint-abc")
	}
	if certState.LastDeployStatus != "success" {
		t.Errorf("LastDeployStatus: got %q, want %q", certState.LastDeployStatus, "success")
	}
}

func TestDeploy_CertKeyMismatch(t *testing.T) {
	certPEM, _ := generateTestCertAndKey(t)
	mismatchedKey := generateMismatchedKey(t)
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/agent/machine-certificates/mc-1/download":
			resp := struct {
				Code    int                  `json:"code"`
				Message string               `json:"message"`
				Data    CertDownloadResponse `json:"data"`
			}{
				Code:    200,
				Message: "success",
				Data: CertDownloadResponse{
					CertificateID:     "cert-1",
					FingerprintSHA256: "fp-123",
					FullchainPEM:      string(certPEM),
					PrivateKeyPEM:     string(mismatchedKey),
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)

		case r.URL.Path == "/api/agent/deployment-logs":
			var logEntry struct {
				Status       string `json:"status"`
				ErrorMessage string `json:"error_message"`
			}
			json.NewDecoder(r.Body).Decode(&logEntry)
			if logEntry.Status != "failed" {
				t.Errorf("expected deployment log status 'failed', got %q", logEntry.Status)
			}
			w.WriteHeader(http.StatusCreated)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	agentCfg := &config.AgentConfig{
		ServerURL:           server.URL,
		MachineID:           "machine-1",
		AgentToken:          "test-token",
		PollIntervalSeconds: 60,
	}

	state := config.NewAgentLocalState()
	dw := NewDeployWorker(agentCfg, state, statePath)

	certCfg := CertConfigResponse{
		MachineCertificateID: "mc-1",
		CertificateID:        "cert-1",
		FingerprintSHA256:    "fp-123",
		CertPath:             filepath.Join(tmpDir, "cert.pem"),
		PrivateKeyPath:       filepath.Join(tmpDir, "key.pem"),
		ConfigRevision:       1,
	}

	result, err := dw.Deploy(context.Background(), certCfg)
	if err == nil {
		t.Fatal("expected error for cert/key mismatch, got nil")
	}

	if result.Status != "failed" {
		t.Errorf("expected status 'failed', got %q", result.Status)
	}

	// Verify no files were written
	if _, err := os.Stat(certCfg.CertPath); !os.IsNotExist(err) {
		t.Error("cert file should not exist after mismatch failure")
	}
}

func TestDeploy_CommandFailure(t *testing.T) {
	certPEM, keyPEM := generateTestCertAndKey(t)
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "cert.pem")
	keyPath := filepath.Join(tmpDir, "key.pem")
	statePath := filepath.Join(tmpDir, "state.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/agent/machine-certificates/mc-1/download":
			resp := struct {
				Code    int                  `json:"code"`
				Message string               `json:"message"`
				Data    CertDownloadResponse `json:"data"`
			}{
				Code:    200,
				Message: "success",
				Data: CertDownloadResponse{
					CertificateID:     "cert-1",
					FingerprintSHA256: "fp-123",
					FullchainPEM:      string(certPEM),
					PrivateKeyPEM:     string(keyPEM),
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)

		case r.URL.Path == "/api/agent/deployment-logs":
			var logEntry struct {
				Status         string                `json:"status"`
				CommandOutputs []model.CommandOutput `json:"command_outputs"`
			}
			json.NewDecoder(r.Body).Decode(&logEntry)
			if logEntry.Status != "failed" {
				t.Errorf("expected deployment log status 'failed', got %q", logEntry.Status)
			}
			// Should have 2 command outputs: first success, second failure
			if len(logEntry.CommandOutputs) != 2 {
				t.Errorf("expected 2 command outputs, got %d", len(logEntry.CommandOutputs))
			}
			w.WriteHeader(http.StatusCreated)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	agentCfg := &config.AgentConfig{
		ServerURL:           server.URL,
		MachineID:           "machine-1",
		AgentToken:          "test-token",
		PollIntervalSeconds: 60,
	}

	state := config.NewAgentLocalState()
	dw := NewDeployWorker(agentCfg, state, statePath)

	certCfg := CertConfigResponse{
		MachineCertificateID: "mc-1",
		CertificateID:        "cert-1",
		FingerprintSHA256:    "fp-123",
		CertPath:             certPath,
		PrivateKeyPath:       keyPath,
		PostDeployCommands:   "",
		ConfigRevision:       1,
	}

	// Set platform-specific commands
	if runtime.GOOS == "windows" {
		certCfg.PostDeployCommands = "echo ok\ncmd /C exit 1\necho should_not_run"
	} else {
		certCfg.PostDeployCommands = "echo ok\nexit 1\necho should_not_run"
	}

	result, err := dw.Deploy(context.Background(), certCfg)
	if err == nil {
		t.Fatal("expected error for command failure, got nil")
	}

	if result.Status != "failed" {
		t.Errorf("expected status 'failed', got %q", result.Status)
	}

	// Files should still exist (they were written before commands ran)
	if _, err := os.Stat(certPath); err != nil {
		t.Errorf("cert file should exist even after command failure: %v", err)
	}

	// Local state should be updated with failed status
	certState := state.GetCertState("mc-1")
	if certState == nil {
		t.Fatal("local state should be updated even on command failure")
	}
	if certState.LastDeployStatus != "failed" {
		t.Errorf("LastDeployStatus: got %q, want %q", certState.LastDeployStatus, "failed")
	}
}

func TestReportDeploymentLog(t *testing.T) {
	var receivedLog struct {
		MachineCertificateID  string                `json:"machine_certificate_id"`
		MachineID             string                `json:"machine_id"`
		CertificateID         string                `json:"certificate_id"`
		Status                string                `json:"status"`
		CertFingerprintSHA256 string                `json:"cert_fingerprint_sha256"`
		CertPath              string                `json:"cert_path"`
		PrivateKeyPath        string                `json:"private_key_path"`
		CommandOutputs        []model.CommandOutput `json:"command_outputs"`
		ErrorMessage          string                `json:"error_message"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/deployment-logs" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected auth header")
		}
		json.NewDecoder(r.Body).Decode(&receivedLog)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	agentCfg := &config.AgentConfig{
		ServerURL:           server.URL,
		MachineID:           "machine-1",
		AgentToken:          "test-token",
		PollIntervalSeconds: 60,
	}

	dw := NewDeployWorker(agentCfg, config.NewAgentLocalState(), "")

	certCfg := CertConfigResponse{
		MachineCertificateID: "mc-1",
		CertificateID:        "cert-1",
		FingerprintSHA256:    "fp-abc",
		CertPath:             "/etc/ssl/cert.pem",
		PrivateKeyPath:       "/etc/ssl/key.pem",
	}

	result := &DeployResult{
		Status: "success",
		CommandOutputs: []model.CommandOutput{
			{Command: "nginx -s reload", ExitCode: 0, Stdout: "", Stderr: ""},
		},
		StartedAt:  time.Now().Add(-5 * time.Second),
		FinishedAt: time.Now(),
	}

	dw.reportDeploymentLog(context.Background(), certCfg, result)

	if receivedLog.MachineCertificateID != "mc-1" {
		t.Errorf("MachineCertificateID: got %q, want %q", receivedLog.MachineCertificateID, "mc-1")
	}
	if receivedLog.MachineID != "machine-1" {
		t.Errorf("MachineID: got %q, want %q", receivedLog.MachineID, "machine-1")
	}
	if receivedLog.Status != "success" {
		t.Errorf("Status: got %q, want %q", receivedLog.Status, "success")
	}
	if receivedLog.CertFingerprintSHA256 != "fp-abc" {
		t.Errorf("CertFingerprintSHA256: got %q, want %q", receivedLog.CertFingerprintSHA256, "fp-abc")
	}
}
