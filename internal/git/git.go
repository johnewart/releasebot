package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ValidateTag checks that tag exists in the repo and returns its commit SHA.
func ValidateTag(ctx context.Context, repoPath, tag string) (sha string, err error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", "refs/tags/"+tag)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() != 0 {
			return "", fmt.Errorf("tag %q not found in repository", tag)
		}
		return "", fmt.Errorf("git rev-parse: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// RevParse resolves a ref (tag, branch, or SHA) to a full SHA.
func RevParse(ctx context.Context, repoPath, ref string) (string, error) {
	if ref == "" {
		ref = "HEAD"
	}
	cmd := exec.CommandContext(ctx, "git", "rev-parse", ref)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse %s: %w", ref, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// LogBetween returns commit messages (one per line, format: hash subject) between base and head (exclusive of base).
func LogBetween(ctx context.Context, repoPath, baseRef, headRef string) ([]Commit, error) {
	head := headRef
	if head == "" {
		head = "HEAD"
	}
	cmd := exec.CommandContext(ctx, "git", "log", "--format=%H%x00%s%x00%b%x00", baseRef+".."+head)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}
	var commits []Commit
	for _, block := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		if block == "" {
			continue
		}
		parts := strings.SplitN(block, "\x00", 3)
		if len(parts) < 2 {
			continue
		}
		body := ""
		if len(parts) >= 3 {
			body = strings.TrimSpace(parts[2])
		}
		commits = append(commits, Commit{
			SHA:     parts[0],
			Subject: parts[1],
			Body:    body,
		})
	}
	return commits, nil
}

// Commit represents a git commit for changelog input.
type Commit struct {
	SHA     string
	Subject string
	Body    string
}

// RemoteOriginURL returns the fetch URL for origin (e.g. https://github.com/owner/repo or git@github.com:owner/repo.git).
func RemoteOriginURL(ctx context.Context, repoPath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "config", "--get", "remote.origin.url")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git config remote.origin.url: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ParseGitHubOwnerRepo extracts owner and repo from a git remote URL.
// Supports https://github.com/owner/repo[.git] and git@github.com:owner/repo[.git].
func ParseGitHubOwnerRepo(remoteURL string) (owner, repo string, err error) {
	remoteURL = strings.TrimSuffix(remoteURL, ".git")
	var rest string
	if strings.HasPrefix(remoteURL, "https://github.com/") {
		rest = strings.TrimPrefix(remoteURL, "https://github.com/")
	} else if strings.HasPrefix(remoteURL, "git@github.com:") {
		rest = strings.TrimPrefix(remoteURL, "git@github.com:")
	} else {
		return "", "", fmt.Errorf("not a GitHub URL: %s", remoteURL)
	}
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid GitHub URL: %s", remoteURL)
	}
	return parts[0], parts[1], nil
}
