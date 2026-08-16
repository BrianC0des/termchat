package system

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// IsAGYInstalled checks if the agy CLI binary is present
func IsAGYInstalled() bool {
	_, err := exec.LookPath("agy")
	return err == nil
}

// QueryAGY executes a prompt using the local Antigravity CLI and returns the AI response
func QueryAGY(userPrompt string) (string, error) {
	agyPath, err := exec.LookPath("agy")
	if err != nil {
		return "", fmt.Errorf("Antigravity CLI (agy) not found in PATH")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	fullPrompt := fmt.Sprintf("You are AGY, an intelligent AI assistant in TermChat connecting a user's PC and Android phone. If the user asks you to perform a task, execute commands, or check system status, use your tools directly. Provide a clear, direct, and concise chat response.\n\nUser: %s", userPrompt)

	cmd := exec.CommandContext(ctx, agyPath, "--dangerously-skip-permissions", "-p", fullPrompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	out := strings.TrimSpace(stdout.String())
	errOut := strings.TrimSpace(stderr.String())

	if err != nil {
		if out != "" {
			return out, nil
		}
		if errOut != "" {
			return "", fmt.Errorf("%s", errOut)
		}
		return "", err
	}

	if out == "" && errOut != "" {
		return errOut, nil
	}

	return out, nil
}
