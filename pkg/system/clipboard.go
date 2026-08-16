package system

import (
	"bytes"
	"os/exec"
	"strings"

	"github.com/atotto/clipboard"
)

// ReadClipboard reads clipboard content across Linux, Wayland, and Termux
func ReadClipboard() (string, error) {
	// 1. Try termux-clipboard-get if on Android
	if isCommandAvailable("termux-clipboard-get") {
		cmd := exec.Command("termux-clipboard-get")
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil {
			return strings.TrimRight(out.String(), "\r\n"), nil
		}
	}

	// 2. Try wl-paste if on Wayland
	if isCommandAvailable("wl-paste") {
		cmd := exec.Command("wl-paste", "--no-newline")
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil && out.Len() > 0 {
			return out.String(), nil
		}
	}

	// 3. Fallback to atotto/clipboard or xclip
	return clipboard.ReadAll()
}

// WriteClipboard writes clipboard content across Linux, Wayland, and Termux
func WriteClipboard(text string) error {
	// 1. Try termux-clipboard-set if on Android
	if isCommandAvailable("termux-clipboard-set") {
		cmd := exec.Command("termux-clipboard-set")
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	// 2. Try wl-copy if on Wayland
	if isCommandAvailable("wl-copy") {
		cmd := exec.Command("wl-copy")
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	// 3. Fallback to atotto/clipboard or xclip
	return clipboard.WriteAll(text)
}

func isCommandAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
