package ghbridge

import (
	"strings"
	"testing"
)

func TestFormatIssueCard(t *testing.T) {
	iss := &IssueDetails{
		Number: 21,
		Title:  "Test Issue Title",
		State:  "OPEN",
		Author: "BrianC0des",
		Labels: []string{"ui", "enhancement"},
		URL:    "https://github.com/BrianC0des/termchat/issues/21",
		Body:   "This is a sample description of the issue.",
	}

	card := FormatIssueCard(iss)
	if !strings.Contains(card, "#21") {
		t.Errorf("expected issue number in card, got: %s", card)
	}
	if !strings.Contains(card, "● OPEN") {
		t.Errorf("expected open status in card, got: %s", card)
	}
	if !strings.Contains(card, "@BrianC0des") {
		t.Errorf("expected author in card, got: %s", card)
	}
	if !strings.Contains(card, "[ui, enhancement]") {
		t.Errorf("expected labels in card, got: %s", card)
	}
}

func TestFormatIssueCardClosed(t *testing.T) {
	iss := &IssueDetails{
		Number: 18,
		Title:  "Closed issue",
		State:  "CLOSED",
		Author: "BrianC0des",
		URL:    "https://github.com/BrianC0des/termchat/issues/18",
	}

	card := FormatIssueCard(iss)
	if !strings.Contains(card, "✓ CLOSED") {
		t.Errorf("expected closed badge in card, got: %s", card)
	}
}

func TestFormatPRCard(t *testing.T) {
	pr := &PRDetails{
		Number:      10,
		Title:       "Add feature",
		State:       "MERGED",
		Author:      "dev",
		HeadRefName: "feat/foo",
		BaseRefName: "main",
		Additions:   100,
		Deletions:   20,
		URL:         "https://github.com/BrianC0des/termchat/pull/10",
		Body:        "Short PR summary",
	}

	card := FormatPRCard(pr)
	if !strings.Contains(card, "#10") {
		t.Errorf("expected PR number in card, got: %s", card)
	}
	if !strings.Contains(card, "✓ MERGED") {
		t.Errorf("expected merged badge, got: %s", card)
	}
	if !strings.Contains(card, "⎇ feat/foo → main (+100/-20)") {
		t.Errorf("expected branch info, got: %s", card)
	}
}

func TestFormatIssueList(t *testing.T) {
	issues := []IssueSummary{
		{Number: 21, Title: "Issue 21", State: "OPEN", Author: "alice", Labels: []string{"bug"}},
		{Number: 20, Title: "Issue 20", State: "CLOSED", Author: "bob"},
	}

	out := FormatIssueList(issues, "owner/repo")
	if !strings.Contains(out, "owner/repo") {
		t.Errorf("expected repo name in output, got: %s", out)
	}
	if !strings.Contains(out, "#21  ● Issue 21 (@alice) [bug]") {
		t.Errorf("expected formatted row for issue 21, got: %s", out)
	}
	if !strings.Contains(out, "#20  ✓ Issue 20 (@bob)") {
		t.Errorf("expected formatted row for issue 20, got: %s", out)
	}
}

func TestRunGHCapturesStderr(t *testing.T) {
	_, err := runGH("issue", "list", "--this-flag-does-not-exist")
	if err == nil {
		t.Fatal("expected error for invalid flag")
	}
	if strings.Contains(err.Error(), "exit status 1") {
		t.Errorf("expected error to contain actual stderr message, but got bare: %v", err)
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("expected error to mention 'unknown flag', got: %v", err)
	}
}

