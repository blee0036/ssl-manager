package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
	"github.com/ssl-manager/ssl-manager/internal/web/service"

	_ "github.com/glebarez/sqlite"
)

// setupVersionCache creates a VersionCache with a temp dir containing fake binaries.
// Uses a 1-hour scan interval to prevent background goroutine interference.
func setupVersionCache(t *testing.T) (*service.VersionCache, string) {
	t.Helper()

	binDir := t.TempDir()

	// Write agent-version.txt
	if err := os.WriteFile(filepath.Join(binDir, "agent-version.txt"), []byte("1.2.3"), 0644); err != nil {
		t.Fatalf("failed to write agent-version.txt: %v", err)
	}

	// Create fake binaries for supported platforms
	platforms := []struct {
		os   string
		arch string
	}{
		{"linux", "amd64"},
		{"linux", "arm64"},
		{"darwin", "amd64"},
		{"darwin", "arm64"},
	}

	for _, p := range platforms {
		fileName := "ssl-manager-agent-" + p.os + "-" + p.arch
		content := []byte("fake-binary-" + p.os + "-" + p.arch)
		if err := os.WriteFile(filepath.Join(binDir, fileName), content, 0755); err != nil {
			t.Fatalf("failed to write fake binary %s: %v", fileName, err)
		}
	}

	// Use 1-hour scan interval to prevent background goroutine interference
	vc := service.NewVersionCache(binDir, 1*time.Hour)
	t.Cleanup(func() { vc.Stop() })

	return vc, binDir
}

// setupInstallHandlerWithVersionCache creates an InstallHandler with a VersionCache for testing.
func setupInstallHandlerWithVersionCache(t *testing.T) (*InstallHandler, *chi.Mux, *service.VersionCache) {
	t.Helper()

	vc, binDir := setupVersionCache(t)

	cfg := &config.Config{
		Server: config.ServerConfig{
			ExternalURL: "https://ssl.example.com",
		},
		Agent: config.AgentConfig{
			PollIntervalSeconds: 60,
		},
	}
	runtimeCfg := config.NewRuntimeConfig(cfg)

	handler := NewInstallHandler(runtimeCfg, binDir, vc)

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	return handler, r, vc
}

func TestGetVersionInfo_ReturnsAllReleases(t *testing.T) {
	_, r, _ := setupInstallHandlerWithVersionCache(t)

	req := httptest.NewRequest(http.MethodGet, "/api/agent/version", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Verify version field
	version, ok := resp["version"].(string)
	if !ok || version != "1.2.3" {
		t.Errorf("expected version '1.2.3', got '%v'", resp["version"])
	}

	// Verify releases array
	releases, ok := resp["releases"].([]interface{})
	if !ok {
		t.Fatalf("expected releases to be an array, got %T", resp["releases"])
	}

	if len(releases) != 4 {
		t.Fatalf("expected 4 releases, got %d", len(releases))
	}

	// Verify each release has required fields
	for _, rel := range releases {
		release := rel.(map[string]interface{})
		if _, ok := release["os"]; !ok {
			t.Error("release missing 'os' field")
		}
		if _, ok := release["arch"]; !ok {
			t.Error("release missing 'arch' field")
		}
		if _, ok := release["md5"]; !ok {
			t.Error("release missing 'md5' field")
		}
		if _, ok := release["size"]; !ok {
			t.Error("release missing 'size' field")
		}
		if _, ok := release["download_url"]; !ok {
			t.Error("release missing 'download_url' field")
		}
	}
}

func TestGetVersionInfo_FilterByOsArch(t *testing.T) {
	_, r, _ := setupInstallHandlerWithVersionCache(t)

	req := httptest.NewRequest(http.MethodGet, "/api/agent/version?os=linux&arch=amd64", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Verify version field
	version, ok := resp["version"].(string)
	if !ok || version != "1.2.3" {
		t.Errorf("expected version '1.2.3', got '%v'", resp["version"])
	}

	// Verify only one release returned
	releases, ok := resp["releases"].([]interface{})
	if !ok {
		t.Fatalf("expected releases to be an array, got %T", resp["releases"])
	}

	if len(releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(releases))
	}

	release := releases[0].(map[string]interface{})
	if release["os"] != "linux" {
		t.Errorf("expected os 'linux', got '%v'", release["os"])
	}
	if release["arch"] != "amd64" {
		t.Errorf("expected arch 'amd64', got '%v'", release["arch"])
	}
	if release["md5"] == "" {
		t.Error("expected non-empty md5")
	}
	if release["download_url"] != "/api/agent/binary?os=linux&arch=amd64" {
		t.Errorf("expected download_url '/api/agent/binary?os=linux&arch=amd64', got '%v'", release["download_url"])
	}
}

func TestGetVersionInfo_EmptyCache(t *testing.T) {
	// Create a VersionCache with an empty temp dir (no binaries, no version file)
	emptyDir := t.TempDir()
	vc := service.NewVersionCache(emptyDir, 1*time.Hour)
	t.Cleanup(func() { vc.Stop() })

	cfg := &config.Config{
		Server: config.ServerConfig{
			ExternalURL: "https://ssl.example.com",
		},
		Agent: config.AgentConfig{
			PollIntervalSeconds: 60,
		},
	}
	runtimeCfg := config.NewRuntimeConfig(cfg)

	handler := NewInstallHandler(runtimeCfg, emptyDir, vc)

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/api/agent/version", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Should return 404 when version is empty
	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestHeartbeat_IncludesVersionInfo(t *testing.T) {
	// Set up a VersionCache with binaries
	vc, _ := setupVersionCache(t)

	db := setupAgentTestDB(t)
	dataDir := t.TempDir()

	machineRepo := repository.NewMachineRepository(db)
	mcRepo := repository.NewMachineCertificateRepository(db)
	certRepo := repository.NewCertificateRepository(db, dataDir)
	deployLogRepo := repository.NewDeploymentLogRepository(db)

	cfg := &config.Config{
		Server: config.ServerConfig{
			ExternalURL: "https://ssl.example.com",
		},
	}
	machineService := service.NewMachineService(machineRepo, config.NewRuntimeConfig(cfg))
	mcService := service.NewMachineCertificateService(mcRepo)
	deployLogService := service.NewDeploymentLogService(deployLogRepo)

	// Create handler WITH versionCache
	handler := NewAgentHandler(machineService, mcService, deployLogService, certRepo, mcRepo, &mockAgentAlertSender{}, vc)

	// The token for our test machine
	token := "test-agent-token-version"
	tokenHash := hashToken(token)

	// Insert a test machine
	now := time.Now().UTC()
	_, err := db.Exec(`INSERT INTO machines (
		id, name, ip, hostname, os, arch, tags, remark, status,
		agent_version, agent_token_hash, agent_token_revoked_at,
		last_heartbeat_at, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"machine-ver-1", "Version Machine", "192.168.1.20", "", "", "",
		"[]", "", "pending", "", tokenHash, nil, nil,
		now.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("failed to insert test machine: %v", err)
	}

	// Create mock machine repo for middleware
	mockRepo := &mockMachineRepo{
		machines: map[string]*model.Machine{
			tokenHash: {
				ID:     "machine-ver-1",
				Name:   "Version Machine",
				IP:     "192.168.1.20",
				Status: "pending",
			},
		},
	}

	// Setup router with AgentAuthMiddleware
	r := chi.NewRouter()
	handler.RegisterRoutes(r, mockRepo, &mockAgentAlertSender{}, &mockAuditRepo{})

	// Send heartbeat with OS and Arch
	body := model.HeartbeatInfo{
		AgentVersion: "1.0.0",
		Hostname:     "version-test-host",
		IP:           "10.0.0.10",
		OS:           "linux",
		Arch:         "amd64",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/agent/heartbeat", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Verify heartbeat response includes version info
	if resp["status"] != "ok" {
		t.Errorf("expected status 'ok', got '%v'", resp["status"])
	}

	latestVersion, ok := resp["latest_version"].(string)
	if !ok || latestVersion != "1.2.3" {
		t.Errorf("expected latest_version '1.2.3', got '%v'", resp["latest_version"])
	}

	md5Val, ok := resp["md5"].(string)
	if !ok || md5Val == "" {
		t.Errorf("expected non-empty md5, got '%v'", resp["md5"])
	}

	downloadURL, ok := resp["download_url"].(string)
	if !ok || downloadURL != "/api/agent/binary?os=linux&arch=amd64" {
		t.Errorf("expected download_url '/api/agent/binary?os=linux&arch=amd64', got '%v'", resp["download_url"])
	}
}

// TestHeartbeat_NoVersionInfoWithoutOsArch verifies that heartbeat response
// does not include version info when OS/Arch are not provided.
func TestHeartbeat_NoVersionInfoWithoutOsArch(t *testing.T) {
	vc, _ := setupVersionCache(t)

	db := setupAgentTestDB(t)
	dataDir := t.TempDir()

	machineRepo := repository.NewMachineRepository(db)
	mcRepo := repository.NewMachineCertificateRepository(db)
	certRepo := repository.NewCertificateRepository(db, dataDir)
	deployLogRepo := repository.NewDeploymentLogRepository(db)

	cfg := &config.Config{
		Server: config.ServerConfig{
			ExternalURL: "https://ssl.example.com",
		},
	}
	machineService := service.NewMachineService(machineRepo, config.NewRuntimeConfig(cfg))
	mcService := service.NewMachineCertificateService(mcRepo)
	deployLogService := service.NewDeploymentLogService(deployLogRepo)

	handler := NewAgentHandler(machineService, mcService, deployLogService, certRepo, mcRepo, &mockAgentAlertSender{}, vc)

	token := "test-agent-token-no-os"
	tokenHash := hashToken(token)

	now := time.Now().UTC()
	_, err := db.Exec(`INSERT INTO machines (
		id, name, ip, hostname, os, arch, tags, remark, status,
		agent_version, agent_token_hash, agent_token_revoked_at,
		last_heartbeat_at, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"machine-no-os-1", "No OS Machine", "192.168.1.30", "", "", "",
		"[]", "", "pending", "", tokenHash, nil, nil,
		now.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("failed to insert test machine: %v", err)
	}

	mockRepo := &mockMachineRepo{
		machines: map[string]*model.Machine{
			tokenHash: {
				ID:     "machine-no-os-1",
				Name:   "No OS Machine",
				IP:     "192.168.1.30",
				Status: "pending",
			},
		},
	}

	r := chi.NewRouter()
	handler.RegisterRoutes(r, mockRepo, &mockAgentAlertSender{}, &mockAuditRepo{})

	// Send heartbeat WITHOUT OS and Arch
	body := model.HeartbeatInfo{
		AgentVersion: "1.0.0",
		Hostname:     "no-os-host",
		IP:           "10.0.0.30",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/agent/heartbeat", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Should NOT include version info when OS/Arch are empty
	if _, exists := resp["latest_version"]; exists {
		t.Errorf("expected no latest_version in response when OS/Arch not provided, got '%v'", resp["latest_version"])
	}
	if _, exists := resp["md5"]; exists {
		t.Errorf("expected no md5 in response when OS/Arch not provided, got '%v'", resp["md5"])
	}
	if _, exists := resp["download_url"]; exists {
		t.Errorf("expected no download_url in response when OS/Arch not provided, got '%v'", resp["download_url"])
	}
}

// TestDownloadBinary_Returns409WhenMD5Mismatch verifies that when the binary file
// changes after the VersionCache scan (MD5 mismatch), the download endpoint returns
// 409 Conflict instead of serving a file that won't match the client's expected MD5.
func TestDownloadBinary_Returns409WhenMD5Mismatch(t *testing.T) {
	vc, binDir := setupVersionCache(t)

	cfg := &config.Config{
		Server: config.ServerConfig{
			ExternalURL: "https://ssl.example.com",
		},
		Agent: config.AgentConfig{
			PollIntervalSeconds: 60,
		},
	}
	runtimeCfg := config.NewRuntimeConfig(cfg)

	handler := NewInstallHandler(runtimeCfg, binDir, vc)

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	// First, verify normal download works (200 OK)
	req := httptest.NewRequest(http.MethodGet, "/api/agent/binary?os=linux&arch=amd64", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected initial download to return 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Now modify the binary file AFTER the cache was built (simulating a release update)
	binaryPath := filepath.Join(binDir, "ssl-manager-agent-linux-amd64")
	newContent := []byte("completely-different-binary-content-v2.0.0")
	if err := os.WriteFile(binaryPath, newContent, 0755); err != nil {
		t.Fatalf("failed to overwrite binary: %v", err)
	}

	// Download again — should get 409 because file MD5 no longer matches cached MD5
	req = httptest.NewRequest(http.MethodGet, "/api/agent/binary?os=linux&arch=amd64", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict after binary changed, got %d; body: %s", w.Code, w.Body.String())
	}

	// After the 409, the cache should have been rescanned.
	// A subsequent download should succeed with the new content.
	req = httptest.NewRequest(http.MethodGet, "/api/agent/binary?os=linux&arch=amd64", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 after rescan, got %d; body: %s", w.Code, w.Body.String())
	}

	// Verify the served content is the new binary
	if w.Body.String() != string(newContent) {
		t.Errorf("expected new binary content after rescan, got different content")
	}
}
