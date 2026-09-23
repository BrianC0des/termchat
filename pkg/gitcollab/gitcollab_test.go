package gitcollab

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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
