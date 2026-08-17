package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RoomConfig defines project-bound collab room metadata
type RoomConfig struct {
	Repo        string          `json:"repo,omitempty"`
	Room        string          `json:"room"`
	Passphrase  string          `json:"passphrase,omitempty"`
	Relay       string          `json:"relay,omitempty"`
	AutoConnect bool            `json:"auto_connect"`
	Features    map[string]bool `json:"features,omitempty"`
}

const (
	DirName  = ".termchat"
	FileName = "room.json"
)

// FindWorkspace searches upward from startDir for .termchat/room.json or .termchat.json
func FindWorkspace(startDir string) (*RoomConfig, string, error) {
	if startDir == "" {
		var err error
		startDir, err = os.Getwd()
		if err != nil {
			return nil, "", err
		}
	}

	curr := startDir
	for {
		// 1. Check .termchat/room.json
		p1 := filepath.Join(curr, DirName, FileName)
		if info, err := os.Stat(p1); err == nil && !info.IsDir() {
			cfg, err := LoadConfig(p1)
			if err == nil {
				return cfg, curr, nil
			}
		}

		// 2. Check .termchat.json
		p2 := filepath.Join(curr, ".termchat.json")
		if info, err := os.Stat(p2); err == nil && !info.IsDir() {
			cfg, err := LoadConfig(p2)
			if err == nil {
				return cfg, curr, nil
			}
		}

		parent := filepath.Dir(curr)
		if parent == curr || parent == "" {
			break
		}
		curr = parent
	}

	return nil, "", errors.New("no termchat project workspace found")
}

// LoadConfig reads and parses a room configuration JSON file
func LoadConfig(filePath string) (*RoomConfig, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var cfg RoomConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.Relay == "" {
		cfg.Relay = "wss://termchat-o51d.onrender.com/ws"
	}
	return &cfg, nil
}

// InitWorkspace creates .termchat/room.json in targetDir
func InitWorkspace(targetDir, repo, room, pass string) (*RoomConfig, string, error) {
	if targetDir == "" {
		var err error
		targetDir, err = os.Getwd()
		if err != nil {
			return nil, "", err
		}
	}

	if repo == "" {
		repo, _ = DetectGitRepo(targetDir)
	}

	if room == "" {
		if repo != "" {
			parts := strings.Split(repo, "/")
			room = parts[len(parts)-1] + "-collab"
		} else {
			dirName := filepath.Base(targetDir)
			room = dirName + "-collab"
		}
	}

	cfg := RoomConfig{
		Repo:        repo,
		Room:        room,
		Passphrase:  pass,
		Relay:       "wss://termchat-o51d.onrender.com/ws",
		AutoConnect: true,
		Features: map[string]bool{
			"git_diff_sharing": true,
			"ci_alerts":        true,
			"pr_cards":         true,
		},
	}

	termchatDir := filepath.Join(targetDir, DirName)
	if err := os.MkdirAll(termchatDir, 0755); err != nil {
		return nil, "", err
	}

	filePath := filepath.Join(termchatDir, FileName)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, "", err
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return nil, "", err
	}

	return &cfg, filePath, nil
}

// DetectGitRepo tries to parse the git remote origin (e.g. owner/repo)
func DetectGitRepo(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	raw := strings.TrimSpace(string(out))
	return ParseGitRemote(raw)
}

// ParseGitRemote extracts "owner/repo" from SSH or HTTPS URLs
func ParseGitRemote(raw string) (string, error) {
	raw = strings.TrimSuffix(raw, ".git")
	if strings.HasPrefix(raw, "git@github.com:") {
		return strings.TrimPrefix(raw, "git@github.com:"), nil
	}
	if strings.HasPrefix(raw, "https://github.com/") {
		return strings.TrimPrefix(raw, "https://github.com/"), nil
	}
	if strings.HasPrefix(raw, "http://github.com/") {
		return strings.TrimPrefix(raw, "http://github.com/"), nil
	}
	// General owner/repo if contains slash
	if strings.Count(raw, "/") == 1 && !strings.Contains(raw, ":") {
		return raw, nil
	}
	return "", fmt.Errorf("could not parse git remote: %s", raw)
}

// GetCurrentBranch returns the active git branch name
func GetCurrentBranch(dir string) string {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD")
	if out, err := cmd.Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}
