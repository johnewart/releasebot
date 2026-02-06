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

// ListTags returns all tag names in the repository (refs/tags/* stripped to tag name).
func ListTags(ctx context.Context, repoPath string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "tag", "-l")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git tag: %w", err)
	}
	if len(out) == 0 {
		return nil, nil
	}
	var tags []string
	for _, line := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		if t := strings.TrimSpace(line); t != "" {
			tags = append(tags, t)
		}
	}
	return tags, nil
}

// CurrentBranch returns the current branch name (e.g. main). Fails if HEAD is detached.
func CurrentBranch(ctx context.Context, repoPath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w", err)
	}
	name := strings.TrimSpace(string(out))
	if name == "HEAD" {
		return "", fmt.Errorf("HEAD is detached; checkout a branch to release")
	}
	return name, nil
}

// RemoteURL returns the fetch URL for the given remote (e.g. origin).
func RemoteURL(ctx context.Context, repoPath, remote string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "config", "--get", "remote."+remote+".url")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git config remote.%s.url: %w", remote, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Checkout switches to the given branch.
func Checkout(ctx context.Context, repoPath, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "checkout", branch)
	cmd.Dir = repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout %s: %w (%s)", branch, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Add stages paths for commit.
func Add(ctx context.Context, repoPath string, paths ...string) error {
	args := append([]string{"add"}, paths...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// CreateCommit creates a commit with the given message.
func CreateCommit(ctx context.Context, repoPath, message string) error {
	cmd := exec.CommandContext(ctx, "git", "commit", "-m", message)
	cmd.Dir = repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// CreateTag creates an annotated tag at HEAD. Message is the tag message.
func CreateTag(ctx context.Context, repoPath, tag, message string) error {
	cmd := exec.CommandContext(ctx, "git", "tag", "-a", tag, "-m", message)
	cmd.Dir = repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git tag: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Push pushes a ref (e.g. refs/heads/main or refs/tags/v1.0.0) to the remote.
func Push(ctx context.Context, repoPath, remote, ref string) error {
	cmd := exec.CommandContext(ctx, "git", "push", remote, ref)
	cmd.Dir = repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git push %s %s: %w (%s)", remote, ref, err, strings.TrimSpace(string(out)))
	}
	return nil
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
