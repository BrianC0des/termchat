package system

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net"
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

func createOptimizedHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        10,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
		ReadBufferSize:      64 * 1024,
		WriteBufferSize:     64 * 1024,
	}
	return &http.Client{Transport: transport, Timeout: 0}
}

func processAndWriteBinary(rawBytes []byte, destFile *os.File) error {
	if len(rawBytes) >= 2 && rawBytes[0] == 0x1f && rawBytes[1] == 0x8b {
		return extractTarGz(rawBytes, destFile)
	}
	if len(rawBytes) >= 4 && rawBytes[0] == 'P' && rawBytes[1] == 'K' && rawBytes[2] == 0x03 && rawBytes[3] == 0x04 {
		return extractZip(rawBytes, destFile)
	}
	_, err := destFile.Write(rawBytes)
	return err
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
		archiveName := binaryName + ext

		// High-speed URLs: 1. High-speed Relay Proxy Mirror, 2. GitHub Release Asset
		urls := []string{
			fmt.Sprintf("https://termchat-o51d.onrender.com/api/update?file=%s", archiveName),
			fmt.Sprintf("https://github.com/BrianC0des/termchat/releases/latest/download/%s", archiveName),
			fmt.Sprintf("https://github.com/BrianC0des/termchat/releases/latest/download/%s", binaryName),
		}

		client := createOptimizedHTTPClient()
		var resp *http.Response

		for _, u := range urls {
			req, rErr := http.NewRequest("GET", u, nil)
			if rErr != nil {
				continue
			}
			req.Header.Set("User-Agent", "TermChat-Updater/1.4")
			r, dErr := client.Do(req)
			if dErr == nil && r.StatusCode == http.StatusOK {
				resp = r
				break
			}
			if r != nil {
				r.Body.Close()
			}
		}

		if resp == nil {
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

		err = processAndWriteBinary(gzData, stagedFile)

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
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	archiveName := binaryName + ext

	urls := []string{
		fmt.Sprintf("https://termchat-o51d.onrender.com/api/update?file=%s", archiveName),
		fmt.Sprintf("https://github.com/BrianC0des/termchat/releases/latest/download/%s", archiveName),
		fmt.Sprintf("https://github.com/BrianC0des/termchat/releases/latest/download/%s", binaryName),
	}

	client := createOptimizedHTTPClient()
	var resp *http.Response

	for _, u := range urls {
		req, rErr := http.NewRequest("GET", u, nil)
		if rErr != nil {
			continue
		}
		req.Header.Set("User-Agent", "TermChat-Updater/1.4")
		r, dErr := client.Do(req)
		if dErr == nil && r.StatusCode == http.StatusOK {
			resp = r
			break
		}
		if r != nil {
			r.Body.Close()
		}
	}

	if resp == nil {
		return "", fmt.Errorf("failed to download update from all available mirrors")
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
	err = processAndWriteBinary(rawBytes, tmpFile)
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
