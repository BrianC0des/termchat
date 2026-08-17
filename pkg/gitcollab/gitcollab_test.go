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
