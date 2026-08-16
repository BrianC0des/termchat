package system

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const AppVersion = "v1.2.0"

type progressWriter struct {
	total      int64
	current    int64
	onProgress func(string)
	lastReport time.Time
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n := len(p)
	pw.current += int64(n)
	if time.Since(pw.lastReport) > 500*time.Millisecond || pw.current == pw.total {
		pw.lastReport = time.Now()
		if pw.onProgress != nil && pw.total > 0 {
			pct := int((float64(pw.current) / float64(pw.total)) * 100)
			pw.onProgress(fmt.Sprintf("[NET] Downloading update: %d%% (%.1f / %.1f MB)...", pct, float64(pw.current)/(1024*1024), float64(pw.total)/(1024*1024)))
		}
	}
	return n, nil
}

func getPlatformBinaryName() string {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	// Detect Android / Termux
	if os.Getenv("PREFIX") != "" && strings.Contains(os.Getenv("PREFIX"), "com.termux") {
		if goarch == "arm64" {
			return "termchat-android-arm64"
		}
		return "termchat-android-arm"
	}

	switch goos {
	case "windows":
		if goarch == "arm64" {
			return "termchat-windows-arm64.exe"
		}
		return "termchat-windows.exe"
	case "darwin":
		if goarch == "arm64" {
			return "termchat-mac-apple-silicon"
		}
		return "termchat-mac-intel"
	case "linux":
		if goarch == "arm64" {
			return "termchat-linux-arm64"
		}
		return "termchat-linux-amd64"
	case "android":
		if goarch == "arm64" {
			return "termchat-android-arm64"
		}
		return "termchat-android-arm"
	default:
		return "termchat-linux-amd64"
	}
}

type ReleaseInfo struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Body    string `json:"body"`
}

func FetchLatestVersionTag() (string, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/repos/BrianC0des/termchat/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "TermChat-Updater/1.1")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var rel ReleaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	return rel.TagName, nil
}

func UpdateSelfWithProgress(onProgress func(msg string)) (string, error) {
	if onProgress != nil {
		onProgress("⚡ Checking for updates from GitHub...")
	}

	latestTag, err := FetchLatestVersionTag()
	if err == nil && latestTag != "" {
		if strings.EqualFold(latestTag, AppVersion) || latestTag <= AppVersion {
			return fmt.Sprintf("✨ You are already on the latest version of TermChat (%s)!", AppVersion), nil
		}
		if onProgress != nil {
			onProgress(fmt.Sprintf("⚡ Found new version: %s (Current: %s)", latestTag, AppVersion))
		}
	} else {
		latestTag = AppVersion
	}

	binaryName := getPlatformBinaryName()
	downloadURL := fmt.Sprintf("https://github.com/BrianC0des/termchat/releases/latest/download/%s", binaryName)

	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("could not determine executable path: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return "", fmt.Errorf("could not resolve symlinks: %w", err)
	}

	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("request creation error: %w", err)
	}
	req.Header.Set("User-Agent", "TermChat-Updater/1.1")

	// No hard timeout during body streaming
	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download update (HTTP %d)", resp.StatusCode)
	}

	totalSize := resp.ContentLength
	if onProgress != nil {
		if totalSize > 0 {
			onProgress(fmt.Sprintf("⚡ Downloading TermChat update (%.1f MB)...", float64(totalSize)/(1024*1024)))
		} else {
			onProgress("⚡ Downloading TermChat update...")
		}
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(execPath), "termchat-update-*")
	if err != nil {
		tmpFile, err = os.CreateTemp("", "termchat-update-*")
		if err != nil {
			return "", fmt.Errorf("could not create temp file: %w", err)
		}
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	pw := &progressWriter{
		total:      totalSize,
		onProgress: onProgress,
	}
	destWriter := io.MultiWriter(tmpFile, pw)
	_, err = io.Copy(destWriter, resp.Body)
	_ = tmpFile.Close()
	if err != nil {
		return "", fmt.Errorf("error writing download: %w", err)
	}

	_ = os.Chmod(tmpPath, 0755)

	// On Windows, rename old file first then replace
	if runtime.GOOS == "windows" {
		oldPath := execPath + ".old"
		_ = os.Remove(oldPath)
		_ = os.Rename(execPath, oldPath)
	}

	err = os.Rename(tmpPath, execPath)
	if err != nil {
		// Fallback: Copy content directly if rename fails across filesystems
		src, rErr := os.Open(tmpPath)
		if rErr == nil {
			dst, wErr := os.OpenFile(execPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if wErr == nil {
				_, err = io.Copy(dst, src)
				_ = dst.Close()
			}
			_ = src.Close()
		}
	}

	if err != nil {
		return "", fmt.Errorf("failed to replace binary at %s: %w", execPath, err)
	}

	return fmt.Sprintf("✅ Successfully updated TermChat to %s!\n💡 Please restart termchat to apply.", latestTag), nil
}

func UpdateSelf() (string, error) {
	return UpdateSelfWithProgress(nil)
}
