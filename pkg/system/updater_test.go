package system

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func createDummyBinary(size int) []byte {
	b := make([]byte, size)
	copy(b, []byte("\x7fELF\x02\x01\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00"))
	var seed uint32 = 123456789
	for i := 16; i < size; i++ {
		seed = seed*1664525 + 1013904223
		b[i] = byte(seed >> 24)
	}
	return b
}

func createTestArchive(binaryName string, content []byte) ([]byte, error) {
	if runtime.GOOS == "windows" {
		var zipBuf bytes.Buffer
		zw := zip.NewWriter(&zipBuf)
		w, err := zw.Create(binaryName)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(content); err != nil {
			return nil, err
		}
		if err := zw.Close(); err != nil {
			return nil, err
		}
		return zipBuf.Bytes(), nil
	}

	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	hdr := &tar.Header{
		Name:     binaryName,
		Mode:     0755,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return nil, err
	}
	if _, err := tw.Write(content); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}

	var zstBuf bytes.Buffer
	zw, err := zstd.NewWriter(&zstBuf)
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(tarBuf.Bytes()); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return zstBuf.Bytes(), nil
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		v1       string
		v2       string
		expected int
	}{
		{"v2.1.1", "v2.1.1", 0},
		{"2.1.1", "v2.1.1", 0},
		{"v2.1.2", "v2.1.1", 1},
		{"v2.2.0", "v2.1.9", 1},
		{"v2.10.0", "v2.1.1", 1},
		{"v3.0.0", "v2.9.9", 1},
		{"v2.1.0", "v2.1.1", -1},
		{"v1.9.9", "v2.0.0", -1},
		{"v2.1.1-beta", "v2.1.1", 0},
	}

	for _, tt := range tests {
		got := compareVersions(tt.v1, tt.v2)
		if got != tt.expected {
			t.Errorf("compareVersions(%q, %q) = %d; want %d", tt.v1, tt.v2, got, tt.expected)
		}
	}
}

func TestIsNewerVersion(t *testing.T) {
	if !isNewerVersion("v2.1.2", "v2.1.1") {
		t.Errorf("expected v2.1.2 to be newer than v2.1.1")
	}
	if !isNewerVersion("v2.10.0", "v2.1.1") {
		t.Errorf("expected v2.10.0 to be newer than v2.1.1")
	}
	if isNewerVersion("v2.1.1", "v2.1.1") {
		t.Errorf("expected v2.1.1 not to be newer than v2.1.1")
	}
	if isNewerVersion("v2.1.0", "v2.1.1") {
		t.Errorf("expected v2.1.0 not to be newer than v2.1.1")
	}
}

func TestUpdateSelfWithProgress_Success(t *testing.T) {
	isolateCache(t)
	tmpDir := t.TempDir()

	origContent := []byte("original binary content")
	dummyExec := filepath.Join(tmpDir, "termchat-dummy")
	if err := os.WriteFile(dummyExec, origContent, 0755); err != nil {
		t.Fatalf("failed to create dummy exec: %v", err)
	}

	newBinary := createDummyBinary(200000)
	archiveData, err := createTestArchive(getPlatformBinaryName(), newBinary)
	if err != nil {
		t.Fatalf("failed to create test archive: %v", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(archiveData)))
		_, _ = w.Write(archiveData)
	}))
	defer ts.Close()

	oldTag := overrideVersionTag
	oldVer := currentVersionOverride
	oldURLs := overrideDownloadURLs
	oldExec := execPathOverride
	defer func() {
		overrideVersionTag = oldTag
		currentVersionOverride = oldVer
		overrideDownloadURLs = oldURLs
		execPathOverride = oldExec
		clearStaged()
	}()

	overrideVersionTag = "v2.2.0"
	currentVersionOverride = "v2.1.2"
	overrideDownloadURLs = []string{ts.URL + "/termchat.archive"}
	execPathOverride = dummyExec

	msg, err := UpdateSelfWithProgress(func(m string) {})
	if err != nil {
		t.Fatalf("UpdateSelfWithProgress failed: %v", err)
	}

	if !strings.Contains(msg, "[OK]") || !strings.Contains(msg, "v2.2.0") {
		t.Errorf("unexpected success message: %s", msg)
	}

	updatedContent, err := os.ReadFile(dummyExec)
	if err != nil {
		t.Fatalf("failed to read updated executable: %v", err)
	}
	if !bytes.Equal(updatedContent, newBinary) {
		t.Errorf("updated binary content does not match expected payload (got %d bytes, want %d bytes)", len(updatedContent), len(newBinary))
	}
}

func TestUpdateSelfWithProgress_MirrorFallback(t *testing.T) {
	isolateCache(t)
	tmpDir := t.TempDir()

	dummyExec := filepath.Join(tmpDir, "termchat-dummy")
	if err := os.WriteFile(dummyExec, []byte("old-binary"), 0755); err != nil {
		t.Fatalf("failed to create dummy exec: %v", err)
	}

	newBinary := createDummyBinary(200000)
	archiveData, err := createTestArchive(getPlatformBinaryName(), newBinary)
	if err != nil {
		t.Fatalf("failed to create test archive: %v", err)
	}

	// Bad primary mirror (fails with 500)
	tsBad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer tsBad.Close()

	// Good fallback mirror
	tsGood := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(archiveData)
	}))
	defer tsGood.Close()

	oldTag := overrideVersionTag
	oldVer := currentVersionOverride
	oldURLs := overrideDownloadURLs
	oldExec := execPathOverride
	defer func() {
		overrideVersionTag = oldTag
		currentVersionOverride = oldVer
		overrideDownloadURLs = oldURLs
		execPathOverride = oldExec
		clearStaged()
	}()

	overrideVersionTag = "v2.3.0"
	currentVersionOverride = "v2.1.2"
	overrideDownloadURLs = []string{tsBad.URL + "/bad-mirror", tsGood.URL + "/good-mirror"}
	execPathOverride = dummyExec

	msg, err := UpdateSelfWithProgress(func(m string) {})
	if err != nil {
		t.Fatalf("UpdateSelfWithProgress failed on mirror fallback: %v", err)
	}

	if !strings.Contains(msg, "[OK]") {
		t.Errorf("expected successful update via fallback mirror, got: %s", msg)
	}

	updated, _ := os.ReadFile(dummyExec)
	if !bytes.Equal(updated, newBinary) {
		t.Errorf("expected updated binary content from fallback mirror")
	}
}

func TestUpdateSelfWithProgress_CorruptedPayloadRejected(t *testing.T) {
	isolateCache(t)
	tmpDir := t.TempDir()

	origContent := []byte("unmodified-binary-content-12345")
	dummyExec := filepath.Join(tmpDir, "termchat-dummy")
	if err := os.WriteFile(dummyExec, origContent, 0755); err != nil {
		t.Fatalf("failed to create dummy exec: %v", err)
	}

	// Server returns 120KB of invalid/corrupt data
	corruptData := bytes.Repeat([]byte{0xde, 0xad, 0xbe, 0xef}, 30000)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(corruptData)
	}))
	defer ts.Close()

	oldTag := overrideVersionTag
	oldVer := currentVersionOverride
	oldURLs := overrideDownloadURLs
	oldExec := execPathOverride
	defer func() {
		overrideVersionTag = oldTag
		currentVersionOverride = oldVer
		overrideDownloadURLs = oldURLs
		execPathOverride = oldExec
		clearStaged()
	}()

	overrideVersionTag = "v2.4.0"
	currentVersionOverride = "v2.1.2"
	overrideDownloadURLs = []string{ts.URL + "/corrupt"}
	execPathOverride = dummyExec

	_, err := UpdateSelfWithProgress(func(m string) {})
	if err == nil {
		t.Fatalf("expected error when processing corrupted payload, got nil")
	}

	// Executable must remain untouched
	content, _ := os.ReadFile(dummyExec)
	if !bytes.Equal(content, origContent) {
		t.Errorf("original binary was overwritten despite corrupt payload")
	}
}

func TestUpdateSelfWithProgress_AlreadyLatest(t *testing.T) {
	isolateCache(t)
	tmpDir := t.TempDir()

	dummyExec := filepath.Join(tmpDir, "termchat-dummy")
	_ = os.WriteFile(dummyExec, []byte("binary"), 0755)

	oldTag := overrideVersionTag
	oldVer := currentVersionOverride
	oldExec := execPathOverride
	defer func() {
		overrideVersionTag = oldTag
		currentVersionOverride = oldVer
		execPathOverride = oldExec
	}()

	overrideVersionTag = "v2.1.2"
	currentVersionOverride = "v2.1.2"
	execPathOverride = dummyExec

	msg, err := UpdateSelfWithProgress(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(msg, "already on the latest version") {
		t.Errorf("expected already on latest version message, got: %s", msg)
	}
}

func TestUpdateSelfWithProgress_PreFetchedUpdate(t *testing.T) {
	isolateCache(t)
	tmpDir := t.TempDir()

	origContent := []byte("orig-before-prefetch")
	dummyExec := filepath.Join(tmpDir, "termchat-dummy")
	_ = os.WriteFile(dummyExec, origContent, 0755)

	newBinary := createDummyBinary(1100000)

	// Stage the update manually using staging API
	stagedPath := getStagedBinaryPath()
	if err := os.WriteFile(stagedPath, newBinary, 0700); err != nil {
		t.Fatalf("failed to stage binary: %v", err)
	}
	sum, err := hashFile(stagedPath)
	if err != nil {
		t.Fatalf("failed to hash staged binary: %v", err)
	}
	targetTag := "v2.5.0"
	if err := writeStagedMeta(targetTag, sum); err != nil {
		t.Fatalf("failed to write staged metadata: %v", err)
	}

	oldTag := overrideVersionTag
	oldVer := currentVersionOverride
	oldExec := execPathOverride
	defer func() {
		overrideVersionTag = oldTag
		currentVersionOverride = oldVer
		execPathOverride = oldExec
		clearStaged()
	}()

	overrideVersionTag = targetTag
	currentVersionOverride = "v2.1.2"
	execPathOverride = dummyExec

	var progressMessages []string
	msg, err := UpdateSelfWithProgress(func(m string) {
		progressMessages = append(progressMessages, m)
	})
	if err != nil {
		t.Fatalf("UpdateSelfWithProgress failed with pre-fetched update: %v", err)
	}

	if !strings.Contains(msg, "Instant update applied") {
		t.Errorf("expected instant update applied message, got: %s", msg)
	}

	applied, _ := os.ReadFile(dummyExec)
	if !bytes.Equal(applied, newBinary) {
		t.Errorf("pre-fetched binary was not correctly applied to target executable")
	}

	// Verify staging artifacts are cleaned up after application
	if stagedUpdateReady(targetTag) {
		t.Errorf("expected staged update to be cleared after application")
	}
}
