package worker

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	agentconfig "github.com/ssl-manager/ssl-manager/internal/agent/config"
	"github.com/ssl-manager/ssl-manager/internal/agent/platform"
	"github.com/ssl-manager/ssl-manager/internal/agent/updater"
	"github.com/ssl-manager/ssl-manager/internal/agent/version"
)

// HeartbeatResponse represents the heartbeat response from the server,
// including optional version information for auto-update.
type HeartbeatResponse struct {
	Status        string `json:"status"`
	Message       string `json:"message"`
	LatestVersion string `json:"latest_version,omitempty"`
	MD5           string `json:"md5,omitempty"`
	DownloadURL   string `json:"download_url,omitempty"`
}

// AutoUpdateWorker checks for new versions in heartbeat responses and performs auto-updates.
type AutoUpdateWorker struct {
	config     *agentconfig.AgentConfig
	updater    *updater.Updater
	svcMgr     platform.ServiceManager
	currentVer string
}

// NewAutoUpdateWorker creates a new AutoUpdateWorker.
func NewAutoUpdateWorker(cfg *agentconfig.AgentConfig, u *updater.Updater, svcMgr platform.ServiceManager, currentVer string) *AutoUpdateWorker {
	return &AutoUpdateWorker{
		config:     cfg,
		updater:    u,
		svcMgr:     svcMgr,
		currentVer: currentVer,
	}
}

// HandleHeartbeatResponse processes the heartbeat response and triggers an update if a newer version is available.
// It follows a fail-safe strategy: any step failure logs an error but does not affect the current running agent.
func (w *AutoUpdateWorker) HandleHeartbeatResponse(resp *HeartbeatResponse) error {
	// 1. If LatestVersion is empty, no version info available
	if resp.LatestVersion == "" {
		return nil
	}

	// 2. Compare LatestVersion with currentVer
	newer, err := version.IsNewer(w.currentVer, resp.LatestVersion)
	if err != nil {
		return fmt.Errorf("auto-update: version comparison failed: %w", err)
	}
	if !newer {
		return nil
	}

	log.Printf("[INFO] Auto-update: new version available: %s (current: %s)", resp.LatestVersion, w.currentVer)

	// 3. Download from DownloadURL
	if resp.DownloadURL == "" {
		return fmt.Errorf("auto-update: download URL is empty for version %s", resp.LatestVersion)
	}

	tmpPath, err := w.updater.Download(resp.DownloadURL)
	if err != nil {
		return fmt.Errorf("auto-update: download failed: %w", err)
	}

	// 4. Verify MD5
	if resp.MD5 == "" {
		os.Remove(tmpPath)
		return fmt.Errorf("auto-update: MD5 checksum is empty for version %s", resp.LatestVersion)
	}

	if err := updater.VerifyMD5(tmpPath, resp.MD5); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("auto-update: MD5 verification failed: %w", err)
	}

	// 5. Atomic replace
	if err := updater.AtomicReplace(w.updater.CurrentPath, tmpPath); err != nil {
		return fmt.Errorf("auto-update: atomic replace failed: %w", err)
	}

	// 6. Restart service
	if err := w.svcMgr.Restart(); err != nil {
		log.Printf("[CRITICAL] Auto-update: binary replaced (%s → %s) but service restart failed: %v",
			w.currentVer, resp.LatestVersion, err)
		return fmt.Errorf("auto-update: service restart failed: %w", err)
	}

	// 7. Log success
	log.Printf("[INFO] Auto-update: successfully updated from %s to %s and restarted service",
		w.currentVer, resp.LatestVersion)

	return nil
}

// DecodeHeartbeatResponse decodes an HTTP response body into a HeartbeatResponse.
func DecodeHeartbeatResponse(resp *http.Response) (*HeartbeatResponse, error) {
	if resp == nil || resp.Body == nil {
		return nil, fmt.Errorf("response or response body is nil")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var hbResp HeartbeatResponse
	if err := json.Unmarshal(body, &hbResp); err != nil {
		return nil, fmt.Errorf("failed to decode heartbeat response: %w", err)
	}

	return &hbResp, nil
}
