package updater

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/ssl-manager/ssl-manager/internal/agent/platform"
	"github.com/ssl-manager/ssl-manager/internal/agent/version"
)

// Updater 负责下载、校验、替换二进制
type Updater struct {
	ServerURL   string       // 服务器基础 URL（如 https://ssl-manager.example.com）
	CurrentPath string       // 当前二进制路径（通常 /usr/local/bin/ssl-manager-agent）
	HTTPClient  *http.Client // HTTP 客户端
}

// VersionInfo 从服务端获取的版本信息（扁平结构，已按 OS/Arch 筛选）
type VersionInfo struct {
	Version     string `json:"version"`
	MD5         string `json:"md5"`
	Size        int64  `json:"size"`
	DownloadURL string `json:"download_url"`
}

// VersionResponse GET /api/agent/version 的完整响应
type VersionResponse struct {
	Version  string        `json:"version"`
	Releases []ReleaseItem `json:"releases"`
}

// ReleaseItem 单个平台的发布信息
type ReleaseItem struct {
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	MD5         string `json:"md5"`
	Size        int64  `json:"size"`
	DownloadURL string `json:"download_url"`
}

// CheckVersion 查询最新版本，解析 releases 列表并按 os/arch 筛选匹配项。
// 请求: GET <ServerURL>/api/agent/version
// 返回匹配当前平台的 VersionInfo，如果没有匹配项返回 nil。
func (u *Updater) CheckVersion(osName, arch string) (*VersionInfo, error) {
	url := u.ServerURL + "/api/agent/version"

	resp, err := u.HTTPClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to query version: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("version endpoint returned status %d", resp.StatusCode)
	}

	var versionResp VersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&versionResp); err != nil {
		return nil, fmt.Errorf("failed to decode version response: %w", err)
	}

	// 遍历 releases，找到 os/arch 匹配项
	for _, release := range versionResp.Releases {
		if release.OS == osName && release.Arch == arch {
			return &VersionInfo{
				Version:     versionResp.Version,
				MD5:         release.MD5,
				Size:        release.Size,
				DownloadURL: release.DownloadURL,
			}, nil
		}
	}

	return nil, nil
}

// Download 下载二进制到与目标同目录的临时文件，返回临时文件路径。
// downloadURL 是相对路径，会拼接 ServerURL 形成完整 URL: ServerURL + downloadURL
func (u *Updater) Download(downloadURL string) (string, error) {
	fullURL := u.ServerURL + downloadURL

	resp, err := u.HTTPClient.Get(fullURL)
	if err != nil {
		return "", fmt.Errorf("failed to download binary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download endpoint returned status %d", resp.StatusCode)
	}

	tmpPath := filepath.Join(filepath.Dir(u.CurrentPath), "ssl-manager-agent.download.tmp")

	f, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file %s: %w", tmpPath, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to write download to temp file: %w", err)
	}

	return tmpPath, nil
}

// Execute 执行完整更新流程: check → compare → download → verify → replace → restart
func (u *Updater) Execute(currentVersion, osName, arch string, svcMgr platform.ServiceManager) error {
	// 1. 查询最新版本
	info, err := u.CheckVersion(osName, arch)
	if err != nil {
		return fmt.Errorf("check version failed: %w", err)
	}
	if info == nil {
		return fmt.Errorf("no release found for %s/%s", osName, arch)
	}

	// 2. 比较版本
	newer, err := version.IsNewer(currentVersion, info.Version)
	if err != nil {
		return fmt.Errorf("version comparison failed: %w", err)
	}
	if !newer {
		return nil // 已是最新版本，无需更新
	}

	// 3. 下载新二进制
	tmpPath, err := u.Download(info.DownloadURL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// 4. 校验 MD5
	if err := VerifyMD5(tmpPath, info.MD5); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("MD5 verification failed: %w", err)
	}

	// 5. 原子替换
	if err := AtomicReplace(u.CurrentPath, tmpPath); err != nil {
		return fmt.Errorf("atomic replace failed: %w", err)
	}

	// 6. 重启服务
	if err := svcMgr.Restart(); err != nil {
		return fmt.Errorf("service restart failed: %w", err)
	}

	return nil
}
