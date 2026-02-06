package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/johnewart/releasebot/internal/config"
	"github.com/johnewart/releasebot/internal/dockerhub"
	"github.com/johnewart/releasebot/internal/git"
	"github.com/johnewart/releasebot/internal/github"
	"github.com/johnewart/releasebot/internal/just"
	"github.com/johnewart/releasebot/internal/pypi"
	"github.com/johnewart/releasebot/internal/semver"
	"github.com/spf13/cobra"
)

var (
	releasePrevTag     string
	releaseBranch      string
	releaseRemote      string
	releaseRC          bool
	releaseAlpha       bool
	releaseMinor       bool
	releaseMajor       bool
	releaseNoTUI       bool
	releaseWaitTimeout time.Duration
	releasePyPIWait    time.Duration
	releaseDockerWait  time.Duration
)

var releaseCmd = &cobra.Command{
	Use:   "release",
	Short: "Full release: changelog, commit, tag, push, wait for CI and artifacts",
	Long: `Release figures out the previous version tag (or uses --prev-tag), generates a changelog
from commits/PRs between that tag and the release branch, commits the changelog, creates the next
tag (patch by default; use --release for minor, --major, --rc, --alpha), pushes branch and tags
to the remote, waits for release workflows to complete, then checks/waits for PyPI and Docker Hub
if configured. Uses an interactive TUI by default when run in a terminal (use --no-tui for plain
output). Honors --dry-run.`,
	RunE: runRelease,
}

func init() {
	rootCmd.AddCommand(releaseCmd)
	releaseCmd.Flags().StringVar(&releasePrevTag, "prev-tag", "", "previous release tag (default: latest semver tag in repo)")
	releaseCmd.Flags().StringVar(&releaseBranch, "branch", "", "branch to release from (default: current branch)")
	releaseCmd.Flags().StringVar(&releaseRemote, "remote", "", "remote to push to (default: origin or release.remote in config)")
	releaseCmd.Flags().BoolVar(&releaseRC, "rc", false, "create release candidate tag (X.Y.ZrcN)")
	releaseCmd.Flags().BoolVar(&releaseAlpha, "alpha", false, "create alpha tag (X.Y.ZaN)")
	releaseCmd.Flags().BoolVar(&releaseMinor, "release", false, "create new minor version (X.Y+1.0)")
	releaseCmd.Flags().BoolVar(&releaseMajor, "major", false, "with --release, create new major version (X+1.0.0)")
	releaseCmd.Flags().BoolVar(&releaseNoTUI, "no-tui", false, "disable TUI and use plain stderr output (default: TUI when in a terminal)")
	releaseCmd.Flags().DurationVar(&releaseWaitTimeout, "workflow-timeout", 30*time.Minute, "max time to wait for release workflows")
	releaseCmd.Flags().DurationVar(&releasePyPIWait, "pypi-timeout", 10*time.Minute, "max time to wait for PyPI package")
	releaseCmd.Flags().DurationVar(&releaseDockerWait, "docker-timeout", 10*time.Minute, "max time to wait for Docker image")
}

// releaseParams holds resolved values for the release steps (passed to doReleaseSteps / TUI).
type releaseParams struct {
	ctx             context.Context
	repoAbs         string
	cfg             *config.Config
	prev            string
	branch          string
	nextTagForRef   string
	remote          string
	outPathAbs      string
	outPath         string
	dryRun          bool
	releaseWaitTo   time.Duration
	releasePyPITo   time.Duration
	releaseDockerTo time.Duration
}

// releaseReporter is called after each step (step index, error if any, skipped).
// If nil, doReleaseSteps prints progress to stderr.
type releaseReporter func(step int, err error, skipped bool)

func runRelease(cmd *cobra.Command, args []string) error {
	if releaseRC && releaseAlpha {
		return fmt.Errorf("cannot use both --rc and --alpha")
	}
	if (releaseMinor || releaseMajor) && (releaseRC || releaseAlpha) {
		return fmt.Errorf("cannot combine --release/--major with --rc or --alpha")
	}
	if releaseMajor && !releaseMinor {
		return fmt.Errorf("--major must be used with --release")
	}

	ctx := context.Background()
	repoAbs, err := filepath.Abs(repoPath)
	if err != nil {
		return fmt.Errorf("repo path: %w", err)
	}

	configPath := cfgFile
	if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(repoAbs, configPath)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	cfg.Resolve(repoAbs)

	// Resolve branch (current or --branch)
	branch := releaseBranch
	if branch == "" {
		branch, err = git.CurrentBranch(ctx, repoAbs)
		if err != nil {
			return err
		}
	}
	// If --branch was set and we're not on it, checkout (skip in dry-run)
	if releaseBranch != "" && !dryRun {
		current, err := git.CurrentBranch(ctx, repoAbs)
		if err != nil {
			return err
		}
		if current != releaseBranch {
			if err := git.Checkout(ctx, repoAbs, releaseBranch); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "✓ Checked out %s\n", releaseBranch)
		}
	}

	// Resolve previous tag (--prev-tag or latest semver)
	prev := releasePrevTag
	if prev == "" {
		prev = cfg.PreviousReleaseTag
	}
	if prev == "" {
		tags, err := git.ListTags(ctx, repoAbs)
		if err != nil {
			return err
		}
		prev = semver.LatestStableTag(tags)
		if prev == "" {
			return fmt.Errorf("could not determine previous release tag: use --prev-tag, set previous_release_tag in config, or ensure repo has semver tags (e.g. v1.0.0)")
		}
	}
	if _, err := git.ValidateTag(ctx, repoAbs, prev); err != nil {
		return err
	}

	// Next tag (same logic as tag next)
	tags, err := git.ListTags(ctx, repoAbs)
	if err != nil {
		return err
	}
	nextTag := semver.NextFromTags(tags, releaseRC, releaseAlpha, releaseMinor, releaseMajor)
	// Ensure tag has 'v' for push (NextFromTags returns "v1.2.3" for stable, "1.2.3rc0" for rc)
	nextTagForRef := nextTag
	if !strings.HasPrefix(nextTag, "v") && (releaseRC || releaseAlpha) {
		// keep as-is for rc/alpha
	} else if !strings.HasPrefix(nextTag, "v") {
		nextTagForRef = "v" + nextTag
	}

	// Remote
	remote := releaseRemote
	if remote == "" && cfg.Release != nil && cfg.Release.Remote != "" {
		remote = cfg.Release.Remote
	}
	if remote == "" {
		remote = "origin"
	}
	if _, err := git.RemoteURL(ctx, repoAbs, remote); err != nil {
		return fmt.Errorf("remote %s: %w", remote, err)
	}

	// Changelog output path (relative to repo)
	outPath := "CHANGELOG.md"
	if cfg.Changelog != nil && cfg.Changelog.Output != "" {
		outPath = cfg.Changelog.Output
	}
	outPathAbs := outPath
	if !filepath.IsAbs(outPathAbs) {
		outPathAbs = filepath.Join(repoAbs, outPathAbs)
	}

	params := &releaseParams{
		ctx:             ctx,
		repoAbs:         repoAbs,
		cfg:             cfg,
		prev:            prev,
		branch:          branch,
		nextTagForRef:   nextTagForRef,
		remote:          remote,
		outPathAbs:      outPathAbs,
		outPath:         outPath,
		dryRun:          dryRun,
		releaseWaitTo:   releaseWaitTimeout,
		releasePyPITo:   releasePyPIWait,
		releaseDockerTo: releaseDockerWait,
	}

	// TUI is the default when stdout is a TTY (Bubble Tea renders to stdout); use --no-tui for plain output.
	if isTerminal(os.Stdout) && !releaseNoTUI {
		return runReleaseTUI(params)
	}

	// Plain output path (no TUI): dry-run or --no-tui or not a TTY
	if dryRun {
		fmt.Fprintf(os.Stderr, "✓ Previous tag %s validated\n", prev)
		if cfg.Justfile != nil && len(cfg.Justfile.Targets) > 0 {
			fmt.Fprintf(os.Stderr, "✓ Just targets completed: %v\n", cfg.Justfile.Targets)
		}
		src, err := gatherChangelogSource(ctx, cfg, repoAbs, prev, branch, 0, nil, nil)
		if err != nil {
			return fmt.Errorf("dry-run gather: %w", err)
		}
		if len(src.PRs) > 0 {
			fmt.Fprintf(os.Stderr, "✓ Found %d merged PR(s) between %s and %s\n", len(src.PRs), prev, branch)
		} else {
			fmt.Fprintf(os.Stderr, "✓ Found %d commit(s) between %s and %s\n", len(src.Commits), prev, branch)
		}
		fmt.Fprintf(os.Stderr, "✓ Changelog written to %s\n", outPathAbs)
		fmt.Fprintf(os.Stderr, "✓ Committed and tagged %s\n", nextTagForRef)
		fmt.Fprintf(os.Stderr, "✓ Pushed %s to %s\n", branch, remote)
		fmt.Fprintf(os.Stderr, "✓ Pushed tag %s to %s\n", nextTagForRef, remote)
		fmt.Fprintf(os.Stderr, "✓ All release workflow(s) completed\n")
		if cfg.Release != nil && cfg.Release.PyPIPackage != "" {
			pkgVersion := strings.TrimPrefix(nextTagForRef, "v")
			fmt.Fprintf(os.Stderr, "✓ Package %s==%s is available on PyPI\n", cfg.Release.PyPIPackage, pkgVersion)
		}
		if cfg.Release != nil && cfg.Release.DockerImage != "" {
			fmt.Fprintf(os.Stderr, "✓ Image %s:%s is available on Docker Hub\n", cfg.Release.DockerImage, nextTagForRef)
		}
		fmt.Fprintf(os.Stderr, "✓ Release %s complete (dry-run)\n", nextTagForRef)
		return nil
	}
	return doReleaseSteps(params, nil)
}

// doReleaseSteps runs the 7 release steps. If report is non-nil, it's called after each step (for TUI);
// if nil, progress is printed to stderr.
func doReleaseSteps(params *releaseParams, report releaseReporter) error {
	ctx := params.ctx
	repoAbs := params.repoAbs
	cfg := params.cfg
	branch := params.branch
	nextTagForRef := params.nextTagForRef
	remote := params.remote
	outPathAbs := params.outPathAbs
	outPath := params.outPath

	// 0. Just targets
	hasJust := cfg.Justfile != nil && len(cfg.Justfile.Targets) > 0
	if hasJust {
		workDir := repoAbs
		if cfg.Justfile.WorkingDir != "" {
			workDir = cfg.Justfile.WorkingDir
		}
		result, err := just.Runner(workDir, cfg.Justfile.Targets)
		if err != nil {
			if report != nil {
				report(0, err, false)
			}
			return fmt.Errorf("just: %w", err)
		}
		if !result.Success() {
			err := fmt.Errorf("just target(s) failed: %v", result.Failed)
			if report != nil {
				report(0, err, false)
			}
			return err
		}
		if report != nil {
			report(0, nil, false)
		} else {
			fmt.Fprintf(os.Stderr, "✓ Just targets completed: %v\n", cfg.Justfile.Targets)
		}
	} else if report != nil {
		report(0, nil, true)
	}

	// 1. Generate changelog
	if err := generateChangelogSection(ctx, cfg, repoAbs, params.prev, branch, nextTagForRef, outPathAbs, 0); err != nil {
		if report != nil {
			report(1, err, false)
		}
		return fmt.Errorf("changelog: %w", err)
	}
	if report != nil {
		report(1, nil, false)
	} else {
		fmt.Fprintf(os.Stderr, "✓ Changelog written to %s\n", outPathAbs)
	}

	// 2. Git add, commit, tag
	changelogRel, err := filepath.Rel(repoAbs, outPathAbs)
	if err != nil {
		changelogRel = outPath
	}
	if err := git.Add(ctx, repoAbs, changelogRel); err != nil {
		if report != nil {
			report(2, err, false)
		}
		return err
	}
	if err := git.CreateCommit(ctx, repoAbs, "changelog: release "+nextTagForRef); err != nil {
		if report != nil {
			report(2, err, false)
		}
		return err
	}
	if err := git.CreateTag(ctx, repoAbs, nextTagForRef, "Release "+nextTagForRef); err != nil {
		if report != nil {
			report(2, err, false)
		}
		return err
	}
	if report != nil {
		report(2, nil, false)
	} else {
		fmt.Fprintf(os.Stderr, "✓ Committed and tagged %s\n", nextTagForRef)
	}

	// 3. Push branch and tag
	if err := git.Push(ctx, repoAbs, remote, "refs/heads/"+branch); err != nil {
		if report != nil {
			report(3, err, false)
		}
		return err
	}
	if err := git.Push(ctx, repoAbs, remote, "refs/tags/"+nextTagForRef); err != nil {
		if report != nil {
			report(3, err, false)
		}
		return err
	}
	if report != nil {
		report(3, nil, false)
	} else {
		fmt.Fprintf(os.Stderr, "✓ Pushed %s to %s\n", branch, remote)
		fmt.Fprintf(os.Stderr, "✓ Pushed tag %s to %s\n", nextTagForRef, remote)
	}

	// 4. Wait for release workflows
	sha, err := git.RevParse(ctx, repoAbs, nextTagForRef)
	if err != nil {
		if report != nil {
			report(4, err, false)
		}
		return fmt.Errorf("resolve tag to SHA: %w", err)
	}
	owner, repoName := "", ""
	if cfg.GitHub != nil && cfg.GitHub.Owner != "" && cfg.GitHub.Repo != "" {
		owner = cfg.GitHub.Owner
		repoName = cfg.GitHub.Repo
	} else {
		remoteURL, err := git.RemoteURL(ctx, repoAbs, remote)
		if err != nil {
			if report != nil {
				report(4, err, false)
			}
			return err
		}
		owner, repoName, err = git.ParseGitHubOwnerRepo(remoteURL)
		if err != nil {
			if report != nil {
				report(4, err, false)
			}
			return fmt.Errorf("github remote: %w", err)
		}
	}
	token := ""
	if cfg.GitHub != nil && cfg.GitHub.Token != "" {
		token = cfg.GitHub.Token
	}
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token == "" {
		if report != nil {
			report(4, nil, true)
		} else {
			fmt.Fprintf(os.Stderr, "warning: no GITHUB_TOKEN; skipping workflow wait\n")
		}
	} else {
		gh := github.NewClient(ctx, token, owner, repoName)
		tagPushTriggers, _ := github.WorkflowsTriggeredByTag(repoAbs, nextTagForRef)
		deadline := time.Now().Add(params.releaseWaitTo)
		pollInterval := 15 * time.Second
		workflowsDone := false
		for time.Now().Before(deadline) {
			runs, err := gh.ListWorkflowRunsForCommit(ctx, sha)
			if err != nil {
				if report != nil {
					report(4, err, false)
				}
				return fmt.Errorf("list workflow runs: %w", err)
			}
			waitedRuns := runs
			if len(tagPushTriggers) > 0 {
				waitedRuns = github.RunsForTagPushWorkflows(runs, tagPushTriggers)
			}
			if len(waitedRuns) == 0 {
				if report == nil {
					fmt.Fprintf(os.Stderr, "Waiting for release workflows... (next check in %s)\n", pollInterval)
				}
				time.Sleep(pollInterval)
				continue
			}
			allSeen := len(tagPushTriggers) == 0 || len(waitedRuns) >= len(tagPushTriggers)
			if allSeen && github.AllRunsFinished(waitedRuns) {
				if github.AnyRunFailed(waitedRuns) {
					err := fmt.Errorf("one or more release workflows failed")
					if report != nil {
						report(4, err, false)
					}
					return err
				}
				workflowsDone = true
				break
			}
			if report == nil {
				fmt.Fprintf(os.Stderr, "Waiting for workflows... (next check in %s)\n", pollInterval)
			}
			time.Sleep(pollInterval)
		}
		if !workflowsDone {
			err := fmt.Errorf("timeout waiting for release workflows")
			if report != nil {
				report(4, err, false)
			}
			return err
		}
		if report != nil {
			report(4, nil, false)
		} else {
			fmt.Fprintf(os.Stderr, "✓ All release workflow(s) completed\n")
		}
	}

	// 5. PyPI wait
	if cfg.Release != nil && cfg.Release.PyPIPackage != "" {
		pkgVersion := strings.TrimPrefix(nextTagForRef, "v")
		opts := pypi.WaitOptions{Timeout: params.releasePyPITo, Interval: 5 * time.Second}
		if err := pypi.Wait(ctx, cfg.Release.PyPIPackage, pkgVersion, opts); err != nil {
			if report != nil {
				report(5, err, false)
			}
			return fmt.Errorf("pypi wait: %w", err)
		}
		if report != nil {
			report(5, nil, false)
		} else {
			fmt.Fprintf(os.Stderr, "✓ Package %s==%s is available on PyPI\n", cfg.Release.PyPIPackage, pkgVersion)
		}
	} else if report != nil {
		report(5, nil, true)
	}

	// 6. Docker Hub wait
	if cfg.Release != nil && cfg.Release.DockerImage != "" {
		imageRef := cfg.Release.DockerImage + ":" + nextTagForRef
		opts := dockerhub.WaitOptions{Timeout: params.releaseDockerTo, Interval: 5 * time.Second}
		if err := dockerhub.Wait(ctx, imageRef, opts); err != nil {
			if report != nil {
				report(6, err, false)
			}
			return fmt.Errorf("docker hub wait: %w", err)
		}
		if report != nil {
			report(6, nil, false)
		} else {
			fmt.Fprintf(os.Stderr, "✓ Image %s is available on Docker Hub\n", imageRef)
		}
	} else if report != nil {
		report(6, nil, true)
	}

	if report == nil {
		fmt.Fprintf(os.Stderr, "✓ Release %s complete\n", nextTagForRef)
	}
	return nil
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
