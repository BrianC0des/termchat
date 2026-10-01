package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestExtractDiffBlock(t *testing.T) {
	inner := "diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@ -1,2 +1,5 @@\n # Title\n+```go\n+fmt.Println(1)\n+```\n+done"
	tests := []struct {
		name, msg, want string
		ok              bool
	}{
		{"plain", "[GIT PATCH #patch-x]\n```diff\n" + inner[:60] + "\n```", inner[:60], true},
		{"inner fences are diff content", "card\n```diff\n" + inner + "\n```\ntrailer", inner, true},
		{"longer outer fence", "````diff\n+```\n+x\n+```\n````", "+```\n+x\n+```", true},
		{"crlf closing", "```diff\n+a\r\n```\r\n", "+a\r", true},
		{"unclosed", "```diff\n+a\n+b", "", false},
		{"no block", "just chat", "", false},
	}
	for _, tc := range tests {
		got, ok := extractDiffBlock(tc.msg)
		if ok != tc.ok || got != tc.want {
			t.Errorf("%s: got (%q,%v) want (%q,%v)", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestGHAuthRefreshMsgUpdatesModel(t *testing.T) {
	m := &Model{}
	before := time.Now()
	m.Update(ghAuthRefreshMsg{authed: true})
	if !m.isGHAuthed || m.ghAuthLastCheck.Before(before) {
		t.Fatalf("auth state not applied: authed=%v last=%v", m.isGHAuthed, m.ghAuthLastCheck)
	}
	m.Update(ghAuthRefreshMsg{authed: false})
	if m.isGHAuthed {
		t.Fatal("auth state not cleared")
	}
}

func TestPendingCmdsDrainOnce(t *testing.T) {
	m := &Model{}
	if m.takePendingCmds() != nil {
		t.Fatal("expected nil with nothing queued")
	}
	m.queueCmd(nil) // ignored
	m.queueCmd(func() tea.Msg { return nil })
	if m.takePendingCmds() == nil {
		t.Fatal("expected a batched cmd")
	}
	if len(m.pendingCmds) != 0 || m.takePendingCmds() != nil {
		t.Fatal("queue not drained")
	}
}

func TestGHFetchErrorClearsLoadingToastAndReports(t *testing.T) {
	m := &Model{toastMsg: ghFetchLoadingPrefix + " PR #1..."}
	m.Update(ghFetchDoneMsg{kind: ghFetchPR, err: errTest("boom")})
	if strings.HasPrefix(m.toastMsg, ghFetchLoadingPrefix) {
		t.Fatalf("loading toast not cleared: %q", m.toastMsg)
	}
	// short system notices surface as a toast, long ones as chat messages
	found := strings.Contains(m.toastMsg, "[GH] boom")
	for _, msg := range m.messages {
		if strings.Contains(msg.Content, "[GH] boom") {
			found = true
		}
	}
	if !found {
		t.Fatalf("error not surfaced: toast=%q msgs=%+v", m.toastMsg, m.messages)
	}

	// an unrelated toast must survive (long errors go to chat, not the toast)
	m = &Model{toastMsg: "Code ABCD copied"}
	m.Update(ghFetchDoneMsg{kind: ghFetchCI, err: errTest(strings.Repeat("x", 100))})
	if m.toastMsg != "Code ABCD copied" {
		t.Fatalf("unrelated toast clobbered: %q", m.toastMsg)
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }
