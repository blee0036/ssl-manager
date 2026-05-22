package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/config"
)

func newTestInstallHandler(t *testing.T, agentDir string) *InstallHandler {
	t.Helper()
	cfg := &config.Config{
		Server: config.ServerConfig{
			ExternalURL: "https://ssl.example.com",
			ListenAddr:  ":8080",
		},
		Agent: config.AgentConfig{
			HeartbeatTimeoutSeconds: 120,
			PollIntervalSeconds:     60,
		},
		Alert: config.AlertConfig{
			DefaultBeforeDays: 15,
		},
		DomainMonitor: config.DomainMonitorConfig{
			DefaultPort:     443,
			IntervalMinutes: 60,
		},
	}
	return NewInstallHandler(config.NewRuntimeConfig(cfg), agentDir)
}

func TestInstallHandler_GetInstallScript(t *testing.T) {
	handler := newTestInstallHandler(t, "./bin")

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/api/agent/install.sh", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/x-shellscript") {
		t.Errorf("expected Content-Type text/x-shellscript, got %s", contentType)
	}

	body := w.Body.String()

	// Verify script starts with shebang
	if !strings.HasPrefix(body, "#!/bin/bash") {
		t.Error("install script should start with #!/bin/bash")
	}

	// Verify script contains server URL
	if !strings.Contains(body, "https://ssl.example.com") {
		t.Error("install script should contain the server external URL")
	}

	// Verify script contains systemd detection
	if !strings.Contains(body, "check_systemd") {
		t.Error("install script should contain systemd detection logic")
	}

	// Verify script contains non-systemd error message
	if !strings.Contains(body, "systemd not detected") {
		t.Error("install script should contain non-systemd error message")
	}

	// Verify script contains manual run instructions for non-systemd
	if !strings.Contains(body, "To run the agent manually") {
		t.Error("install script should contain manual run instructions")
	}

	// Verify script creates config directory
	if !strings.Contains(body, "/etc/ssl-manager-agent") {
		t.Error("install script should reference /etc/ssl-manager-agent config directory")
	}

	// Verify script creates systemd service
	if !strings.Contains(body, "systemctl") {
		t.Error("install script should use systemctl to manage the service")
	}
	if !strings.Contains(body, "launchctl") {
		t.Error("install script should use launchctl to manage the macOS service")
	}

	// Verify script downloads binary from the correct endpoint
	if !strings.Contains(body, "/api/agent/binary") {
		t.Error("install script should download binary from /api/agent/binary")
	}

	// Verify script writes config.yaml
	if !strings.Contains(body, "config.yaml") {
		t.Error("install script should write config.yaml")
	}

	// Verify script accepts required parameters
	if !strings.Contains(body, "--server-url") {
		t.Error("install script should accept --server-url parameter")
	}
	if !strings.Contains(body, "--machine-id") {
		t.Error("install script should accept --machine-id parameter")
	}
	if !strings.Contains(body, "--agent-token") {
		t.Error("install script should accept --agent-token parameter")
	}
}

func TestInstallHandler_DownloadBinary_Success(t *testing.T) {
	// Create a temporary directory with a fake binary
	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "ssl-manager-agent-linux-amd64")
	if err := os.WriteFile(binaryPath, []byte("fake-binary-content"), 0755); err != nil {
		t.Fatal(err)
	}

	handler := newTestInstallHandler(t, tmpDir)

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/api/agent/binary", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/octet-stream") {
		t.Errorf("expected Content-Type application/octet-stream, got %s", contentType)
	}

	if w.Body.String() != "fake-binary-content" {
		t.Errorf("unexpected body: %s", w.Body.String())
	}
}

func TestInstallHandler_DownloadBinary_NotFound(t *testing.T) {
	// Use a directory without the binary
	tmpDir := t.TempDir()

	handler := newTestInstallHandler(t, tmpDir)

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/api/agent/binary", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestInstallHandler_DownloadBinary_UnsupportedOS(t *testing.T) {
	tmpDir := t.TempDir()
	handler := newTestInstallHandler(t, tmpDir)

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/api/agent/binary?os=windows&arch=amd64", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestInstallHandler_DownloadBinary_CustomArch(t *testing.T) {
	// Create a temporary directory with an arm64 binary
	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "ssl-manager-agent-linux-arm64")
	if err := os.WriteFile(binaryPath, []byte("arm64-binary"), 0755); err != nil {
		t.Fatal(err)
	}

	handler := newTestInstallHandler(t, tmpDir)

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/api/agent/binary?os=linux&arch=arm64", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	if w.Body.String() != "arm64-binary" {
		t.Errorf("unexpected body: %s", w.Body.String())
	}
}

func TestInstallHandler_DownloadBinary_Darwin(t *testing.T) {
	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "ssl-manager-agent-darwin-arm64")
	if err := os.WriteFile(binaryPath, []byte("darwin-arm64-binary"), 0755); err != nil {
		t.Fatal(err)
	}

	handler := newTestInstallHandler(t, tmpDir)

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/api/agent/binary?os=darwin&arch=arm64", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	if w.Body.String() != "darwin-arm64-binary" {
		t.Errorf("unexpected body: %s", w.Body.String())
	}
}
