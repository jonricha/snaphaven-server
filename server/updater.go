package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

const (
	DefaultGitHubRepoOwner = "jonricha"
	DefaultGitHubRepoName  = "snaphaven-server"
	DefaultCheckInterval   = 24 * time.Hour
)

type UpdateState string

const (
	StateIdle            UpdateState = "idle"
	StateChecking        UpdateState = "checking"
	StateUpdateAvailable UpdateState = "update_available"
	StateNoUpdate        UpdateState = "no_update"
	StateDownloading     UpdateState = "downloading"
	StateReadyToInstall  UpdateState = "ready_to_install"
	StateError           UpdateState = "error"
)

type UpdateInfo struct {
	Version      string `json:"version"`
	ReleaseNotes string `json:"release_notes"`
	AssetURL     string `json:"asset_url"`
	AssetName    string `json:"asset_name"`
	PublishedAt  string `json:"published_at"`
	HTMLURL      string `json:"html_url"`
}

type UpdateStatus struct {
	State        UpdateState `json:"state"`
	CurrentVer   string      `json:"current_version"`
	LatestVer    string      `json:"latest_version"`
	UpdateInfo   *UpdateInfo `json:"update_info,omitempty"`
	Progress     int         `json:"progress"` // 0-100%
	ErrorMessage string      `json:"error_message,omitempty"`
	DownloadedAt string      `json:"downloaded_at,omitempty"`
	InstallerPath string     `json:"installer_path,omitempty"`
}

type gitHubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type gitHubReleaseResponse struct {
	TagName     string               `json:"tag_name"`
	Name        string               `json:"name"`
	Body        string               `json:"body"`
	PublishedAt string               `json:"published_at"`
	HTMLURL     string               `json:"html_url"`
	Assets      []gitHubReleaseAsset `json:"assets"`
}

type UpdateManager struct {
	mu             sync.Mutex
	status         UpdateStatus
	repoOwner      string
	repoName       string
	client         *http.Client
	downloadPath   string
	onUpdateFound  func(info *UpdateInfo)
	stopTickerChan chan struct{}
}

func NewUpdateManager(owner, repo string) *UpdateManager {
	if owner == "" {
		owner = DefaultGitHubRepoOwner
	}
	if repo == "" {
		repo = DefaultGitHubRepoName
	}

	return &UpdateManager{
		repoOwner: owner,
		repoName:  repo,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		status: UpdateStatus{
			State:      StateIdle,
			CurrentVer: GetVersion(),
		},
		stopTickerChan: make(chan struct{}),
	}
}

func (um *UpdateManager) SetUpdateCallback(cb func(info *UpdateInfo)) {
	um.mu.Lock()
	defer um.mu.Unlock()
	um.onUpdateFound = cb
}

func (um *UpdateManager) GetStatus() UpdateStatus {
	um.mu.Lock()
	defer um.mu.Unlock()
	um.status.CurrentVer = GetVersion()
	return um.status
}

func (um *UpdateManager) CheckForUpdates() (*UpdateInfo, bool, error) {
	um.mu.Lock()
	um.status.State = StateChecking
	um.status.ErrorMessage = ""
	um.mu.Unlock()

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", um.repoOwner, um.repoName)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		um.setError(fmt.Sprintf("Failed to create request: %v", err))
		return nil, false, err
	}

	req.Header.Set("User-Agent", "SnapHaven-Server-Updater")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := um.client.Do(req)
	if err != nil {
		um.setError(fmt.Sprintf("Failed to fetch release info: %v", err))
		return nil, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		um.mu.Lock()
		um.status.State = StateNoUpdate
		um.status.LatestVer = GetVersion()
		um.mu.Unlock()
		return nil, false, nil
	}

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
		um.setError(err.Error())
		return nil, false, err
	}

	var release gitHubReleaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		um.setError(fmt.Sprintf("Failed to decode release JSON: %v", err))
		return nil, false, err
	}

	latestTag := release.TagName
	currentVer := GetVersion()

	// Find suitable release asset (.exe)
	var assetURL string
	var assetName string
	for _, asset := range release.Assets {
		if filepath.Ext(asset.Name) == ".exe" {
			assetURL = asset.BrowserDownloadURL
			assetName = asset.Name
			break
		}
	}

	// Fallback to first asset if no .exe specifically named
	if assetURL == "" && len(release.Assets) > 0 {
		assetURL = release.Assets[0].BrowserDownloadURL
		assetName = release.Assets[0].Name
	}

	info := &UpdateInfo{
		Version:      latestTag,
		ReleaseNotes: release.Body,
		AssetURL:     assetURL,
		AssetName:    assetName,
		PublishedAt:  release.PublishedAt,
		HTMLURL:      release.HTMLURL,
	}

	hasUpdate := isNewerVersion(latestTag, currentVer)

	um.mu.Lock()
	um.status.LatestVer = latestTag
	um.status.UpdateInfo = info
	if hasUpdate {
		um.status.State = StateUpdateAvailable
		LogEvent(fmt.Sprintf("🎉 New update available: %s (current: %s)", latestTag, currentVer))
		if um.onUpdateFound != nil {
			go um.onUpdateFound(info)
		}
	} else {
		um.status.State = StateNoUpdate
		LogEvent(fmt.Sprintf("✅ Server is up to date (%s)", currentVer))
	}
	um.mu.Unlock()

	return info, hasUpdate, nil
}

func (um *UpdateManager) StartAutoCheckTicker(interval time.Duration) {
	if interval <= 0 {
		interval = DefaultCheckInterval
	}

	// Initial background check on startup
	go func() {
		time.Sleep(10 * time.Second)
		um.CheckForUpdates()
	}()

	ticker := time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-ticker.C:
				um.CheckForUpdates()
			case <-um.stopTickerChan:
				ticker.Stop()
				return
			}
		}
	}()
}

func (um *UpdateManager) DownloadUpdate() error {
	um.mu.Lock()
	info := um.status.UpdateInfo
	if info == nil || info.AssetURL == "" {
		um.mu.Unlock()
		err := fmt.Errorf("no valid update asset available for download")
		um.setError(err.Error())
		return err
	}
	um.status.State = StateDownloading
	um.status.Progress = 0
	um.status.ErrorMessage = ""
	um.mu.Unlock()

	LogEvent(fmt.Sprintf("📥 Starting download of update %s from %s", info.Version, info.AssetURL))

	req, err := http.NewRequest("GET", info.AssetURL, nil)
	if err != nil {
		um.setError(fmt.Sprintf("Failed to create download request: %v", err))
		return err
	}
	req.Header.Set("User-Agent", "SnapHaven-Server-Updater")

	resp, err := um.client.Do(req)
	if err != nil {
		um.setError(fmt.Sprintf("Download failed: %v", err))
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("download server returned HTTP %d", resp.StatusCode)
		um.setError(err.Error())
		return err
	}

	tempDir := os.TempDir()
	fileName := info.AssetName
	if fileName == "" {
		fileName = "SnapHavenInstaller.exe"
	}
	targetPath := filepath.Join(tempDir, fmt.Sprintf("snaphaven_update_%s_%s", info.Version, fileName))

	out, err := os.Create(targetPath)
	if err != nil {
		um.setError(fmt.Sprintf("Failed to create installer target file: %v", err))
		return err
	}
	defer out.Close()

	contentLength := resp.ContentLength
	var downloaded int64
	buf := make([]byte, 32*1024)

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			_, wErr := out.Write(buf[:n])
			if wErr != nil {
				um.setError(fmt.Sprintf("Error writing download content: %v", wErr))
				return wErr
			}
			downloaded += int64(n)
			if contentLength > 0 {
				progress := int((float64(downloaded) / float64(contentLength)) * 100)
				um.mu.Lock()
				um.status.Progress = progress
				um.mu.Unlock()
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			um.setError(fmt.Sprintf("Error during file download: %v", err))
			return err
		}
	}

	um.mu.Lock()
	um.downloadPath = targetPath
	um.status.InstallerPath = targetPath
	um.status.State = StateReadyToInstall
	um.status.Progress = 100
	um.status.DownloadedAt = time.Now().Format(time.RFC3339)
	um.mu.Unlock()

	LogEvent(fmt.Sprintf("✅ Update downloaded successfully to %s", targetPath))
	return nil
}

func (um *UpdateManager) ApplyUpdate() error {
	um.mu.Lock()
	installerPath := um.downloadPath
	state := um.status.State
	um.mu.Unlock()

	if state != StateReadyToInstall || installerPath == "" {
		err := fmt.Errorf("installer is not ready to be applied")
		um.setError(err.Error())
		return err
	}

	if _, err := os.Stat(installerPath); os.IsNotExist(err) {
		err := fmt.Errorf("installer file not found at %s", installerPath)
		um.setError(err.Error())
		return err
	}

	LogEvent(fmt.Sprintf("🚀 Executing updater installer: %s /S", installerPath))

	// Launch installer silently using Windows cmd /c start or exec.Command
	// Note: NSIS installer accepts /S for silent install
	cmd := exec.Command(installerPath, "/S")
	err := cmd.Start()
	if err != nil {
		um.setError(fmt.Sprintf("Failed to launch installer: %v", err))
		return err
	}

	LogEvent("🔄 Update installer launched. Server will restart shortly...")

	// Exit current process after 1 second so installer can replace binary and launch new instance cleanly
	go func() {
		time.Sleep(1 * time.Second)
		os.Exit(0)
	}()

	return nil
}

func (um *UpdateManager) setError(msg string) {
	um.mu.Lock()
	defer um.mu.Unlock()
	um.status.State = StateError
	um.status.ErrorMessage = msg
	LogEvent(fmt.Sprintf("⚠️ Updater error: %s", msg))
}
