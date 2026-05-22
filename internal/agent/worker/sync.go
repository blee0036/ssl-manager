package worker

import (
	"context"
	"encoding/json"
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
// 1. Local certificate file doesn't exist
// 2. Local fingerprint doesn't match server fingerprint
// 3. config_revision differs from local last_synced_revision
// 4. Server status is "pending"
func NeedsDeployment(cfg CertConfigResponse, localState *config.MachineCertState) bool {
	// If no local state exists, this is a new config that needs deployment
	if localState == nil {
		return true
	}

	// Check if local certificate file doesn't exist
	if !fileExists(cfg.CertPath) {
		return true
	}

	// Check if local fingerprint doesn't match server fingerprint
	if localState.LastSyncedFingerprint != cfg.FingerprintSHA256 {
		return true
	}

	// Check if config_revision differs from local last_synced_revision
	if localState.LastSyncedRevision != cfg.ConfigRevision {
		return true
	}

	// Check if server status is "pending"
	if cfg.LastDeployStatus == "pending" {
		return true
	}

	return false
}

// fileExists checks if a file exists at the given path.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
