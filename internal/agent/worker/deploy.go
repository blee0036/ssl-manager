package worker

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/agent/config"
	"github.com/ssl-manager/ssl-manager/internal/model"
)

// DeployResult holds the outcome of a certificate deployment.
type DeployResult struct {
	Status         string                `json:"status"`
	CommandOutputs []model.CommandOutput `json:"command_outputs"`
	ErrorMessage   string                `json:"error_message"`
	StartedAt      time.Time             `json:"started_at"`
	FinishedAt     time.Time             `json:"finished_at"`
}

// CertDownloadResponse represents the response from the certificate download API.
type CertDownloadResponse struct {
	CertificateID     string `json:"certificate_id"`
	FingerprintSHA256 string `json:"fingerprint_sha256"`
	FullchainPEM      string `json:"fullchain_pem"`
	PrivateKeyPEM     string `json:"private_key_pem"`
}

// DeployWorker handles certificate deployment to the local filesystem.
type DeployWorker struct {
	cfg        *config.AgentConfig
	state      *config.AgentLocalState
	statePath  string
	httpClient *http.Client

	// CommandTimeout is the maximum duration for each post-deploy command.
	CommandTimeout time.Duration
}

// NewDeployWorker creates a new DeployWorker.
func NewDeployWorker(cfg *config.AgentConfig, state *config.AgentLocalState, statePath string) *DeployWorker {
	return &DeployWorker{
		cfg:       cfg,
		state:     state,
		statePath: statePath,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		CommandTimeout: 60 * time.Second,
	}
}

// Deploy performs the full certificate deployment workflow for a given config.
// Steps:
// 1. Download certificate from server
// 2. Validate cert and key match
// 3. Create target directories
// 4. Write cert and key to temp files, then atomically rename
// 5. Set file permissions
// 6. Execute post_deploy_commands sequentially
// 7. Report deployment log to server
// 8. Update local state
func (w *DeployWorker) Deploy(ctx context.Context, certCfg CertConfigResponse) (*DeployResult, error) {
	startedAt := time.Now()
	result := &DeployResult{
		StartedAt: startedAt,
	}

	// Step 1: Download certificate
	certContent, err := w.DownloadCertificate(ctx, certCfg.MachineCertificateID)
	if err != nil {
		result.Status = "failed"
		result.ErrorMessage = fmt.Sprintf("failed to download certificate: %v", err)
		result.FinishedAt = time.Now()
		w.reportDeploymentLog(ctx, certCfg, result)
		w.updateLocalState(certCfg, certCfg.FingerprintSHA256, "failed")
		return result, err
	}

	// Step 2: Validate cert and key match
	if err := ValidateCertKeyPair([]byte(certContent.FullchainPEM), []byte(certContent.PrivateKeyPEM)); err != nil {
		result.Status = "failed"
		result.ErrorMessage = fmt.Sprintf("certificate and private key do not match: %v", err)
		result.FinishedAt = time.Now()
		w.reportDeploymentLog(ctx, certCfg, result)
		w.updateLocalState(certCfg, certCfg.FingerprintSHA256, "failed")
		return result, err
	}

	// Step 3 & 4 & 5: Write files atomically with proper permissions
	if err := w.WriteFilesAtomically(certCfg.CertPath, certCfg.PrivateKeyPath,
		[]byte(certContent.FullchainPEM), []byte(certContent.PrivateKeyPEM)); err != nil {
		result.Status = "failed"
		result.ErrorMessage = fmt.Sprintf("failed to write certificate files: %v", err)
		result.FinishedAt = time.Now()
		w.reportDeploymentLog(ctx, certCfg, result)
		w.updateLocalState(certCfg, certCfg.FingerprintSHA256, "failed")
		return result, err
	}

	// Step 6: Execute post_deploy_commands
	var commandOutputs []model.CommandOutput
	if certCfg.PostDeployCommands != "" {
		commands := parseCommands(certCfg.PostDeployCommands)
		outputs, err := w.ExecuteCommands(ctx, commands)
		commandOutputs = outputs
		if err != nil {
			result.Status = "failed"
			result.CommandOutputs = commandOutputs
			result.ErrorMessage = fmt.Sprintf("post-deploy command failed: %v", err)
			result.FinishedAt = time.Now()
			w.reportDeploymentLog(ctx, certCfg, result)
			// Update local state even on command failure
			w.updateLocalState(certCfg, certContent.FingerprintSHA256, "failed")
			return result, err
		}
	}

	// Success
	result.Status = "success"
	result.CommandOutputs = commandOutputs
	result.FinishedAt = time.Now()

	// Step 7: Report deployment log
	w.reportDeploymentLog(ctx, certCfg, result)

	// Step 8: Update local state
	w.updateLocalState(certCfg, certContent.FingerprintSHA256, "success")

	return result, nil
}

// DownloadCertificate downloads the certificate content from the server.
// GET /api/agent/machine-certificates/{id}/download
func (w *DeployWorker) DownloadCertificate(ctx context.Context, machineCertID string) (*CertDownloadResponse, error) {
	url := fmt.Sprintf("%s/api/agent/machine-certificates/%s/download", w.cfg.ServerURL, machineCertID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create download request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+w.cfg.AgentToken)

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download certificate: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("unauthorized: agent token rejected")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		Code    int                  `json:"code"`
		Message string               `json:"message"`
		Data    CertDownloadResponse `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode download response: %w", err)
	}

	return &apiResp.Data, nil
}

// ValidateCertKeyPair validates that the certificate and private key match
// by comparing their public keys.
func ValidateCertKeyPair(certPEM, keyPEM []byte) error {
	// Parse certificate
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return fmt.Errorf("failed to decode certificate PEM")
	}

	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Parse private key
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return fmt.Errorf("failed to decode private key PEM")
	}

	privKey, err := parsePrivateKey(keyBlock.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse private key: %w", err)
	}

	// Compare public keys
	if !publicKeysMatch(cert.PublicKey, privKey) {
		return fmt.Errorf("certificate public key does not match private key")
	}

	return nil
}

// parsePrivateKey tries to parse a private key in PKCS8, PKCS1, or EC format.
func parsePrivateKey(der []byte) (interface{}, error) {
	if key, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		return key, nil
	}
	if key, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return key, nil
	}
	if key, err := x509.ParseECPrivateKey(der); err == nil {
		return key, nil
	}
	return nil, fmt.Errorf("failed to parse private key in any known format")
}

// publicKeysMatch checks if the public key from the certificate matches the private key.
func publicKeysMatch(certPubKey interface{}, privKey interface{}) bool {
	switch priv := privKey.(type) {
	case *rsa.PrivateKey:
		pub, ok := certPubKey.(*rsa.PublicKey)
		if !ok {
			return false
		}
		return priv.N.Cmp(pub.N) == 0 && priv.E == pub.E
	case *ecdsa.PrivateKey:
		pub, ok := certPubKey.(*ecdsa.PublicKey)
		if !ok {
			return false
		}
		return priv.PublicKey.X.Cmp(pub.X) == 0 && priv.PublicKey.Y.Cmp(pub.Y) == 0
	case ed25519.PrivateKey:
		pub, ok := certPubKey.(ed25519.PublicKey)
		if !ok {
			return false
		}
		return bytes.Equal(priv.Public().(ed25519.PublicKey), pub)
	default:
		return false
	}
}

// WriteFilesAtomically writes the certificate and private key to their target paths
// using atomic file operations (write to temp, then rename).
// It creates target directories with 0755 permissions and sets file permissions
// (cert: 0644, privkey: 0600).
// If the key rename fails after the cert was already replaced, it rolls back the cert
// from a backup to avoid a partial update.
func (w *DeployWorker) WriteFilesAtomically(certPath, keyPath string, certData, keyData []byte) error {
	// Create target directories
	certDir := filepath.Dir(certPath)
	keyDir := filepath.Dir(keyPath)

	if err := os.MkdirAll(certDir, 0755); err != nil {
		return fmt.Errorf("failed to create certificate directory %s: %w", certDir, err)
	}

	if certDir != keyDir {
		if err := os.MkdirAll(keyDir, 0755); err != nil {
			return fmt.Errorf("failed to create private key directory %s: %w", keyDir, err)
		}
	}

	// Write cert to temp file
	certTmpPath, err := writeTempFile(certDir, certData)
	if err != nil {
		return fmt.Errorf("failed to write cert temp file: %w", err)
	}

	// Write key to temp file
	keyTmpPath, err := writeTempFile(keyDir, keyData)
	if err != nil {
		// Clean up cert temp file
		os.Remove(certTmpPath)
		return fmt.Errorf("failed to write key temp file: %w", err)
	}

	// Set permissions on temp files before rename
	if err := os.Chmod(certTmpPath, 0644); err != nil {
		os.Remove(certTmpPath)
		os.Remove(keyTmpPath)
		return fmt.Errorf("failed to set cert file permissions: %w", err)
	}

	if err := os.Chmod(keyTmpPath, 0600); err != nil {
		os.Remove(certTmpPath)
		os.Remove(keyTmpPath)
		return fmt.Errorf("failed to set key file permissions: %w", err)
	}

	// Back up existing files if they exist
	certBakPath := certPath + ".bak"
	keyBakPath := keyPath + ".bak"
	certBackedUp := false
	keyBackedUp := false

	if _, err := os.Stat(certPath); err == nil {
		if err := copyFile(certPath, certBakPath); err == nil {
			certBackedUp = true
		}
	}
	if _, err := os.Stat(keyPath); err == nil {
		if err := copyFile(keyPath, keyBakPath); err == nil {
			keyBackedUp = true
		}
	}

	// Atomic rename: cert first, then key
	if err := os.Rename(certTmpPath, certPath); err != nil {
		os.Remove(certTmpPath)
		os.Remove(keyTmpPath)
		if certBackedUp {
			os.Remove(certBakPath)
		}
		if keyBackedUp {
			os.Remove(keyBakPath)
		}
		return fmt.Errorf("failed to rename cert file to target: %w", err)
	}

	if err := os.Rename(keyTmpPath, keyPath); err != nil {
		// Key rename failed - rollback cert from backup
		if certBackedUp {
			os.Rename(certBakPath, certPath)
		} else {
			os.Remove(certPath)
		}
		os.Remove(keyTmpPath)
		if keyBackedUp {
			os.Remove(keyBakPath)
		}
		return fmt.Errorf("failed to rename key file to target: %w", err)
	}

	// Success - clean up backup files
	if certBackedUp {
		os.Remove(certBakPath)
	}
	if keyBackedUp {
		os.Remove(keyBakPath)
	}

	return nil
}

// copyFile copies a file from src to dst.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, info.Mode())
}

// writeTempFile writes data to a temporary file in the specified directory
// and returns the path to the temp file.
func writeTempFile(dir string, data []byte) (string, error) {
	tmpFile, err := os.CreateTemp(dir, ".ssl-manager-tmp-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}

	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to write to temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to close temp file: %w", err)
	}

	return tmpPath, nil
}

// ExecuteCommands runs the given commands sequentially with the configured timeout.
// If any command fails (non-zero exit code or timeout), it stops and returns an error.
func (w *DeployWorker) ExecuteCommands(ctx context.Context, commands []string) ([]model.CommandOutput, error) {
	var outputs []model.CommandOutput

	for _, cmdStr := range commands {
		cmdStr = strings.TrimSpace(cmdStr)
		if cmdStr == "" {
			continue
		}

		output := w.executeCommand(ctx, cmdStr)
		outputs = append(outputs, output)

		if output.ExitCode != 0 {
			return outputs, fmt.Errorf("command %q failed with exit code %d", cmdStr, output.ExitCode)
		}
	}

	return outputs, nil
}

// executeCommand runs a single shell command with timeout.
func (w *DeployWorker) executeCommand(ctx context.Context, cmdStr string) model.CommandOutput {
	return executeCommandWithTimeout(ctx, cmdStr, w.CommandTimeout)
}

// executeCommandWithTimeout runs a single shell command with the given timeout.
func executeCommandWithTimeout(ctx context.Context, cmdStr string, timeout time.Duration) model.CommandOutput {
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := newShellCommand(cmdCtx, cmdStr)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	output := model.CommandOutput{
		Command: cmdStr,
		Stdout:  stdout.String(),
		Stderr:  stderr.String(),
	}

	if err != nil {
		if cmdCtx.Err() == context.DeadlineExceeded {
			output.ExitCode = -1
			output.TimedOut = true
			output.Stderr = fmt.Sprintf("command timed out after %v: %s", timeout, stderr.String())
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			output.ExitCode = exitErr.ExitCode()
		} else {
			output.ExitCode = -1
			output.Stderr = fmt.Sprintf("failed to execute command: %v", err)
		}
	}

	return output
}

// newShellCommand creates a platform-appropriate shell command.
func newShellCommand(ctx context.Context, cmdStr string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", cmdStr)
	}
	return exec.CommandContext(ctx, "sh", "-c", cmdStr)
}

// parseCommands splits the post_deploy_commands string into individual commands.
// Commands are separated by newlines.
func parseCommands(commands string) []string {
	lines := strings.Split(commands, "\n")
	var result []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// ExecuteCommands is a package-level function that runs commands sequentially with the given timeout.
// If any command fails (non-zero exit code or timeout), it stops and returns an error.
func ExecuteCommands(ctx context.Context, commands []string, timeout time.Duration) ([]model.CommandOutput, error) {
	var outputs []model.CommandOutput

	for _, cmdStr := range commands {
		cmdStr = strings.TrimSpace(cmdStr)
		if cmdStr == "" {
			continue
		}

		output := executeCommandWithTimeout(ctx, cmdStr, timeout)
		outputs = append(outputs, output)

		if output.ExitCode != 0 {
			return outputs, fmt.Errorf("command %q failed with exit code %d", cmdStr, output.ExitCode)
		}
	}

	return outputs, nil
}

// WriteFilesAtomic is a package-level function that writes cert and key files atomically.
// It creates target directories with 0755 permissions and sets file permissions
// (cert: 0644, privkey: 0600).
func WriteFilesAtomic(certPath, keyPath string, certData, keyData []byte) error {
	w := &DeployWorker{}
	return w.WriteFilesAtomically(certPath, keyPath, certData, keyData)
}

// reportDeploymentLog sends the deployment result to the server.
// POST /api/agent/deployment-logs
func (w *DeployWorker) reportDeploymentLog(ctx context.Context, certCfg CertConfigResponse, result *DeployResult) {
	logEntry := struct {
		MachineCertificateID  string                `json:"machine_certificate_id"`
		MachineID             string                `json:"machine_id"`
		CertificateID         string                `json:"certificate_id"`
		Status                string                `json:"status"`
		CertFingerprintSHA256 string                `json:"cert_fingerprint_sha256"`
		CertPath              string                `json:"cert_path"`
		PrivateKeyPath        string                `json:"private_key_path"`
		CommandOutputs        []model.CommandOutput `json:"command_outputs"`
		ErrorMessage          string                `json:"error_message"`
		StartedAt             time.Time             `json:"started_at"`
		FinishedAt            time.Time             `json:"finished_at"`
	}{
		MachineCertificateID:  certCfg.MachineCertificateID,
		MachineID:             w.cfg.MachineID,
		CertificateID:         certCfg.CertificateID,
		Status:                result.Status,
		CertFingerprintSHA256: certCfg.FingerprintSHA256,
		CertPath:              certCfg.CertPath,
		PrivateKeyPath:        certCfg.PrivateKeyPath,
		CommandOutputs:        result.CommandOutputs,
		ErrorMessage:          result.ErrorMessage,
		StartedAt:             result.StartedAt,
		FinishedAt:            result.FinishedAt,
	}

	body, err := json.Marshal(logEntry)
	if err != nil {
		log.Printf("[ERROR] Failed to marshal deployment log: %v", err)
		return
	}

	url := fmt.Sprintf("%s/api/agent/deployment-logs", w.cfg.ServerURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		log.Printf("[ERROR] Failed to create deployment log request: %v", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.cfg.AgentToken)

	resp, err := w.httpClient.Do(req)
	if err != nil {
		log.Printf("[WARN] Failed to report deployment log: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("[WARN] Deployment log report returned status %d: %s", resp.StatusCode, string(respBody))
	} else {
		log.Printf("[DEBUG] Deployment log reported successfully for %s", certCfg.MachineCertificateID)
	}
}

// updateLocalState updates the local state file with the deployment result.
func (w *DeployWorker) updateLocalState(certCfg CertConfigResponse, fingerprint string, status string) {
	w.state.SetCertState(certCfg.MachineCertificateID, &config.MachineCertState{
		MachineCertificateID:  certCfg.MachineCertificateID,
		LastSyncedRevision:    certCfg.ConfigRevision,
		LastSyncedFingerprint: fingerprint,
		LastDeployStatus:      status,
		LastDeployAt:          time.Now().UTC().Format(time.RFC3339),
	})

	if err := config.SaveState(w.statePath, w.state); err != nil {
		log.Printf("[ERROR] Failed to save local state: %v", err)
	}
}
