package system

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Size limits for anything that comes off the network. They are variables
// only so tests can shrink them.
var (
	maxUpdateDownload  int64 = 200 << 20 // compressed asset, in memory
	maxUpdateExtracted int64 = 400 << 20 // extracted binary (decompression-bomb guard)
	maxDeltaDownload   int64 = 64 << 20
)

var errTooLarge = errors.New("download exceeds the maximum allowed size")

// copyCapped copies at most max bytes from src and fails if src has more.
func copyCapped(dst io.Writer, src io.Reader, max int64) (int64, error) {
	n, err := io.Copy(dst, io.LimitReader(src, max+1))
	if err != nil {
		return n, err
	}
	if n > max {
		return n, errTooLarge
	}
	return n, nil
}

var tagPattern = regexp.MustCompile(`^v?[0-9]{1,4}\.[0-9]{1,4}\.[0-9]{1,4}([-+][0-9A-Za-z.]{1,32})?$`)

// isValidTag reports whether tag looks like a release version. Tags come from
// mirrors and are interpolated into download URLs, so anything else (path
// separators, query strings, whitespace...) is refused.
func isValidTag(tag string) bool { return tagPattern.MatchString(tag) }

// stagingDir returns the private per-user directory used to pre-stage updates.
// It replaces the shared, predictable os.TempDir() paths, where any other
// local user could plant a binary (or a symlink) that /update would later
// install over the user's executable.
func stagingDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", fmt.Errorf("no per-user cache directory: %w", err)
		}
		base = filepath.Join(home, ".cache")
	}
	dir := filepath.Join(base, "termchat", "update")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return "", fmt.Errorf("refusing to use %s: not a plain directory", dir)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// getStagedBinaryPath / getStagedTagPath return "" if no safe staging
// directory is available; every caller treats that as "nothing staged".
func getStagedBinaryPath() string {
	dir, err := stagingDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "staged-binary")
}

func getStagedTagPath() string {
	dir, err := stagingDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "staged-meta.txt")
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// writeStagedMeta records "<tag>\n<sha256>" atomically (temp file + rename).
func writeStagedMeta(tag, sum string) error {
	path := getStagedTagPath()
	if path == "" {
		return errors.New("no staging directory")
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "meta-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.WriteString(tag + "\n" + sum + "\n"); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// readStagedMeta returns the staged tag and the SHA-256 of the staged binary
// ("" , "" if nothing valid is staged).
func readStagedMeta() (tag, sum string) {
	path := getStagedTagPath()
	if path == "" {
		return "", ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 || !isValidTag(strings.TrimSpace(lines[0])) || len(strings.TrimSpace(lines[1])) != 64 {
		return "", ""
	}
	return strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1])
}

// stagedUpdateReady reports whether an intact, pre-downloaded binary for tag
// is staged: the metadata matches and the binary still has the recorded hash.
func stagedUpdateReady(tag string) bool {
	stagedTag, sum := readStagedMeta()
	if stagedTag == "" || stagedTag != tag {
		return false
	}
	bin := getStagedBinaryPath()
	if bin == "" {
		return false
	}
	fi, err := os.Lstat(bin)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() <= 1000000 {
		return false
	}
	got, err := hashFile(bin)
	return err == nil && got == sum
}

func clearStaged() {
	if p := getStagedTagPath(); p != "" {
		_ = os.Remove(p)
	}
	if p := getStagedBinaryPath(); p != "" {
		_ = os.Remove(p)
	}
}
