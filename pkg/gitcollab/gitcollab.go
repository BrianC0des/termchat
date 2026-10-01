package gitcollab

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// maxPatchSize bounds any patch we will generate, store, scan or apply.
	maxPatchSize = 15 << 20 // 15 MB
	// maxStoredPatches / maxStoredBytes bound the in-memory patch store.
	maxStoredPatches = 100
	maxStoredBytes   = 64 << 20 // 64 MB total
	// gitTimeout bounds every git subprocess so a hung git can never wedge
	// the UI or pile up goroutines from the background radar.
	gitTimeout      = 30 * time.Second
	maxStatusOutput = 32 << 20
)

var (
	errOutputTooLarge = errors.New("git output exceeds size limit")
	errPatchTooLarge  = fmt.Errorf("patch is too large (limit %d MB)", maxPatchSize>>20)
)

var (
	// patchStore is a bounded FIFO: patchOrder records insertion order so the
	// oldest entries are evicted first. All fields are guarded by patchMu.
	patchStore = make(map[string]string)
	patchOrder []string
	patchBytes int
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

var idFallbackCounter atomic.Uint64

func generatePatchID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("could not generate patch id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// GeneratePatchID creates a 16-character hex ID (8 random bytes, e.g.
// 7f8a9b1c2d3e4f50). If the system CSPRNG is unavailable it falls back to a
// hash of the clock and a process-wide counter, which is unique but not
// unpredictable; CaptureDiff itself fails closed instead of using it.
func GeneratePatchID() string {
	if id, err := generatePatchID(); err == nil {
		return id
	}
	h := sha256.Sum256([]byte(fmt.Sprintf("%d-%d", time.Now().UnixNano(), idFallbackCounter.Add(1))))
	return hex.EncodeToString(h[:8])
}

// StorePatch saves a patch in memory for quick retrieval. The store is a
// FIFO bounded to maxStoredPatches entries and maxStoredBytes total. An
// existing ID is never overwritten with different content (so a stored patch
// cannot be swapped underneath a user who already reviewed its summary).
// It reports whether the patch is now stored under patchID.
func StorePatch(patchID, content string) bool {
	if patchID == "" || len(content) > maxPatchSize {
		return false
	}
	patchMu.Lock()
	defer patchMu.Unlock()

	if existing, ok := patchStore[patchID]; ok {
		return existing == content
	}
	for len(patchOrder) > 0 &&
		(len(patchOrder) >= maxStoredPatches || patchBytes+len(content) > maxStoredBytes) {
		oldest := patchOrder[0]
		patchOrder = patchOrder[1:]
		patchBytes -= len(patchStore[oldest])
		delete(patchStore, oldest)
	}
	patchStore[patchID] = content
	patchOrder = append(patchOrder, patchID)
	patchBytes += len(content)
	return true
}

// GetPatch retrieves a stored patch by ID
func GetPatch(patchID string) (string, bool) {
	patchMu.RLock()
	defer patchMu.RUnlock()
	p, ok := patchStore[patchID]
	return p, ok
}

// ---------------------------------------------------------------------------
// git subprocess helpers
// ---------------------------------------------------------------------------

// limitedBuffer is an io.Writer with a size cap. With truncate=false, going
// over the cap cancels the command (so a runaway git cannot block on a full
// pipe) and marks the buffer as over; with truncate=true excess is dropped.
type limitedBuffer struct {
	buf      bytes.Buffer
	max      int
	truncate bool
	over     bool
	cancel   context.CancelFunc
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if l.buf.Len()+len(p) > l.max {
		if l.truncate {
			if room := l.max - l.buf.Len(); room > 0 {
				l.buf.Write(p[:room])
			}
			return len(p), nil
		}
		l.over = true
		if l.cancel != nil {
			l.cancel()
		}
		return 0, errOutputTooLarge
	}
	return l.buf.Write(p)
}

// runGit runs `git [-C dir] args...` with a timeout and a stdout size cap. It
// returns stdout on success; on failure it returns the (trimmed) stderr text
// alongside the error so callers can show git's own explanation.
func runGit(dir string, stdin []byte, maxOut int, args ...string) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	full := args
	if dir != "" {
		full = append([]string{"-C", dir}, args...)
	}
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	stdout := &limitedBuffer{max: maxOut, cancel: cancel}
	stderr := &limitedBuffer{max: 64 << 10, truncate: true}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	errText := strings.TrimSpace(stderr.buf.String())
	if stdout.over {
		return nil, errText, errOutputTooLarge
	}
	if ctx.Err() == context.DeadlineExceeded {
		return nil, errText, fmt.Errorf("git %s timed out after %s", strings.Join(args, " "), gitTimeout)
	}
	if err != nil {
		return nil, errText, err
	}
	return stdout.buf.Bytes(), errText, nil
}

func failText(stderr string, err error) string {
	if stderr != "" {
		return stderr
	}
	return err.Error()
}

// repoTopLevel resolves the top-level directory of the git work tree that
// contains dir. It fails if dir is not inside a work tree.
func repoTopLevel(dir string) (string, error) {
	out, stderr, err := runGit(dir, nil, 64<<10, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%q is not inside a git work tree: %s", dir, failText(stderr, err))
	}
	top := strings.TrimRight(string(out), "\r\n")
	if top == "" {
		return "", fmt.Errorf("%q is not inside a git work tree", dir)
	}
	return top, nil
}

func resolveDir(dir string) (string, error) {
	if dir != "" {
		return dir, nil
	}
	return os.Getwd()
}

// ---------------------------------------------------------------------------
// numstat (authoritative file list + stats, as git itself parses the patch)
// ---------------------------------------------------------------------------

type numstatEntry struct {
	Added, Deleted int
	Binary         bool
	Path           string
}

// patchNumstat asks git to parse patch and report, per file, the exact
// (unquoted, NUL-delimited) target path and line counts. Using git's own
// parser avoids every C-quoting / whitespace ambiguity of hand-rolled parsing.
func patchNumstat(dir, patch string) ([]numstatEntry, error) {
	out, stderr, err := runGit(dir, []byte(patch), 4<<20, "apply", "--numstat", "-z", "-")
	if err != nil {
		return nil, fmt.Errorf("could not parse patch: %s", failText(stderr, err))
	}
	var entries []numstatEntry
	for _, rec := range strings.Split(string(out), "\x00") {
		if rec == "" {
			continue
		}
		f := strings.SplitN(rec, "\t", 3)
		if len(f) != 3 {
			return nil, fmt.Errorf("could not parse patch: unexpected numstat record %q", rec)
		}
		e := numstatEntry{Path: f[2]}
		if f[0] == "-" && f[1] == "-" {
			e.Binary = true
		} else {
			a, errA := strconv.Atoi(f[0])
			d, errD := strconv.Atoi(f[1])
			if errA != nil || errD != nil {
				return nil, fmt.Errorf("could not parse patch: bad numstat counts in %q", rec)
			}
			e.Added, e.Deleted = a, d
		}
		entries = append(entries, e)
	}
	if len(entries) == 0 {
		return nil, errors.New("could not parse patch: it contains no file changes")
	}
	return entries, nil
}

// CaptureDiff extracts git diff or git diff --staged from target directory.
// The diff is generated with flags that make it independent of the user's git
// configuration (colors, external diff drivers, prefix settings) and able to
// carry binary files, so it can be applied by any peer with `git apply`.
func CaptureDiff(dir string, staged bool) (*DiffResult, error) {
	dir, err := resolveDir(dir)
	if err != nil {
		return nil, err
	}
	top, err := repoTopLevel(dir)
	if err != nil {
		return nil, err
	}

	args := []string{
		"-c", "core.quotepath=false",
		"diff", "--binary", "--no-color", "--no-ext-diff", "--no-textconv",
		"--src-prefix=a/", "--dst-prefix=b/",
	}
	if staged {
		args = append(args, "--staged")
	}

	out, stderr, err := runGit(top, nil, maxPatchSize+1, args...)
	if errors.Is(err, errOutputTooLarge) {
		return nil, fmt.Errorf("diff exceeds the %d MB sharing limit; share fewer files", maxPatchSize>>20)
	}
	if err != nil {
		return nil, fmt.Errorf("git diff failed: %s", failText(stderr, err))
	}

	raw := string(out)
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("no uncommitted changes found in repository")
	}

	entries, err := patchNumstat(top, raw)
	if err != nil {
		return nil, err
	}
	additions, deletions := 0, 0
	fileSet := make(map[string]bool)
	for _, e := range entries {
		additions += e.Added
		deletions += e.Deleted
		fileSet[e.Path] = true
	}
	files := make([]string, 0, len(fileSet))
	for f := range fileSet {
		files = append(files, f)
	}
	sort.Strings(files)

	var patchID string
	for attempt := 0; ; attempt++ {
		id, err := generatePatchID()
		if err != nil {
			return nil, err
		}
		if StorePatch(id, raw) {
			patchID = id
			break
		}
		if attempt >= 3 {
			return nil, errors.New("could not store patch in memory")
		}
	}

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

// ---------------------------------------------------------------------------
// patch validation
// ---------------------------------------------------------------------------

// unsafePathReason reports why p must not be touched by a peer-supplied patch
// ("" means the path is acceptable). p is a repository-relative path as git
// reports it (already unquoted). Backslashes are treated as separators so the
// check holds on Windows, and .git is matched case-insensitively (and with
// NTFS/HFS+ aliases) because those filesystems fold names.
func unsafePathReason(p string) string {
	if p == "" {
		return "empty path"
	}
	if strings.ContainsRune(p, 0) {
		return "NUL in path"
	}
	q := strings.ReplaceAll(p, "\\", "/")
	if strings.HasPrefix(q, "/") {
		return "absolute path"
	}
	if len(q) >= 2 && q[1] == ':' && ((q[0] >= 'a' && q[0] <= 'z') || (q[0] >= 'A' && q[0] <= 'Z')) {
		return "drive-letter path"
	}
	for _, c := range strings.Split(q, "/") {
		if c == ".." {
			return "path traversal"
		}
		if isDotGit(c) {
			return "touches .git"
		}
		if isGitModules(c) {
			return "touches .gitmodules"
		}
	}
	return ""
}

// isDotGit reports whether a single path component names git's metadata
// directory on any common filesystem (case folding, trailing dots/spaces on
// NTFS, the 8.3 alias GIT~1, and HFS+-ignored zero-width code points).
func isDotGit(component string) bool {
	n := strings.Map(func(r rune) rune {
		switch {
		case r >= 0x200c && r <= 0x200f, r >= 0x202a && r <= 0x202e, r >= 0x206a && r <= 0x206f, r == 0xfeff:
			return -1
		}
		return r
	}, component)
	n = strings.ToLower(strings.TrimRight(n, ". "))
	return n == ".git" || n == "git~1"
}

// isGitModules reports whether a path component names .gitmodules on any
// common filesystem (case folding, NTFS trailing dots/spaces, the 8.3 alias
// GITMOD~1, HFS+-ignored zero-width code points). A peer patch that edits it
// can redirect submodule URLs, so it is refused like .git itself.
func isGitModules(component string) bool {
	n := strings.Map(func(r rune) rune {
		switch {
		case r >= 0x200c && r <= 0x200f, r >= 0x202a && r <= 0x202e, r >= 0x206a && r <= 0x206f, r == 0xfeff:
			return -1
		}
		return r
	}, component)
	n = strings.ToLower(strings.TrimRight(n, ". "))
	return n == ".gitmodules" || n == "gitmod~1"
}

// unquoteGitPath decodes a path as it appears in a patch header: either plain
// text or a C-style quoted string ("caf\303\251.txt").
func unquoteGitPath(s string) (string, bool) {
	if strings.HasPrefix(s, "\"") {
		u, err := strconv.Unquote(s)
		return u, err == nil
	}
	return s, true
}

// markerPath extracts the path from the remainder of a "--- " / "+++ " line,
// handling quoting and the tab git appends after names containing spaces.
func markerPath(rest string) (string, bool) {
	if strings.HasPrefix(rest, "\"") {
		for i := 1; i < len(rest); i++ {
			if rest[i] == '\\' {
				i++
				continue
			}
			if rest[i] == '"' {
				return unquoteGitPath(rest[:i+1])
			}
		}
		return "", false
	}
	if i := strings.IndexByte(rest, '\t'); i >= 0 {
		rest = rest[:i]
	}
	return rest, true
}

func hunkCounts(header string) (oldN, newN int) {
	f := strings.Fields(header)
	if len(f) < 3 || !strings.HasPrefix(f[1], "-") || !strings.HasPrefix(f[2], "+") {
		return 0, 0
	}
	count := func(s string) int {
		if i := strings.IndexByte(s, ','); i >= 0 {
			n, err := strconv.Atoi(s[i+1:])
			if err != nil || n < 0 {
				return 0
			}
			return n
		}
		return 1
	}
	return count(f[1][1:]), count(f[2][1:])
}

func isSymlinkModeLine(line string) bool {
	return modeLineHas(line, "120000")
}

// isGitlinkModeLine reports a submodule (gitlink, mode 160000) being added,
// removed or moved: applying it would change what the repo pulls in.
func isGitlinkModeLine(line string) bool {
	return modeLineHas(line, "160000")
}

func modeLineHas(line, mode string) bool {
	for _, p := range []string{"old mode ", "new mode ", "new file mode ", "deleted file mode ", "index "} {
		if strings.HasPrefix(line, p) {
			return strings.HasSuffix(strings.TrimRight(line, " \t"), " "+mode)
		}
	}
	return false
}

// dangerousPatchTargets scans a patch received from a peer (untrusted input)
// and returns a description of everything that must block it: paths that
// escape the work tree or touch git's own machinery, and any symlink
// creation/modification (120000 mode). top is the repository top-level
// directory (from repoTopLevel); git is asked to parse the patch there so the
// final target paths are validated exactly as git will interpret them.
//
// The header scan is deliberately a superset of what git accepts; the numstat
// pass is the authoritative one. Patches over maxPatchSize are refused.
func dangerousPatchTargets(top, patchContent string) ([]string, error) {
	if len(patchContent) > maxPatchSize {
		return nil, errPatchTooLarge
	}

	var offenders []string
	seen := make(map[string]bool)
	flag := func(msg string) {
		if !seen[msg] {
			seen[msg] = true
			offenders = append(offenders, msg)
		}
	}
	checkPath := func(p string) {
		if reason := unsafePathReason(p); reason != "" {
			flag(fmt.Sprintf("%q (%s)", p, reason))
		}
	}
	checkHeaderValue := func(v string) {
		p, ok := unquoteGitPath(v)
		if !ok {
			flag(fmt.Sprintf("%q (malformed quoted path)", v))
			return
		}
		checkPath(p)
	}

	oldLeft, newLeft := 0, 0 // remaining lines of the current hunk
	inBinary := false        // inside a "GIT binary patch" payload

	for _, line := range strings.Split(patchContent, "\n") {
		line = strings.TrimSuffix(line, "\r")

		// Checks that are safe to run on EVERY line: hunk content lines always
		// start with ' ', '+', '-' or '\', and binary payload lines are
		// base85 (no spaces), so none of these prefixes can be content.
		switch {
		case strings.HasPrefix(line, "diff --git "):
			inBinary = false
			for _, tok := range strings.FieldsFunc(line[len("diff --git "):], func(r rune) bool { return r == ' ' || r == '"' }) {
				checkPath(tok)
			}
		case strings.HasPrefix(line, "rename from "):
			checkHeaderValue(line[len("rename from "):])
		case strings.HasPrefix(line, "rename to "):
			checkHeaderValue(line[len("rename to "):])
		case strings.HasPrefix(line, "copy from "):
			checkHeaderValue(line[len("copy from "):])
		case strings.HasPrefix(line, "copy to "):
			checkHeaderValue(line[len("copy to "):])
		case isSymlinkModeLine(line):
			flag("(symlink creation or modification is not allowed in shared patches)")
		case isGitlinkModeLine(line):
			flag("(submodule changes are not allowed in shared patches)")
		}

		// Track hunks so "--- x" / "+++ x" *content* lines are not mistaken
		// for file headers (and vice versa).
		if oldLeft > 0 || newLeft > 0 {
			consumed := true
			switch {
			case line == "" || line[0] == ' ':
				oldLeft--
				newLeft--
			case line[0] == '-':
				oldLeft--
			case line[0] == '+':
				newLeft--
			case line[0] == '\\':
				// "\ No newline at end of file"
			default:
				consumed = false
				oldLeft, newLeft = 0, 0
			}
			if oldLeft < 0 {
				oldLeft = 0
			}
			if newLeft < 0 {
				newLeft = 0
			}
			if consumed {
				continue
			}
		}
		if inBinary {
			continue
		}
		switch {
		case strings.HasPrefix(line, "GIT binary patch"):
			inBinary = true
		case strings.HasPrefix(line, "@@ "):
			oldLeft, newLeft = hunkCounts(line)
		case strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "):
			p, ok := markerPath(line[4:])
			if !ok {
				flag(fmt.Sprintf("%q (malformed quoted path)", line[4:]))
			} else if p != "/dev/null" {
				checkPath(p)
			}
		}
	}
	if len(offenders) > 0 {
		return offenders, nil
	}

	// Authoritative pass: the exact target paths git resolves from the patch.
	entries, err := patchNumstat(top, patchContent)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		checkPath(e.Path)
	}
	return offenders, nil
}

// ApplyPatch applies patch content to the local repository. The repository
// top-level is resolved first and all git commands run there, so patches
// (whose paths are repository-root-relative) apply correctly even when dir is
// a subdirectory, and nothing is ever applied outside a git work tree.
func ApplyPatch(dir string, patchContent string) (string, error) {
	dir, err := resolveDir(dir)
	if err != nil {
		return "", err
	}
	if len(patchContent) > maxPatchSize {
		return "", errPatchTooLarge
	}
	if strings.TrimSpace(patchContent) == "" {
		return "", errors.New("patch is empty")
	}
	// Patches lifted out of a chat message may have lost their final newline,
	// which git apply treats as a corrupt patch.
	if !strings.HasSuffix(patchContent, "\n") {
		patchContent += "\n"
	}

	top, err := repoTopLevel(dir)
	if err != nil {
		return "", err
	}

	offenders, err := dangerousPatchTargets(top, patchContent)
	if err != nil {
		return "", fmt.Errorf("refusing to apply patch: %w", err)
	}
	if len(offenders) > 0 {
		return "", fmt.Errorf("refusing to apply patch: it contains disallowed content: %s (patches from peers may only modify regular files inside the project)", strings.Join(offenders, ", "))
	}

	patch := []byte(patchContent)

	// 1. Dry run check
	if _, stderr, err := runGit(top, patch, 1<<20, "apply", "--check", "-"); err != nil {
		return "", fmt.Errorf("patch collision / cannot apply cleanly: %s", failText(stderr, err))
	}

	// 2. Apply cleanly
	if _, stderr, err := runGit(top, patch, 1<<20, "apply", "-"); err != nil {
		return "", fmt.Errorf("git apply failed: %s", failText(stderr, err))
	}

	return "Patch applied cleanly to your local workspace.", nil
}

// GetCurrentBranch returns the active git branch or empty string if not in a git repo
func GetCurrentBranch(dir string) string {
	out, _, err := runGit(dir, nil, 64<<10, "branch", "--show-current")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// GetDirtyFiles returns the modified, staged, renamed, or untracked files in
// the repository (paths relative to the repository root, no duplicates).
//
// It uses NUL-delimited porcelain output, so names with spaces, quotes,
// unicode or " -> " are returned exactly, and --no-optional-locks so the
// background radar never takes index.lock away from the user's own git
// commands. Renames/copies report both the new and the original path.
func GetDirtyFiles(dir string) ([]string, error) {
	dir, err := resolveDir(dir)
	if err != nil {
		return nil, err
	}

	out, stderr, err := runGit(dir, nil, maxStatusOutput,
		"--no-optional-locks", "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, fmt.Errorf("git status failed: %s", failText(stderr, err))
	}

	var files []string
	seen := make(map[string]bool)
	add := func(p string) {
		if p == "" || p == ".termchat" || strings.HasPrefix(p, ".termchat/") || seen[p] {
			return
		}
		seen[p] = true
		files = append(files, p)
	}

	recs := strings.Split(string(out), "\x00")
	for i := 0; i < len(recs); i++ {
		rec := recs[i]
		if len(rec) < 4 { // "XY path"
			continue
		}
		x, y := rec[0], rec[1]
		add(rec[3:])
		// Renames and copies are followed by the original path as its own record.
		if x == 'R' || x == 'C' || y == 'R' || y == 'C' {
			if i+1 < len(recs) {
				i++
				add(recs[i])
			}
		}
	}
	return files, nil
}
