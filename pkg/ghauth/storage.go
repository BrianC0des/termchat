// Package ghauth provides optional GitHub authentication for TermChat's
// Git & GitHub Collab Hub features. It resolves credentials through a
// 4-tier hierarchy (env var > gh CLI > termchat's own store > guest) and
// implements the RFC 8628 OAuth Device Authorization Grant so a user can
// log in without ever pasting a token.
package ghauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNoToken is returned by GetToken when no credentials are available
// from any tier of the resolution hierarchy.
var ErrNoToken = errors.New("ghauth: no github token available")

// Credentials is the on-disk shape of ~/.config/termchat/hosts.json.
type Credentials struct {
	User   string   `json:"user"`
	Token  string   `json:"token"`
	Scopes []string `json:"scopes,omitempty"`
}

// Source identifies which tier of the resolution hierarchy a token came
// from. Callers can use this to decide whether to display "logged in via
// gh CLI" vs "logged in via termchat", etc.
type Source string

const (
	SourceEnv      Source = "env"
	SourceGHCLI    Source = "gh-cli"
	SourceTermChat Source = "termchat"
	SourceNone     Source = "none"
)

// Resolved is the result of running the full resolution hierarchy.
type Resolved struct {
	Token  string
	Source Source
}

// configDir returns ~/.config/termchat (or the OS equivalent via
// os.UserConfigDir()), creating it if it does not already exist.
func configDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", fmt.Errorf("ghauth: resolve config dir: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	dir := filepath.Join(base, "termchat")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("ghauth: create config dir: %w", err)
	}
	return dir, nil
}

// hostsPath returns the path to termchat's own credential file:
// ~/.config/termchat/hosts.json
func hostsPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "hosts.json"), nil
}

// GetToken resolves a GitHub token using the 4-tier hierarchy:
//  1. GITHUB_TOKEN or GH_TOKEN environment variable
//  2. Existing GitHub CLI credentials (gh auth token / ~/.config/gh/hosts.yml)
//  3. termchat's own store (~/.config/termchat/hosts.json)
//  4. Guest / unauthenticated (returns ErrNoToken)
func GetToken() (Resolved, error) {
	if tok := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); tok != "" {
		return Resolved{Token: tok, Source: SourceEnv}, nil
	}
	if tok := strings.TrimSpace(os.Getenv("GH_TOKEN")); tok != "" {
		return Resolved{Token: tok, Source: SourceEnv}, nil
	}

	if tok, err := ghCLIToken(); err == nil && tok != "" {
		return Resolved{Token: tok, Source: SourceGHCLI}, nil
	}

	if creds, err := loadTermChatCreds(); err == nil && creds != nil && creds.Token != "" {
		return Resolved{Token: creds.Token, Source: SourceTermChat}, nil
	}

	return Resolved{Source: SourceNone}, ErrNoToken
}

// ghCLIToken attempts to reuse an existing `gh` CLI login. It first tries
// invoking `gh auth token`, which is the officially supported way to read
// the active token without touching gh's internal files directly. If the
// gh binary isn't available, it falls back to parsing
// ~/.config/gh/hosts.yml for a github.com oauth_token entry.
var ghCLIRunner = ghCLITokenDefault

func ghCLITokenDefault() (string, error) {
	if path, err := exec.LookPath("gh"); err == nil {
		cmd := exec.Command(path, "auth", "token")
		out, err := cmd.Output()
		if err == nil {
			tok := strings.TrimSpace(string(out))
			if tok != "" {
				return tok, nil
			}
		}
	}
	return ghHostsYAMLToken()
}

func ghCLIToken() (string, error) {
	return ghCLIRunner()
}

// ghHostsYAMLToken performs a minimal, dependency-free parse of gh CLI's
// hosts.yml looking for a github.com "oauth_token:" line. It intentionally
// avoids pulling in a YAML library to keep this package dependency-free;
// gh's hosts.yml format for this purpose is a simple flat mapping.
func ghHostsYAMLToken() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, ".config", "gh", "hosts.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	lines := strings.Split(string(data), "\n")
	inGitHubBlock := false
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t\r")
		if trimmed == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))

		if indent == 0 {
			key := strings.TrimSuffix(strings.TrimSpace(trimmed), ":")
			inGitHubBlock = key == "github.com"
			continue
		}

		if inGitHubBlock && strings.Contains(trimmed, "oauth_token:") {
			parts := strings.SplitN(trimmed, "oauth_token:", 2)
			if len(parts) == 2 {
				tok := strings.TrimSpace(parts[1])
				tok = strings.Trim(tok, `"'`)
				if tok != "" {
					return tok, nil
				}
			}
		}
	}
	return "", errors.New("ghauth: no oauth_token found in gh hosts.yml")
}

// loadTermChatCreds reads and parses termchat's own credential file.
// A missing file is not an error; it simply yields (nil, nil).
func loadTermChatCreds() (*Credentials, error) {
	path, err := hostsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("ghauth: read hosts.json: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("ghauth: parse hosts.json: %w", err)
	}
	return &creds, nil
}

// SaveToken persists a token to termchat's own credential store at
// ~/.config/termchat/hosts.json with strict 0600 permissions.
func SaveToken(user, token string, scopes []string) error {
	if strings.TrimSpace(token) == "" {
		return errors.New("ghauth: refusing to save empty token")
	}
	path, err := hostsPath()
	if err != nil {
		return err
	}
	creds := Credentials{User: user, Token: token, Scopes: scopes}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("ghauth: marshal credentials: %w", err)
	}

	// Write with 0600 from the start (WriteFile applies the mode subject
	// to umask on creation) and then explicitly Chmod to guarantee 0600
	// even if the file already existed with looser permissions or the
	// umask loosened the initial mode.
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("ghauth: write hosts.json: %w", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		return fmt.Errorf("ghauth: chmod hosts.json: %w", err)
	}
	return nil
}

// ClearToken removes termchat's own stored credentials, logging the user
// out of termchat's GitHub integration. It does not affect GITHUB_TOKEN,
// GH_TOKEN, or the gh CLI's own login state. A missing file is not an
// error.
func ClearToken() error {
	path, err := hostsPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("ghauth: remove hosts.json: %w", err)
	}
	return nil
}

// GetAuthenticatedUser returns the username associated with termchat's
// own stored credentials (tier 3). It returns "" with no error if there
// is no termchat-managed session, since env-var and gh-CLI tokens don't
// have a locally cached username without an API call (see FetchUser in
// device.go for that).
func GetAuthenticatedUser() (string, error) {
	creds, err := loadTermChatCreds()
	if err != nil {
		return "", err
	}
	if creds == nil {
		return "", nil
	}
	return creds.User, nil
}
