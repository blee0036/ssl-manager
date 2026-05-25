package handler

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/web/service"
)

// InstallHandler handles Agent installation script and binary download endpoints.
// These endpoints are public (no authentication required) since they are accessed
// during the initial agent setup process.
type InstallHandler struct {
	runtimeCfg   *config.RuntimeConfig
	agentDir     string // directory containing pre-built agent binaries
	versionCache *service.VersionCache
}

// NewInstallHandler creates a new InstallHandler.
// agentDir is the directory where pre-built agent binaries are stored (e.g., "./bin").
// versionCache is optional (can be nil) for backward compatibility.
func NewInstallHandler(runtimeCfg *config.RuntimeConfig, agentDir string, versionCache ...*service.VersionCache) *InstallHandler {
	var vc *service.VersionCache
	if len(versionCache) > 0 {
		vc = versionCache[0]
	}
	return &InstallHandler{
		runtimeCfg:   runtimeCfg,
		agentDir:     agentDir,
		versionCache: vc,
	}
}

// RegisterRoutes registers install-related routes on the given chi router.
// These routes are public and do not require authentication.
func (h *InstallHandler) RegisterRoutes(r chi.Router) {
	r.Get("/api/agent/install.sh", h.GetInstallScript)
	r.Get("/api/agent/binary", h.DownloadBinary)
	r.Get("/api/agent/version", h.GetVersionInfo)
}

// GetInstallScript handles GET /api/agent/install.sh
// Returns a bash install script that:
// - Detects systemd environment
// - Downloads the agent binary
// - Creates config directory /etc/ssl-manager-agent/
// - Writes the config file
// - Creates and starts a systemd service
// For non-systemd environments, outputs an error with manual run instructions.
func (h *InstallHandler) GetInstallScript(w http.ResponseWriter, r *http.Request) {
	cfg := h.runtimeCfg.Get()
	serverURL := cfg.Server.ExternalURL
	pollInterval := cfg.Agent.PollIntervalSeconds

	script := generateInstallScript(serverURL, pollInterval)

	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Content-Disposition", "inline; filename=\"install.sh\"")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(script))
}

// DownloadBinary handles GET /api/agent/binary
// Serves pre-built agent binaries by OS and architecture.
// If versionCache is available, uses it to get the file path for consistency.
// Otherwise falls back to constructing the path from agentDir.
func (h *InstallHandler) DownloadBinary(w http.ResponseWriter, r *http.Request) {
	// Determine the binary filename based on request or default to linux-amd64
	osParam := r.URL.Query().Get("os")
	archParam := r.URL.Query().Get("arch")

	if osParam == "" {
		osParam = "linux"
	}
	if archParam == "" {
		archParam = "amd64"
	}

	if osParam != "linux" && osParam != "darwin" {
		writeErrorResponse(w, http.StatusBadRequest, "only linux and darwin are supported", "")
		return
	}
	if archParam != "amd64" && archParam != "arm64" {
		writeErrorResponse(w, http.StatusBadRequest, "only amd64 and arm64 are supported", "")
		return
	}

	var binaryPath string
	var cachedMD5 string

	// If versionCache is available, use it for consistent file path
	if h.versionCache != nil {
		release, found := h.versionCache.GetRelease(osParam, archParam)
		if !found {
			writeErrorResponse(w, http.StatusNotFound, "agent binary not found for the specified platform", "")
			return
		}
		binaryPath = release.FilePath
		cachedMD5 = release.MD5
	} else {
		// Fallback: construct path manually (backward compatibility)
		binaryName := fmt.Sprintf("ssl-manager-agent-%s-%s", osParam, archParam)
		binaryPath = filepath.Join(h.agentDir, binaryName)
	}

	// Check if binary exists
	info, err := os.Stat(binaryPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeErrorResponse(w, http.StatusNotFound, "agent binary not found, please build it first", "")
			return
		}
		writeErrorResponse(w, http.StatusInternalServerError, "failed to access agent binary", err.Error())
		return
	}

	// If we have a cached MD5, verify the file hasn't changed since the last scan.
	// This ensures the MD5 returned by /api/agent/version matches the actual download.
	// If the file changed, we return 409 Conflict so the Agent re-fetches version info
	// (which will now have the new MD5) and retries the download on the next attempt.
	if cachedMD5 != "" {
		currentMD5, err := computeDownloadMD5(binaryPath)
		if err == nil && currentMD5 != cachedMD5 {
			// File changed since last scan — trigger rescan to update cache for future requests
			h.versionCache.Scan()
			// Return 409 so the Agent knows to re-fetch version info before retrying
			writeErrorResponse(w, http.StatusConflict,
				"binary has been updated since version info was retrieved, please re-fetch version info and retry", "")
			return
		}
	}

	binaryName := filepath.Base(binaryPath)

	// Serve the binary file
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", binaryName))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))

	http.ServeFile(w, r, binaryPath)
}

// GetVersionInfo handles GET /api/agent/version
// Returns version information and available releases.
// Supports optional ?os=<os>&arch=<arch> query params to filter releases.
func (h *InstallHandler) GetVersionInfo(w http.ResponseWriter, r *http.Request) {
	if h.versionCache == nil {
		writeErrorResponse(w, http.StatusNotFound, "version information not available", "")
		return
	}

	version := h.versionCache.GetVersion()
	if version == "" {
		writeErrorResponse(w, http.StatusNotFound, "version information not available", "")
		return
	}

	osParam := r.URL.Query().Get("os")
	archParam := r.URL.Query().Get("arch")

	type releaseResponse struct {
		OS          string `json:"os"`
		Arch        string `json:"arch"`
		MD5         string `json:"md5"`
		Size        int64  `json:"size"`
		DownloadURL string `json:"download_url"`
	}

	var releases []releaseResponse

	if osParam != "" || archParam != "" {
		// Filter by os/arch
		allReleases := h.versionCache.GetReleases()
		for _, rel := range allReleases {
			if (osParam == "" || rel.OS == osParam) && (archParam == "" || rel.Arch == archParam) {
				releases = append(releases, releaseResponse{
					OS:          rel.OS,
					Arch:        rel.Arch,
					MD5:         rel.MD5,
					Size:        rel.Size,
					DownloadURL: rel.DownloadURL,
				})
			}
		}
	} else {
		// Return all releases
		allReleases := h.versionCache.GetReleases()
		for _, rel := range allReleases {
			releases = append(releases, releaseResponse{
				OS:          rel.OS,
				Arch:        rel.Arch,
				MD5:         rel.MD5,
				Size:        rel.Size,
				DownloadURL: rel.DownloadURL,
			})
		}
	}

	if releases == nil {
		releases = []releaseResponse{}
	}

	resp := map[string]interface{}{
		"version":  version,
		"releases": releases,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// generateInstallScript generates the bash install script content.
// The script accepts --server-url, --machine-id, and --agent-token as arguments.
func generateInstallScript(serverURL string, pollIntervalSeconds int) string {
	if pollIntervalSeconds <= 0 {
		pollIntervalSeconds = 60
	}
	return fmt.Sprintf(`#!/bin/bash
set -e

# SSL Manager Agent Install Script
# This script installs and configures the SSL Manager Agent on Linux systemd or macOS launchd.

SERVER_URL=""
MACHINE_ID=""
AGENT_TOKEN=""
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/ssl-manager-agent"
SERVICE_NAME="ssl-manager-agent"

detect_os() {
    local system
    system="$(uname -s)"
    case "${system}" in
        Linux)
            echo "linux"
            ;;
        Darwin)
            echo "darwin"
            ;;
        *)
            echo "ERROR: unsupported OS: ${system}" >&2
            exit 1
            ;;
    esac
}

detect_arch() {
    local machine
    machine="$(uname -m)"
    case "${machine}" in
        x86_64|amd64)
            echo "amd64"
            ;;
        aarch64|arm64)
            echo "arm64"
            ;;
        *)
            echo "ERROR: unsupported architecture: ${machine}" >&2
            exit 1
            ;;
    esac
}

AGENT_OS="$(detect_os)"
AGENT_ARCH="$(detect_arch)"

# Set platform-specific config directory
if [ "${AGENT_OS}" = "darwin" ]; then
    CONFIG_DIR="/Library/Application Support/ssl-manager-agent"
fi

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --server-url)
            SERVER_URL="$2"
            shift 2
            ;;
        --machine-id)
            MACHINE_ID="$2"
            shift 2
            ;;
        --agent-token)
            AGENT_TOKEN="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Validate required parameters
if [ -z "$SERVER_URL" ]; then
    SERVER_URL="%s"
fi

if [ -z "$MACHINE_ID" ]; then
    echo "ERROR: --machine-id is required"
    exit 1
fi

if [ -z "$AGENT_TOKEN" ]; then
    echo "ERROR: --agent-token is required"
    exit 1
fi

# Check if running as root
if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: This script must be run as root (use sudo)"
    exit 1
fi

# Detect systemd
check_systemd() {
    if [ ! -d /run/systemd/system ]; then
        return 1
    fi
    if ! command -v systemctl &> /dev/null; then
        return 1
    fi
    return 0
}

manual_instructions() {
    echo "============================================================"
    echo "Manual run instructions"
    echo "============================================================"
    echo ""
    echo "To run the agent manually, follow these steps:"
    echo ""
    echo "1. Download the agent binary:"
    echo "   curl -fsSL ${SERVER_URL}/api/agent/binary?os=${AGENT_OS}\\&arch=${AGENT_ARCH} -o ${INSTALL_DIR}/ssl-manager-agent"
    echo "   chmod +x ${INSTALL_DIR}/ssl-manager-agent"
    echo ""
    echo "2. Create the config directory:"
    echo "   mkdir -p ${CONFIG_DIR}"
    echo ""
    echo "3. Create the config file ${CONFIG_DIR}/config.yaml:"
    echo "   server_url: ${SERVER_URL}"
    echo "   machine_id: ${MACHINE_ID}"
    echo "   agent_token: ${AGENT_TOKEN}"
    echo "   poll_interval_seconds: %d"
    echo "   log_level: info"
    echo "   auto_update: true"
    echo ""
    echo "4. Run the agent:"
    echo "   ${INSTALL_DIR}/ssl-manager-agent --config ${CONFIG_DIR}/config.yaml"
    echo ""
    echo "============================================================"
}

echo "==> Installing SSL Manager Agent..."
echo "    Server URL: ${SERVER_URL}"
echo "    Machine ID: ${MACHINE_ID}"
echo "    Platform: ${AGENT_OS}/${AGENT_ARCH}"

# Step 1: Download agent binary
echo "==> Downloading agent binary..."
curl -fsSL "${SERVER_URL}/api/agent/binary?os=${AGENT_OS}&arch=${AGENT_ARCH}" -o "${INSTALL_DIR}/ssl-manager-agent"
if [ ! -s "${INSTALL_DIR}/ssl-manager-agent" ]; then
    echo "ERROR: Downloaded file is empty or download failed"
    rm -f "${INSTALL_DIR}/ssl-manager-agent"
    exit 1
fi
chmod +x "${INSTALL_DIR}/ssl-manager-agent"
echo "    Binary installed to ${INSTALL_DIR}/ssl-manager-agent"

# Step 2: Create config directory
echo "==> Creating config directory..."
mkdir -p "${CONFIG_DIR}"
chmod 700 "${CONFIG_DIR}"

# Step 3: Write config file
echo "==> Writing config file..."
cat > "${CONFIG_DIR}/config.yaml" <<EOF
server_url: ${SERVER_URL}
machine_id: ${MACHINE_ID}
agent_token: ${AGENT_TOKEN}
poll_interval_seconds: %d
log_level: info
auto_update: true
EOF
chmod 600 "${CONFIG_DIR}/config.yaml"
echo "    Config written to ${CONFIG_DIR}/config.yaml"

if [ "${AGENT_OS}" = "linux" ]; then
    if ! check_systemd; then
        echo "ERROR: systemd not detected"
        echo "This install script supports automatic service setup only on Linux systems with systemd."
        manual_instructions
        exit 1
    fi

    # Step 4: Create systemd service
    echo "==> Creating systemd service..."
    cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<EOF
[Unit]
Description=SSL Manager Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${INSTALL_DIR}/ssl-manager-agent --config ${CONFIG_DIR}/config.yaml
Restart=always
RestartSec=10
User=root
WorkingDirectory=${CONFIG_DIR}

# Security hardening
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
EOF

    # Step 5: Enable and start service
    echo "==> Starting service..."
    systemctl daemon-reload
    systemctl enable "${SERVICE_NAME}"
    systemctl start "${SERVICE_NAME}"
    SERVICE_STATUS_CMD="systemctl status ${SERVICE_NAME}"
    SERVICE_LOG_CMD="journalctl -u ${SERVICE_NAME} -f"
elif [ "${AGENT_OS}" = "darwin" ]; then
    PLIST_PATH="/Library/LaunchDaemons/com.ssl-manager.agent.plist"

    echo "==> Creating launchd service..."
    cat > "${PLIST_PATH}" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.ssl-manager.agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>${INSTALL_DIR}/ssl-manager-agent</string>
        <string>--config</string>
        <string>${CONFIG_DIR}/config.yaml</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>WorkingDirectory</key>
    <string>${CONFIG_DIR}</string>
    <key>StandardOutPath</key>
    <string>/var/log/ssl-manager-agent.log</string>
    <key>StandardErrorPath</key>
    <string>/var/log/ssl-manager-agent.err.log</string>
</dict>
</plist>
EOF
    chmod 644 "${PLIST_PATH}"

    echo "==> Starting launchd service..."
    launchctl bootout system "${PLIST_PATH}" >/dev/null 2>&1 || true
    launchctl bootstrap system "${PLIST_PATH}"
    launchctl enable system/com.ssl-manager.agent
    SERVICE_STATUS_CMD="launchctl print system/com.ssl-manager.agent"
    SERVICE_LOG_CMD="tail -f /var/log/ssl-manager-agent.log /var/log/ssl-manager-agent.err.log"
else
    echo "ERROR: unsupported OS: ${AGENT_OS}"
    manual_instructions
    exit 1
fi

echo ""
echo "============================================================"
echo "SSL Manager Agent installed successfully!"
echo "============================================================"
echo ""
echo "Service status: ${SERVICE_STATUS_CMD}"
echo "View logs:      ${SERVICE_LOG_CMD}"
echo "Config file:    ${CONFIG_DIR}/config.yaml"
echo ""
echo "Available CLI commands:"
echo "  ssl-manager-agent version      - Show version info"
echo "  ssl-manager-agent update       - Check and update to latest version"
echo "  ssl-manager-agent auto-update  - View/set auto-update (enable/disable)"
echo "  ssl-manager-agent restart      - Restart the Agent service"
echo "  ssl-manager-agent logs         - View Agent logs (--follow, --lines N)"
echo "  ssl-manager-agent config       - View/modify config (--server-url, --token)"
echo "  ssl-manager-agent uninstall    - Completely uninstall the Agent"
echo ""
`, serverURL, pollIntervalSeconds, pollIntervalSeconds)
}

// computeDownloadMD5 calculates the MD5 hash of the file at the given path.
// Used to verify that the binary file hasn't changed since the last VersionCache scan.
func computeDownloadMD5(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
