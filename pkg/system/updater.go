package system

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

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

func UpdateSelf() (string, error) {
	binaryName := getPlatformBinaryName()
	downloadURL := fmt.Sprintf("https://github.com/BrianC0des/termchat/releases/download/v1.0.0/%s", binaryName)

	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("could not determine executable path: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return "", fmt.Errorf("could not resolve symlinks: %w", err)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return "", fmt.Errorf("download error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download update (HTTP %d)", resp.StatusCode)
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

	_, err = io.Copy(tmpFile, resp.Body)
	_ = tmpFile.Close()
	if err != nil {
		return "", fmt.Errorf("failed to write update: %w", err)
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

	return execPath, nil
}
