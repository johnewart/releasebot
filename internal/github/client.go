package github

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/google/go-github/v60/github"
	"golang.org/x/oauth2"
)

// Client wraps GitHub API for releasebot (compare commits, PRs for commits).
type Client struct {
	*github.Client
	Owner string
	Repo  string
}

// NewClient builds a GitHub client. token can be empty for public repo read-only.
func NewClient(ctx context.Context, token, owner, repo string) *Client {
	var httpClient *http.Client
	if token != "" {
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
		httpClient = oauth2.NewClient(ctx, ts)
	}
	gh := github.NewClient(httpClient)
	return &Client{Client: gh, Owner: owner, Repo: repo}
}

// CompareResponse is a subset of compare result we need.
type CompareResponse struct {
	Commits []*github.RepositoryCommit
}

// ListCommitsBetween returns all commits between base and head (base..head).
// It paginates the GitHub compare API so the full list is returned (not capped at 100).
func (c *Client) ListCommitsBetween(ctx context.Context, base, head string) ([]*github.RepositoryCommit, error) {
	const perPage = 100
	var all []*github.RepositoryCommit
	page := 1
	for {
		comp, _, err := c.Repositories.CompareCommits(ctx, c.Owner, c.Repo, base, head, &github.ListOptions{Page: page, PerPage: perPage})
		if err != nil {
			return nil, fmt.Errorf("compare commits: %w", err)
		}
		all = append(all, comp.Commits...)
		total := comp.GetTotalCommits()
		if total <= len(all) || len(comp.Commits) < perPage {
			break
		}
		page++
	}
	return all, nil
}

// PullRequest is a minimal PR for changelog (and cache serialization).
// Diff is populated when include_diff is used for per-PR summarization; not persisted in PR cache.
type PullRequest struct {
	Number   int    `json:"number"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Author   string `json:"author"`
	MergedAt string `json:"merged_at"`
	Diff     string `json:"-"` // optional; set when fetching for per-PR LLM with include_diff
}

// PullRequestsForCommit returns merged PR(s) associated with the given commit SHA.
func (c *Client) PullRequestsForCommit(ctx context.Context, commitSHA string) ([]PullRequest, error) {
	prs, _, err := c.PullRequests.ListPullRequestsWithCommit(ctx, c.Owner, c.Repo, commitSHA, &github.ListOptions{PerPage: 10})
	if err != nil {
		return nil, fmt.Errorf("list pulls for commit: %w", err)
	}
	var result []PullRequest
	for _, pr := range prs {
		if pr.MergedAt == nil {
			continue
		}
		author := ""
		if pr.User != nil && pr.User.Login != nil {
			author = *pr.User.Login
		}
		title := ""
		if pr.Title != nil {
			title = *pr.Title
		}
		body := ""
		if pr.Body != nil {
			body = *pr.Body
		}
		mergedAt := ""
		if pr.MergedAt != nil {
			mergedAt = pr.MergedAt.Format("2006-01-02")
		}
		result = append(result, PullRequest{
			Number:   pr.GetNumber(),
			Title:    title,
			Body:     body,
			Author:   author,
			MergedAt: mergedAt,
		})
	}
	return result, nil
}

// GetPRDiff returns the unified diff for a pull request (for use with per-PR LLM summarization).
func (c *Client) GetPRDiff(ctx context.Context, number int) (string, error) {
	diff, _, err := c.PullRequests.GetRaw(ctx, c.Owner, c.Repo, number, github.RawOptions{Type: github.Diff})
	if err != nil {
		return "", fmt.Errorf("get PR diff: %w", err)
	}
	return diff, nil
}

// MergedPRsBetween returns merged PRs that appear in the commit range base..head.
// It uses CompareCommits then for each commit fetches associated PRs and deduplicates by PR number.
func (c *Client) MergedPRsBetween(ctx context.Context, base, head string) ([]PullRequest, error) {
	return c.MergedPRsBetweenWithProgress(ctx, base, head, nil, nil)
}

// MergedPRsBetweenWithProgress does MergedPRsBetween and calls report with status messages.
// When reportProgress is non-nil, it is called with (current, total) in the PR-fetch loop instead of per-commit status lines.
func (c *Client) MergedPRsBetweenWithProgress(ctx context.Context, base, head string, report func(string), reportProgress func(current, total int)) ([]PullRequest, error) {
	commits, err := c.ListCommitsBetween(ctx, base, head)
	if err != nil {
		return nil, err
	}
	nCommits := len(commits)
	if report != nil && nCommits > 0 {
		report("Fetching PRs from GitHub...")
	}
	seen := make(map[int]struct{})
	var result []PullRequest
	for i, commit := range commits {
		if reportProgress != nil && nCommits > 0 {
			reportProgress(i+1, nCommits)
		} else if report != nil && nCommits > 0 {
			report("Fetching PRs for commit " + strconv.Itoa(i+1) + "/" + strconv.Itoa(nCommits) + "...")
		}
		sha := commit.GetSHA()
		prs, err := c.PullRequestsForCommit(ctx, sha)
		if err != nil {
			continue
		}
		for _, pr := range prs {
			if _, ok := seen[pr.Number]; ok {
				continue
			}
			seen[pr.Number] = struct{}{}
			result = append(result, pr)
		}
	}
	return result, nil
}
