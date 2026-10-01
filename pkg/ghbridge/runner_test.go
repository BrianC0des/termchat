package ghbridge

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"termchat/pkg/ghauth"
)

// fakeGH installs a stub `gh` first on PATH. The script body is a POSIX sh
// program; "$@" are the gh arguments.
func fakeGH(t *testing.T, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script gh stub is unix only")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// isolate keeps the tests away from the developer's real credentials.
func isolate(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
}

func TestRunGHNotInstalled(t *testing.T) {
	isolate(t)
	t.Setenv("PATH", t.TempDir()) // no gh anywhere
	if _, err := runGH("pr", "list"); !errors.Is(err, ErrGHNotInstalled) {
		t.Fatalf("got %v, want ErrGHNotInstalled", err)
	}
	if _, err := CheckoutPR(1); !errors.Is(err, ErrGHNotInstalled) {
		t.Fatalf("CheckoutPR: got %v, want ErrGHNotInstalled", err)
	}
}

func TestRunGHInjectsTermchatToken(t *testing.T) {
	isolate(t)
	// `gh auth token` must fail so ghauth falls through to termchat's own store.
	fakeGH(t, `[ "$1" = "auth" ] && exit 1
printf '%s|%s|%s' "$GH_TOKEN" "$GH_PROMPT_DISABLED" "$*"`)
	if err := ghauth.SaveToken("alice", "tok-from-termchat", nil); err != nil {
		t.Fatal(err)
	}
	out, err := runGH("issue", "list")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(out); got != "tok-from-termchat|1|issue list" {
		t.Errorf("gh saw %q", got)
	}
	if strings.Contains(string(out), "tok-from-termchat|1|issue list tok") {
		t.Error("token leaked onto the command line")
	}
}

func TestRunGHDoesNotOverrideEnvToken(t *testing.T) {
	isolate(t)
	t.Setenv("GITHUB_TOKEN", "env-token")
	fakeGH(t, `printf '%s|%s' "$GH_TOKEN" "$GITHUB_TOKEN"`)
	if err := ghauth.SaveToken("alice", "tok-from-termchat", nil); err != nil {
		t.Fatal(err)
	}
	out, err := runGH("pr", "list")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(out); got != "|env-token" {
		t.Errorf("gh saw GH_TOKEN|GITHUB_TOKEN = %q, want the env token untouched and nothing injected", got)
	}
}

func TestRunGHNoTokenInjectedWithoutLogin(t *testing.T) {
	isolate(t)
	fakeGH(t, `[ "$1" = "auth" ] && exit 1
printf '[%s]' "$GH_TOKEN"`)
	out, err := runGH("pr", "list")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "[]" {
		t.Errorf("unexpected GH_TOKEN %q", out)
	}
}

func TestRunGHTimeout(t *testing.T) {
	isolate(t)
	fakeGH(t, `[ "$1" = "auth" ] && exit 1
exec sleep 10`)
	old := ghTimeout
	ghTimeout = 300 * time.Millisecond
	defer func() { ghTimeout = old }()

	start := time.Now()
	_, err := runGH("run", "list")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("got %v, want a timeout error", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("timeout did not kill the process promptly")
	}
}

func TestCheckoutPRUsesActiveDirectory(t *testing.T) {
	isolate(t)
	fakeGH(t, `[ "$1" = "auth" ] && exit 1
pwd; echo "$*"`)
	dir := t.TempDir()
	prev, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil { // what /cd and /repo switch do
		t.Fatal(err)
	}
	defer os.Chdir(prev)

	out, err := CheckoutPR(12)
	if err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(dir)
	if !strings.Contains(out, real) || !strings.Contains(out, "pr checkout 12") {
		t.Errorf("gh ran with %q; want cwd %s and 'pr checkout 12'", out, real)
	}
}

func TestCheckoutPRFailureMessage(t *testing.T) {
	isolate(t)
	fakeGH(t, `[ "$1" = "auth" ] && exit 1
echo "no pull requests found" >&2; exit 1`)
	_, err := CheckoutPR(99)
	if err == nil || !strings.Contains(err.Error(), "gh pr checkout failed: no pull requests found") {
		t.Errorf("got %v", err)
	}
}
