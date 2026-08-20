package system

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/atotto/clipboard"
)

// isTermux detects if we are running inside Android Termux
func isTermux() bool {
	return os.Getenv("TERMUX_VERSION") != "" ||
		os.Getenv("PREFIX") == "/data/data/com.termux/files/usr" ||
		fileExists("/data/data/com.termux")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// runWithTimeout runs a command and kills it if it exceeds the timeout.
// This prevents termux-clipboard-* from hanging when Termux:API app is missing.
func runWithTimeout(timeout time.Duration, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var out bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf

	if err := cmd.Start(); err != nil {
		return "", err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil {
			return "", fmt.Errorf("%s: %s", err, errBuf.String())
		}
		return out.String(), nil
	case <-time.After(timeout):
		cmd.Process.Kill()
		return "", fmt.Errorf("command timed out after %s (Termux:API app may not be installed)", timeout)
	}
}

// ReadClipboard reads clipboard content across Linux, Wayland, macOS, and Termux
func ReadClipboard() (string, error) {
	// 1. Termux: use termux-clipboard-get with a strict 3s timeout
	//    to avoid hanging when Termux:API app is missing
	if isTermux() {
		if isCommandAvailable("termux-clipboard-get") {
			text, err := runWithTimeout(3*time.Second, "termux-clipboard-get")
			if err == nil {
				return strings.TrimRight(text, "\r\n"), nil
			}
		}
		// No Termux:API available — return empty gracefully, do NOT crash
		return "", fmt.Errorf("clipboard unavailable: install Termux:API app and run 'pkg install termux-api'")
	}

	// 2. macOS
	if runtime.GOOS == "darwin" {
		if isCommandAvailable("pbpaste") {
			cmd := exec.Command("pbpaste")
			var out bytes.Buffer
			cmd.Stdout = &out
			if err := cmd.Run(); err == nil {
				return out.String(), nil
			}
		}
	}

	// 3. Wayland
	if isCommandAvailable("wl-paste") {
		cmd := exec.Command("wl-paste", "--no-newline")
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil && out.Len() > 0 {
			return out.String(), nil
		}
	}

	// 4. X11 / Windows fallback via atotto/clipboard
	return clipboard.ReadAll()
}

// WriteClipboard writes clipboard content across Linux, Wayland, macOS, and Termux
func WriteClipboard(text string) error {
	// 1. Termux: use termux-clipboard-set with a strict 3s timeout
	//    to avoid hanging when Termux:API app is missing
	if isTermux() {
		if isCommandAvailable("termux-clipboard-set") {
			cmd := exec.Command("termux-clipboard-set")
			cmd.Stdin = strings.NewReader(text)
			done := make(chan error, 1)
			if err := cmd.Start(); err == nil {
				go func() { done <- cmd.Wait() }()
				select {
				case err := <-done:
					if err == nil {
						return nil
					}
				case <-time.After(3 * time.Second):
					cmd.Process.Kill()
				}
			}
		}
		// Termux:API not available — return a friendly error, do NOT crash
		return fmt.Errorf("clipboard unavailable: install Termux:API app and run 'pkg install termux-api'")
	}

	// 2. macOS
	if runtime.GOOS == "darwin" {
		if isCommandAvailable("pbcopy") {
			cmd := exec.Command("pbcopy")
			cmd.Stdin = strings.NewReader(text)
			if err := cmd.Run(); err == nil {
				return nil
			}
		}
	}

	// 3. Wayland
	if isCommandAvailable("wl-copy") {
		cmd := exec.Command("wl-copy")
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	// 4. X11 / Windows fallback via atotto/clipboard
	return clipboard.WriteAll(text)
}

func isCommandAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
