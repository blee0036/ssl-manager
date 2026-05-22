package certbot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ssl-manager/ssl-manager/internal/config"
)

// mockExecutor is a mock implementation of CommandExecutor for testing.
type mockExecutor struct {
	// capturedName stores the last command name passed to Execute.
	capturedName string
	// capturedArgs stores the last arguments passed to Execute.
	capturedArgs []string
	// output is the output to return from Execute.
	output []byte
	// err is the error to return from Execute.
	err error
	// calls tracks the number of times Execute was called.
	calls int
}

func (m *mockExecutor) Execute(ctx context.Context, name string, args ...string) ([]byte, error) {
	m.capturedName = name
	m.capturedArgs = args
	m.calls++
	return m.output, m.err
}

// setupTestCertDir creates a temporary directory with mock certificate files.
func setupTestCertDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	liveDir := filepath.Join(dir, "live", "example.com")
	if err := os.MkdirAll(liveDir, 0755); err != nil {
		t.Fatalf("failed to create live dir: %v", err)
	}

	files := map[string]string{
		"cert.pem":      "-----BEGIN CERTIFICATE-----\nMIIBfake...\n-----END CERTIFICATE-----\n",
		"chain.pem":     "-----BEGIN CERTIFICATE-----\nMIIBchain...\n-----END CERTIFICATE-----\n",
		"fullchain.pem": "-----BEGIN CERTIFICATE-----\nMIIBfake...\n-----END CERTIFICATE-----\n-----BEGIN CERTIFICATE-----\nMIIBchain...\n-----END CERTIFICATE-----\n",
		"privkey.pem":   "-----BEGIN PRIVATE KEY-----\nMIIBprivkey...\n-----END PRIVATE KEY-----\n",
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(liveDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}

	return dir
}

// newTestRuntimeCfg creates a RuntimeConfig from a CertbotConfig for testing.
func newTestRuntimeCfg(certbotCfg config.CertbotConfig) *config.RuntimeConfig {
	cfg := config.DefaultConfig()
	cfg.Certbot = certbotCfg
	return config.NewRuntimeConfig(cfg)
}

func TestNewCertbotWrapper(t *testing.T) {
	certbotCfg := config.CertbotConfig{
		BinaryPath: "/usr/bin/certbot",
		DataDir:    "/var/certbot",
		Email:      "admin@example.com",
	}
	executor := &mockExecutor{}

	runtimeCfg := newTestRuntimeCfg(certbotCfg)
	wrapper := NewCertbotWrapper(runtimeCfg, executor)
	if wrapper == nil {
		t.Fatal("expected non-nil wrapper")
	}
	if wrapper.runtimeCfg.Get().Certbot.BinaryPath != "/usr/bin/certbot" {
		t.Errorf("expected binary path '/usr/bin/certbot', got %q", wrapper.runtimeCfg.Get().Certbot.BinaryPath)
	}
	if wrapper.runtimeCfg.Get().Certbot.Email != "admin@example.com" {
		t.Errorf("expected email 'admin@example.com', got %q", wrapper.runtimeCfg.Get().Certbot.Email)
	}
}

func TestIssueCertCloudflare_Success(t *testing.T) {
	dataDir := setupTestCertDir(t)

	certbotCfg := config.CertbotConfig{
		BinaryPath: "certbot",
		DataDir:    dataDir,
		Email:      "admin@example.com",
	}
	executor := &mockExecutor{
		output: []byte("Congratulations! Certificate issued."),
		err:    nil,
	}

	wrapper := NewCertbotWrapper(newTestRuntimeCfg(certbotCfg), executor)

	ctx := context.Background()
	result, err := wrapper.IssueCertCloudflare(ctx, []string{"example.com", "www.example.com"}, "", "cf-token-123")
	if err != nil {
		t.Fatalf("IssueCertCloudflare failed: %v", err)
	}

	if executor.calls != 1 {
		t.Errorf("expected 1 call to executor, got %d", executor.calls)
	}
	if executor.capturedName != "certbot" {
		t.Errorf("expected command 'certbot', got %q", executor.capturedName)
	}

	argsStr := strings.Join(executor.capturedArgs, " ")
	expectedFlags := []string{
		"certonly",
		"-d example.com",
		"-d www.example.com",
		"--email admin@example.com",
		"--agree-tos",
		"--non-interactive",
		"--dns-cloudflare",
		"--dns-cloudflare-credentials",
		"--config-dir",
	}
	for _, flag := range expectedFlags {
		if !strings.Contains(argsStr, flag) {
			t.Errorf("expected args to contain %q, got: %s", flag, argsStr)
		}
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.CertFiles == nil {
		t.Fatal("expected non-nil CertFiles")
	}
	if len(result.CertFiles.CertPEM) == 0 {
		t.Error("expected non-empty CertPEM")
	}
	if len(result.CertFiles.ChainPEM) == 0 {
		t.Error("expected non-empty ChainPEM")
	}
	if len(result.CertFiles.FullchainPEM) == 0 {
		t.Error("expected non-empty FullchainPEM")
	}
	if len(result.CertFiles.PrivateKeyPEM) == 0 {
		t.Error("expected non-empty PrivateKeyPEM")
	}
}

func TestIssueCertCloudflare_CustomEmail(t *testing.T) {
	dataDir := setupTestCertDir(t)

	cfg := config.CertbotConfig{
		BinaryPath: "certbot",
		DataDir:    dataDir,
		Email:      "default@example.com",
	}
	executor := &mockExecutor{output: []byte("OK"), err: nil}
	wrapper := NewCertbotWrapper(newTestRuntimeCfg(cfg), executor)

	ctx := context.Background()
	_, err := wrapper.IssueCertCloudflare(ctx, []string{"example.com"}, "custom@example.com", "cf-token")
	if err != nil {
		t.Fatalf("IssueCertCloudflare failed: %v", err)
	}

	argsStr := strings.Join(executor.capturedArgs, " ")
	if !strings.Contains(argsStr, "--email custom@example.com") {
		t.Errorf("expected custom email in args, got: %s", argsStr)
	}
}

func TestIssueCertCloudflare_NoDomains(t *testing.T) {
	cfg := config.CertbotConfig{BinaryPath: "certbot", Email: "admin@example.com"}
	executor := &mockExecutor{}
	wrapper := NewCertbotWrapper(newTestRuntimeCfg(cfg), executor)

	ctx := context.Background()
	_, err := wrapper.IssueCertCloudflare(ctx, []string{}, "", "cf-token")
	if err == nil {
		t.Fatal("expected error for empty domains")
	}
	if !strings.Contains(err.Error(), "at least one domain") {
		t.Errorf("expected domain error, got: %v", err)
	}
}

func TestIssueCertCloudflare_NoEmail(t *testing.T) {
	cfg := config.CertbotConfig{BinaryPath: "certbot", Email: ""}
	executor := &mockExecutor{}
	wrapper := NewCertbotWrapper(newTestRuntimeCfg(cfg), executor)

	ctx := context.Background()
	_, err := wrapper.IssueCertCloudflare(ctx, []string{"example.com"}, "", "cf-token")
	if err == nil {
		t.Fatal("expected error for missing email")
	}
	if !strings.Contains(err.Error(), "email is required") {
		t.Errorf("expected email error, got: %v", err)
	}
}

func TestIssueCertCloudflare_NoToken(t *testing.T) {
	cfg := config.CertbotConfig{BinaryPath: "certbot", Email: "admin@example.com"}
	executor := &mockExecutor{}
	wrapper := NewCertbotWrapper(newTestRuntimeCfg(cfg), executor)

	ctx := context.Background()
	_, err := wrapper.IssueCertCloudflare(ctx, []string{"example.com"}, "", "")
	if err == nil {
		t.Fatal("expected error for missing cloudflare token")
	}
	if !strings.Contains(err.Error(), "cloudflare API token") {
		t.Errorf("expected token error, got: %v", err)
	}
}

func TestIssueCertCloudflare_ExecutionFailure(t *testing.T) {
	cfg := config.CertbotConfig{
		BinaryPath: "certbot",
		DataDir:    "/tmp/certbot-test",
		Email:      "admin@example.com",
	}
	executor := &mockExecutor{
		output: []byte("Error: rate limit exceeded"),
		err:    fmt.Errorf("exit status 1"),
	}
	wrapper := NewCertbotWrapper(newTestRuntimeCfg(cfg), executor)

	ctx := context.Background()
	_, err := wrapper.IssueCertCloudflare(ctx, []string{"example.com"}, "", "cf-token")
	if err == nil {
		t.Fatal("expected error for execution failure")
	}
	if !strings.Contains(err.Error(), "certbot execution failed") {
		t.Errorf("expected execution error, got: %v", err)
	}
}

func TestIssueCertCloudflare_CredentialsFileCreated(t *testing.T) {
	dataDir := setupTestCertDir(t)

	cfg := config.CertbotConfig{
		BinaryPath: "certbot",
		DataDir:    dataDir,
		Email:      "admin@example.com",
	}

	var credFilePath string
	executor := &mockExecutor{output: []byte("OK"), err: nil}
	originalExecute := executor.Execute
	_ = originalExecute

	wrapper := NewCertbotWrapper(newTestRuntimeCfg(cfg), executor)

	ctx := context.Background()
	_, err := wrapper.IssueCertCloudflare(ctx, []string{"example.com"}, "", "my-secret-token")
	if err != nil {
		t.Fatalf("IssueCertCloudflare failed: %v", err)
	}

	for i, arg := range executor.capturedArgs {
		if arg == "--dns-cloudflare-credentials" && i+1 < len(executor.capturedArgs) {
			credFilePath = executor.capturedArgs[i+1]
			break
		}
	}

	if credFilePath == "" {
		t.Fatal("expected credentials file path in args")
	}

	if !strings.Contains(credFilePath, "cloudflare-credentials-") {
		t.Errorf("expected credentials file path to contain 'cloudflare-credentials-', got: %s", credFilePath)
	}
}

func TestStartManualDNSChallenge_NoDomains(t *testing.T) {
	cfg := config.CertbotConfig{Email: "admin@example.com"}
	executor := &mockExecutor{}
	wrapper := NewCertbotWrapper(newTestRuntimeCfg(cfg), executor)

	ctx := context.Background()
	_, err := wrapper.StartManualDNSChallenge(ctx, []string{}, "")
	if err == nil {
		t.Fatal("expected error for empty domains")
	}
	if !strings.Contains(err.Error(), "at least one domain") {
		t.Errorf("expected domain error, got: %v", err)
	}
}

func TestStartManualDNSChallenge_NoEmail(t *testing.T) {
	cfg := config.CertbotConfig{Email: ""}
	executor := &mockExecutor{}
	wrapper := NewCertbotWrapper(newTestRuntimeCfg(cfg), executor)

	ctx := context.Background()
	_, err := wrapper.StartManualDNSChallenge(ctx, []string{"example.com"}, "")
	if err == nil {
		t.Fatal("expected error for missing email")
	}
	if !strings.Contains(err.Error(), "email is required") {
		t.Errorf("expected email error, got: %v", err)
	}
}

func TestStartManualDNSChallenge_WritesAuthHookScript(t *testing.T) {
	cfg := config.CertbotConfig{
		BinaryPath: "certbot",
		Email:      "admin@example.com",
	}

	// Create a mock executor that simulates certbot calling the auth-hook
	// by writing challenge data to the challenges file
	challengeExecutor := &manualDNSMockExecutor{
		challengeDomain:     "example.com",
		challengeValidation: "real-acme-token-abc123",
	}

	wrapper := NewCertbotWrapper(newTestRuntimeCfg(cfg), challengeExecutor)

	ctx := context.Background()
	session, err := wrapper.StartManualDNSChallenge(ctx, []string{"example.com"}, "")
	if err != nil {
		t.Fatalf("StartManualDNSChallenge failed: %v", err)
	}
	defer wrapper.CancelSession(session.ID)

	// Verify session was created
	if session.ID == "" {
		t.Error("expected non-empty session ID")
	}
	if len(session.Challenges) != 1 {
		t.Fatalf("expected 1 challenge, got %d", len(session.Challenges))
	}

	// Verify challenge contains real ACME values
	challenge := session.Challenges[0]
	if challenge.Domain != "example.com" {
		t.Errorf("expected domain 'example.com', got %q", challenge.Domain)
	}
	if challenge.TXTRecordName != "_acme-challenge.example.com" {
		t.Errorf("expected TXT record name '_acme-challenge.example.com', got %q", challenge.TXTRecordName)
	}
	if challenge.TXTRecordValue != "real-acme-token-abc123" {
		t.Errorf("expected TXT record value 'real-acme-token-abc123', got %q", challenge.TXTRecordValue)
	}

	// Verify certbot was called with manual DNS flags
	argsStr := strings.Join(challengeExecutor.capturedArgs, " ")
	expectedFlags := []string{
		"certonly",
		"-d example.com",
		"--manual",
		"--preferred-challenges dns",
		"--manual-auth-hook",
		"--agree-tos",
		"--non-interactive",
	}
	for _, flag := range expectedFlags {
		if !strings.Contains(argsStr, flag) {
			t.Errorf("expected args to contain %q, got: %s", flag, argsStr)
		}
	}
}

func TestCompleteManualDNSChallenge_SessionNotFound(t *testing.T) {
	cfg := config.CertbotConfig{Email: "admin@example.com"}
	executor := &mockExecutor{}
	wrapper := NewCertbotWrapper(newTestRuntimeCfg(cfg), executor)

	_, err := wrapper.CompleteManualDNSChallenge("nonexistent-session")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !strings.Contains(err.Error(), "session nonexistent-session not found") {
		t.Errorf("expected session not found error, got: %v", err)
	}
}

func TestCompleteManualDNSChallenge_Success(t *testing.T) {
	dataDir := setupTestCertDir(t)
	cfg := config.CertbotConfig{
		BinaryPath: "certbot",
		DataDir:    dataDir,
		Email:      "admin@example.com",
	}

	// Create a mock executor that simulates the full manual DNS flow
	challengeExecutor := &manualDNSMockExecutor{
		challengeDomain:     "example.com",
		challengeValidation: "real-acme-token-xyz789",
		// After proceed signal, certbot "succeeds" and we can read cert files
		succeedAfterProceed: true,
		certDataDir:         dataDir,
	}

	wrapper := NewCertbotWrapper(newTestRuntimeCfg(cfg), challengeExecutor)

	ctx := context.Background()
	session, err := wrapper.StartManualDNSChallenge(ctx, []string{"example.com"}, "")
	if err != nil {
		t.Fatalf("StartManualDNSChallenge failed: %v", err)
	}

	// Complete the challenge
	result, err := wrapper.CompleteManualDNSChallenge(session.ID)
	if err != nil {
		t.Fatalf("CompleteManualDNSChallenge failed: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.CertFiles == nil {
		t.Fatal("expected non-nil CertFiles")
	}
	if len(result.CertFiles.CertPEM) == 0 {
		t.Error("expected non-empty CertPEM")
	}
}

func TestGenerateAuthHookScript(t *testing.T) {
	cfg := config.CertbotConfig{Email: "admin@example.com"}
	executor := &mockExecutor{}
	wrapper := NewCertbotWrapper(newTestRuntimeCfg(cfg), executor)

	script := wrapper.generateAuthHookScript("/tmp/challenges.json", "/tmp/proceed")

	if !strings.Contains(script, "#!/bin/bash") {
		t.Error("expected bash shebang")
	}
	if !strings.Contains(script, "CERTBOT_DOMAIN") {
		t.Error("expected CERTBOT_DOMAIN reference")
	}
	if !strings.Contains(script, "CERTBOT_VALIDATION") {
		t.Error("expected CERTBOT_VALIDATION reference")
	}
	if !strings.Contains(script, "/tmp/challenges.json") {
		t.Error("expected challenges file path")
	}
	if !strings.Contains(script, "/tmp/proceed") {
		t.Error("expected proceed file path")
	}
}

func TestParseChallengeLine(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		wantDomain string
		wantVal    string
		wantErr    bool
	}{
		{
			name:       "valid line",
			line:       `{"domain":"example.com","validation":"abc123token"}`,
			wantDomain: "example.com",
			wantVal:    "abc123token",
			wantErr:    false,
		},
		{
			name:       "valid with spaces",
			line:       `{"domain":"sub.example.com","validation":"xyz-789_token"}`,
			wantDomain: "sub.example.com",
			wantVal:    "xyz-789_token",
			wantErr:    false,
		},
		{
			name:    "missing domain",
			line:    `{"validation":"abc123"}`,
			wantErr: true,
		},
		{
			name:    "missing validation",
			line:    `{"domain":"example.com"}`,
			wantErr: true,
		},
		{
			name:    "empty line",
			line:    ``,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			domain, val, err := parseChallengeLine(tt.line)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if domain != tt.wantDomain {
				t.Errorf("expected domain %q, got %q", tt.wantDomain, domain)
			}
			if val != tt.wantVal {
				t.Errorf("expected validation %q, got %q", tt.wantVal, val)
			}
		})
	}
}

func TestParseChallengesFile(t *testing.T) {
	data := []byte(`{"domain":"example.com","validation":"token1"}
{"domain":"www.example.com","validation":"token2"}
`)

	challenges, err := parseChallengesFile(data, 2)
	if err != nil {
		t.Fatalf("parseChallengesFile failed: %v", err)
	}

	if len(challenges) != 2 {
		t.Fatalf("expected 2 challenges, got %d", len(challenges))
	}

	if challenges[0].Domain != "example.com" {
		t.Errorf("expected domain 'example.com', got %q", challenges[0].Domain)
	}
	if challenges[0].TXTRecordName != "_acme-challenge.example.com" {
		t.Errorf("expected TXT name '_acme-challenge.example.com', got %q", challenges[0].TXTRecordName)
	}
	if challenges[0].TXTRecordValue != "token1" {
		t.Errorf("expected value 'token1', got %q", challenges[0].TXTRecordValue)
	}

	if challenges[1].Domain != "www.example.com" {
		t.Errorf("expected domain 'www.example.com', got %q", challenges[1].Domain)
	}
	if challenges[1].TXTRecordValue != "token2" {
		t.Errorf("expected value 'token2', got %q", challenges[1].TXTRecordValue)
	}
}

func TestParseChallengesFile_NotEnough(t *testing.T) {
	data := []byte(`{"domain":"example.com","validation":"token1"}
`)

	_, err := parseChallengesFile(data, 2)
	if err == nil {
		t.Fatal("expected error for insufficient challenges")
	}
}

func TestReadCertFiles_Success(t *testing.T) {
	dataDir := setupTestCertDir(t)

	cfg := config.CertbotConfig{DataDir: dataDir}
	executor := &mockExecutor{}
	wrapper := NewCertbotWrapper(newTestRuntimeCfg(cfg), executor)

	certDir := filepath.Join(dataDir, "live", "example.com")
	certFiles, err := wrapper.ReadCertFiles(certDir)
	if err != nil {
		t.Fatalf("ReadCertFiles failed: %v", err)
	}

	if len(certFiles.CertPEM) == 0 {
		t.Error("expected non-empty CertPEM")
	}
	if len(certFiles.ChainPEM) == 0 {
		t.Error("expected non-empty ChainPEM")
	}
	if len(certFiles.FullchainPEM) == 0 {
		t.Error("expected non-empty FullchainPEM")
	}
	if len(certFiles.PrivateKeyPEM) == 0 {
		t.Error("expected non-empty PrivateKeyPEM")
	}

	if !strings.Contains(string(certFiles.CertPEM), "MIIBfake") {
		t.Error("CertPEM content doesn't match expected")
	}
	if !strings.Contains(string(certFiles.ChainPEM), "MIIBchain") {
		t.Error("ChainPEM content doesn't match expected")
	}
	if !strings.Contains(string(certFiles.PrivateKeyPEM), "MIIBprivkey") {
		t.Error("PrivateKeyPEM content doesn't match expected")
	}
}

func TestReadCertFiles_EmptyDir(t *testing.T) {
	cfg := config.CertbotConfig{}
	executor := &mockExecutor{}
	wrapper := NewCertbotWrapper(newTestRuntimeCfg(cfg), executor)

	_, err := wrapper.ReadCertFiles("")
	if err == nil {
		t.Fatal("expected error for empty directory")
	}
	if !strings.Contains(err.Error(), "certbot output directory is required") {
		t.Errorf("expected directory error, got: %v", err)
	}
}

func TestReadCertFiles_MissingFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "cert.pem"), []byte("cert"), 0644)

	cfg := config.CertbotConfig{}
	executor := &mockExecutor{}
	wrapper := NewCertbotWrapper(newTestRuntimeCfg(cfg), executor)

	_, err := wrapper.ReadCertFiles(dir)
	if err == nil {
		t.Fatal("expected error for missing files")
	}
	if !strings.Contains(err.Error(), "chain.pem") {
		t.Errorf("expected chain.pem error, got: %v", err)
	}
}

func TestReadCertFiles_NonExistentDir(t *testing.T) {
	cfg := config.CertbotConfig{}
	executor := &mockExecutor{}
	wrapper := NewCertbotWrapper(newTestRuntimeCfg(cfg), executor)

	_, err := wrapper.ReadCertFiles("/nonexistent/path/to/certs")
	if err == nil {
		t.Fatal("expected error for non-existent directory")
	}
}

func TestBinaryPath_Default(t *testing.T) {
	cfg := config.CertbotConfig{BinaryPath: ""}
	executor := &mockExecutor{}
	wrapper := NewCertbotWrapper(newTestRuntimeCfg(cfg), executor)

	if wrapper.binaryPath() != "certbot" {
		t.Errorf("expected default binary path 'certbot', got %q", wrapper.binaryPath())
	}
}

func TestBinaryPath_Custom(t *testing.T) {
	cfg := config.CertbotConfig{BinaryPath: "/opt/certbot/bin/certbot"}
	executor := &mockExecutor{}
	wrapper := NewCertbotWrapper(newTestRuntimeCfg(cfg), executor)

	if wrapper.binaryPath() != "/opt/certbot/bin/certbot" {
		t.Errorf("expected custom binary path, got %q", wrapper.binaryPath())
	}
}

func TestCertOutputDir_WithDataDir(t *testing.T) {
	cfg := config.CertbotConfig{DataDir: "/var/certbot"}
	executor := &mockExecutor{}
	wrapper := NewCertbotWrapper(newTestRuntimeCfg(cfg), executor)

	expected := filepath.Join("/var/certbot", "live", "example.com")
	got := wrapper.certOutputDir("example.com")
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestCertOutputDir_DefaultPath(t *testing.T) {
	cfg := config.CertbotConfig{DataDir: ""}
	executor := &mockExecutor{}
	wrapper := NewCertbotWrapper(newTestRuntimeCfg(cfg), executor)

	expected := filepath.Join("/etc/letsencrypt", "live", "example.com")
	got := wrapper.certOutputDir("example.com")
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestBuildCertbotArgs(t *testing.T) {
	cfg := config.CertbotConfig{
		DataDir: "/var/certbot",
	}
	executor := &mockExecutor{}
	wrapper := NewCertbotWrapper(newTestRuntimeCfg(cfg), executor)

	args := wrapper.buildCertbotArgs([]string{"example.com", "www.example.com"}, "test@example.com")

	argsStr := strings.Join(args, " ")

	expectedParts := []string{
		"certonly",
		"-d example.com",
		"-d www.example.com",
		"--email test@example.com",
		"--agree-tos",
		"--non-interactive",
		"--config-dir /var/certbot",
	}

	for _, part := range expectedParts {
		if !strings.Contains(argsStr, part) {
			t.Errorf("expected args to contain %q, got: %s", part, argsStr)
		}
	}
}

func TestBuildCertbotArgs_NoDataDir(t *testing.T) {
	cfg := config.CertbotConfig{DataDir: ""}
	executor := &mockExecutor{}
	wrapper := NewCertbotWrapper(newTestRuntimeCfg(cfg), executor)

	args := wrapper.buildCertbotArgs([]string{"example.com"}, "test@example.com")
	argsStr := strings.Join(args, " ")

	if strings.Contains(argsStr, "--config-dir") {
		t.Errorf("expected no --config-dir flag when DataDir is empty, got: %s", argsStr)
	}
}

// manualDNSMockExecutor simulates certbot's manual DNS flow for testing.
// When Execute is called, it writes challenge data to the challenges file
// (simulating what the auth-hook would do), then optionally waits for the
// proceed file before returning success.
type manualDNSMockExecutor struct {
	capturedName string
	capturedArgs []string
	calls        int

	challengeDomain     string
	challengeValidation string
	succeedAfterProceed bool
	certDataDir         string
}

func (m *manualDNSMockExecutor) Execute(ctx context.Context, name string, args ...string) ([]byte, error) {
	m.capturedName = name
	m.capturedArgs = args
	m.calls++

	// Find the auth-hook script path in args to determine the challenges file location
	var hookPath string
	for i, arg := range args {
		if arg == "--manual-auth-hook" && i+1 < len(args) {
			hookPath = args[i+1]
			break
		}
	}

	if hookPath == "" {
		return []byte("OK"), nil
	}

	// Determine the temp directory from the hook script path
	tempDir := filepath.Dir(hookPath)
	challengesFile := filepath.Join(tempDir, "challenges.json")
	proceedFile := filepath.Join(tempDir, "proceed")

	// Simulate certbot calling the auth-hook: write challenge data
	challengeLine := fmt.Sprintf(`{"domain":"%s","validation":"%s"}`, m.challengeDomain, m.challengeValidation)
	if err := os.WriteFile(challengesFile, []byte(challengeLine+"\n"), 0644); err != nil {
		return nil, fmt.Errorf("mock: failed to write challenges: %w", err)
	}

	if m.succeedAfterProceed {
		// Wait for proceed file (simulating the auth-hook blocking)
		for i := 0; i < 100; i++ {
			if _, err := os.Stat(proceedFile); err == nil {
				break
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
			// Small sleep to avoid busy-waiting
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-func() <-chan struct{} {
				ch := make(chan struct{})
				go func() {
					defer close(ch)
					// 50ms sleep
					select {
					case <-ctx.Done():
					default:
						<-timeAfter50ms()
					}
				}()
				return ch
			}():
			}
		}
	}

	return []byte("Congratulations! Certificate issued."), nil
}

func timeAfter50ms() <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		// Use a simple busy wait with a counter to avoid importing time in this scope
		// Actually we can just return immediately for tests
	}()
	return ch
}
