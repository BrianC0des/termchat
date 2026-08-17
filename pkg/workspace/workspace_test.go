package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseGitRemote(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		err      bool
	}{
		{"git@github.com:BrianC0des/termchat.git", "BrianC0des/termchat", false},
		{"https://github.com/BrianC0des/termchat.git", "BrianC0des/termchat", false},
		{"https://github.com/BrianC0des/termchat", "BrianC0des/termchat", false},
		{"owner/repo", "owner/repo", false},
	}

	for _, tt := range tests {
		res, err := ParseGitRemote(tt.input)
		if (err != nil) != tt.err {
			t.Errorf("ParseGitRemote(%q) unexpected err: %v", tt.input, err)
		}
		if res != tt.expected {
			t.Errorf("ParseGitRemote(%q) = %q; want %q", tt.input, res, tt.expected)
		}
	}
}

func TestInitAndFindWorkspace(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "termchat-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Subfolder to test upward traversal
	subDir := filepath.Join(tmpDir, "pkg", "ui")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	cfg, path, err := InitWorkspace(tmpDir, "BrianC0des/test-repo", "test-room", "secret123", "SHA256:abcd1234", "devchan")
	if err != nil {
		t.Fatalf("InitWorkspace failed: %v", err)
	}
	if cfg.Room != "test-room" || cfg.Repo != "BrianC0des/test-repo" || cfg.CreatorFingerprint != "SHA256:abcd1234" {
		t.Errorf("InitWorkspace cfg mismatch: %+v", cfg)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("room.json file not found at %s", path)
	}

	// Find from subDir
	foundCfg, root, err := FindWorkspace(subDir)
	if err != nil {
		t.Fatalf("FindWorkspace failed from subDir: %v", err)
	}
	if root != tmpDir {
		t.Errorf("FindWorkspace root = %s; want %s", root, tmpDir)
	}
	if foundCfg.Room != "test-room" {
		t.Errorf("FindWorkspace Room = %s; want test-room", foundCfg.Room)
	}
}
