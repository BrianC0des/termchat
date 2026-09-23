package workspace

import (
	"os"
	"path/filepath"
	"strings"
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

func TestPassphraseNeverCommittedToRoomJSON(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "termchat-secret-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	cfg, path, err := InitWorkspace(tmpDir, "BrianC0des/test-repo", "test-room", "super-secret-pass", "SHA256:abcd1234", "devchan")
	if err != nil {
		t.Fatalf("InitWorkspace failed: %v", err)
	}
	if cfg.Passphrase != "super-secret-pass" {
		t.Errorf("returned cfg should still have the passphrase in memory, got %q", cfg.Passphrase)
	}

	// The committed room.json must never contain the raw passphrase.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "super-secret-pass") || strings.Contains(string(raw), "passphrase") {
		t.Errorf("room.json leaked the passphrase: %s", string(raw))
	}

	termchatDir := filepath.Dir(path)

	// The secret must live in its own gitignored file with restrictive perms.
	secretInfo, err := os.Stat(secretPath(termchatDir))
	if err != nil {
		t.Fatalf("expected secret file to exist: %v", err)
	}
	if perm := secretInfo.Mode().Perm(); perm != 0600 {
		t.Errorf("secret file perms = %v; want 0600", perm)
	}

	ignoreData, err := os.ReadFile(filepath.Join(termchatDir, ".gitignore"))
	if err != nil {
		t.Fatalf("expected .termchat/.gitignore to exist: %v", err)
	}
	if !strings.Contains(string(ignoreData), SecretFileName) {
		t.Errorf(".termchat/.gitignore does not exclude %s: %s", SecretFileName, string(ignoreData))
	}

	// Round-tripping via LoadConfig should transparently restore the passphrase.
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if loaded.Passphrase != "super-secret-pass" {
		t.Errorf("LoadConfig did not recover passphrase from secret file, got %q", loaded.Passphrase)
	}
}
