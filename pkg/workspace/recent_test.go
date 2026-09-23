package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePath(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd failed: %v", err)
	}

	res, err := ResolvePath("")
	if err != nil || res != wd {
		t.Errorf("expected %s, got %s (err: %v)", wd, res, err)
	}

	tempDir := t.TempDir()
	resTemp, err := ResolvePath(tempDir)
	if err != nil || resTemp != tempDir {
		t.Errorf("expected %s, got %s (err: %v)", tempDir, resTemp, err)
	}

	nonExistent := filepath.Join(tempDir, "does-not-exist-12345")
	if _, err := ResolvePath(nonExistent); err == nil {
		t.Errorf("expected error for non-existent path, got nil")
	}
}

func TestRecentRepos(t *testing.T) {
	fakeConfig := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", fakeConfig)

	dirA := t.TempDir()
	dirB := t.TempDir()

	if err := AddRecentRepo(dirA); err != nil {
		t.Fatalf("AddRecentRepo failed: %v", err)
	}
	if err := AddRecentRepo(dirB); err != nil {
		t.Fatalf("AddRecentRepo failed: %v", err)
	}

	repos, err := GetRecentRepos()
	if err != nil {
		t.Fatalf("GetRecentRepos failed: %v", err)
	}
	if len(repos) < 2 {
		t.Fatalf("expected at least 2 repos, got %d", len(repos))
	}
	if repos[0] != dirB {
		t.Errorf("expected dirB at top of recents, got %s", repos[0])
	}
}
