package updater

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// mockServiceManager implements platform.ServiceManager for testing
type mockServiceManager struct {
	restartCalled bool
	restartErr    error
}

func (m *mockServiceManager) Stop() error                    { return nil }
func (m *mockServiceManager) Start() error                   { return nil }
func (m *mockServiceManager) Restart() error                 { m.restartCalled = true; return m.restartErr }
func (m *mockServiceManager) Disable() error                 { return nil }
func (m *mockServiceManager) Enable() error                  { return nil }
func (m *mockServiceManager) IsActive() (bool, error)        { return true, nil }
func (m *mockServiceManager) Uninstall() error               { return nil }
func (m *mockServiceManager) GetLogs(lines int, follow bool) error { return nil }

func TestCheckVersion_Success(t *testing.T) {
	// Mock server returns valid VersionResponse with matching os/arch
	versionResp := VersionResponse{
		Version: "1.2.3",
		Releases: []ReleaseItem{
			{OS: "linux", Arch: "amd64", MD5: "abc123", Size: 1024, DownloadURL: "/downloads/agent-linux-amd64"},
			{OS: "darwin", Arch: "arm64", MD5: "def456", Size: 2048, DownloadURL: "/downloads/agent-darwin-arm64"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/version" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(versionResp)
	}))
	defer server.Close()

	u := &Updater{
		ServerURL:  server.URL,
		HTTPClient: server.Client(),
	}

	info, err := u.CheckVersion("linux", "amd64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil VersionInfo")
	}
	if info.Version != "1.2.3" {
		t.Errorf("expected version 1.2.3, got %s", info.Version)
	}
	if info.MD5 != "abc123" {
		t.Errorf("expected MD5 abc123, got %s", info.MD5)
	}
	if info.Size != 1024 {
		t.Errorf("expected size 1024, got %d", info.Size)
	}
	if info.DownloadURL != "/downloads/agent-linux-amd64" {
		t.Errorf("expected download URL /downloads/agent-linux-amd64, got %s", info.DownloadURL)
	}
}

func TestCheckVersion_NoMatch(t *testing.T) {
	// Mock server returns releases but none match the requested os/arch
	versionResp := VersionResponse{
		Version: "1.2.3",
		Releases: []ReleaseItem{
			{OS: "linux", Arch: "amd64", MD5: "abc123", Size: 1024, DownloadURL: "/downloads/agent-linux-amd64"},
			{OS: "darwin", Arch: "arm64", MD5: "def456", Size: 2048, DownloadURL: "/downloads/agent-darwin-arm64"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(versionResp)
	}))
	defer server.Close()

	u := &Updater{
		ServerURL:  server.URL,
		HTTPClient: server.Client(),
	}

	info, err := u.CheckVersion("windows", "arm64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info != nil {
		t.Errorf("expected nil VersionInfo for non-matching os/arch, got %+v", info)
	}
}

func TestCheckVersion_ServerError(t *testing.T) {
	// Mock server returns 500
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	u := &Updater{
		ServerURL:  server.URL,
		HTTPClient: server.Client(),
	}

	info, err := u.CheckVersion("linux", "amd64")
	if err == nil {
		t.Fatal("expected error for server 500 response")
	}
	if info != nil {
		t.Errorf("expected nil VersionInfo on error, got %+v", info)
	}
}

func TestDownload_Success(t *testing.T) {
	// Mock server returns binary content
	binaryContent := []byte("fake-binary-content-for-testing")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/downloads/agent-linux-amd64" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(binaryContent)
	}))
	defer server.Close()

	// Use a temp directory as CurrentPath so the temp file is created there
	tmpDir := t.TempDir()
	currentPath := filepath.Join(tmpDir, "ssl-manager-agent")

	u := &Updater{
		ServerURL:   server.URL,
		CurrentPath: currentPath,
		HTTPClient:  server.Client(),
	}

	tmpPath, err := u.Download("/downloads/agent-linux-amd64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer os.Remove(tmpPath)

	// Verify temp file exists and has correct content
	data, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatalf("failed to read temp file: %v", err)
	}
	if string(data) != string(binaryContent) {
		t.Errorf("expected content %q, got %q", binaryContent, data)
	}

	// Verify temp file is in the same directory as CurrentPath
	expectedDir := filepath.Dir(currentPath)
	actualDir := filepath.Dir(tmpPath)
	if expectedDir != actualDir {
		t.Errorf("expected temp file in %s, got %s", expectedDir, actualDir)
	}
}

func TestDownload_ServerError(t *testing.T) {
	// Mock server returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	currentPath := filepath.Join(tmpDir, "ssl-manager-agent")

	u := &Updater{
		ServerURL:   server.URL,
		CurrentPath: currentPath,
		HTTPClient:  server.Client(),
	}

	tmpPath, err := u.Download("/downloads/agent-linux-amd64")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	if tmpPath != "" {
		t.Errorf("expected empty path on error, got %s", tmpPath)
	}
}

func TestExecute_AlreadyUpToDate(t *testing.T) {
	// Mock server returns same version as current
	versionResp := VersionResponse{
		Version: "1.0.0",
		Releases: []ReleaseItem{
			{OS: "linux", Arch: "amd64", MD5: "abc123", Size: 1024, DownloadURL: "/downloads/agent-linux-amd64"},
		},
	}

	downloadCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/agent/version" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(versionResp)
			return
		}
		// If download endpoint is hit, record it
		downloadCalled = true
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("binary"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	currentPath := filepath.Join(tmpDir, "ssl-manager-agent")

	u := &Updater{
		ServerURL:   server.URL,
		CurrentPath: currentPath,
		HTTPClient:  server.Client(),
	}

	mock := &mockServiceManager{}
	err := u.Execute("1.0.0", "linux", "amd64", mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if downloadCalled {
		t.Error("download should not be called when already up to date")
	}
	if mock.restartCalled {
		t.Error("restart should not be called when already up to date")
	}
}

func TestExecute_UpdateSuccess(t *testing.T) {
	// Prepare binary content and compute its MD5
	binaryContent := []byte("new-agent-binary-v2.0.0")
	hash := md5.Sum(binaryContent)
	expectedMD5 := hex.EncodeToString(hash[:])

	versionResp := VersionResponse{
		Version: "2.0.0",
		Releases: []ReleaseItem{
			{OS: "linux", Arch: "amd64", MD5: expectedMD5, Size: int64(len(binaryContent)), DownloadURL: "/downloads/agent-linux-amd64"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/agent/version":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(versionResp)
		case "/downloads/agent-linux-amd64":
			w.WriteHeader(http.StatusOK)
			w.Write(binaryContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Create a fake current binary that will be replaced
	tmpDir := t.TempDir()
	currentPath := filepath.Join(tmpDir, "ssl-manager-agent")
	if err := os.WriteFile(currentPath, []byte("old-binary-v1.0.0"), 0755); err != nil {
		t.Fatalf("failed to create fake binary: %v", err)
	}

	u := &Updater{
		ServerURL:   server.URL,
		CurrentPath: currentPath,
		HTTPClient:  server.Client(),
	}

	mock := &mockServiceManager{}
	err := u.Execute("1.0.0", "linux", "amd64", mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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
	if !mock.restartCalled {
		t.Error("expected Restart() to be called after successful update")
	}
}

func TestExecute_UpdateSuccess_RestartError(t *testing.T) {
	// Verify that restart error is propagated
	binaryContent := []byte("new-agent-binary-v2.0.0")
	hash := md5.Sum(binaryContent)
	expectedMD5 := hex.EncodeToString(hash[:])

	versionResp := VersionResponse{
		Version: "2.0.0",
		Releases: []ReleaseItem{
			{OS: "linux", Arch: "amd64", MD5: expectedMD5, Size: int64(len(binaryContent)), DownloadURL: "/downloads/agent-linux-amd64"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/agent/version":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(versionResp)
		case "/downloads/agent-linux-amd64":
			w.WriteHeader(http.StatusOK)
			w.Write(binaryContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	currentPath := filepath.Join(tmpDir, "ssl-manager-agent")
	if err := os.WriteFile(currentPath, []byte("old-binary-v1.0.0"), 0755); err != nil {
		t.Fatalf("failed to create fake binary: %v", err)
	}

	u := &Updater{
		ServerURL:   server.URL,
		CurrentPath: currentPath,
		HTTPClient:  server.Client(),
	}

	mock := &mockServiceManager{restartErr: fmt.Errorf("restart failed")}
	err := u.Execute("1.0.0", "linux", "amd64", mock)
	if err == nil {
		t.Fatal("expected error when restart fails")
	}
	if !mock.restartCalled {
		t.Error("expected Restart() to be called")
	}
}
