package git

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// BranchInfo is a local or remote-tracking branch short name (e.g. main, origin/main) and its tip SHA.
type BranchInfo struct {
	Name string
	SHA  string
}

// ListBranches returns branches from refs/heads and refs/remotes (excluding HEAD symref duplicates).
func ListBranches(ctx context.Context, repoPath string) ([]BranchInfo, error) {
	cmd := exec.CommandContext(ctx, "git", "for-each-ref",
		"refs/heads", "refs/remotes",
		"--format=%(refname:short)\t%(objectname)",
	)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git for-each-ref: %w", err)
	}
	var list []BranchInfo
	seen := make(map[string]struct{})
	for _, line := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if name == "" || strings.HasSuffix(name, "/HEAD") {
			continue
		}
		sha := strings.TrimSpace(parts[1])
		key := name + "\t" + sha
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		list = append(list, BranchInfo{Name: name, SHA: sha})
	}
	return list, nil
}

// MergeBase returns the best common ancestor commit for refs a and b.
func MergeBase(ctx context.Context, repoPath, a, b string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "merge-base", a, b)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git merge-base %s %s: %w", a, b, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// IsAncestor returns true if ancestor is an ancestor of descendant.
func IsAncestor(ctx context.Context, repoPath, ancestor, descendant string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "merge-base", "--is-ancestor", ancestor, descendant)
	cmd.Dir = repoPath
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("git merge-base --is-ancestor: %w", err)
	}
	return true, nil
}

// CommitCount returns the number of commits on reachableSide reachable from fromRef but not from excludingRef (excludingRef..reachableSide).
func CommitCount(ctx context.Context, repoPath, excludingRef, reachableSide string) (int, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-list", "--count", excludingRef+".."+reachableSide)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("git rev-list --count: %w", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("parse rev-list count: %w", err)
	}
	return n, nil
}

// AheadBehind returns (ahead, behind) for left vs right using symmetric difference:
// ahead = commits reachable from left not in right; behind = commits reachable from right not in left.
func AheadBehind(ctx context.Context, repoPath, left, right string) (ahead, behind int, err error) {
	cmd := exec.CommandContext(ctx, "git", "rev-list", "--left-right", "--count", left+"..."+right)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return 0, 0, fmt.Errorf("git rev-list --left-right --count: %w", err)
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("unexpected rev-list output: %q", string(out))
	}
	ahead, err = strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, err
	}
	behind, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, err
	}
	return ahead, behind, nil
}

// CreateBranch creates a new branch at startPoint (-b).
func CreateBranch(ctx context.Context, repoPath, branchName, startPoint string) error {
	cmd := exec.CommandContext(ctx, "git", "checkout", "-b", branchName, startPoint)
	cmd.Dir = repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout -b %s %s: %w (%s)", branchName, startPoint, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Merge merges ref into the current branch. If ffOnly, uses --ff-only; otherwise creates a merge commit if needed.
func Merge(ctx context.Context, repoPath, ref string, ffOnly bool) error {
	args := []string{"merge", ref}
	if ffOnly {
		args = append(args, "--ff-only")
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git merge %s: %w (%s)", ref, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// DeleteLocalBranch removes a local branch (-d or -D).
func DeleteLocalBranch(ctx context.Context, repoPath, branch string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	cmd := exec.CommandContext(ctx, "git", "branch", flag, branch)
	cmd.Dir = repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git branch %s %s: %w (%s)", flag, branch, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// PushDeleteRemote deletes a branch on remote (branch is a short name like release/1.0).
func PushDeleteRemote(ctx context.Context, repoPath, remote, branch string) error {
	branch = strings.TrimPrefix(branch, "refs/heads/")
	cmd := exec.CommandContext(ctx, "git", "push", remote, "--delete", branch)
	cmd.Dir = repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git push %s --delete %s: %w (%s)", remote, branch, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// PushBranch pushes a local branch to remote, optionally setting upstream (-u).
func PushBranch(ctx context.Context, repoPath, remote, branch string, setUpstream bool) error {
	args := []string{"push"}
	if setUpstream {
		args = append(args, "-u")
	}
	args = append(args, remote, branch)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git push: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Fetch runs git fetch with optional --prune.
func Fetch(ctx context.Context, repoPath, remote string, prune bool) error {
	args := []string{"fetch", remote}
	if prune {
		args = append(args, "--prune")
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// SymbolicRefFull returns the fully qualified ref for a short branch name if it exists (e.g. refs/heads/main).
func SymbolicRefFull(ctx context.Context, repoPath, shortName string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--symbolic-full-name", shortName)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("rev-parse --symbolic-full-name %s: %w", shortName, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// RefExists returns nil if ref resolves.
func RefExists(ctx context.Context, repoPath, ref string) error {
	_, err := RevParse(ctx, repoPath, ref)
	return err
}

// ResolveFirst tries each ref in order and returns the first that rev-parse resolves.
func ResolveFirst(ctx context.Context, repoPath string, candidates ...string) (sha, ref string, err error) {
	var last error
	for _, c := range candidates {
		if c == "" {
			continue
		}
		sha, err := RevParse(ctx, repoPath, c)
		if err == nil {
			return sha, c, nil
		}
		last = err
	}
	if last == nil {
		last = fmt.Errorf("no candidates")
	}
	return "", "", last
}
