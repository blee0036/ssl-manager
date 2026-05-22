package service

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestVersionCache_ScanWithVersionFile creates a temp dir with agent-version.txt
// and fake binaries, then verifies Scan populates the cache correctly.
func TestVersionCache_ScanWithVersionFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Write agent-version.txt
	versionContent := "1.2.3"
	err := os.WriteFile(filepath.Join(tmpDir, "agent-version.txt"), []byte(versionContent), 0644)
	if err != nil {
		t.Fatalf("failed to write agent-version.txt: %v", err)
	}

	// Create fake binaries for supported platforms
	fakeBinaries := map[string][]byte{
		"ssl-manager-agent-linux-amd64":  []byte("fake-linux-amd64-binary-content"),
		"ssl-manager-agent-linux-arm64":  []byte("fake-linux-arm64-binary-content"),
		"ssl-manager-agent-darwin-amd64": []byte("fake-darwin-amd64-binary-content"),
		"ssl-manager-agent-darwin-arm64": []byte("fake-darwin-arm64-binary-content"),
	}

	for name, content := range fakeBinaries {
		err := os.WriteFile(filepath.Join(tmpDir, name), content, 0755)
		if err != nil {
			t.Fatalf("failed to write fake binary %s: %v", name, err)
		}
	}

	// Use a very long scan interval to prevent background goroutine from interfering
	vc := NewVersionCache(tmpDir, 1*time.Hour)
	t.Cleanup(func() { vc.Stop() })

	// Verify version
	if got := vc.GetVersion(); got != "1.2.3" {
		t.Errorf("GetVersion() = %q, want %q", got, "1.2.3")
	}

	// Verify releases
	releases := vc.GetReleases()
	if len(releases) != 4 {
		t.Fatalf("GetReleases() returned %d releases, want 4", len(releases))
	}

	// Verify each release has correct metadata
	for _, r := range releases {
		binaryName := "ssl-manager-agent-" + r.OS + "-" + r.Arch
		expectedContent := fakeBinaries[binaryName]
		if expectedContent == nil {
			t.Errorf("unexpected release for %s-%s", r.OS, r.Arch)
			continue
		}

		// Verify MD5
		h := md5.Sum(expectedContent)
		expectedMD5 := hex.EncodeToString(h[:])
		if r.MD5 != expectedMD5 {
			t.Errorf("release %s-%s MD5 = %q, want %q", r.OS, r.Arch, r.MD5, expectedMD5)
		}

		// Verify Size
		expectedSize := int64(len(expectedContent))
		if r.Size != expectedSize {
			t.Errorf("release %s-%s Size = %d, want %d", r.OS, r.Arch, r.Size, expectedSize)
		}

		// Verify DownloadURL format
		expectedURL := "/api/agent/binary?os=" + r.OS + "&arch=" + r.Arch
		if r.DownloadURL != expectedURL {
			t.Errorf("release %s-%s DownloadURL = %q, want %q", r.OS, r.Arch, r.DownloadURL, expectedURL)
		}

		// Verify FilePath
		expectedPath := filepath.Join(tmpDir, binaryName)
		if r.FilePath != expectedPath {
			t.Errorf("release %s-%s FilePath = %q, want %q", r.OS, r.Arch, r.FilePath, expectedPath)
		}
	}
}

// TestVersionCache_ScanWithoutVersionFile verifies behavior when agent-version.txt
// does not exist: version should be empty and releases should be empty.
func TestVersionCache_ScanWithoutVersionFile(t *testing.T) {
	tmpDir := t.TempDir()

	// No agent-version.txt, no binaries
	vc := NewVersionCache(tmpDir, 1*time.Hour)
	t.Cleanup(func() { vc.Stop() })

	// Version should be empty
	if got := vc.GetVersion(); got != "" {
		t.Errorf("GetVersion() = %q, want empty string", got)
	}

	// Releases should be empty
	releases := vc.GetReleases()
	if len(releases) != 0 {
		t.Errorf("GetReleases() returned %d releases, want 0", len(releases))
	}
}

// TestVersionCache_GetRelease_Found verifies GetRelease returns the correct
// release for a matching os/arch combination.
func TestVersionCache_GetRelease_Found(t *testing.T) {
	tmpDir := t.TempDir()

	// Write version file
	err := os.WriteFile(filepath.Join(tmpDir, "agent-version.txt"), []byte("2.0.0"), 0644)
	if err != nil {
		t.Fatalf("failed to write agent-version.txt: %v", err)
	}

	// Create only one binary
	content := []byte("linux-amd64-binary-data-here")
	err = os.WriteFile(filepath.Join(tmpDir, "ssl-manager-agent-linux-amd64"), content, 0755)
	if err != nil {
		t.Fatalf("failed to write fake binary: %v", err)
	}

	vc := NewVersionCache(tmpDir, 1*time.Hour)
	t.Cleanup(func() { vc.Stop() })

	// Should find linux/amd64
	release, found := vc.GetRelease("linux", "amd64")
	if !found {
		t.Fatal("GetRelease(linux, amd64) returned found=false, want true")
	}
	if release == nil {
		t.Fatal("GetRelease(linux, amd64) returned nil release")
	}
	if release.OS != "linux" || release.Arch != "amd64" {
		t.Errorf("GetRelease returned OS=%q Arch=%q, want linux/amd64", release.OS, release.Arch)
	}

	// Verify MD5
	h := md5.Sum(content)
	expectedMD5 := hex.EncodeToString(h[:])
	if release.MD5 != expectedMD5 {
		t.Errorf("release MD5 = %q, want %q", release.MD5, expectedMD5)
	}

	// Verify Size
	if release.Size != int64(len(content)) {
		t.Errorf("release Size = %d, want %d", release.Size, int64(len(content)))
	}
}

// TestVersionCache_GetRelease_NotFound verifies GetRelease returns nil/false
// for a non-matching os/arch combination.
func TestVersionCache_GetRelease_NotFound(t *testing.T) {
	tmpDir := t.TempDir()

	// Write version file
	err := os.WriteFile(filepath.Join(tmpDir, "agent-version.txt"), []byte("1.0.0"), 0644)
	if err != nil {
		t.Fatalf("failed to write agent-version.txt: %v", err)
	}

	// Create only linux-amd64 binary
	err = os.WriteFile(filepath.Join(tmpDir, "ssl-manager-agent-linux-amd64"), []byte("data"), 0755)
	if err != nil {
		t.Fatalf("failed to write fake binary: %v", err)
	}

	vc := NewVersionCache(tmpDir, 1*time.Hour)
	t.Cleanup(func() { vc.Stop() })

	// Should NOT find windows/amd64
	release, found := vc.GetRelease("windows", "amd64")
	if found {
		t.Error("GetRelease(windows, amd64) returned found=true, want false")
	}
	if release != nil {
		t.Error("GetRelease(windows, amd64) returned non-nil release, want nil")
	}

	// Should NOT find linux/arm64 (not created)
	release, found = vc.GetRelease("linux", "arm64")
	if found {
		t.Error("GetRelease(linux, arm64) returned found=true, want false")
	}
	if release != nil {
		t.Error("GetRelease(linux, arm64) returned non-nil release, want nil")
	}
}

// TestVersionCache_ConcurrentAccess verifies that multiple goroutines can safely
// read from the cache while scanning is happening, with no race conditions.
// Run with -race flag to detect data races.
func TestVersionCache_ConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()

	// Write version file
	err := os.WriteFile(filepath.Join(tmpDir, "agent-version.txt"), []byte("3.0.0"), 0644)
	if err != nil {
		t.Fatalf("failed to write agent-version.txt: %v", err)
	}

	// Create fake binaries
	for _, name := range []string{
		"ssl-manager-agent-linux-amd64",
		"ssl-manager-agent-darwin-arm64",
	} {
		err := os.WriteFile(filepath.Join(tmpDir, name), []byte("concurrent-test-data-"+name), 0755)
		if err != nil {
			t.Fatalf("failed to write fake binary %s: %v", name, err)
		}
	}

	vc := NewVersionCache(tmpDir, 1*time.Hour)
	t.Cleanup(func() { vc.Stop() })

	const numReaders = 10
	const numIterations = 100

	var wg sync.WaitGroup

	// Start multiple reader goroutines
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				_ = vc.GetVersion()
				_ = vc.GetReleases()
				_, _ = vc.GetRelease("linux", "amd64")
				_, _ = vc.GetRelease("darwin", "arm64")
				_, _ = vc.GetRelease("windows", "386") // non-existent
			}
		}()
	}

	// Concurrently trigger scans
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = vc.Scan()
			}
		}()
	}

	wg.Wait()

	// After all concurrent operations, verify cache is still consistent
	version := vc.GetVersion()
	if version != "3.0.0" {
		t.Errorf("after concurrent access, GetVersion() = %q, want %q", version, "3.0.0")
	}

	releases := vc.GetReleases()
	if len(releases) != 2 {
		t.Errorf("after concurrent access, GetReleases() returned %d releases, want 2", len(releases))
	}
}
