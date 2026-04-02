package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/johnewart/releasebot/internal/config"
	"github.com/johnewart/releasebot/internal/git"
	"github.com/johnewart/releasebot/internal/gitflow"
	"github.com/johnewart/releasebot/internal/semver"
	"github.com/spf13/cobra"
)

var (
	flowFetch         bool
	flowJSON          bool
	flowMermaid       bool
	flowFrom          string
	flowVersion       string
	flowApply         bool
	flowPush          bool
	flowFFOnly        bool
	flowHotfixBranch  string
)

var flowCmd = &cobra.Command{
	Use:   "flow",
	Short: "GitFlow-like branching: status, start branches, finish hotfix, prune",
}

var flowStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show main/develop drift and release/hotfix lines (use git fetch --prune for fresh data)",
	RunE:  runFlowStatus,
}

var flowStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Create a release or hotfix branch",
}

var flowStartReleaseCmd = &cobra.Command{
	Use:   "release",
	Short: "Create release/M.N from --from or branching.release_cut_from",
	RunE:  runFlowStartRelease,
}

var flowStartHotfixCmd = &cobra.Command{
	Use:   "hotfix",
	Short: "Create a hotfix branch from --from (release/M.N or tag)",
	RunE:  runFlowStartHotfix,
}

var flowFinishCmd = &cobra.Command{
	Use:   "finish",
	Short: "Merge flow branches (e.g. finish a hotfix)",
}

var flowFinishHotfixCmd = &cobra.Command{
	Use:   "hotfix",
	Short: "Merge --branch hotfix into release line, main, and develop (then optional --push)",
	RunE:  runFlowFinishHotfix,
}

var flowPruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "List release branches to delete under pruning policy (use --apply to push --delete)",
	RunE:  runFlowPrune,
}

func init() {
	rootCmd.AddCommand(flowCmd)
	flowCmd.AddCommand(flowStatusCmd, flowStartCmd, flowFinishCmd, flowPruneCmd)
	flowStartCmd.AddCommand(flowStartReleaseCmd, flowStartHotfixCmd)
	flowFinishCmd.AddCommand(flowFinishHotfixCmd)

	flowStatusCmd.Flags().BoolVar(&flowFetch, "fetch", false, "run git fetch --prune on configured remote first")
	flowStatusCmd.Flags().BoolVar(&flowJSON, "json", false, "print JSON report")
	flowStatusCmd.Flags().BoolVar(&flowMermaid, "mermaid", false, "print Mermaid flowchart to stdout")

	flowStartReleaseCmd.Flags().StringVar(&flowFrom, "from", "", "ref to branch from (overrides branching.release_cut_from)")
	flowStartReleaseCmd.Flags().StringVar(&flowVersion, "version", "", "release line as MAJOR.MINOR (default: next minor after latest stable tag)")
	flowStartReleaseCmd.Flags().BoolVar(&flowPush, "push", false, "push new branch to remote and set upstream")

	flowStartHotfixCmd.Flags().StringVar(&flowFrom, "from", "", "base ref: release/M.N, tag vM.N.P, or branch (required)")
	flowStartHotfixCmd.Flags().StringVar(&flowVersion, "version", "", "hotfix version as tag (e.g. v1.2.4); default next patch on line")
	flowStartHotfixCmd.Flags().BoolVar(&flowPush, "push", false, "push new hotfix branch")
	_ = flowStartHotfixCmd.MarkFlagRequired("from")

	flowFinishHotfixCmd.Flags().StringVar(&flowHotfixBranch, "branch", "", "hotfix branch name (required)")
	flowFinishHotfixCmd.Flags().BoolVar(&flowPush, "push", false, "push each branch after merge")
	flowFinishHotfixCmd.Flags().BoolVar(&flowFFOnly, "ff-only", false, "git merge --ff-only")
	_ = flowFinishHotfixCmd.MarkFlagRequired("branch")

	flowPruneCmd.Flags().BoolVar(&flowApply, "apply", false, "actually run git push <remote> --delete (default is dry-run)")
}

func loadFlowConfig() (*config.Config, string, config.BranchingConfig, error) {
	repoAbs, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, "", config.BranchingConfig{}, fmt.Errorf("repo path: %w", err)
	}
	configPath := cfgFile
	if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(repoAbs, configPath)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, "", config.BranchingConfig{}, err
	}
	cfg.Resolve(repoAbs)
	b := cfg.BranchDefaults()
	return cfg, repoAbs, b, nil
}

func resolveRemote(c *config.Config, b config.BranchingConfig) string {
	if c.Release != nil && c.Release.Remote != "" {
		return c.Release.Remote
	}
	if b.Prune != nil && b.Prune.Remote != "" {
		return b.Prune.Remote
	}
	return "origin"
}

func runFlowStatus(cmd *cobra.Command, args []string) error {
	cfg, repoAbs, b, err := loadFlowConfig()
	if err != nil {
		return err
	}
	ctx := context.Background()
	remote := resolveRemote(cfg, b)
	if flowFetch {
		if err := git.Fetch(ctx, repoAbs, remote, true); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "✓ Fetched %s (--prune)\n", remote)
	}
	rep, err := gitflow.BuildReport(ctx, repoAbs, b)
	if err != nil {
		return err
	}
	switch {
	case flowJSON:
		return gitflow.WriteJSON(os.Stdout, rep)
	case flowMermaid:
		gitflow.WriteMermaid(os.Stdout, rep)
		return nil
	default:
		gitflow.PrintTextTable(rep)
		return nil
	}
}

func runFlowStartRelease(cmd *cobra.Command, args []string) error {
	cfg, repoAbs, b, err := loadFlowConfig()
	if err != nil {
		return err
	}
	base := strings.TrimSpace(flowFrom)
	if base == "" {
		base = strings.TrimSpace(b.ReleaseCutFrom)
	}
	if base == "" {
		return fmt.Errorf("set --from or branching.release_cut_from in .releasebot.yml (no default)")
	}
	ctx := context.Background()
	rem := resolveRemote(cfg, b)
	_, baseRef, err := git.ResolveFirst(ctx, repoAbs, base, rem+"/"+base)
	if err != nil {
		return fmt.Errorf("resolve base %q: %w", base, err)
	}
	var maj, min int
	if flowVersion != "" {
		maj, min, err = gitflow.ParseMajorMinor(flowVersion)
		if err != nil {
			return err
		}
	} else {
		tags, err := git.ListTags(ctx, repoAbs)
		if err != nil {
			return err
		}
		maj, min, err = gitflow.NextReleaseMinorFromTags(tags)
		if err != nil {
			return err
		}
	}
	branchName := gitflow.ReleaseBranchName(b.ReleasePrefix, maj, min)
	if dryRun {
		fmt.Fprintf(os.Stderr, "[dry-run] Would create %s from %s\n", branchName, base)
		return nil
	}
	if err := git.CreateBranch(ctx, repoAbs, branchName, baseRef); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "✓ Created and checked out %s from %s\n", branchName, baseRef)
	if flowPush {
		if err := git.PushBranch(ctx, repoAbs, rem, branchName, true); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "✓ Pushed %s to %s\n", branchName, rem)
	}
	return nil
}

func resolveHotfixVersion(ctx context.Context, repoAbs string, b config.BranchingConfig, fromRef string) (semver.Version, error) {
	if flowVersion != "" {
		v := semver.ParseTag(flowVersion)
		if v == nil {
			return semver.Version{}, fmt.Errorf("--version must be a semver tag like v1.2.3")
		}
		return *v, nil
	}
	fromRef = strings.TrimSpace(fromRef)
	if v := semver.ParseTag(fromRef); v != nil && v.IsStable() {
		return v.NextPatch(), nil
	}
	remote := "origin"
	if b.Prune != nil && b.Prune.Remote != "" {
		remote = b.Prune.Remote
	}
	logical := gitflow.LogicalBranch(remote, fromRef)
	relPrefix := b.ReleasePrefix
	if relPrefix == "" {
		relPrefix = "release/"
	}
	if !strings.HasSuffix(relPrefix, "/") {
		relPrefix += "/"
	}
	if strings.HasPrefix(logical, relPrefix) {
		maj, min, err := gitflow.ParseReleaseMinor(logical, relPrefix)
		if err != nil {
			return semver.Version{}, err
		}
		tags, err := git.ListTags(ctx, repoAbs)
		if err != nil {
			return semver.Version{}, err
		}
		latest := gitflow.LatestStablePatchOnMinor(tags, maj, min)
		if latest == nil {
			return semver.Version{Major: maj, Minor: min, Patch: 0}, nil
		}
		return latest.NextPatch(), nil
	}
	return semver.Version{}, fmt.Errorf("could not derive hotfix version from %q; pass --version", fromRef)
}

func runFlowStartHotfix(cmd *cobra.Command, args []string) error {
	cfg, repoAbs, b, err := loadFlowConfig()
	if err != nil {
		return err
	}
	ctx := context.Background()
	fromRef := strings.TrimSpace(flowFrom)
	hv, err := resolveHotfixVersion(ctx, repoAbs, b, fromRef)
	if err != nil {
		return err
	}
	branchName := gitflow.HotfixBranchName(b.HotfixPrefix, hv)
	if dryRun {
		fmt.Fprintf(os.Stderr, "[dry-run] Would create %s from %s\n", branchName, fromRef)
		return nil
	}
	rem := resolveRemote(cfg, b)
	startRef, _, err := git.ResolveFirst(ctx, repoAbs, fromRef, rem+"/"+strings.TrimPrefix(fromRef, rem+"/"))
	if err != nil {
		return fmt.Errorf("resolve --from %q: %w", fromRef, err)
	}
	if err := git.CreateBranch(ctx, repoAbs, branchName, startRef); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "✓ Created and checked out %s\n", branchName)
	if flowPush {
		rem := resolveRemote(cfg, b)
		if err := git.PushBranch(ctx, repoAbs, rem, branchName, true); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "✓ Pushed %s to %s\n", branchName, rem)
	}
	return nil
}

func runFlowFinishHotfix(cmd *cobra.Command, args []string) error {
	cfg, repoAbs, b, err := loadFlowConfig()
	if err != nil {
		return err
	}
	hotfix := strings.TrimSpace(flowHotfixBranch)
	v, err := gitflow.ParseHotfixVersion(hotfix, b.HotfixPrefix)
	if err != nil {
		return err
	}
	releaseBr := gitflow.ReleaseBranchName(b.ReleasePrefix, v.Major, v.Minor)
	order := []string{releaseBr, b.Main, b.Develop}
	remote := resolveRemote(cfg, b)
	ctx := context.Background()
	hfRef, _, err := git.ResolveFirst(ctx, repoAbs, hotfix, remote+"/"+strings.TrimPrefix(hotfix, remote+"/"))
	if err != nil {
		return fmt.Errorf("resolve hotfix branch %q: %w", hotfix, err)
	}

	if dryRun {
		for _, target := range order {
			fmt.Fprintf(os.Stderr, "git checkout %s && git merge %s", target, hfRef)
			if flowFFOnly {
				fmt.Fprintf(os.Stderr, " --ff-only")
			}
			fmt.Fprintln(os.Stderr)
			if flowPush {
				fmt.Fprintf(os.Stderr, "git push %s %s\n", remote, target)
			}
		}
		return nil
	}

	for _, target := range order {
		if err := git.Checkout(ctx, repoAbs, target); err != nil {
			if err2 := git.Checkout(ctx, repoAbs, remote+"/"+target); err2 != nil {
				return fmt.Errorf("checkout %s: %w", target, err)
			}
		}
		if err := git.Merge(ctx, repoAbs, hfRef, flowFFOnly); err != nil {
			return fmt.Errorf("merge %s into %s: %w", hfRef, target, err)
		}
		fmt.Fprintf(os.Stderr, "✓ Merged %s into %s\n", hfRef, target)
		if flowPush {
			br, err := git.CurrentBranch(ctx, repoAbs)
			if err != nil {
				return err
			}
			if err := git.PushBranch(ctx, repoAbs, remote, br, false); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "✓ Pushed %s\n", br)
		}
	}
	return nil
}

func runFlowPrune(cmd *cobra.Command, args []string) error {
	cfg, repoAbs, b, err := loadFlowConfig()
	if err != nil {
		return err
	}
	if b.Prune == nil || (b.Prune.MaxAgeDays <= 0 && b.Prune.RetainMinors <= 0) {
		return fmt.Errorf("configure branching.prune with max_age_days and/or retain_minors in .releasebot.yml")
	}
	ctx := context.Background()
	remote := resolveRemote(cfg, b)
	if flowFetch {
		if err := git.Fetch(ctx, repoAbs, remote, true); err != nil {
			return err
		}
	}
	branches, err := git.ListBranches(ctx, repoAbs)
	if err != nil {
		return err
	}
	var remoteBranches []string
	for _, br := range branches {
		if strings.HasPrefix(br.Name, remote+"/") {
			remoteBranches = append(remoteBranches, br.Name)
		}
	}
	tags, err := git.ListTags(ctx, repoAbs)
	if err != nil {
		return err
	}
	tagEpoch := make(map[string]int64)
	for _, t := range tags {
		epoch, err := git.CommitterEpoch(ctx, repoAbs, t)
		if err != nil {
			continue
		}
		tagEpoch[t] = epoch
	}
	candidates, ok := gitflow.PruneReleaseCandidates(b, remoteBranches, tags, tagEpoch, time.Now())
	if !ok {
		return fmt.Errorf("prune rules disabled (set max_age_days and/or retain_minors)")
	}
	if len(candidates) == 0 {
		fmt.Fprintln(os.Stderr, "No release branches match prune policy.")
		return nil
	}
	fmt.Fprintf(os.Stderr, "Prune candidates (%d):\n", len(candidates))
	for _, c := range candidates {
		fmt.Fprintf(os.Stderr, "  %s  (%s)  tag=%s\n", c.Branch, c.Reason, c.LatestTag)
	}
	if !flowApply || dryRun {
		if !flowApply {
			fmt.Fprintln(os.Stderr, "Dry-run only. Re-run with --apply to delete on remote.")
		}
		return nil
	}
	for _, c := range candidates {
		if err := git.PushDeleteRemote(ctx, repoAbs, remote, c.Branch); err != nil {
			return fmt.Errorf("delete %s: %w", c.Branch, err)
		}
		fmt.Fprintf(os.Stderr, "✓ Deleted %s/%s\n", remote, c.Branch)
	}
	return nil
}
