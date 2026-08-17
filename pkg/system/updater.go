package system

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/klauspost/compress/zstd"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const AppVersion = "v1.8.4"

var (
	preFetchMu       sync.RWMutex
	isPreFetching    bool
	preFetchTag      string
	preFetchProgress int
)

func GetPreFetchStatus() (bool, string, int) {
	preFetchMu.RLock()
	defer preFetchMu.RUnlock()
	return isPreFetching, preFetchTag, preFetchProgress
}

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
			pw.onProgress(fmt.Sprintf("[NET] Downloading update: %d%% (%.1f / %.1f MB)...",
				pct,
				float64(pw.current)/(1024*1024),
				float64(pw.total)/(1024*1024),
			))
		}
	}
	return n, nil
}

func extractTarGz(gzipData []byte, destFile *os.File) error {
	gr, err := gzip.NewReader(bytes.NewReader(gzipData))
	if err != nil {
		return err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
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
	return fmt.Errorf("no binary found in tar.gz")
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
			defer rc.Close()
			_, err = io.Copy(destFile, rc)
			return err
		}
	}
	return fmt.Errorf("no binary found in zip")
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

func getPlatformArchiveName() string {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	if goos == "windows" {
		if goarch == "arm64" {
			return "termchat-windows-arm64.zip"
		}
		return "termchat-windows.zip"
	}

	binaryName := getPlatformBinaryName()
	return binaryName + ".tar.zst"
}

// FetchLatestVersionTag queries CDN edge and web redirects without encountering GitHub REST API rate limits
func FetchLatestVersionTag() (string, error) {
	client := createOptimizedHTTPClient(false)
	client.Timeout = 5 * time.Second

	// Tier 1: Fastly CDN Edge (Instant, Zero Rate Limits)
	cdnURLs := []string{
		"https://raw.githubusercontent.com/BrianC0des/termchat/main/version.json",
		"https://cdn.jsdelivr.net/gh/BrianC0des/termchat@main/version.json",
	}
	for _, cdnURL := range cdnURLs {
		req, err := http.NewRequest("GET", cdnURL, nil)
		if err == nil {
			req.Header.Set("User-Agent", "TermChat-Updater/1.8")
			resp, rErr := client.Do(req)
			if rErr == nil && resp.StatusCode == http.StatusOK {
				defer resp.Body.Close()
				var vInfo struct {
					Version string `json:"version"`
				}
				if json.NewDecoder(resp.Body).Decode(&vInfo) == nil && vInfo.Version != "" {
					return vInfo.Version, nil
				}
			}
			if resp != nil {
				resp.Body.Close()
			}
		}
	}

	// Tier 2: GitHub Web 302 Header Redirect (Zero API Rate Limit)
	redirectClient := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	headReq, hErr := http.NewRequest("HEAD", "https://github.com/BrianC0des/termchat/releases/latest", nil)
	if hErr == nil {
		headReq.Header.Set("User-Agent", "TermChat-Updater/1.8")
		hResp, dErr := redirectClient.Do(headReq)
		if dErr == nil && (hResp.StatusCode == http.StatusFound || hResp.StatusCode == http.StatusMovedPermanently || hResp.StatusCode == http.StatusTemporaryRedirect) {
			defer hResp.Body.Close()
			loc := hResp.Header.Get("Location")
			if loc != "" {
				parts := strings.Split(loc, "/tag/")
				if len(parts) == 2 && parts[1] != "" {
					return parts[1], nil
				}
			}
		}
		if hResp != nil {
			hResp.Body.Close()
		}
	}

	// Tier 3: GitHub REST API Fallback
	apiReq, aErr := http.NewRequest("GET", "https://api.github.com/repos/BrianC0des/termchat/releases/latest", nil)
	if aErr == nil {
		apiReq.Header.Set("User-Agent", "TermChat-Updater/1.8")
		apiResp, rErr := client.Do(apiReq)
		if rErr == nil && apiResp.StatusCode == http.StatusOK {
			defer apiResp.Body.Close()
			var data struct {
				TagName string `json:"tag_name"`
			}
			if json.NewDecoder(apiResp.Body).Decode(&data) == nil && data.TagName != "" {
				return data.TagName, nil
			}
		}
		if apiResp != nil {
			apiResp.Body.Close()
		}
	}

	return AppVersion, nil
}

func getStagedBinaryPath() string {
	return filepath.Join(os.TempDir(), "termchat-staged-binary.tmp")
}

func getStagedTagPath() string {
	return filepath.Join(os.TempDir(), "termchat-staged-tag.txt")
}

func createOptimizedHTTPClient(insecure bool) *http.Client {
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
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: insecure,
		},
	}
	return &http.Client{Transport: transport, Timeout: 0}
}

func resolveFinalDownloadURL(client *http.Client, initialURL string) (string, int64, error) {
	req, err := http.NewRequest("HEAD", initialURL, nil)
	if err != nil {
		req, err = http.NewRequest("GET", initialURL, nil)
		if err != nil {
			return initialURL, 0, err
		}
	}
	req.Header.Set("User-Agent", "TermChat-Updater/1.7")

	redirectClient := &http.Client{
		Transport: client.Transport,
		Timeout:   10 * time.Second,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := redirectClient.Do(req)
	if err != nil {
		return initialURL, 0, err
	}
	defer resp.Body.Close()

	if loc := resp.Header.Get("Location"); loc != "" {
		return loc, resp.ContentLength, nil
	}

	return initialURL, resp.ContentLength, nil
}

func downloadMultiThreaded(client *http.Client, targetURL string, totalSize int64, pw *progressWriter) ([]byte, error) {
	numChunks := 4
	chunkSize := totalSize / int64(numChunks)
	chunks := make([][]byte, numChunks)
	var wg sync.WaitGroup
	var errOnce sync.Once
	var downloadErr error

	for i := 0; i < numChunks; i++ {
		wg.Add(1)
		go func(chunkIndex int) {
			defer wg.Done()
			start := int64(chunkIndex) * chunkSize
			end := start + chunkSize - 1
			if chunkIndex == numChunks-1 {
				end = totalSize - 1
			}

			req, err := http.NewRequest("GET", targetURL, nil)
			if err != nil {
				errOnce.Do(func() { downloadErr = err })
				return
			}
			req.Header.Set("User-Agent", "TermChat-Updater/1.8")
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

			chunkClient := &http.Client{
				Transport: client.Transport,
				Timeout:   8 * time.Second,
			}
			resp, err := chunkClient.Do(req)
			if err != nil || (resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK) {
				if resp != nil {
					resp.Body.Close()
				}
				errOnce.Do(func() { downloadErr = fmt.Errorf("chunk download error: %v", err) })
				return
			}
			defer resp.Body.Close()

			var buf bytes.Buffer
			dest := io.MultiWriter(&buf, pw)
			_, err = io.Copy(dest, resp.Body)
			if err != nil {
				errOnce.Do(func() { downloadErr = err })
				return
			}
			chunks[chunkIndex] = buf.Bytes()
		}(i)
	}

	wg.Wait()
	if downloadErr != nil {
		return nil, downloadErr
	}

	var finalData bytes.Buffer
	for _, c := range chunks {
		finalData.Write(c)
	}
	return finalData.Bytes(), nil
}

func extractTarZst(zstdData []byte, destFile *os.File) error {
	zr, err := zstd.NewReader(bytes.NewReader(zstdData))
	if err != nil {
		return err
	}
	defer zr.Close()

	tr := tar.NewReader(zr)
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
	return fmt.Errorf("no executable binary found in zstd archive")
}

func processAndWriteBinary(rawBytes []byte, destFile *os.File) error {
	if len(rawBytes) < 100000 {
		return fmt.Errorf("downloaded asset is too small (%d bytes), likely an error response", len(rawBytes))
	}

	// 0. Zstandard Archive (.tar.zst) -> 0x28 0xB5 0x2F 0xFD
	if rawBytes[0] == 0x28 && rawBytes[1] == 0xb5 && rawBytes[2] == 0x2f && rawBytes[3] == 0xfd {
		return extractTarZst(rawBytes, destFile)
	}

	// 1. GZIP Archive (.tar.gz) -> 0x1F 0x8B
	if rawBytes[0] == 0x1f && rawBytes[1] == 0x8b {
		return extractTarGz(rawBytes, destFile)
	}

	// 2. ZIP Archive (.zip) -> 'P' 'K' 0x03 0x04
	if rawBytes[0] == 'P' && rawBytes[1] == 'K' && rawBytes[2] == 0x03 && rawBytes[3] == 0x04 {
		return extractZip(rawBytes, destFile)
	}

	// 3. Raw ELF Binary (Linux/Android) -> 0x7F 'E' 'L' 'F'
	if rawBytes[0] == 0x7f && rawBytes[1] == 'E' && rawBytes[2] == 'L' && rawBytes[3] == 'F' {
		_, err := destFile.Write(rawBytes)
		return err
	}

	// 4. Raw Mach-O Binary (macOS 64-bit / Universal)
	if (rawBytes[0] == 0xcf && rawBytes[1] == 0xfa) || (rawBytes[0] == 0xce && rawBytes[1] == 0xfa) || (rawBytes[0] == 0xca && rawBytes[1] == 0xfe) {
		_, err := destFile.Write(rawBytes)
		return err
	}

	// 5. Raw PE Binary (Windows .exe) -> 'M' 'Z'
	if rawBytes[0] == 'M' && rawBytes[1] == 'Z' {
		_, err := destFile.Write(rawBytes)
		return err
	}

	return fmt.Errorf("invalid binary format (received non-executable data or HTML response)")
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

		preFetchMu.Lock()
		isPreFetching = true
		preFetchTag = latestTag
		preFetchProgress = 0
		preFetchMu.Unlock()

		defer func() {
			preFetchMu.Lock()
			isPreFetching = false
			preFetchMu.Unlock()
		}()

		binaryName := getPlatformBinaryName()
		archiveName := getPlatformArchiveName()

		urls := []string{
			// Tier 1: Fastly CDN Edge (Manila node, 15ms latency)
			fmt.Sprintf("https://raw.githubusercontent.com/BrianC0des/termchat/binaries/%s", archiveName),
			fmt.Sprintf("https://cdn.jsdelivr.net/gh/BrianC0des/termchat@binaries/%s", archiveName),
			fmt.Sprintf("https://fastly.jsdelivr.net/gh/BrianC0des/termchat@binaries/%s", archiveName),

			// Tier 2: GitHub Releases Direct S3
			fmt.Sprintf("https://github.com/BrianC0des/termchat/releases/latest/download/%s", archiveName),
			fmt.Sprintf("https://github.com/BrianC0des/termchat/releases/latest/download/%s", binaryName),
			fmt.Sprintf("https://github.com/BrianC0des/termchat/releases/download/%s/%s", latestTag, archiveName),
			fmt.Sprintf("https://github.com/BrianC0des/termchat/releases/download/%s/%s", latestTag, binaryName),
			fmt.Sprintf("https://github.com/BrianC0des/termchat/releases/download/%s/%s.tar.gz", latestTag, binaryName),
		}

		clients := []*http.Client{
			createOptimizedHTTPClient(false),
			createOptimizedHTTPClient(true),
			http.DefaultClient,
			&http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				},
			},
		}
		var resp *http.Response

		for _, client := range clients {
			for _, u := range urls {
				req, rErr := http.NewRequest("GET", u, nil)
				if rErr != nil {
					continue
				}
				req.Header.Set("User-Agent", "TermChat-Updater/1.5")
				r, dErr := client.Do(req)
				if dErr == nil && r.StatusCode == http.StatusOK {
					resp = r
					break
				}
				if r != nil {
					r.Body.Close()
				}
			}
			if resp != nil {
				break
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
	if err != nil || latestTag == "" || strings.EqualFold(latestTag, AppVersion) || latestTag <= AppVersion {
		return fmt.Sprintf("[OK] You are already on the latest version of TermChat (%s)!", AppVersion), nil
	}

	if onProgress != nil {
		onProgress(fmt.Sprintf("[NET] Found new version: %s (Current: %s)", latestTag, AppVersion))
	}

	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("could not determine executable path: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return "", fmt.Errorf("could not resolve symlinks: %w", err)
	}

	// Check if background pre-download is currently in progress
	if active, tag, _ := GetPreFetchStatus(); active {
		if onProgress != nil {
			onProgress(fmt.Sprintf("[NET] Background pre-download for %s is currently active... Waiting for completion...", tag))
		}
		for i := 0; i < 60; i++ {
			time.Sleep(500 * time.Millisecond)
			if active, _, pct := GetPreFetchStatus(); active {
				if onProgress != nil && i%4 == 0 {
					onProgress(fmt.Sprintf("[NET] Background pre-download progress: %d%%...", pct))
				}
			} else {
				break
			}
		}
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
	archiveName := getPlatformArchiveName()

	urls := []string{
		// Tier 1: Fastly CDN Edge (Manila node, 15ms latency)
		fmt.Sprintf("https://raw.githubusercontent.com/BrianC0des/termchat/binaries/%s", archiveName),
		fmt.Sprintf("https://cdn.jsdelivr.net/gh/BrianC0des/termchat@binaries/%s", archiveName),
		fmt.Sprintf("https://fastly.jsdelivr.net/gh/BrianC0des/termchat@binaries/%s", archiveName),

		// Tier 2: GitHub Releases Direct S3
		fmt.Sprintf("https://github.com/BrianC0des/termchat/releases/latest/download/%s", archiveName),
		fmt.Sprintf("https://github.com/BrianC0des/termchat/releases/latest/download/%s", binaryName),
		fmt.Sprintf("https://github.com/BrianC0des/termchat/releases/download/%s/%s", latestTag, archiveName),
		fmt.Sprintf("https://github.com/BrianC0des/termchat/releases/download/%s/%s", latestTag, binaryName),
		fmt.Sprintf("https://github.com/BrianC0des/termchat/releases/download/%s/%s.tar.gz", latestTag, binaryName),
	}

	clients := []*http.Client{
		createOptimizedHTTPClient(false),
		createOptimizedHTTPClient(true),
		http.DefaultClient,
		&http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
	}
	var resp *http.Response
	var activeClient *http.Client
	var targetURL string

	for _, client := range clients {
		for _, u := range urls {
			finalURL, totalLen, rErr := resolveFinalDownloadURL(client, u)
			if rErr == nil && finalURL != "" {
				targetURL = finalURL
			} else {
				targetURL = u
			}

			req, rErr := http.NewRequest("GET", targetURL, nil)
			if rErr != nil {
				continue
			}
			req.Header.Set("User-Agent", "TermChat-Updater/1.8")
			r, dErr := client.Do(req)
			if dErr == nil && r.StatusCode == http.StatusOK {
				resp = r
				activeClient = client
				if totalLen > 0 {
					r.ContentLength = totalLen
				}
				break
			}
			if r != nil {
				r.Body.Close()
			}
		}
		if resp != nil {
			break
		}
	}

	if resp == nil {
		return "", fmt.Errorf("failed to download update from all available mirrors")
	}
	defer resp.Body.Close()

	totalSize := resp.ContentLength
	finalURL, finalSize, _ := resolveFinalDownloadURL(activeClient, targetURL)
	if finalSize > 0 {
		totalSize = finalSize
	}

	if onProgress != nil {
		if totalSize > 0 {
			onProgress(fmt.Sprintf("[NET] Downloading TermChat update (%.1f MB)...", float64(totalSize)/(1024*1024)))
		} else {
			onProgress("[NET] Downloading TermChat update...")
		}
	}

	var rawBytes []byte
	pw := &progressWriter{
		total:      totalSize,
		onProgress: onProgress,
	}

	if totalSize > 500000 {
		multiBytes, mErr := downloadMultiThreaded(activeClient, finalURL, totalSize, pw)
		if mErr == nil && len(multiBytes) > 100000 {
			rawBytes = multiBytes
		}
	}

	if len(rawBytes) == 0 {
		var downloadedData bytes.Buffer
		destWriter := io.MultiWriter(&downloadedData, pw)
		_, err = io.Copy(destWriter, resp.Body)
		if err != nil {
			return "", fmt.Errorf("error reading download stream: %w", err)
		}
		rawBytes = downloadedData.Bytes()
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
