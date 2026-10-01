package gitcollab

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCaptureAndApplyPatch(t *testing.T) {
	// Create a temporary git repo
	tmpDir, err := os.MkdirTemp("", "gitcollab-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	runGit := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", tmpDir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v (%s)", args, err, string(out))
		}
	}

	runGit("init")
	runGit("config", "user.name", "TestUser")
	runGit("config", "user.email", "test@example.com")

	// Commit an initial file
	testFile := filepath.Join(tmpDir, "hello.txt")
	if err := os.WriteFile(testFile, []byte("Hello World\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit("add", "hello.txt")
	runGit("commit", "-m", "initial commit")

	// Modify file
	if err := os.WriteFile(testFile, []byte("Hello World\nAdded new line\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Capture Diff
	diffRes, err := CaptureDiff(tmpDir, false)
	if err != nil {
		t.Fatalf("CaptureDiff failed: %v", err)
	}
	if diffRes.Additions != 1 || len(diffRes.Files) != 1 {
		t.Errorf("DiffResult stats mismatch: %+v", diffRes)
	}

	// Revert file
	runGit("checkout", "hello.txt")

	// Apply captured patch
	msg, err := ApplyPatch(tmpDir, diffRes.RawDiff)
	if err != nil {
		t.Fatalf("ApplyPatch failed: %v", err)
	}
	if msg == "" {
		t.Errorf("Expected success message from ApplyPatch")
	}

	// Verify content was restored
	content, _ := os.ReadFile(testFile)
	if string(content) != "Hello World\nAdded new line\n" {
		t.Errorf("File content after patch = %q", string(content))
	}
}

func TestApplyPatchRejectsEscapingPaths(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gitcollab-security-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	runGit := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", tmpDir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v (%s)", args, err, string(out))
		}
	}
	runGit("init")
	runGit("config", "user.name", "TestUser")
	runGit("config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(tmpDir, "keep.txt"), []byte("safe\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit("add", "keep.txt")
	runGit("commit", "-m", "initial commit")

	malicious := []struct {
		name  string
		patch string
	}{
		{
			name: "git hook overwrite",
			patch: "diff --git a/.git/hooks/pre-commit b/.git/hooks/pre-commit\n" +
				"new file mode 100755\n" +
				"index 0000000..1111111\n" +
				"--- /dev/null\n" +
				"+++ b/.git/hooks/pre-commit\n" +
				"@@ -0,0 +1,2 @@\n" +
				"+#!/bin/sh\n" +
				"+echo pwned\n",
		},
		{
			name: "path traversal outside repo",
			patch: "diff --git a/../evil.txt b/../evil.txt\n" +
				"new file mode 100644\n" +
				"index 0000000..1111111\n" +
				"--- /dev/null\n" +
				"+++ b/../evil.txt\n" +
				"@@ -0,0 +1 @@\n" +
				"+pwned\n",
		},
	}

	for _, tc := range malicious {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ApplyPatch(tmpDir, tc.patch); err == nil {
				t.Fatalf("expected ApplyPatch to reject malicious patch %q, but it succeeded", tc.name)
			}
		})
	}
}

func TestGetDirtyFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gitcollab-dirty-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	runGit := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", tmpDir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v (%s)", args, err, string(out))
		}
	}

	runGit("init")
	runGit("config", "user.name", "TestUser")
	runGit("config", "user.email", "test@example.com")

	dirty, err := GetDirtyFiles(tmpDir)
	if err != nil {
		t.Fatalf("GetDirtyFiles failed: %v", err)
	}
	if len(dirty) != 0 {
		t.Fatalf("expected 0 dirty files, got %v", dirty)
	}

	f1 := filepath.Join(tmpDir, "file1.go")
	if err := os.WriteFile(f1, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dirty, err = GetDirtyFiles(tmpDir)
	if err != nil {
		t.Fatalf("GetDirtyFiles failed: %v", err)
	}
	if len(dirty) != 1 || dirty[0] != "file1.go" {
		t.Fatalf("expected ['file1.go'], got %v", dirty)
	}

	runGit("add", "file1.go")
	runGit("commit", "-m", "init")

	if err := os.WriteFile(f1, []byte("package main\nfunc hello() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	f2 := filepath.Join(tmpDir, "file2.go")
	if err := os.WriteFile(f2, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	termchatDir := filepath.Join(tmpDir, ".termchat")
	_ = os.MkdirAll(termchatDir, 0755)
	_ = os.WriteFile(filepath.Join(termchatDir, "room.json"), []byte("{}"), 0644)

	dirty, err = GetDirtyFiles(tmpDir)
	if err != nil {
		t.Fatalf("GetDirtyFiles failed: %v", err)
	}
	if len(dirty) != 2 {
		t.Fatalf("expected 2 dirty files (excluding .termchat), got %v", dirty)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

type testRepo struct {
	t    *testing.T
	root string // parent dir that contains the repo (to detect escapes)
	dir  string // the repository work tree
}

func newTestRepo(t *testing.T) *testRepo {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "repo")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := &testRepo{t: t, root: root, dir: dir}
	r.git("init")
	r.git("config", "user.name", "TestUser")
	r.git("config", "user.email", "test@example.com")
	r.git("config", "commit.gpgsign", "false")
	return r
}

func (r *testRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", r.dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v failed: %v (%s)", args, err, out)
	}
	return string(out)
}

func (r *testRepo) write(rel string, data []byte) {
	r.t.Helper()
	p := filepath.Join(r.dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *testRepo) read(rel string) []byte {
	r.t.Helper()
	b, err := os.ReadFile(filepath.Join(r.dir, rel))
	if err != nil {
		r.t.Fatal(err)
	}
	return b
}

func (r *testRepo) commitAll(msg string) {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-m", msg)
}

func (r *testRepo) exists(rel string) bool {
	_, err := os.Lstat(filepath.Join(r.dir, rel))
	return err == nil
}

// ---------------------------------------------------------------------------
// patch ids and the bounded store
// ---------------------------------------------------------------------------

func TestGeneratePatchID(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 2000; i++ {
		id := GeneratePatchID()
		if len(id) != 16 {
			t.Fatalf("patch id %q has length %d, want 16", id, len(id))
		}
		if _, err := hex.DecodeString(id); err != nil {
			t.Fatalf("patch id %q is not hex: %v", id, err)
		}
		if seen[id] {
			t.Fatalf("duplicate patch id %q", id)
		}
		seen[id] = true
	}
}

func resetStore() {
	patchMu.Lock()
	defer patchMu.Unlock()
	patchStore = make(map[string]string)
	patchOrder = nil
	patchBytes = 0
}

func TestPatchStoreIsBoundedFIFO(t *testing.T) {
	resetStore()
	defer resetStore()

	for i := 0; i < 150; i++ {
		if !StorePatch(fmt.Sprintf("id-%03d", i), "content") {
			t.Fatalf("StorePatch(%d) unexpectedly refused", i)
		}
	}
	patchMu.RLock()
	n, order := len(patchStore), len(patchOrder)
	patchMu.RUnlock()
	if n != maxStoredPatches || order != maxStoredPatches {
		t.Fatalf("store holds %d entries (order %d), want %d", n, order, maxStoredPatches)
	}
	if _, ok := GetPatch("id-000"); ok {
		t.Error("oldest entry should have been evicted")
	}
	if _, ok := GetPatch("id-049"); ok {
		t.Error("id-049 should have been evicted")
	}
	if _, ok := GetPatch("id-050"); !ok {
		t.Error("id-050 should still be present")
	}
	if _, ok := GetPatch("id-149"); !ok {
		t.Error("newest entry should be present")
	}
}

func TestPatchStoreRefusesOverwriteAndOversize(t *testing.T) {
	resetStore()
	defer resetStore()

	if !StorePatch("abc", "original") {
		t.Fatal("first store should succeed")
	}
	if StorePatch("abc", "tampered") {
		t.Error("overwriting an id with different content must be refused")
	}
	if got, _ := GetPatch("abc"); got != "original" {
		t.Errorf("content changed to %q", got)
	}
	if !StorePatch("abc", "original") {
		t.Error("re-storing identical content should be a no-op success")
	}
	if StorePatch("big", strings.Repeat("x", maxPatchSize+1)) {
		t.Error("oversized patch must be refused")
	}
	if StorePatch("", "x") {
		t.Error("empty id must be refused")
	}
}

func TestPatchStoreByteBudget(t *testing.T) {
	resetStore()
	defer resetStore()

	chunk := strings.Repeat("y", 10<<20) // 10 MB
	for i := 0; i < 10; i++ {
		StorePatch(fmt.Sprintf("big-%d", i), chunk)
	}
	patchMu.RLock()
	total := patchBytes
	patchMu.RUnlock()
	if total > maxStoredBytes {
		t.Fatalf("store holds %d bytes, over budget %d", total, maxStoredBytes)
	}
	if _, ok := GetPatch("big-9"); !ok {
		t.Error("newest patch should be retained")
	}
}

func TestPatchStoreConcurrent(t *testing.T) {
	resetStore()
	defer resetStore()

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				id := fmt.Sprintf("g%d-%d", g, i)
				StorePatch(id, "data")
				GetPatch(id)
			}
		}(g)
	}
	wg.Wait()
	patchMu.RLock()
	defer patchMu.RUnlock()
	if len(patchStore) > maxStoredPatches || len(patchStore) != len(patchOrder) {
		t.Fatalf("inconsistent store: map=%d order=%d", len(patchStore), len(patchOrder))
	}
}

// ---------------------------------------------------------------------------
// CaptureDiff
// ---------------------------------------------------------------------------

func TestCaptureDiffIgnoresUserGitConfig(t *testing.T) {
	r := newTestRepo(t)
	r.write("a.txt", []byte("one\n"))
	r.commitAll("init")
	// Settings that would corrupt a naive `git diff` capture.
	r.git("config", "color.ui", "always")
	r.git("config", "color.diff", "always")
	r.git("config", "diff.noprefix", "true")
	r.git("config", "diff.mnemonicPrefix", "true")
	r.git("config", "diff.external", "/bin/false")
	r.write("a.txt", []byte("one\ntwo\n"))

	res, err := CaptureDiff(r.dir, false)
	if err != nil {
		t.Fatalf("CaptureDiff: %v", err)
	}
	if strings.Contains(res.RawDiff, "\x1b[") {
		t.Error("diff contains ANSI colour codes")
	}
	if !strings.Contains(res.RawDiff, "diff --git a/a.txt b/a.txt") {
		t.Errorf("diff does not use a/ b/ prefixes:\n%s", res.RawDiff)
	}
	if len(res.PatchID) != 16 {
		t.Errorf("patch id %q, want 16 chars", res.PatchID)
	}
	if stored, ok := GetPatch(res.PatchID); !ok || stored != res.RawDiff {
		t.Error("captured patch was not stored")
	}
	if res.Additions != 1 || res.Deletions != 0 || len(res.Files) != 1 || res.Files[0] != "a.txt" {
		t.Errorf("stats mismatch: %+v", res)
	}
}

func TestCaptureDiffEmptyAndNotARepo(t *testing.T) {
	r := newTestRepo(t)
	r.write("a.txt", []byte("one\n"))
	r.commitAll("init")
	if _, err := CaptureDiff(r.dir, false); err == nil {
		t.Error("expected error for an empty diff")
	}
	if _, err := CaptureDiff(r.dir, true); err == nil {
		t.Error("expected error for an empty staged diff")
	}
	if _, err := CaptureDiff(t.TempDir(), false); err == nil {
		t.Error("expected error outside a git work tree")
	}
}

func TestCaptureDiffStaged(t *testing.T) {
	r := newTestRepo(t)
	r.write("a.txt", []byte("one\n"))
	r.commitAll("init")
	r.write("a.txt", []byte("one\ntwo\n"))
	r.git("add", "a.txt")
	r.write("b.txt", []byte("unrelated\n"))
	r.git("add", "b.txt")
	r.write("a.txt", []byte("one\ntwo\nthree\n")) // unstaged on top

	staged, err := CaptureDiff(r.dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if staged.Additions != 2 || len(staged.Files) != 2 {
		t.Errorf("staged stats: %+v", staged)
	}
	unstaged, err := CaptureDiff(r.dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if unstaged.Additions != 1 || len(unstaged.Files) != 1 {
		t.Errorf("unstaged stats: %+v", unstaged)
	}
}

func TestCaptureDiffStatsAreHunkAware(t *testing.T) {
	r := newTestRepo(t)
	// Lines whose *content* looks like diff headers ("--- ", "+++ ").
	r.write("q.sql", []byte("-- comment\nSELECT 1;\n"))
	r.commitAll("init")
	r.write("q.sql", []byte("++ not a header\nSELECT 1;\n"))

	res, err := CaptureDiff(r.dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Additions != 1 || res.Deletions != 1 {
		t.Errorf("want +1/-1, got +%d/-%d", res.Additions, res.Deletions)
	}
	if len(res.Files) != 1 || res.Files[0] != "q.sql" {
		t.Errorf("files = %v, want [q.sql]", res.Files)
	}
	// And such a patch must still be accepted by the security scan.
	r.git("checkout", "q.sql")
	if _, err := ApplyPatch(r.dir, res.RawDiff); err != nil {
		t.Errorf("legitimate patch with header-like content rejected: %v", err)
	}
}

func TestBinaryPatchRoundTrip(t *testing.T) {
	r := newTestRepo(t)
	orig := []byte{0x00, 0x01, 0x02, 0xff, 0xfe, 'a', 0x00, 'b'}
	r.write("blob.bin", orig)
	r.commitAll("init")
	changed := append(append([]byte{}, orig...), 0x00, 0x10, 0x20, 0x30, 0x40)
	r.write("blob.bin", changed)

	res, err := CaptureDiff(r.dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.RawDiff, "GIT binary patch") {
		t.Fatalf("binary change not captured with --binary:\n%s", res.RawDiff)
	}
	if res.Additions != 0 || res.Deletions != 0 || len(res.Files) != 1 {
		t.Errorf("binary stats: %+v", res)
	}
	r.git("checkout", "blob.bin")
	if _, err := ApplyPatch(r.dir, res.RawDiff); err != nil {
		t.Fatalf("ApplyPatch: %v", err)
	}
	if !bytes.Equal(r.read("blob.bin"), changed) {
		t.Error("binary content differs after round trip")
	}
}

// ---------------------------------------------------------------------------
// ApplyPatch: worktree handling
// ---------------------------------------------------------------------------

func TestApplyPatchFromSubdirectory(t *testing.T) {
	r := newTestRepo(t)
	r.write("root.txt", []byte("r\n"))
	r.write("sub/inner.txt", []byte("i\n"))
	r.commitAll("init")
	r.write("root.txt", []byte("r\nroot change\n"))
	r.write("sub/inner.txt", []byte("i\ninner change\n"))

	res, err := CaptureDiff(filepath.Join(r.dir, "sub"), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 2 {
		t.Fatalf("expected both files captured from a subdirectory, got %v", res.Files)
	}
	r.git("checkout", ".")

	// Applying from the subdirectory must patch files outside of it, too.
	if _, err := ApplyPatch(filepath.Join(r.dir, "sub"), res.RawDiff); err != nil {
		t.Fatalf("ApplyPatch from subdir: %v", err)
	}
	if string(r.read("root.txt")) != "r\nroot change\n" {
		t.Errorf("root.txt was silently skipped: %q", r.read("root.txt"))
	}
	if string(r.read("sub/inner.txt")) != "i\ninner change\n" {
		t.Errorf("sub/inner.txt not patched: %q", r.read("sub/inner.txt"))
	}
}

func TestApplyPatchRequiresGitWorkTree(t *testing.T) {
	notRepo := t.TempDir()
	patch := "diff --git a/new.txt b/new.txt\n" +
		"new file mode 100644\n" +
		"index 0000000..1111111\n" +
		"--- /dev/null\n" +
		"+++ b/new.txt\n" +
		"@@ -0,0 +1 @@\n" +
		"+hello\n"
	_, err := ApplyPatch(notRepo, patch)
	if err == nil || !strings.Contains(err.Error(), "not inside a git work tree") {
		t.Fatalf("expected work-tree error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(notRepo, "new.txt")); statErr == nil {
		t.Error("patch was applied outside a git work tree")
	}
}

func TestApplyPatchToleratesMissingTrailingNewline(t *testing.T) {
	r := newTestRepo(t)
	r.write("a.txt", []byte("one\n"))
	r.commitAll("init")
	r.write("a.txt", []byte("one\ntwo\n"))
	res, err := CaptureDiff(r.dir, false)
	if err != nil {
		t.Fatal(err)
	}
	r.git("checkout", "a.txt")
	if _, err := ApplyPatch(r.dir, strings.TrimRight(res.RawDiff, "\n")); err != nil {
		t.Fatalf("ApplyPatch without trailing newline: %v", err)
	}
	if string(r.read("a.txt")) != "one\ntwo\n" {
		t.Errorf("content = %q", r.read("a.txt"))
	}
}

func TestApplyPatchQuotedUnicodePathIsAccepted(t *testing.T) {
	r := newTestRepo(t)
	r.write("café.txt", []byte("a\n"))
	r.write("with space.txt", []byte("a\n"))
	r.commitAll("init")
	r.write("café.txt", []byte("a\nb\n"))
	r.write("with space.txt", []byte("a\nb\n"))

	// Default quotepath produces C-quoted headers ("caf\303\251.txt").
	patch := r.git("-c", "core.quotepath=true", "diff", "--binary", "--no-color", "--src-prefix=a/", "--dst-prefix=b/")
	if !strings.Contains(patch, `"a/caf\303\251.txt"`) {
		t.Fatalf("test setup: expected quoted header in\n%s", patch)
	}
	r.git("checkout", ".")
	if _, err := ApplyPatch(r.dir, patch); err != nil {
		t.Fatalf("legitimate quoted patch rejected: %v", err)
	}
	if string(r.read("café.txt")) != "a\nb\n" || string(r.read("with space.txt")) != "a\nb\n" {
		t.Error("files not patched")
	}
}

func TestApplyPatchCollisionLeavesTreeUntouched(t *testing.T) {
	r := newTestRepo(t)
	r.write("a.txt", []byte("one\n"))
	r.commitAll("init")
	r.write("a.txt", []byte("one\ntwo\n"))
	res, err := CaptureDiff(r.dir, false)
	if err != nil {
		t.Fatal(err)
	}
	// Tree already contains the change: applying again must fail cleanly.
	if _, err := ApplyPatch(r.dir, res.RawDiff); err == nil {
		t.Fatal("expected collision error")
	}
	if string(r.read("a.txt")) != "one\ntwo\n" {
		t.Error("working tree modified by a failed apply")
	}
}

func TestApplyPatchRejectsEmptyAndOversized(t *testing.T) {
	r := newTestRepo(t)
	r.write("a.txt", []byte("x\n"))
	r.commitAll("init")
	if _, err := ApplyPatch(r.dir, "   \n"); err == nil {
		t.Error("empty patch must be rejected")
	}
	huge := strings.Repeat("x", maxPatchSize+1)
	_, err := ApplyPatch(r.dir, huge)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("oversized patch: got %v", err)
	}
	if _, err := dangerousPatchTargets(r.dir, huge); err == nil {
		t.Error("dangerousPatchTargets must bound patch size before scanning")
	}
}

// ---------------------------------------------------------------------------
// ApplyPatch: security
// ---------------------------------------------------------------------------

func newFilePatch(headerPath, plusPath string) string {
	return "diff --git a/x b/x\n" +
		"new file mode 100644\n" +
		"index 0000000..1111111\n" +
		"--- /dev/null\n" +
		"+++ " + plusPath + "\n" +
		"@@ -0,0 +1 @@\n" +
		"+pwned\n"
}

func TestApplyPatchSecurityRejections(t *testing.T) {
	r := newTestRepo(t)
	r.write("keep.txt", []byte("safe\n"))
	r.write("link-target.txt", []byte("t\n"))
	r.commitAll("init")

	cases := []struct {
		name  string
		patch string
	}{
		{"plain traversal", "diff --git a/../evil.txt b/../evil.txt\nnew file mode 100644\nindex 0000000..1111111\n--- /dev/null\n+++ b/../evil.txt\n@@ -0,0 +1 @@\n+pwned\n"},
		{"deep traversal", newFilePatch("", "b/a/b/../../../evil.txt")},
		{"quoted traversal (C-quote bypass)", "diff --git \"a/../evil.txt\" \"b/../evil.txt\"\nnew file mode 100644\nindex 0000000..1111111\n--- /dev/null\n+++ \"b/../evil.txt\"\n@@ -0,0 +1 @@\n+pwned\n"},
		{"octal-escaped traversal", "diff --git \"a/\\056\\056/evil.txt\" \"b/\\056\\056/evil.txt\"\nnew file mode 100644\nindex 0000000..1111111\n--- /dev/null\n+++ \"b/\\056\\056/evil.txt\"\n@@ -0,0 +1 @@\n+pwned\n"},
		{"backslash traversal", newFilePatch("", "b/..\\evil.txt")},
		{"absolute path", newFilePatch("", "/tmp/termchat-evil.txt")},
		{"git hook", "diff --git a/.git/hooks/pre-commit b/.git/hooks/pre-commit\nnew file mode 100755\nindex 0000000..1111111\n--- /dev/null\n+++ b/.git/hooks/pre-commit\n@@ -0,0 +1,2 @@\n+#!/bin/sh\n+echo pwned\n"},
		{"upper-case .GIT", newFilePatch("", "b/.GIT/hooks/pre-commit")},
		{"mixed-case .Git config", newFilePatch("", "b/.Git/config")},
		{"nested .git", newFilePatch("", "b/vendor/lib/.git/config")},
		{"NTFS trailing dot .git.", newFilePatch("", "b/.git./config")},
		{"NTFS short name GIT~1", newFilePatch("", "b/GIT~1/config")},
		{"quoted .git", "diff --git \"a/.git/config\" \"b/.git/config\"\nnew file mode 100644\nindex 0000000..1111111\n--- /dev/null\n+++ \"b/.git/config\"\n@@ -0,0 +1 @@\n+x\n"},
		{"rename into .git", "diff --git a/keep.txt b/keep.txt\nsimilarity index 100%\nrename from keep.txt\nrename to .git/hooks/post-checkout\n"},
		{"rename out of .git", "diff --git a/x b/keep.txt\nsimilarity index 100%\nrename from .git/config\nrename to keep.txt\n"},
		{"rename to traversal", "diff --git a/keep.txt b/keep.txt\nsimilarity index 100%\nrename from keep.txt\nrename to ../escaped.txt\n"},
		{"quoted rename to traversal", "diff --git a/keep.txt b/keep.txt\nsimilarity index 100%\nrename from keep.txt\nrename to \"../escaped.txt\"\n"},
		{"copy to traversal", "diff --git a/keep.txt b/keep.txt\nsimilarity index 100%\ncopy from keep.txt\ncopy to ../escaped.txt\n"},
		{"malformed quoted rename", "diff --git a/keep.txt b/keep.txt\nsimilarity index 100%\nrename from keep.txt\nrename to \"broken\n"},
		{"symlink creation", "diff --git a/lnk b/lnk\nnew file mode 120000\nindex 0000000..1111111\n--- /dev/null\n+++ b/lnk\n@@ -0,0 +1 @@\n+/etc/passwd\n\\ No newline at end of file\n"},
		{"symlink via index line only", "diff --git a/lnk b/lnk\nindex 1111111..2222222 120000\n--- a/lnk\n+++ b/lnk\n@@ -1 +1 @@\n-a\n\\ No newline at end of file\n+/etc/shadow\n\\ No newline at end of file\n"},
		{"symlink via old mode", "diff --git a/keep.txt b/keep.txt\nold mode 120000\nnew mode 100644\n"},
		{"symlink via new mode", "diff --git a/keep.txt b/keep.txt\nold mode 100644\nnew mode 120000\n"},
		{"symlink deletion", "diff --git a/lnk b/lnk\ndeleted file mode 120000\nindex 1111111..0000000\n--- a/lnk\n+++ /dev/null\n@@ -1 +0,0 @@\n-x\n\\ No newline at end of file\n"},
		{"octal-escaped traversal in mode-only header (numstat layer)", "diff --git \"a/\\056\\056/x\" \"b/\\056\\056/x\"\nold mode 100644\nnew mode 100755\n"},
		{".gitmodules edit", newFilePatch("", "b/.gitmodules")},
		{".GitModules mixed case", newFilePatch("", "b/.GitModules")},
		{"NTFS trailing dot .gitmodules.", newFilePatch("", "b/.gitmodules./x")},
		{"NTFS short name GITMOD~1", newFilePatch("", "b/GITMOD~1")},
		{"nested .gitmodules", newFilePatch("", "b/sub/.gitmodules")},
		{"submodule add (gitlink)", "diff --git a/sub b/sub\nnew file mode 160000\nindex 0000000..1111111\n--- /dev/null\n+++ b/sub\n@@ -0,0 +1 @@\n+Subproject commit 1111111111111111111111111111111111111111\n"},
		{"submodule pointer change (index line)", "diff --git a/sub b/sub\nindex 1111111..2222222 160000\n--- a/sub\n+++ b/sub\n@@ -1 +1 @@\n-Subproject commit 1111111111111111111111111111111111111111\n+Subproject commit 2222222222222222222222222222222222222222\n"},
		{"submodule removal", "diff --git a/sub b/sub\ndeleted file mode 160000\nindex 1111111..0000000\n--- a/sub\n+++ /dev/null\n@@ -1 +0,0 @@\n-Subproject commit 1111111111111111111111111111111111111111\n"},
		{"traditional (non-git) patch", "--- ../evil.txt\n+++ ../evil.txt\n@@ -0,0 +1 @@\n+pwned\n"},
		{"second, traditional section after a git section", "diff --git a/keep.txt b/keep.txt\nindex 1111111..2222222 100644\n--- a/keep.txt\n+++ b/keep.txt\n@@ -1 +1,2 @@\n safe\n+more\n--- /dev/null\n+++ ../evil.txt\n@@ -0,0 +1 @@\n+pwned\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ApplyPatch(r.dir, tc.patch)
			if err == nil {
				t.Fatalf("patch was accepted, want rejection")
			}
			if !strings.Contains(err.Error(), "refusing to apply patch") {
				t.Fatalf("rejected, but not by the security filter: %v", err)
			}
		})
	}

	// Nothing may have escaped the repository or changed inside it.
	for _, name := range []string{"evil.txt", "escaped.txt"} {
		if _, err := os.Stat(filepath.Join(r.root, name)); err == nil {
			t.Errorf("%s was created outside the repository", name)
		}
	}
	if _, err := os.Stat("/tmp/termchat-evil.txt"); err == nil {
		t.Error("/tmp/termchat-evil.txt was created")
		os.Remove("/tmp/termchat-evil.txt")
	}
	if got := r.git("status", "--porcelain"); strings.TrimSpace(got) != "" {
		t.Errorf("working tree changed by rejected patches:\n%s", got)
	}
}

func TestSecurityScanIgnoresHunkContent(t *testing.T) {
	r := newTestRepo(t)
	r.write("doc.md", []byte("text\n"))
	r.commitAll("init")
	// Content lines that merely *look* like headers pointing outside the repo.
	r.write("doc.md", []byte("--- ../notes\n+++ ../notes\nrename to ../x\ntext\n"))
	res, err := CaptureDiff(r.dir, false)
	if err != nil {
		t.Fatal(err)
	}
	offenders, err := dangerousPatchTargets(r.dir, res.RawDiff)
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) != 0 {
		t.Errorf("false positive on hunk content: %v", offenders)
	}
	r.git("checkout", "doc.md")
	if _, err := ApplyPatch(r.dir, res.RawDiff); err != nil {
		t.Errorf("legitimate patch rejected: %v", err)
	}
}

func TestUnsafePathReason(t *testing.T) {
	bad := []string{
		"", "/etc/passwd", "C:/Windows/x", "c:\\x", "\\\\server\\share", "..", "../x", "a/../../x",
		"a/..", ".git", ".git/config", "a/.git/x", ".GIT/x", ".Git", ".git.", ".git ", "GIT~1/x",
		".g\u200cit/x", "a\\..\\b", "nul\x00byte",
	}
	for _, p := range bad {
		if unsafePathReason(p) == "" {
			t.Errorf("unsafePathReason(%q) = ok, want rejection", p)
		}
	}
	good := []string{
		"a.txt", "dir/file.go", ".gitignore", ".github/workflows/ci.yml", "gitcollab/x.go",
		"my.git/x", "a/b/c.txt", "..foo", "foo..", "dir/.gitattributes", "café.txt", "with space.txt",
	}
	for _, p := range good {
		if reason := unsafePathReason(p); reason != "" {
			t.Errorf("unsafePathReason(%q) = %q, want ok", p, reason)
		}
	}
}

// ---------------------------------------------------------------------------
// GetDirtyFiles
// ---------------------------------------------------------------------------

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func TestGetDirtyFilesNULParsing(t *testing.T) {
	r := newTestRepo(t)
	r.write("tracked.txt", []byte("t\n"))
	r.write("old name.txt", []byte("content that is long enough to detect a rename\n"))
	r.commitAll("init")

	r.write("with space.txt", []byte("x\n"))
	r.write("café.txt", []byte("x\n"))
	r.write("we ird -> arrow.txt", []byte("x\n"))
	r.write("newdir/one.go", []byte("x\n"))
	r.write("newdir/nested/two.go", []byte("x\n"))
	r.write(".termchat/room.json", []byte("{}"))
	r.write("tracked.txt", []byte("t\nchanged\n"))
	r.git("mv", "old name.txt", "renamed name.txt")

	got, err := GetDirtyFiles(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"café.txt", "newdir/nested/two.go", "newdir/one.go", "old name.txt",
		"renamed name.txt", "tracked.txt", "we ird -> arrow.txt", "with space.txt",
	}
	if !reflect.DeepEqual(sorted(got), want) {
		t.Errorf("GetDirtyFiles =\n  %q\nwant\n  %q", sorted(got), want)
	}
}

func TestGetDirtyFilesQuoteCharacter(t *testing.T) {
	r := newTestRepo(t)
	r.write("seed.txt", []byte("s\n"))
	r.commitAll("init")
	r.write("say \"hi\".txt", []byte("x\n"))

	got, err := GetDirtyFiles(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "say \"hi\".txt" {
		t.Errorf("got %q", got)
	}
}

func TestGetDirtyFilesFromSubdirAndErrors(t *testing.T) {
	r := newTestRepo(t)
	r.write("sub/a.txt", []byte("a\n"))
	r.commitAll("init")
	r.write("sub/a.txt", []byte("a\nb\n"))
	r.write("top.txt", []byte("t\n"))

	got, err := GetDirtyFiles(filepath.Join(r.dir, "sub"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"sub/a.txt", "top.txt"}; !reflect.DeepEqual(sorted(got), want) {
		t.Errorf("from subdir: got %q, want %q", sorted(got), want)
	}
	if _, err := GetDirtyFiles(t.TempDir()); err == nil {
		t.Error("expected error outside a git work tree")
	}
}

func TestGetDirtyFilesLeavesNoIndexLock(t *testing.T) {
	r := newTestRepo(t)
	r.write("a.txt", []byte("a\n"))
	r.commitAll("init")
	// Make the index stale (same content, new mtime) so a plain `git status`
	// would want to rewrite it.
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(filepath.Join(r.dir, "a.txt"), future, future); err != nil {
		t.Fatal(err)
	}
	// Hold the lock as if the user were mid-commit: the radar must not fail.
	lock := filepath.Join(r.dir, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := GetDirtyFiles(r.dir); err != nil {
		t.Errorf("GetDirtyFiles failed while index.lock was held: %v", err)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Error("GetDirtyFiles removed a lock it did not own")
	}
}

func TestGetCurrentBranch(t *testing.T) {
	r := newTestRepo(t)
	r.write("a.txt", []byte("a\n"))
	r.commitAll("init")
	r.git("checkout", "-b", "feature/x")
	if got := GetCurrentBranch(r.dir); got != "feature/x" {
		t.Errorf("branch = %q", got)
	}
	if got := GetCurrentBranch(t.TempDir()); got != "" {
		t.Errorf("outside a repo branch = %q, want empty", got)
	}
}
