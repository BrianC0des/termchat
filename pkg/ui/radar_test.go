package ui

import (
	"os"
	"testing"
)

// A radar scan that ran in a workspace we've since switched away from must
// not overwrite the current workspace's branch/dirty state.
func TestStaleRadarResultIsDiscarded(t *testing.T) {
	m := &Model{gitBranch: "main", myDirtyFiles: []string{"a.go"}}

	m.Update(localRadarResult{dir: "/some/other/repo", branch: "feature/x", dirty: []string{"b.go", "c.go"}})
	if m.gitBranch != "main" || len(m.myDirtyFiles) != 1 || m.myDirtyFiles[0] != "a.go" {
		t.Fatalf("stale result was applied: branch=%q dirty=%v", m.gitBranch, m.myDirtyFiles)
	}

	m.Update(localRadarResult{branch: "feature/x", dirty: []string{"b.go"}}) // no dir recorded
	if m.gitBranch != "main" {
		t.Fatalf("result without a workspace dir was applied: branch=%q", m.gitBranch)
	}
}

func TestRadarResultForCurrentWorkspaceIsAccepted(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// A changed branch makes Update broadcast via m.manager. With a nil
	// manager that broadcast panics, which proves the result got past the
	// stale-workspace guard (a dropped result returns before that point).
	m := &Model{gitBranch: "main"}
	reachedApply := false
	func() {
		defer func() { reachedApply = recover() != nil }()
		m.Update(localRadarResult{dir: cwd, branch: "renamed"})
	}()
	if !reachedApply {
		t.Fatal("result for the current workspace was dropped")
	}
	if m.gitBranch != "renamed" {
		t.Fatalf("branch not updated: %q", m.gitBranch)
	}
}
