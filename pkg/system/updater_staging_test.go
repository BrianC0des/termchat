package system

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// isolateCache points the per-user cache dir at a temp directory.
func isolateCache(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("LocalAppData", filepath.Join(root, "cache"))
	return root
}

func TestIsValidTag(t *testing.T) {
	for _, ok := range []string{"v2.1.2", "2.1.2", "v10.20.30", "v2.1.2-beta.1", "v2.1.2+build5"} {
		if !isValidTag(ok) {
			t.Errorf("isValidTag(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{
		"", "latest", "v2.1", "v2.1.2/../../evil", "v2.1.2?x=1", "v2.1.2\n", " v2.1.2",
		"v2.1.2#frag", "../v2.1.2", "v2.1.2 ", "v2.1.2-<script>", "http://evil/v1.0.0",
		strings.Repeat("9", 40) + ".1.1",
	} {
		if isValidTag(bad) {
			t.Errorf("isValidTag(%q) = true, want false", bad)
		}
	}
}

func TestCopyCapped(t *testing.T) {
	var buf bytes.Buffer
	if n, err := copyCapped(&buf, strings.NewReader("12345"), 5); err != nil || n != 5 {
		t.Errorf("at the limit: n=%d err=%v", n, err)
	}
	buf.Reset()
	if _, err := copyCapped(&buf, strings.NewReader("123456"), 5); err != errTooLarge {
		t.Errorf("over the limit: err=%v, want errTooLarge", err)
	}
	if buf.Len() > 6 {
		t.Errorf("read %d bytes past the cap", buf.Len())
	}
}

func TestExtractRespectsSizeCap(t *testing.T) {
	old := maxUpdateExtracted
	maxUpdateExtracted = 1 << 20
	defer func() { maxUpdateExtracted = old }()

	var raw bytes.Buffer
	gz := gzip.NewWriter(&raw)
	tw := tar.NewWriter(gz)
	body := bytes.Repeat([]byte{0}, 2<<20) // 2 MB of zeros: tiny once compressed
	_ = tw.WriteHeader(&tar.Header{Name: "termchat", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(body)
	_ = tw.Close()
	_ = gz.Close()

	dest, err := os.CreateTemp(t.TempDir(), "x")
	if err != nil {
		t.Fatal(err)
	}
	defer dest.Close()
	if err := extractTarGz(raw.Bytes(), dest); err == nil {
		t.Fatal("expected the decompression-bomb guard to trigger")
	}
	if fi, _ := dest.Stat(); fi.Size() > maxUpdateExtracted+1 {
		t.Errorf("wrote %d bytes, cap is %d", fi.Size(), maxUpdateExtracted)
	}
}

func TestStagingDirIsPrivate(t *testing.T) {
	isolateCache(t)
	dir, err := stagingDir()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
			t.Errorf("staging dir perms = %o, want 700", fi.Mode().Perm())
		}
		// A loosened directory is tightened again.
		_ = os.Chmod(dir, 0o777)
		if _, err := stagingDir(); err != nil {
			t.Fatal(err)
		}
		if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
			t.Errorf("perms not tightened: %o", fi.Mode().Perm())
		}
	}
	// Staged files must not live in the shared temp dir.
	if p := getStagedBinaryPath(); filepath.Dir(p) == os.TempDir() {
		t.Errorf("staged binary path %s is in the shared temp dir", p)
	}
}

func TestStagingDirRefusesSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on windows")
	}
	root := isolateCache(t)
	target := t.TempDir()
	parent := filepath.Join(root, "cache", "termchat")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(parent, "update")); err != nil {
		t.Fatal(err)
	}
	if _, err := stagingDir(); err == nil {
		t.Fatal("stagingDir followed a symlink planted at the staging path")
	}
	if getStagedBinaryPath() != "" || getStagedTagPath() != "" {
		t.Error("staged paths must be empty when no safe dir exists")
	}
	if stagedUpdateReady("v9.9.9") {
		t.Error("nothing can be staged without a safe dir")
	}
}

func stageFake(t *testing.T, tag string, size int) string {
	t.Helper()
	bin := getStagedBinaryPath()
	if bin == "" {
		t.Fatal("no staging dir")
	}
	if err := os.WriteFile(bin, bytes.Repeat([]byte{0x7f}, size), 0o700); err != nil {
		t.Fatal(err)
	}
	sum, err := hashFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeStagedMeta(tag, sum); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestStagedUpdateIntegrity(t *testing.T) {
	isolateCache(t)
	if stagedUpdateReady("v2.2.0") {
		t.Fatal("nothing staged yet")
	}
	bin := stageFake(t, "v2.2.0", 1_200_000)
	if !stagedUpdateReady("v2.2.0") {
		t.Fatal("intact staged update should be ready")
	}
	if stagedUpdateReady("v2.3.0") {
		t.Error("tag mismatch must not be ready")
	}

	// Tampering after staging (same size, one byte flipped) is detected.
	data, _ := os.ReadFile(bin)
	data[len(data)/2] ^= 0xff
	if err := os.WriteFile(bin, data, 0o700); err != nil {
		t.Fatal(err)
	}
	if stagedUpdateReady("v2.2.0") {
		t.Error("tampered staged binary must be rejected")
	}
}

func TestStagedUpdateRejectsSymlinkedBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on windows")
	}
	isolateCache(t)
	bin := stageFake(t, "v2.2.0", 1_200_000)
	real := filepath.Join(t.TempDir(), "real")
	if err := os.Rename(bin, real); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, bin); err != nil {
		t.Fatal(err)
	}
	if stagedUpdateReady("v2.2.0") {
		t.Error("a symlinked staged binary must not be accepted")
	}
}

func TestStagedMetaMalformed(t *testing.T) {
	isolateCache(t)
	for _, content := range []string{"", "v2.2.0", "v2.2.0\nshort\n", "../../x\n" + strings.Repeat("a", 64) + "\n", "v2.2.0\n" + strings.Repeat("a", 64) + "\nextra\n"} {
		if err := os.WriteFile(getStagedTagPath(), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if tag, sum := readStagedMeta(); tag != "" || sum != "" {
			t.Errorf("meta %q accepted as (%q,%q)", content, tag, sum)
		}
	}
}

func TestClearStaged(t *testing.T) {
	isolateCache(t)
	bin := stageFake(t, "v2.2.0", 1_200_000)
	clearStaged()
	if _, err := os.Stat(bin); err == nil {
		t.Error("staged binary not removed")
	}
	if _, err := os.Stat(getStagedTagPath()); err == nil {
		t.Error("staged meta not removed")
	}
}
