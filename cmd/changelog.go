package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/johnewart/releasebot/internal/config"
	"github.com/johnewart/releasebot/internal/git"
	"github.com/johnewart/releasebot/internal/semver"
	"github.com/spf13/cobra"
)

var changelogCmd = &cobra.Command{
	Use:   "changelog",
	Short: "Generate changelog only",
	Long: `Generate or update the changelog file (e.g. CHANGELOG.md) between the previous
release tag and HEAD (or --head). Does not run justfile targets, commit, tag, or push.
Uses the same config, GitHub PRs or git commits, and LLM/template as 'run'.`,
	RunE: runChangelog,
}

func init() {
	rootCmd.AddCommand(changelogCmd)
	changelogCmd.Flags().StringVar(&prevTag, "prev-tag", "", "previous release tag (overrides config)")
	changelogCmd.Flags().StringVar(&headRef, "head", "HEAD", "head ref for changelog range (default: HEAD)")
	changelogCmd.Flags().IntVar(&prLimit, "limit", 0, "max number of PRs to include (0 = no limit)")
}

func runChangelog(cmd *cobra.Command, args []string) error {
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

	prev := prevTag
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

	outPath := "CHANGELOG.md"
	if cfg.Changelog != nil && cfg.Changelog.Output != "" {
		outPath = cfg.Changelog.Output
		if !filepath.IsAbs(outPath) {
			outPath = filepath.Join(repoAbs, outPath)
		}
	}

	version := "Unreleased"
	if headRef != "" && headRef != "HEAD" {
		version = headRef
	}

	if isTerminal(os.Stdout) && !noTUI {
		return runChangelogTUI(ctx, cfg, repoAbs, prev, headRef, version, outPath, prLimit, dryRun)
	}

	if dryRun {
		src, err := gatherChangelogSource(ctx, cfg, repoAbs, prev, headRef, prLimit, nil, nil)
		if err != nil {
			return err
		}
		entries := len(src.PRs)
		if entries == 0 {
			entries = len(src.Commits)
		}
		sourceDesc := "commits"
		if len(src.PRs) > 0 {
			sourceDesc = "PRs"
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "[dry-run] Would generate changelog and write to %s (%d %s)\n", outPath, entries, sourceDesc)
		return nil
	}

	return generateChangelogSection(ctx, cfg, repoAbs, prev, headRef, version, outPath, prLimit, nil, nil, nil, nil)
}

func runChangelogTUI(ctx context.Context, cfg *config.Config, repoAbs, prev, headRef, version, outPath string, prLimit int, dryRun bool) error {
	if dryRun {
		steps := []string{"Gathering plan..."}
		return RunTaskTUI(" releasebot  changelog (dry-run) ", steps, func(ch chan<- interface{}) {
			report := func(line string) { ch <- taskStatusMsg{Line: line} }
			reportProgress := func(current, total int) { ch <- taskProgressMsg{Current: current, Total: total} }
			src, err := gatherChangelogSource(ctx, cfg, repoAbs, prev, headRef, prLimit, report, reportProgress)
			if err != nil {
				ch <- taskDoneMsg{Err: err}
				return
			}
			entries := len(src.PRs)
			if entries == 0 {
				entries = len(src.Commits)
			}
			sourceDesc := "commits"
			if len(src.PRs) > 0 {
				sourceDesc = "PRs"
			}
			lines := []string{
				"✅ Previous tag " + prev + " validated",
			}
			if len(src.PRs) > 0 {
				lines = append(lines, fmt.Sprintf("✅ Found %d merged PR(s) between %s and %s", len(src.PRs), prev, headRef))
			} else {
				lines = append(lines, fmt.Sprintf("✅ Found %d commit(s) between %s and %s", len(src.Commits), prev, headRef))
			}
			lines = append(lines, fmt.Sprintf("⏭️ Would generate changelog and write to %s (%d %s)", outPath, entries, sourceDesc))
			ch <- taskPlanMsg{Lines: lines}
		})
	}
	steps := []string{"Generate changelog"}
	return RunTaskTUI(" releasebot  changelog ", steps, func(ch chan<- interface{}) {
		report := func(line string) { ch <- taskStatusMsg{Line: line} }
		reportProgress := func(current, total int) { ch <- taskProgressMsg{Current: current, Total: total} }
		reportLLM := func(msg string) { ch <- taskStatusMsg{Line: msg} }
		reportLLMProgressBar := func(current, total int) {
			ch <- taskProgressMsg{Current: current, Total: total, Label: "Generating summaries"}
		}
		err := generateChangelogSection(ctx, cfg, repoAbs, prev, headRef, version, outPath, prLimit, report, reportProgress, reportLLM, reportLLMProgressBar)
		ch <- taskStepResultMsg{Step: 0, Err: err}
		ch <- taskDoneMsg{Err: err}
	})
}
