package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const RecentReposFileName = "recent_repos.json"

func recentReposPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		home, hErr := os.UserHomeDir()
		if hErr != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	dir := filepath.Join(base, "termchat")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return filepath.Join(dir, RecentReposFileName), nil
}

// GetRecentRepos returns the list of valid, existing recent repositories.
func GetRecentRepos() ([]string, error) {
	p, err := recentReposPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var repos []string
	if err := json.Unmarshal(data, &repos); err != nil {
		return nil, err
	}

	// Filter out non-existent directories
	var valid []string
	for _, r := range repos {
		if info, err := os.Stat(r); err == nil && info.IsDir() {
			valid = append(valid, r)
		}
	}
	return valid, nil
}

// AddRecentRepo adds or moves a directory to the top of the recent repos list.
func AddRecentRepo(dir string) error {
	abs, err := ResolvePath(dir)
	if err != nil {
		return err
	}

	p, err := recentReposPath()
	if err != nil {
		return err
	}

	repos, _ := GetRecentRepos()
	var updated []string
	updated = append(updated, abs)
	for _, r := range repos {
		if r != abs {
			updated = append(updated, r)
		}
		if len(updated) >= 10 {
			break
		}
	}

	data, err := json.MarshalIndent(updated, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0600)
}

// ResolvePath expands ~ and resolves relative paths to clean absolute paths.
func ResolvePath(input string) (string, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return os.Getwd()
	}
	if strings.HasPrefix(s, "~/") || s == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if s == "~" {
			s = home
		} else {
			s = filepath.Join(home, s[2:])
		}
	}
	abs, err := filepath.Abs(s)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("path does not exist: %s", abs)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", abs)
	}
	return filepath.Clean(abs), nil
}

// IsGitRepo checks if a directory is inside a Git repository work tree.
func IsGitRepo(dir string) bool {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}
