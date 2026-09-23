package ghbridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// runGH executes a GitHub CLI command and captures stderr on failure
func runGH(args ...string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.Command("gh", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		errStr := strings.TrimSpace(stderr.String())
		if errStr != "" {
			return nil, fmt.Errorf("%s", errStr)
		}
		return nil, err
	}
	return out, nil
}

// PRDetails holds key pull request metadata
type PRDetails struct {
	Number      int      `json:"number"`
	Title       string   `json:"title"`
	State       string   `json:"state"`
	Author      string   `json:"author"`
	HeadRefName string   `json:"headRefName"`
	BaseRefName string   `json:"baseRefName"`
	Additions   int      `json:"additions"`
	Deletions   int      `json:"deletions"`
	ReviewState string   `json:"reviewDecision"`
	Body        string   `json:"body"`
	Labels      []string `json:"labels"`
	URL         string   `json:"url"`
}

// IssueDetails holds key issue metadata
type IssueDetails struct {
	Number int      `json:"number"`
	Title  string   `json:"title"`
	State  string   `json:"state"`
	Author string   `json:"author"`
	Body   string   `json:"body"`
	Labels []string `json:"labels"`
	URL    string   `json:"url"`
}

// FetchPR queries GitHub CLI for PR info
func FetchPR(repo string, prNum int) (*PRDetails, error) {
	args := []string{"pr", "view", strconv.Itoa(prNum), "--json", "number,title,state,author,headRefName,baseRefName,additions,deletions,reviewDecision,body,labels,url"}
	if repo != "" {
		args = append(args, "-R", repo)
	}

	out, err := runGH(args...)
	if err != nil {
		return nil, fmt.Errorf("could not fetch PR #%d: %v", prNum, err)
	}

	var raw struct {
		Number         int    `json:"number"`
		Title          string `json:"title"`
		State          string `json:"state"`
		Author         struct {
			Login string `json:"login"`
		} `json:"author"`
		HeadRefName    string `json:"headRefName"`
		BaseRefName    string `json:"baseRefName"`
		Additions      int    `json:"additions"`
		Deletions      int    `json:"deletions"`
		ReviewDecision string `json:"reviewDecision"`
		Body           string `json:"body"`
		Labels         []struct {
			Name string `json:"name"`
		} `json:"labels"`
		URL            string `json:"url"`
	}

	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}

	var labelNames []string
	for _, l := range raw.Labels {
		labelNames = append(labelNames, l.Name)
	}

	return &PRDetails{
		Number:      raw.Number,
		Title:       raw.Title,
		State:       raw.State,
		Author:      raw.Author.Login,
		HeadRefName: raw.HeadRefName,
		BaseRefName: raw.BaseRefName,
		Additions:   raw.Additions,
		Deletions:   raw.Deletions,
		ReviewState: raw.ReviewDecision,
		Body:        raw.Body,
		Labels:      labelNames,
		URL:         raw.URL,
	}, nil
}

// FetchIssue queries GitHub CLI for issue info
func FetchIssue(repo string, issueNum int) (*IssueDetails, error) {
	args := []string{"issue", "view", strconv.Itoa(issueNum), "--json", "number,title,state,author,body,labels,url"}
	if repo != "" {
		args = append(args, "-R", repo)
	}

	out, err := runGH(args...)
	if err != nil {
		return nil, fmt.Errorf("could not fetch Issue #%d: %v", issueNum, err)
	}

	var raw struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		State  string `json:"state"`
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		Body   string `json:"body"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
		URL string `json:"url"`
	}

	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}

	var labelNames []string
	for _, l := range raw.Labels {
		labelNames = append(labelNames, l.Name)
	}

	return &IssueDetails{
		Number: raw.Number,
		Title:  raw.Title,
		State:  raw.State,
		Author: raw.Author.Login,
		Body:   raw.Body,
		Labels: labelNames,
		URL:    raw.URL,
	}, nil
}

// CheckoutPR switches to the branch of the given PR number
func CheckoutPR(prNum int) (string, error) {
	cmd := exec.Command("gh", "pr", "checkout", strconv.Itoa(prNum))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gh pr checkout failed: %s", strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// FetchCIStatus checks the latest GitHub Actions workflow status
func FetchCIStatus(repo, branch string) (string, error) {
	args := []string{"run", "list", "--limit", "1", "--json", "status,conclusion,name,headBranch,url"}
	if repo != "" {
		args = append(args, "-R", repo)
	}
	if branch != "" {
		args = append(args, "--branch", branch)
	}

	out, err := runGH(args...)
	if err != nil {
		return "", fmt.Errorf("could not fetch CI status: %v", err)
	}

	var runs []struct {
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		Name       string `json:"name"`
		HeadBranch string `json:"headBranch"`
		URL        string `json:"url"`
	}

	if err := json.Unmarshal(out, &runs); err != nil || len(runs) == 0 {
		return "No recent CI runs found.", nil
	}

	r := runs[0]
	icon := "[PASS]"
	statusStr := "Passing"
	if r.Conclusion == "failure" {
		icon = "[FAIL]"
		statusStr = "Failing"
	} else if r.Status == "in_progress" {
		icon = "[RUNNING]"
		statusStr = "In Progress"
	}

	return fmt.Sprintf("%s **CI Status (%s @ %s):** %s (%s)\n• Workflow: %s\n• URL: %s",
		icon, r.Name, r.HeadBranch, statusStr, r.Conclusion, r.Name, r.URL), nil
}

// IssueSummary holds brief issue metadata for lists
type IssueSummary struct {
	Number int      `json:"number"`
	Title  string   `json:"title"`
	State  string   `json:"state"`
	Author string   `json:"author"`
	Labels []string `json:"labels"`
	URL    string   `json:"url"`
}

// FetchIssueList queries GitHub CLI for recent issues
func FetchIssueList(repo string, state string, limit int) ([]IssueSummary, error) {
	if limit <= 0 {
		limit = 15
	}
	if state == "" {
		state = "open"
	}
	args := []string{"issue", "list", "--limit", strconv.Itoa(limit), "--state", state, "--json", "number,title,state,author,labels,url"}
	if repo != "" {
		args = append(args, "-R", repo)
	}

	out, err := runGH(args...)
	if err != nil {
		return nil, fmt.Errorf("could not fetch issues: %v", err)
	}

	var raw []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		State  string `json:"state"`
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
		URL string `json:"url"`
	}

	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}

	var results []IssueSummary
	for _, item := range raw {
		var labels []string
		for _, l := range item.Labels {
			labels = append(labels, l.Name)
		}
		results = append(results, IssueSummary{
			Number: item.Number,
			Title:  item.Title,
			State:  item.State,
			Author: item.Author.Login,
			Labels: labels,
			URL:    item.URL,
		})
	}
	return results, nil
}

// FormatIssueList formats a list of issues into a clean, Primer Dark table
func FormatIssueList(issues []IssueSummary, repo string) string {
	var sb strings.Builder
	repoLabel := "Current Repository"
	if repo != "" {
		repoLabel = repo
	}
	sb.WriteString(fmt.Sprintf("◆ GITHUB ISSUES (%s)\n", repoLabel))
	if len(issues) == 0 {
		sb.WriteString("  (No issues found matching criteria)\n")
		return sb.String()
	}

	for _, iss := range issues {
		status := "●"
		if strings.ToUpper(iss.State) == "CLOSED" {
			status = "✓"
		}
		labelsStr := ""
		if len(iss.Labels) > 0 {
			labelsStr = fmt.Sprintf(" [%s]", strings.Join(iss.Labels, ", "))
		}
		sb.WriteString(fmt.Sprintf("  • #%-3d %s %s (@%s)%s\n", iss.Number, status, iss.Title, iss.Author, labelsStr))
	}
	sb.WriteString("\n  ↳ Type /issue <#id> to preview an issue card in chat")
	return sb.String()
}

// FormatIssueCard formats an Issue into a clean Primer Dark card
func FormatIssueCard(iss *IssueDetails) string {
	stateBadge := "● OPEN"
	if strings.ToUpper(iss.State) == "CLOSED" {
		stateBadge = "✓ CLOSED"
	}

	labelsStr := ""
	if len(iss.Labels) > 0 {
		labelsStr = fmt.Sprintf(" • Labels: [%s]", strings.Join(iss.Labels, ", "))
	}

	bodyContent := strings.TrimSpace(iss.Body)
	if bodyContent == "" {
		bodyContent = "(No description provided)"
	} else if len(bodyContent) > 360 {
		bodyContent = bodyContent[:350] + "...\n↳ (Full description at " + iss.URL + ")"
	}

	return fmt.Sprintf("╭── ◆ GITHUB ISSUE #%d ───────────────────────────────────╮\n│ Title:   %s\n│ State:   %s • Author: @%s%s\n│ URL:     %s\n├────────────────────────────────────────────────────────────┤\n%s\n╰────────────────────────────────────────────────────────────╯",
		iss.Number, iss.Title, stateBadge, iss.Author, labelsStr, iss.URL, bodyContent)
}

// FormatPRCard formats a PR into a clean Primer Dark card
func FormatPRCard(pr *PRDetails) string {
	stateBadge := "● OPEN"
	if strings.ToUpper(pr.State) == "MERGED" {
		stateBadge = "✓ MERGED"
	} else if strings.ToUpper(pr.State) == "CLOSED" {
		stateBadge = "✗ CLOSED"
	}

	reviewStr := ""
	if pr.ReviewState != "" {
		reviewStr = fmt.Sprintf(" • Review: [%s]", pr.ReviewState)
	}

	bodyContent := strings.TrimSpace(pr.Body)
	if bodyContent == "" {
		bodyContent = "(No description provided)"
	} else if len(bodyContent) > 360 {
		bodyContent = bodyContent[:350] + "...\n↳ (Full description at " + pr.URL + ")"
	}

	return fmt.Sprintf("╭── ◆ GITHUB PR #%d ──────────────────────────────────────╮\n│ Title:   %s\n│ State:   %s%s • Author: @%s\n│ Branch:  ⎇ %s → %s (+%d/-%d)\n│ URL:     %s\n├────────────────────────────────────────────────────────────┤\n%s\n╰────────────────────────────────────────────────────────────╯",
		pr.Number, pr.Title, stateBadge, reviewStr, pr.Author, pr.HeadRefName, pr.BaseRefName, pr.Additions, pr.Deletions, pr.URL, bodyContent)
}
