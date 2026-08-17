package ghbridge

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// PRDetails holds key pull request metadata
type PRDetails struct {
	Number        int      `json:"number"`
	Title         string   `json:"title"`
	State         string   `json:"state"`
	Author        string   `json:"author"`
	HeadRefName   string   `json:"headRefName"`
	BaseRefName   string   `json:"baseRefName"`
	Additions     int      `json:"additions"`
	Deletions     int      `json:"deletions"`
	ReviewState   string   `json:"reviewDecision"`
	URL           string   `json:"url"`
}

// IssueDetails holds key issue metadata
type IssueDetails struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Author string `json:"author"`
	URL    string `json:"url"`
}

// FetchPR queries GitHub CLI for PR info
func FetchPR(repo string, prNum int) (*PRDetails, error) {
	args := []string{"pr", "view", strconv.Itoa(prNum), "--json", "number,title,state,author,headRefName,baseRefName,additions,deletions,reviewDecision,url"}
	if repo != "" {
		args = append(args, "-R", repo)
	}

	cmd := exec.Command("gh", args...)
	out, err := cmd.Output()
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
		URL            string `json:"url"`
	}

	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
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
		URL:         raw.URL,
	}, nil
}

// FetchIssue queries GitHub CLI for issue info
func FetchIssue(repo string, issueNum int) (*IssueDetails, error) {
	args := []string{"issue", "view", strconv.Itoa(issueNum), "--json", "number,title,state,author,url"}
	if repo != "" {
		args = append(args, "-R", repo)
	}

	cmd := exec.Command("gh", args...)
	out, err := cmd.Output()
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
		URL string `json:"url"`
	}

	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}

	return &IssueDetails{
		Number: raw.Number,
		Title:  raw.Title,
		State:  raw.State,
		Author: raw.Author.Login,
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

	cmd := exec.Command("gh", args...)
	out, err := cmd.Output()
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
	icon := "🟢"
	statusStr := "Passing"
	if r.Conclusion == "failure" {
		icon = "🔴"
		statusStr = "Failing"
	} else if r.Status == "in_progress" {
		icon = "🟡"
		statusStr = "In Progress"
	}

	return fmt.Sprintf("%s **CI Status (%s @ %s):** %s (%s)\n• Workflow: %s\n• URL: %s",
		icon, r.Name, r.HeadBranch, statusStr, r.Conclusion, r.Name, r.URL), nil
}
