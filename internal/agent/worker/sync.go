package worker

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/agent/config"
)

// CertConfigResponse represents the certificate deployment config returned by the API.
// This is what GET /api/agent/machines/{machine_id}/certificates returns for each config.
type CertConfigResponse struct {
	MachineCertificateID string `json:"machine_certificate_id"`
	CertificateID        string `json:"certificate_id"`
	FingerprintSHA256    string `json:"fingerprint_sha256"`
	CertPath             string `json:"cert_path"`
	PrivateKeyPath       string `json:"private_key_path"`
	PostDeployCommands   string `json:"post_deploy_commands"`
	ConfigRevision       int    `json:"config_revision"`
	LastDeployStatus     string `json:"last_deploy_status"`
}

// SyncWorker is responsible for pulling certificate configs from the server
// and determining which ones need deployment.
type SyncWorker struct {
	cfg        *config.AgentConfig
	state      *config.AgentLocalState
	httpClient *http.Client
}

// NewSyncWorker creates a new SyncWorker.
func NewSyncWorker(cfg *config.AgentConfig, state *config.AgentLocalState) *SyncWorker {
	return &SyncWorker{
		cfg:   cfg,
		state: state,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// FetchCertConfigs calls GET /api/agent/machines/{machine_id}/certificates
// and returns the list of certificate deployment configs from the server.
func (w *SyncWorker) FetchCertConfigs(ctx context.Context) ([]CertConfigResponse, error) {
	url := fmt.Sprintf("%s/api/agent/machines/%s/certificates", w.cfg.ServerURL, w.cfg.MachineID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+w.cfg.AgentToken)

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch cert configs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("unauthorized: agent token rejected")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	// The API returns a SuccessResponse wrapper with data field containing the configs
	var apiResp struct {
		Code    int                  `json:"code"`
		Message string               `json:"message"`
		Data    []CertConfigResponse `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return apiResp.Data, nil
}

// GetConfigsNeedingDeployment fetches certificate configs from the server and
// returns only those that need deployment based on local state comparison.
func (w *SyncWorker) GetConfigsNeedingDeployment(ctx context.Context) ([]CertConfigResponse, error) {
	configs, err := w.FetchCertConfigs(ctx)
	if err != nil {
		return nil, err
	}

	var needDeploy []CertConfigResponse
	for _, cfg := range configs {
		localState := w.state.GetCertState(cfg.MachineCertificateID)
		if NeedsDeployment(cfg, localState) {
			needDeploy = append(needDeploy, cfg)
		}
	}

	return needDeploy, nil
}

// NeedsDeployment determines if a certificate config needs deployment by checking:
// 1. Local certificate file doesn't exist or cannot be parsed
// 2. Actual local certificate fingerprint doesn't match the server fingerprint
// 3. Local state fingerprint doesn't match the server fingerprint
// 4. config_revision differs from local last_synced_revision
// 5. The previous deployment failed
// 6. Server status is "pending" or "failed"
func NeedsDeployment(cfg CertConfigResponse, localState *config.MachineCertState) bool {
	// If no local state exists, this is a new config that needs deployment
	if localState == nil {
		return true
	}

	// Check if local certificate file doesn't exist
	if !fileExists(cfg.CertPath) {
		return true
	}

	// Check the actual certificate on disk, not only the persisted Agent state.
	// This catches certificates replaced or deleted by another process.
	localFingerprint, err := certificateFingerprint(cfg.CertPath)
	if err != nil || localFingerprint != cfg.FingerprintSHA256 {
		return true
	}

	// Check if the persisted local fingerprint doesn't match the server
	// fingerprint. This also repairs stale state after an interrupted deploy.
	if localState.LastSyncedFingerprint != cfg.FingerprintSHA256 {
		return true
	}

	// Check if config_revision differs from local last_synced_revision
	if localState.LastSyncedRevision != cfg.ConfigRevision {
		return true
	}

	// A failed deployment must remain retryable on every subsequent polling
	// cycle until a deployment succeeds.
	if localState.LastDeployStatus == "failed" || cfg.LastDeployStatus == "failed" {
		return true
	}

	// Check if server status is "pending"
	if cfg.LastDeployStatus == "pending" {
		return true
	}

	return false
}

// certificateFingerprint returns the SHA-256 fingerprint of the leaf
// certificate in a PEM or fullchain PEM file.
func certificateFingerprint(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read certificate: %w", err)
	}

	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", fmt.Errorf("certificate PEM block not found")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse certificate: %w", err)
	}

	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:]), nil
}

// fileExists checks if a file exists at the given path.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
