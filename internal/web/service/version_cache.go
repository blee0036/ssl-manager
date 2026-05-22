package service

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ReleaseInfo holds release metadata for a single platform binary.
type ReleaseInfo struct {
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	MD5         string `json:"md5"`
	Size        int64  `json:"size"`
	DownloadURL string `json:"download_url"`
	FilePath    string `json:"-"` // internal use, not serialized
}

// VersionCache manages an in-memory cache of Agent binary version information.
// It periodically scans the binary directory to detect new releases and compute
// MD5 checksums, ensuring consistency between version queries and downloads.
type VersionCache struct {
	mu       sync.RWMutex
	version  string
	releases []ReleaseInfo
	binDir   string
	ticker   *time.Ticker
	done     chan struct{}
}

// supportedPlatforms defines the OS/Arch combinations to scan for.
var supportedPlatforms = []struct {
	OS   string
	Arch string
}{
	{"linux", "amd64"},
	{"linux", "arm64"},
	{"darwin", "amd64"},
	{"darwin", "arm64"},
}

// NewVersionCache creates a new VersionCache that scans binDir for agent binaries
// at the given scanInterval. It performs an initial scan immediately and starts
// a background goroutine for periodic rescanning.
func NewVersionCache(binDir string, scanInterval time.Duration) *VersionCache {
	vc := &VersionCache{
		binDir: binDir,
		ticker: time.NewTicker(scanInterval),
		done:   make(chan struct{}),
	}

	// Perform initial scan
	_ = vc.Scan()

	// Start periodic scanning
	go vc.scanLoop()

	return vc
}

// scanLoop runs periodic scans until Stop is called.
func (vc *VersionCache) scanLoop() {
	for {
		select {
		case <-vc.ticker.C:
			_ = vc.Scan()
		case <-vc.done:
			return
		}
	}
}

// Scan reads agent-version.txt and traverses binary files in binDir,
// computing MD5 checksums and file sizes for each supported platform.
// It atomically updates the cache under a write lock.
func (vc *VersionCache) Scan() error {
	// Read version from agent-version.txt
	version := ""
	versionFile := filepath.Join(vc.binDir, "agent-version.txt")
	data, err := os.ReadFile(versionFile)
	if err == nil {
		version = strings.TrimSpace(string(data))
	}

	// Scan binary files for each supported platform
	var releases []ReleaseInfo
	for _, p := range supportedPlatforms {
		fileName := fmt.Sprintf("ssl-manager-agent-%s-%s", p.OS, p.Arch)
		filePath := filepath.Join(vc.binDir, fileName)

		info, err := os.Stat(filePath)
		if err != nil {
			continue // file doesn't exist, skip
		}

		md5Hash, err := computeFileMD5(filePath)
		if err != nil {
			continue // skip files we can't read
		}

		releases = append(releases, ReleaseInfo{
			OS:          p.OS,
			Arch:        p.Arch,
			MD5:         md5Hash,
			Size:        info.Size(),
			DownloadURL: fmt.Sprintf("/api/agent/binary?os=%s&arch=%s", p.OS, p.Arch),
			FilePath:    filePath,
		})
	}

	// Atomically update the cache
	vc.mu.Lock()
	vc.version = version
	vc.releases = releases
	vc.mu.Unlock()

	return nil
}

// GetVersion returns the current cached version string.
func (vc *VersionCache) GetVersion() string {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	return vc.version
}

// GetReleases returns a copy of all cached release information.
func (vc *VersionCache) GetReleases() []ReleaseInfo {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	result := make([]ReleaseInfo, len(vc.releases))
	copy(result, vc.releases)
	return result
}

// GetRelease returns the release information for the specified OS and architecture.
// Returns nil and false if no matching release is found.
func (vc *VersionCache) GetRelease(os, arch string) (*ReleaseInfo, bool) {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	for _, r := range vc.releases {
		if r.OS == os && r.Arch == arch {
			release := r
			return &release, true
		}
	}
	return nil, false
}

// Stop stops the periodic scanning goroutine and releases resources.
func (vc *VersionCache) Stop() {
	vc.ticker.Stop()
	close(vc.done)
}

// computeFileMD5 calculates the MD5 hash of the file at the given path.
func computeFileMD5(filePath string) (string, error) {
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
