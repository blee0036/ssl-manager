package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"time"

	agentconfig "github.com/ssl-manager/ssl-manager/internal/agent/config"
	"github.com/ssl-manager/ssl-manager/internal/agent/platform"
	"github.com/ssl-manager/ssl-manager/internal/agent/updater"
	"github.com/ssl-manager/ssl-manager/internal/model"
)

// HeartbeatWorker sends periodic heartbeats to the Web Backend.
type HeartbeatWorker struct {
	config           *agentconfig.AgentConfig
	httpClient       *http.Client
	stopCh           chan struct{}
	autoUpdateWorker *AutoUpdateWorker
}

// NewHeartbeatWorker creates a new HeartbeatWorker.
func NewHeartbeatWorker(cfg *agentconfig.AgentConfig) *HeartbeatWorker {
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Create AutoUpdateWorker with dependencies
	var autoUpdateWorker *AutoUpdateWorker
	currentPath, err := os.Executable()
	if err == nil {
		u := &updater.Updater{
			ServerURL:   cfg.ServerURL,
			CurrentPath: currentPath,
			HTTPClient:  httpClient,
		}
		svcMgr := platform.NewServiceManager()
		if svcMgr != nil {
			autoUpdateWorker = NewAutoUpdateWorker(cfg, u, svcMgr, AgentVersion)
		}
	} else {
		log.Printf("[WARN] Failed to get executable path, auto-update disabled: %v", err)
	}

	return &HeartbeatWorker{
		config:           cfg,
		httpClient:       httpClient,
		stopCh:           make(chan struct{}),
		autoUpdateWorker: autoUpdateWorker,
	}
}

// Run starts the heartbeat worker. It sends the first heartbeat immediately,
// then repeats every poll_interval_seconds. If the server responds with 401,
// it signals the agent to stop all operations by closing the returned channel.
//
// The returned channel is closed when the agent should stop (token revoked).
// The caller should select on both ctx.Done() and the returned channel.
func (w *HeartbeatWorker) Run(ctx context.Context) <-chan struct{} {
	tokenRevokedCh := make(chan struct{})

	go func() {
		defer close(tokenRevokedCh)

		// Send first heartbeat immediately
		log.Println("[INFO] Sending initial heartbeat...")
		if revoked := w.sendHeartbeat(); revoked {
			log.Println("[ERROR] Agent token has been revoked, stopping all operations")
			return
		}

		ticker := time.NewTicker(time.Duration(w.config.PollIntervalSeconds) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Println("[INFO] Heartbeat worker stopped")
				return
			case <-w.stopCh:
				log.Println("[INFO] Heartbeat worker stopped externally")
				return
			case <-ticker.C:
				if revoked := w.sendHeartbeat(); revoked {
					log.Println("[ERROR] Agent token has been revoked, stopping all operations")
					return
				}
			}
		}
	}()

	return tokenRevokedCh
}

// Stop signals the heartbeat worker to stop.
func (w *HeartbeatWorker) Stop() {
	select {
	case <-w.stopCh:
		// Already closed
	default:
		close(w.stopCh)
	}
}

// sendHeartbeat sends a single heartbeat to the Web Backend.
// Returns true if the token has been revoked (401 response).
func (w *HeartbeatWorker) sendHeartbeat() bool {
	info := w.collectSystemInfo()

	body, err := json.Marshal(info)
	if err != nil {
		log.Printf("[ERROR] Failed to marshal heartbeat info: %v", err)
		return false
	}

	url := fmt.Sprintf("%s/api/agent/heartbeat", w.config.ServerURL)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		log.Printf("[ERROR] Failed to create heartbeat request: %v", err)
		return false
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.config.AgentToken)

	resp, err := w.httpClient.Do(req)
	if err != nil {
		log.Printf("[WARN] Failed to send heartbeat: %v", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		log.Println("[ERROR] Heartbeat rejected with 401: token revoked or invalid")
		return true
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[WARN] Heartbeat returned unexpected status: %d", resp.StatusCode)
		return false
	}

	// Decode response body for auto-update version info
	hbResp, err := DecodeHeartbeatResponse(resp)
	if err != nil {
		log.Printf("[WARN] Failed to decode heartbeat response: %v", err)
		// Don't stop heartbeat worker on decode failure
	} else if w.config.IsAutoUpdateEnabled() && w.autoUpdateWorker != nil {
		if updateErr := w.autoUpdateWorker.HandleHeartbeatResponse(hbResp); updateErr != nil {
			log.Printf("[ERROR] Auto-update failed: %v", updateErr)
			// Don't stop the heartbeat worker on auto-update failure
		}
	}

	log.Printf("[DEBUG] Heartbeat sent successfully to %s", w.config.ServerURL)
	return false
}

// collectSystemInfo gathers system information for the heartbeat payload.
func (w *HeartbeatWorker) collectSystemInfo() model.HeartbeatInfo {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	ip := getLocalIP()

	return model.HeartbeatInfo{
		MachineID:    w.config.MachineID,
		AgentVersion: AgentVersion,
		Hostname:     hostname,
		IP:           ip,
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
	}
}

// getLocalIP returns the preferred outbound IP address of the machine.
func getLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "unknown"
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

// AgentVersion is the current version of the agent binary.
// This should be set at build time via ldflags.
var AgentVersion = "dev"
