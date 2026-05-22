package worker

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	agentconfig "github.com/ssl-manager/ssl-manager/internal/agent/config"
	"github.com/ssl-manager/ssl-manager/internal/agent/updater"
)

// mockSvcMgr implements platform.ServiceManager for testing
type mockSvcMgr struct {
	restartCalled bool
	restartErr    error
}

func (m *mockSvcMgr) Stop() error                          { return nil }
func (m *mockSvcMgr) Start() error                         { return nil }
func (m *mockSvcMgr) Restart() error                       { m.restartCalled = true; return m.restartErr }
func (m *mockSvcMgr) Disable() error                       { return nil }
func (m *mockSvcMgr) Enable() error                        { return nil }
func (m *mockSvcMgr) IsActive() (bool, error)              { return true, nil }
func (m *mockSvcMgr) Uninstall() error                     { return nil }
func (m *mockSvcMgr) GetLogs(lines int, follow bool) error { return nil }

// helper to create a test config with auto_update enabled
func testConfig(serverURL string, autoUpdate *bool) *agentconfig.AgentConfig {
	return &agentconfig.AgentConfig{
		ServerURL:           serverURL,
		MachineID:           "test-machine",
		AgentToken:          "test-token",
		PollIntervalSeconds: 60,
		AutoUpdate:          autoUpdate,
	}
}

// helper to create a fake current binary and return its path
func createFakeBinary(t *testing.T, dir string) string {
	t.Helper()
	currentPath := filepath.Join(dir, "ssl-manager-agent")
	if err := os.WriteFile(currentPath, []byte("old-binary-v1.0.0"), 0755); err != nil {
		t.Fatalf("failed to create fake binary: %v", err)
	}
	return currentPath
}

func TestAutoUpdateWorker_NoVersionInfo(t *testing.T) {
	// When LatestVersion is empty, no action should be taken
	tmpDir := t.TempDir()
	currentPath := createFakeBinary(t, tmpDir)

	cfg := testConfig("http://localhost", nil)
	u := &updater.Updater{
		ServerURL:   "http://localhost",
		CurrentPath: currentPath,
		HTTPClient:  http.DefaultClient,
	}
	svc := &mockSvcMgr{}

	worker := NewAutoUpdateWorker(cfg, u, svc, "1.0.0")

	resp := &HeartbeatResponse{
		Status:        "ok",
		Message:       "heartbeat received",
		LatestVersion: "", // empty - no version info
	}

	err := worker.HandleHeartbeatResponse(resp)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if svc.restartCalled {
		t.Error("restart should not be called when no version info is available")
	}
}

func TestAutoUpdateWorker_AlreadyUpToDate(t *testing.T) {
	// When LatestVersion equals current version, no download should occur
	downloadCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloadCalled = true
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("binary"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	currentPath := createFakeBinary(t, tmpDir)

	cfg := testConfig(server.URL, nil)
	u := &updater.Updater{
		ServerURL:   server.URL,
		CurrentPath: currentPath,
		HTTPClient:  server.Client(),
	}
	svc := &mockSvcMgr{}

	worker := NewAutoUpdateWorker(cfg, u, svc, "1.0.0")

	resp := &HeartbeatResponse{
		Status:        "ok",
		Message:       "heartbeat received",
		LatestVersion: "1.0.0", // same as current
		MD5:           "abc123",
		DownloadURL:   "/downloads/agent",
	}

	err := worker.HandleHeartbeatResponse(resp)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if downloadCalled {
		t.Error("download should not be called when already up to date")
	}
	if svc.restartCalled {
		t.Error("restart should not be called when already up to date")
	}
}

func TestAutoUpdateWorker_UpdateTriggered(t *testing.T) {
	// When a newer version is available, full update flow should execute
	binaryContent := []byte("new-agent-binary-v2.0.0")
	hash := md5.Sum(binaryContent)
	expectedMD5 := hex.EncodeToString(hash[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/downloads/agent-linux-amd64" {
			w.WriteHeader(http.StatusOK)
			w.Write(binaryContent)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	currentPath := createFakeBinary(t, tmpDir)

	cfg := testConfig(server.URL, nil)
	u := &updater.Updater{
		ServerURL:   server.URL,
		CurrentPath: currentPath,
		HTTPClient:  server.Client(),
	}
	svc := &mockSvcMgr{}

	worker := NewAutoUpdateWorker(cfg, u, svc, "1.0.0")

	resp := &HeartbeatResponse{
		Status:        "ok",
		Message:       "heartbeat received",
		LatestVersion: "2.0.0",
		MD5:           expectedMD5,
		DownloadURL:   "/downloads/agent-linux-amd64",
	}

	err := worker.HandleHeartbeatResponse(resp)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify the binary was replaced with new content
	data, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatalf("failed to read replaced binary: %v", err)
	}
	if string(data) != string(binaryContent) {
		t.Errorf("expected binary content %q, got %q", binaryContent, data)
	}

	// Verify restart was called
	if !svc.restartCalled {
		t.Error("expected Restart() to be called after successful update")
	}
}

func TestAutoUpdateWorker_MD5Mismatch(t *testing.T) {
	// Download succeeds but MD5 doesn't match - verify error and temp file cleaned
	binaryContent := []byte("new-agent-binary-v2.0.0")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/downloads/agent-linux-amd64" {
			w.WriteHeader(http.StatusOK)
			w.Write(binaryContent)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	currentPath := createFakeBinary(t, tmpDir)

	cfg := testConfig(server.URL, nil)
	u := &updater.Updater{
		ServerURL:   server.URL,
		CurrentPath: currentPath,
		HTTPClient:  server.Client(),
	}
	svc := &mockSvcMgr{}

	worker := NewAutoUpdateWorker(cfg, u, svc, "1.0.0")

	resp := &HeartbeatResponse{
		Status:        "ok",
		Message:       "heartbeat received",
		LatestVersion: "2.0.0",
		MD5:           "0000000000000000000000000000dead", // wrong MD5
		DownloadURL:   "/downloads/agent-linux-amd64",
	}

	err := worker.HandleHeartbeatResponse(resp)
	if err == nil {
		t.Fatal("expected error for MD5 mismatch")
	}

	// Verify the temp file was cleaned up
	tmpPath := filepath.Join(tmpDir, "ssl-manager-agent.download.tmp")
	if _, statErr := os.Stat(tmpPath); !os.IsNotExist(statErr) {
		t.Error("expected temp file to be cleaned up after MD5 mismatch")
	}

	// Verify original binary is unchanged
	data, err2 := os.ReadFile(currentPath)
	if err2 != nil {
		t.Fatalf("failed to read original binary: %v", err2)
	}
	if string(data) != "old-binary-v1.0.0" {
		t.Errorf("expected original binary content to be unchanged, got %q", data)
	}

	// Verify restart was NOT called
	if svc.restartCalled {
		t.Error("restart should not be called when MD5 verification fails")
	}
}

func TestAutoUpdateWorker_DownloadFails(t *testing.T) {
	// Server returns error for download - verify error returned
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	currentPath := createFakeBinary(t, tmpDir)

	cfg := testConfig(server.URL, nil)
	u := &updater.Updater{
		ServerURL:   server.URL,
		CurrentPath: currentPath,
		HTTPClient:  server.Client(),
	}
	svc := &mockSvcMgr{}

	worker := NewAutoUpdateWorker(cfg, u, svc, "1.0.0")

	resp := &HeartbeatResponse{
		Status:        "ok",
		Message:       "heartbeat received",
		LatestVersion: "2.0.0",
		MD5:           "abc123",
		DownloadURL:   "/downloads/agent-linux-amd64",
	}

	err := worker.HandleHeartbeatResponse(resp)
	if err == nil {
		t.Fatal("expected error when download fails")
	}

	// Verify restart was NOT called
	if svc.restartCalled {
		t.Error("restart should not be called when download fails")
	}

	// Verify original binary is unchanged
	data, err2 := os.ReadFile(currentPath)
	if err2 != nil {
		t.Fatalf("failed to read original binary: %v", err2)
	}
	if string(data) != "old-binary-v1.0.0" {
		t.Errorf("expected original binary content to be unchanged, got %q", data)
	}
}

func TestAutoUpdateWorker_RestartFails(t *testing.T) {
	// Everything succeeds except restart - verify error is returned
	binaryContent := []byte("new-agent-binary-v2.0.0")
	hash := md5.Sum(binaryContent)
	expectedMD5 := hex.EncodeToString(hash[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/downloads/agent-linux-amd64" {
			w.WriteHeader(http.StatusOK)
			w.Write(binaryContent)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	currentPath := createFakeBinary(t, tmpDir)

	cfg := testConfig(server.URL, nil)
	u := &updater.Updater{
		ServerURL:   server.URL,
		CurrentPath: currentPath,
		HTTPClient:  server.Client(),
	}
	svc := &mockSvcMgr{restartErr: fmt.Errorf("systemctl restart failed")}

	worker := NewAutoUpdateWorker(cfg, u, svc, "1.0.0")

	resp := &HeartbeatResponse{
		Status:        "ok",
		Message:       "heartbeat received",
		LatestVersion: "2.0.0",
		MD5:           expectedMD5,
		DownloadURL:   "/downloads/agent-linux-amd64",
	}

	err := worker.HandleHeartbeatResponse(resp)
	if err == nil {
		t.Fatal("expected error when restart fails")
	}

	// Verify restart was called
	if !svc.restartCalled {
		t.Error("expected Restart() to be called")
	}

	// Binary should still be replaced (update succeeded, only restart failed)
	data, err2 := os.ReadFile(currentPath)
	if err2 != nil {
		t.Fatalf("failed to read binary: %v", err2)
	}
	if string(data) != string(binaryContent) {
		t.Errorf("expected binary to be updated even though restart failed, got %q", data)
	}
}
