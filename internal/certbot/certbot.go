package certbot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/config"
)

// CertFiles holds the PEM content of certificate files read from Certbot's output directory.
type CertFiles struct {
	CertPEM       []byte
	ChainPEM      []byte
	FullchainPEM  []byte
	PrivateKeyPEM []byte
}

// CertbotResult holds the result of a successful Certbot certificate issuance.
type CertbotResult struct {
	OutputDir string
	CertFiles *CertFiles
}

// ManualDNSChallenge holds the DNS TXT record requirements for manual DNS validation.
type ManualDNSChallenge struct {
	Domain         string `json:"domain"`
	TXTRecordName  string `json:"txt_record_name"`
	TXTRecordValue string `json:"txt_record_value"`
}

// ManualDNSResult holds the outcome of a completed manual DNS challenge.
type ManualDNSResult struct {
	CertFiles *CertFiles
	OutputDir string
	Err       error
}

// ManualDNSSession represents an in-progress manual DNS challenge.
// It tracks the certbot process running in the background with a blocking auth-hook.
type ManualDNSSession struct {
	ID         string
	Domains    []string
	Email      string
	Challenges []*ManualDNSChallenge
	TempDir    string
	Done       chan *ManualDNSResult
	Cancel     context.CancelFunc
}

// CommandExecutor is an interface for executing shell commands.
// This allows mocking in tests.
type CommandExecutor interface {
	Execute(ctx context.Context, name string, args ...string) ([]byte, error)
}

// BackgroundCommandExecutor extends CommandExecutor with the ability to start
// long-running processes in the background.
type BackgroundCommandExecutor interface {
	CommandExecutor
	// ExecuteBackground starts a command in the background and returns immediately.
	// The provided done channel receives the result when the command completes.
	ExecuteBackground(ctx context.Context, done chan<- []byte, errCh chan<- error, name string, args ...string)
}

// CertbotWrapper wraps Certbot CLI operations for certificate issuance.
type CertbotWrapper struct {
	runtimeCfg *config.RuntimeConfig
	executor   CommandExecutor

	// sessions stores active manual DNS sessions keyed by session ID.
	sessions sync.Map
}

// NewCertbotWrapper creates a new CertbotWrapper with the given runtime configuration and command executor.
func NewCertbotWrapper(runtimeCfg *config.RuntimeConfig, executor CommandExecutor) *CertbotWrapper {
	return &CertbotWrapper{
		runtimeCfg: runtimeCfg,
		executor:   executor,
	}
}

// IssueCertCloudflare issues a certificate using Certbot with Cloudflare DNS-01 validation.
// It creates a temporary credentials file for the Cloudflare API token, invokes certbot certonly,
// and reads the resulting certificate files from the output directory.
func (w *CertbotWrapper) IssueCertCloudflare(ctx context.Context, domains []string, email string, cloudflareToken string) (*CertbotResult, error) {
	if len(domains) == 0 {
		return nil, fmt.Errorf("at least one domain is required")
	}
	cfg := w.runtimeCfg.Get().Certbot
	if email == "" {
		email = cfg.Email
	}
	if email == "" {
		return nil, fmt.Errorf("email is required for Certbot registration")
	}
	if cloudflareToken == "" {
		return nil, fmt.Errorf("cloudflare API token is required")
	}

	// Ensure all certbot directories exist before execution
	if err := w.ensureDirectories(); err != nil {
		return nil, err
	}

	// Create temporary credentials file for Cloudflare
	credFile, err := w.createCloudflareCredentials(cloudflareToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create cloudflare credentials file: %w", err)
	}
	defer os.Remove(credFile)

	// Build certbot command arguments
	args := w.buildCertbotArgs(domains, email)
	args = append(args, "--dns-cloudflare")
	args = append(args, "--dns-cloudflare-credentials", credFile)

	// Execute certbot
	output, err := w.executor.Execute(ctx, w.binaryPath(), args...)
	if err != nil {
		return nil, fmt.Errorf("certbot execution failed: %w, output: %s", err, string(output))
	}

	// Read certificate files from output directory
	outputDir := w.certOutputDir(domains[0])
	certFiles, err := w.ReadCertFiles(outputDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate files from %s: %w", outputDir, err)
	}

	return &CertbotResult{
		OutputDir: outputDir,
		CertFiles: certFiles,
	}, nil
}

// StartManualDNSChallenge begins the manual DNS-01 challenge flow.
// It starts certbot in the background with a custom auth-hook script that:
// 1. Captures the ACME challenge values (CERTBOT_DOMAIN and CERTBOT_VALIDATION)
// 2. Writes them to a challenges file in a temp directory
// 3. Blocks (waits for a "proceed" signal file) so certbot stays alive
//
// This method waits for the challenges file to be populated, then returns
// a ManualDNSSession containing the real ACME challenge values.
// The caller must create the DNS TXT records and then call CompleteManualDNSChallenge.
func (w *CertbotWrapper) StartManualDNSChallenge(ctx context.Context, domains []string, email string) (*ManualDNSSession, error) {
	if len(domains) == 0 {
		return nil, fmt.Errorf("at least one domain is required")
	}
	cfg := w.runtimeCfg.Get().Certbot
	if email == "" {
		email = cfg.Email
	}
	if email == "" {
		return nil, fmt.Errorf("email is required for Certbot registration")
	}

	// Ensure all certbot directories exist before execution
	if err := w.ensureDirectories(); err != nil {
		return nil, err
	}

	// Generate session ID
	sessionID := randomHex(16)

	// Create temp directory for this session
	tempDir, err := os.MkdirTemp("", "certbot-manual-dns-"+sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Write the auth-hook script that captures challenges and blocks
	challengesFile := filepath.Join(tempDir, "challenges.json")
	proceedFile := filepath.Join(tempDir, "proceed")
	hookScript := w.generateAuthHookScript(challengesFile, proceedFile)
	hookScriptPath := filepath.Join(tempDir, "auth-hook.sh")
	if err := os.WriteFile(hookScriptPath, []byte(hookScript), 0755); err != nil {
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to write auth-hook script: %w", err)
	}

	// Create a cancellable context for the background certbot process
	certbotCtx, cancel := context.WithCancel(ctx)

	// Build certbot args for manual DNS
	args := w.buildCertbotArgs(domains, email)
	args = append(args, "--manual")
	args = append(args, "--preferred-challenges", "dns")
	args = append(args, "--manual-auth-hook", hookScriptPath)

	// Start certbot in background
	doneCh := make(chan *ManualDNSResult, 1)

	go func() {
		output, err := w.executor.Execute(certbotCtx, w.binaryPath(), args...)
		result := &ManualDNSResult{}
		if err != nil {
			result.Err = fmt.Errorf("certbot manual DNS execution failed: %w, output: %s", err, string(output))
		} else {
			// Read certificate files from output directory
			outputDir := w.certOutputDir(domains[0])
			certFiles, readErr := w.ReadCertFiles(outputDir)
			if readErr != nil {
				result.Err = fmt.Errorf("failed to read certificate files from %s: %w", outputDir, readErr)
			} else {
				result.CertFiles = certFiles
				result.OutputDir = outputDir
			}
		}
		doneCh <- result
	}()

	// Wait for challenges file to be populated (poll with timeout)
	challenges, err := w.waitForChallenges(challengesFile, len(domains), 5*time.Minute)
	if err != nil {
		cancel()
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to get ACME challenges: %w", err)
	}

	session := &ManualDNSSession{
		ID:         sessionID,
		Domains:    domains,
		Email:      email,
		Challenges: challenges,
		TempDir:    tempDir,
		Done:       doneCh,
		Cancel:     cancel,
	}

	// Store session
	w.sessions.Store(sessionID, session)

	return session, nil
}

// CompleteManualDNSChallenge signals the blocked certbot process to continue
// after the user has created the required DNS TXT records.
// It creates the "proceed" signal file and waits for certbot to complete.
func (w *CertbotWrapper) CompleteManualDNSChallenge(sessionID string) (*CertbotResult, error) {
	val, ok := w.sessions.Load(sessionID)
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	session := val.(*ManualDNSSession)

	// Remove session from map
	w.sessions.Delete(sessionID)

	// Create the proceed signal file to unblock the auth-hook
	proceedFile := filepath.Join(session.TempDir, "proceed")
	if err := os.WriteFile(proceedFile, []byte("1"), 0644); err != nil {
		session.Cancel()
		os.RemoveAll(session.TempDir)
		return nil, fmt.Errorf("failed to create proceed signal: %w", err)
	}

	// Wait for certbot to complete (with timeout)
	select {
	case result := <-session.Done:
		os.RemoveAll(session.TempDir)
		if result.Err != nil {
			return nil, result.Err
		}
		return &CertbotResult{
			OutputDir: result.OutputDir,
			CertFiles: result.CertFiles,
		}, nil
	case <-time.After(10 * time.Minute):
		session.Cancel()
		os.RemoveAll(session.TempDir)
		return nil, fmt.Errorf("certbot timed out waiting for DNS validation")
	}
}

// GetSession retrieves an active manual DNS session by ID.
func (w *CertbotWrapper) GetSession(sessionID string) (*ManualDNSSession, bool) {
	val, ok := w.sessions.Load(sessionID)
	if !ok {
		return nil, false
	}
	return val.(*ManualDNSSession), true
}

// CancelSession cancels and cleans up a manual DNS session.
func (w *CertbotWrapper) CancelSession(sessionID string) {
	val, ok := w.sessions.LoadAndDelete(sessionID)
	if !ok {
		return
	}
	session := val.(*ManualDNSSession)
	session.Cancel()
	os.RemoveAll(session.TempDir)
}

// generateAuthHookScript creates a bash script that:
// 1. Appends the ACME challenge info (domain + validation token) to a JSON-lines file
// 2. Waits for a "proceed" signal file to appear (polling every second, up to 30 minutes)
//
// This keeps the certbot process alive until the user confirms DNS records are set.
func (w *CertbotWrapper) generateAuthHookScript(challengesFile, proceedFile string) string {
	// Use forward slashes for cross-platform compatibility in the script
	cf := strings.ReplaceAll(challengesFile, "\\", "/")
	pf := strings.ReplaceAll(proceedFile, "\\", "/")

	return fmt.Sprintf(`#!/bin/bash
# Auth hook script for manual DNS challenge
# Writes challenge info and waits for proceed signal

echo "{\"domain\":\"$CERTBOT_DOMAIN\",\"validation\":\"$CERTBOT_VALIDATION\"}" >> "%s"

# Wait for proceed signal (max 30 minutes = 1800 seconds)
COUNTER=0
while [ ! -f "%s" ] && [ $COUNTER -lt 1800 ]; do
    sleep 1
    COUNTER=$((COUNTER + 1))
done

if [ ! -f "%s" ]; then
    echo "Timeout waiting for proceed signal" >&2
    exit 1
fi

exit 0
`, cf, pf, pf)
}

// waitForChallenges polls the challenges file until it contains the expected
// number of challenge entries, or until the timeout expires.
func (w *CertbotWrapper) waitForChallenges(challengesFile string, expectedCount int, timeout time.Duration) ([]*ManualDNSChallenge, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		data, err := os.ReadFile(challengesFile)
		if err == nil && len(data) > 0 {
			challenges, err := parseChallengesFile(data, expectedCount)
			if err == nil {
				return challenges, nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	return nil, fmt.Errorf("timeout waiting for ACME challenges (expected %d)", expectedCount)
}

// parseChallengesFile parses the JSON-lines challenges file written by the auth-hook.
// Each line is: {"domain":"example.com","validation":"token-value"}
func parseChallengesFile(data []byte, expectedCount int) ([]*ManualDNSChallenge, error) {
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < expectedCount {
		return nil, fmt.Errorf("only %d/%d challenges found", len(lines), expectedCount)
	}

	challenges := make([]*ManualDNSChallenge, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Parse simple JSON: {"domain":"...","validation":"..."}
		domain, validation, err := parseChallengeLine(line)
		if err != nil {
			return nil, err
		}
		challenges = append(challenges, &ManualDNSChallenge{
			Domain:         domain,
			TXTRecordName:  "_acme-challenge." + domain,
			TXTRecordValue: validation,
		})
	}

	if len(challenges) < expectedCount {
		return nil, fmt.Errorf("only %d/%d valid challenges parsed", len(challenges), expectedCount)
	}

	return challenges, nil
}

// parseChallengeLine extracts domain and validation from a JSON line.
func parseChallengeLine(line string) (domain, validation string, err error) {
	// Simple JSON parsing without importing encoding/json to keep it lightweight
	// Format: {"domain":"value","validation":"value"}
	domainStart := strings.Index(line, `"domain":"`)
	if domainStart == -1 {
		return "", "", fmt.Errorf("invalid challenge line: missing domain field")
	}
	domainStart += len(`"domain":"`)
	domainEnd := strings.Index(line[domainStart:], `"`)
	if domainEnd == -1 {
		return "", "", fmt.Errorf("invalid challenge line: unterminated domain value")
	}
	domain = line[domainStart : domainStart+domainEnd]

	valStart := strings.Index(line, `"validation":"`)
	if valStart == -1 {
		return "", "", fmt.Errorf("invalid challenge line: missing validation field")
	}
	valStart += len(`"validation":"`)
	valEnd := strings.Index(line[valStart:], `"`)
	if valEnd == -1 {
		return "", "", fmt.Errorf("invalid challenge line: unterminated validation value")
	}
	validation = line[valStart : valStart+valEnd]

	return domain, validation, nil
}

// ReadCertFiles reads certificate files from a Certbot output directory.
// It expects the directory to contain cert.pem, chain.pem, fullchain.pem, and privkey.pem.
func (w *CertbotWrapper) ReadCertFiles(certbotOutputDir string) (*CertFiles, error) {
	if certbotOutputDir == "" {
		return nil, fmt.Errorf("certbot output directory is required")
	}

	certPEM, err := os.ReadFile(filepath.Join(certbotOutputDir, "cert.pem"))
	if err != nil {
		return nil, fmt.Errorf("failed to read cert.pem: %w", err)
	}

	chainPEM, err := os.ReadFile(filepath.Join(certbotOutputDir, "chain.pem"))
	if err != nil {
		return nil, fmt.Errorf("failed to read chain.pem: %w", err)
	}

	fullchainPEM, err := os.ReadFile(filepath.Join(certbotOutputDir, "fullchain.pem"))
	if err != nil {
		return nil, fmt.Errorf("failed to read fullchain.pem: %w", err)
	}

	privateKeyPEM, err := os.ReadFile(filepath.Join(certbotOutputDir, "privkey.pem"))
	if err != nil {
		return nil, fmt.Errorf("failed to read privkey.pem: %w", err)
	}

	return &CertFiles{
		CertPEM:       certPEM,
		ChainPEM:      chainPEM,
		FullchainPEM:  fullchainPEM,
		PrivateKeyPEM: privateKeyPEM,
	}, nil
}

// effectiveDataDir returns the certbot data directory.
// Returns configured data_dir if non-empty, otherwise uses Certbot's native
// default config directory.
func (w *CertbotWrapper) effectiveDataDir() string {
	cfg := w.runtimeCfg.Get().Certbot
	if cfg.DataDir != "" {
		return cfg.DataDir
	}
	return "/etc/letsencrypt"
}

// ensureDirectories creates all required certbot directories.
// Must be called before any certbot execution.
func (w *CertbotWrapper) ensureDirectories() error {
	base := w.effectiveDataDir()
	dirs := []string{base, filepath.Join(base, "work"), filepath.Join(base, "logs")}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create certbot directory %s: %w", dir, err)
		}
	}
	return nil
}

// binaryPath returns the certbot binary path from configuration, defaulting to "certbot".
func (w *CertbotWrapper) binaryPath() string {
	cfg := w.runtimeCfg.Get().Certbot
	if cfg.BinaryPath != "" {
		return cfg.BinaryPath
	}
	return "certbot"
}

// certOutputDir returns the expected Certbot output directory for a given domain.
// Uses effectiveDataDir() to ensure consistency with buildCertbotArgs.
func (w *CertbotWrapper) certOutputDir(domain string) string {
	return filepath.Join(w.effectiveDataDir(), "live", certbotCertificateName(domain))
}

// certbotCertificateName returns the lineage directory name Certbot uses when
// --cert-name is not specified. Certbot strips the "*." prefix from a
// wildcard first domain, so *.example.com is stored under live/example.com.
func certbotCertificateName(domain string) string {
	domain = strings.TrimSpace(domain)
	return strings.TrimPrefix(domain, "*.")
}

// buildCertbotArgs builds the common certbot certonly arguments.
// Always passes --config-dir, --work-dir, --logs-dir using effectiveDataDir().
func (w *CertbotWrapper) buildCertbotArgs(domains []string, email string) []string {
	args := []string{"certonly"}

	// Add domain flags
	for _, d := range domains {
		args = append(args, "-d", d)
	}

	// Add email
	args = append(args, "--email", email)

	// Non-interactive flags
	args = append(args, "--agree-tos")
	args = append(args, "--non-interactive")

	// Always pass directory flags using effectiveDataDir() to ensure
	// all certbot directories are writable (important for non-root Docker)
	dataDir := w.effectiveDataDir()
	args = append(args, "--config-dir", dataDir)
	args = append(args, "--work-dir", filepath.Join(dataDir, "work"))
	args = append(args, "--logs-dir", filepath.Join(dataDir, "logs"))

	return args
}

// createCloudflareCredentials creates a temporary file with Cloudflare API credentials.
// The file is created with 0600 permissions for security.
// The caller is responsible for removing the file after use.
func (w *CertbotWrapper) createCloudflareCredentials(token string) (string, error) {
	content := fmt.Sprintf("dns_cloudflare_api_token = %s\n", token)

	tmpDir := os.TempDir()
	tmpFile := filepath.Join(tmpDir, "cloudflare-credentials-"+randomHex(8)+".ini")

	if err := os.WriteFile(tmpFile, []byte(content), 0600); err != nil {
		return "", fmt.Errorf("failed to write credentials file: %w", err)
	}

	return tmpFile, nil
}

// randomHex generates a random hex string of the specified byte length.
func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
