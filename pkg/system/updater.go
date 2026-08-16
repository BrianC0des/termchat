package system

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
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

const AppVersion = "v1.3.0"

type progressWriter struct {
	total      int64
	current    int64
	onProgress func(string)
	lastReport time.Time
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n := len(p)
	pw.current += int64(n)
	if time.Since(pw.lastReport) > 400*time.Millisecond || pw.current == pw.total {
		pw.lastReport = time.Now()
		if pw.onProgress != nil && pw.total > 0 {
			pct := int((float64(pw.current) / float64(pw.total)) * 100)
			pw.onProgress(fmt.Sprintf("[NET] Downloading update: %d%% (%.1f / %.1f MB)...", pct, float64(pw.current)/(1024*1024), float64(pw.total)/(1024*1024)))
		}
	}
	return n, nil
}

func extractTarGz(gzData []byte, destFile *os.File) error {
	gzr, err := gzip.NewReader(bytes.NewReader(gzData))
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if header.Typeflag == tar.TypeReg {
			_, err := io.Copy(destFile, tr)
			return err
		}
	}
	return fmt.Errorf("no executable binary found in archive")
}

func extractZip(zipData []byte, destFile *os.File) error {
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		if !f.FileInfo().IsDir() {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			_, err = io.Copy(destFile, rc)
			_ = rc.Close()
			return err
		}
	}
	return fmt.Errorf("no executable binary found in zip")
}

func getPlatformBinaryName() string {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	// Termux detection
	if os.Getenv("PREFIX") != "" {
		if goarch == "arm64" {
			return "termchat-android-arm64"
		}
		return "termchat-android-arm"
	}

	switch goos {
	case "linux":
		if goarch == "arm64" {
			return "termchat-linux-arm64"
		}
		return "termchat-linux-amd64"
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
	case "android":
		if goarch == "arm64" {
			return "termchat-android-arm64"
		}
		return "termchat-android-arm"
	default:
		return "termchat-linux-amd64"
	}
}

func FetchLatestVersionTag() (string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", "https://api.github.com/repos/BrianC0des/termchat/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "TermChat-Updater/1.3")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var data struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	return data.TagName, nil
}

func getStagedBinaryPath() string {
	return filepath.Join(os.TempDir(), "termchat-staged-binary.tmp")
}

func getStagedTagPath() string {
	return filepath.Join(os.TempDir(), "termchat-staged-tag.txt")
}

func CheckAndPreFetchUpdateAsync(onNotice func(string)) {
	go func() {
		latestTag, err := FetchLatestVersionTag()
		if err != nil || latestTag == "" || latestTag <= AppVersion {
			return
		}

		stagedTag, _ := os.ReadFile(getStagedTagPath())
		if strings.TrimSpace(string(stagedTag)) == latestTag {
			if info, err := os.Stat(getStagedBinaryPath()); err == nil && info.Size() > 1000000 {
				if onNotice != nil {
					onNotice(fmt.Sprintf("[NET] New update %s is pre-downloaded & ready! Type `/update` to apply instantly.", latestTag))
				}
				return
			}
		}

		binaryName := getPlatformBinaryName()
		ext := ".tar.gz"
		if runtime.GOOS == "windows" {
			ext = ".zip"
		}
		downloadURL := fmt.Sprintf("https://github.com/BrianC0des/termchat/releases/latest/download/%s%s", binaryName, ext)

		req, err := http.NewRequest("GET", downloadURL, nil)
		if err != nil {
			return
		}
		req.Header.Set("User-Agent", "TermChat-Updater/1.3")

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			return
		}
		defer resp.Body.Close()

		gzData, err := io.ReadAll(resp.Body)
		if err != nil || len(gzData) == 0 {
			return
		}

		stagedFile, err := os.Create(getStagedBinaryPath())
		if err != nil {
			return
		}
		defer stagedFile.Close()

		if strings.HasSuffix(downloadURL, ".tar.gz") {
			err = extractTarGz(gzData, stagedFile)
		} else if strings.HasSuffix(downloadURL, ".zip") {
			err = extractZip(gzData, stagedFile)
		} else {
			_, err = stagedFile.Write(gzData)
		}

		if err == nil {
			_ = os.Chmod(getStagedBinaryPath(), 0755)
			_ = os.WriteFile(getStagedTagPath(), []byte(latestTag), 0644)
			if onNotice != nil {
				onNotice(fmt.Sprintf("[NET] New update %s pre-downloaded! Type `/update` to apply instantly.", latestTag))
			}
		}
	}()
}

func UpdateSelfWithProgress(onProgress func(msg string)) (string, error) {
	if onProgress != nil {
		onProgress("[NET] Checking for updates from GitHub...")
	}

	latestTag, err := FetchLatestVersionTag()
	if err == nil && latestTag != "" {
		if strings.EqualFold(latestTag, AppVersion) || latestTag <= AppVersion {
			return fmt.Sprintf("[OK] You are already on the latest version of TermChat (%s)!", AppVersion), nil
		}
		if onProgress != nil {
			onProgress(fmt.Sprintf("[NET] Found new version: %s (Current: %s)", latestTag, AppVersion))
		}
	} else {
		latestTag = AppVersion
	}

	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("could not determine executable path: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return "", fmt.Errorf("could not resolve symlinks: %w", err)
	}

	// Check if update is ALREADY pre-fetched in background
	stagedTag, _ := os.ReadFile(getStagedTagPath())
	if strings.TrimSpace(string(stagedTag)) == latestTag {
		stagedBin := getStagedBinaryPath()
		if info, sErr := os.Stat(stagedBin); sErr == nil && info.Size() > 1000000 {
			if onProgress != nil {
				onProgress("[OK] Applying pre-downloaded update instantly (0s wait)...")
			}
			if runtime.GOOS == "windows" {
				oldPath := execPath + ".old"
				_ = os.Remove(oldPath)
				_ = os.Rename(execPath, oldPath)
			}
			rErr := os.Rename(stagedBin, execPath)
			if rErr != nil {
				src, _ := os.Open(stagedBin)
				dst, _ := os.OpenFile(execPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
				if src != nil && dst != nil {
					_, _ = io.Copy(dst, src)
					_ = dst.Close()
					_ = src.Close()
				}
			}
			_ = os.Remove(getStagedTagPath())
			_ = os.Remove(stagedBin)
			return fmt.Sprintf("[OK] Instant update applied! TermChat updated to %s.\n:: Please restart termchat to run new version.", latestTag), nil
		}
	}

	binaryName := getPlatformBinaryName()
	
	// Prefer downloading compressed archive (.tar.gz / .zip) for 4x faster download (2.1 MB vs 8.4 MB)
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	archiveName := binaryName + ext
	downloadURL := fmt.Sprintf("https://github.com/BrianC0des/termchat/releases/latest/download/%s", archiveName)

	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("request creation error: %w", err)
	}
	req.Header.Set("User-Agent", "TermChat-Updater/1.3")

	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		// Fallback to direct raw binary download if compressed archive isn't available
		if resp != nil {
			resp.Body.Close()
		}
		downloadURL = fmt.Sprintf("https://github.com/BrianC0des/termchat/releases/latest/download/%s", binaryName)
		req, _ = http.NewRequest("GET", downloadURL, nil)
		req.Header.Set("User-Agent", "TermChat-Updater/1.3")
		resp, err = client.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("failed to download update asset")
		}
	}
	defer resp.Body.Close()

	totalSize := resp.ContentLength
	if onProgress != nil {
		if totalSize > 0 {
			onProgress(fmt.Sprintf("[NET] Downloading TermChat update (%.1f MB)...", float64(totalSize)/(1024*1024)))
		} else {
			onProgress("[NET] Downloading TermChat update...")
		}
	}

	var downloadedData bytes.Buffer
	pw := &progressWriter{
		total:      totalSize,
		onProgress: onProgress,
	}
	destWriter := io.MultiWriter(&downloadedData, pw)
	_, err = io.Copy(destWriter, resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading download stream: %w", err)
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

	rawBytes := downloadedData.Bytes()
	if strings.HasSuffix(downloadURL, ".tar.gz") {
		err = extractTarGz(rawBytes, tmpFile)
	} else if strings.HasSuffix(downloadURL, ".zip") {
		err = extractZip(rawBytes, tmpFile)
	} else {
		_, err = tmpFile.Write(rawBytes)
	}
	_ = tmpFile.Close()

	if err != nil {
		return "", fmt.Errorf("error extracting downloaded binary: %w", err)
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

	return fmt.Sprintf("[OK] Successfully updated TermChat to %s!\n:: Please restart termchat to apply.", latestTag), nil
}

func UpdateSelf() (string, error) {
	return UpdateSelfWithProgress(nil)
}
