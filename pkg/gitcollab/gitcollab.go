package gitcollab

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

var (
	patchStore = make(map[string]string)
	patchMu    sync.RWMutex
)

// DiffResult holds summary and raw patch content
type DiffResult struct {
	PatchID   string
	Summary   string
	Additions int
	Deletions int
	Files     []string
	RawDiff   string
}

// GeneratePatchID creates an 8-character hex ID (e.g. 7f8a9b1c)
func GeneratePatchID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// StorePatch saves a patch in memory for quick retrieval
func StorePatch(patchID, content string) {
	patchMu.Lock()
	defer patchMu.Unlock()
	patchStore[patchID] = content
}

// GetPatch retrieves a stored patch by ID
func GetPatch(patchID string) (string, bool) {
	patchMu.RLock()
	defer patchMu.RUnlock()
	p, ok := patchStore[patchID]
	return p, ok
}

// CaptureDiff extracts git diff or git diff --staged from target directory
func CaptureDiff(dir string, staged bool) (*DiffResult, error) {
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}

	args := []string{"-C", dir, "diff"}
	if staged {
		args = append(args, "--staged")
	}

	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff failed: %v", err)
	}

	raw := string(out)
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("no uncommitted changes found in repository")
	}

	// Calculate stats
	additions := 0
	deletions := 0
	fileMap := make(map[string]bool)

	lines := strings.Split(raw, "\n")
	for _, l := range lines {
		if strings.HasPrefix(l, "diff --git ") {
			parts := strings.Fields(l)
			if len(parts) >= 4 {
				targetFile := parts[3]
				if idx := strings.Index(targetFile, "/"); idx != -1 {
					targetFile = targetFile[idx+1:]
				}
				fileMap[targetFile] = true
			}
		} else if strings.HasPrefix(l, "+++ ") {
			targetFile := strings.TrimPrefix(l, "+++ ")
			if idx := strings.Index(targetFile, "/"); idx != -1 {
				targetFile = targetFile[idx+1:]
			}
			if targetFile != "dev/null" && targetFile != "" {
				fileMap[targetFile] = true
			}
		} else if strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++") {
			additions++
		} else if strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---") {
			deletions++
		}
	}

	var files []string
	for f := range fileMap {
		files = append(files, f)
	}

	patchID := GeneratePatchID()
	StorePatch(patchID, raw)

	summary := fmt.Sprintf("+%d / -%d across %d file(s)", additions, deletions, len(files))
	return &DiffResult{
		PatchID:   patchID,
		Summary:   summary,
		Additions: additions,
		Deletions: deletions,
		Files:     files,
		RawDiff:   raw,
	}, nil
}

// ApplyPatch applies patch content to the local repository directory
func ApplyPatch(dir string, patchContent string) (string, error) {
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}

	// 1. Dry run check
	checkCmd := exec.Command("git", "-C", dir, "apply", "--check", "-")
	checkCmd.Stdin = strings.NewReader(patchContent)
	var checkErr bytes.Buffer
	checkCmd.Stderr = &checkErr

	if err := checkCmd.Run(); err != nil {
		return "", fmt.Errorf("patch collision / cannot apply cleanly: %s", strings.TrimSpace(checkErr.String()))
	}

	// 2. Apply cleanly
	applyCmd := exec.Command("git", "-C", dir, "apply", "-")
	applyCmd.Stdin = strings.NewReader(patchContent)
	var applyErr bytes.Buffer
	applyCmd.Stderr = &applyErr

	if err := applyCmd.Run(); err != nil {
		return "", fmt.Errorf("git apply failed: %s", strings.TrimSpace(applyErr.String()))
	}

	return "Patch applied cleanly to your local workspace.", nil
}
