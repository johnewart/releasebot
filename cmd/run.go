package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/johnewart/releasebot/internal/cache"
	"github.com/johnewart/releasebot/internal/changelog"
	"github.com/johnewart/releasebot/internal/config"
	"github.com/johnewart/releasebot/internal/git"
	"github.com/johnewart/releasebot/internal/github"
	"github.com/johnewart/releasebot/internal/just"
	"github.com/johnewart/releasebot/internal/semver"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Execute the release plan",
	Long: `Run loads .releasebot.yml, validates the previous release tag, optionally runs
justfile targets, then generates or updates CHANGELOG.md using an LLM (or simple template)
with data from GitHub PRs (if configured) or git commit log.`,
	RunE: runRun,
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.Flags().StringVar(&prevTag, "prev-tag", "", "previous release tag (overrides config)")
	runCmd.Flags().StringVar(&headRef, "head", "HEAD", "head ref for changelog range (default: HEAD)")
	runCmd.Flags().IntVar(&prLimit, "limit", 0, "max number of PRs to include in changelog (0 = no limit)")
}

func runRun(cmd *cobra.Command, args []string) error {
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

	// Resolve previous tag (CLI overrides config, then latest stable tag)
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

	// 1. Validate previous tag
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
		return runRunTUI(ctx, cfg, repoAbs, prev, headRef, outPath, version, prLimit, dryRun)
	}

	fmt.Fprintf(os.Stderr, "✓ Previous tag %s validated\n", prev)

	// Run justfile targets if configured (plain path only)
	if cfg.Justfile != nil && len(cfg.Justfile.Targets) > 0 {
		if dryRun {
			fmt.Fprintf(os.Stderr, "[dry-run] Would run just targets: %v\n", cfg.Justfile.Targets)
		} else {
			workDir := repoAbs
			if cfg.Justfile.WorkingDir != "" {
				workDir = cfg.Justfile.WorkingDir
			}
			result, err := just.Runner(workDir, cfg.Justfile.Targets)
			if err != nil {
				return fmt.Errorf("just: %w", err)
			}
			if !result.Success() {
				return fmt.Errorf("just target(s) failed: %v", result.Failed)
			}
			fmt.Fprintf(os.Stderr, "✓ Just targets completed: %v\n", cfg.Justfile.Targets)
		}
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
		fmt.Fprintf(os.Stderr, "[dry-run] Would generate changelog and write to %s (%d %s)\n", outPath, entries, sourceDesc)
		return nil
	}

	if err := generateChangelogSection(ctx, cfg, repoAbs, prev, headRef, version, outPath, prLimit, nil, nil, nil); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "✓ Changelog written to %s\n", outPath)
	return nil
}

func runRunTUI(ctx context.Context, cfg *config.Config, repoAbs, prev, headRef, outPath, version string, prLimit int, dryRun bool) error {
	if dryRun {
		steps := []string{"Gathering plan..."}
		return RunTaskTUI(" releasebot  run (dry-run) ", steps, func(ch chan<- interface{}) {
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
			if cfg.Justfile != nil && len(cfg.Justfile.Targets) > 0 {
				lines = append(lines, fmt.Sprintf("⏭️ Would run just targets: %v", cfg.Justfile.Targets))
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
	steps := []string{"Just targets", "Generate changelog"}
	return RunTaskTUI(" releasebot  run ", steps, func(ch chan<- interface{}) {
		// Step 0: Just targets
		hasJust := cfg.Justfile != nil && len(cfg.Justfile.Targets) > 0
		if hasJust {
			workDir := repoAbs
			if cfg.Justfile.WorkingDir != "" {
				workDir = cfg.Justfile.WorkingDir
			}
			result, err := just.Runner(workDir, cfg.Justfile.Targets)
			if err != nil {
				ch <- taskStepResultMsg{Step: 0, Err: err}
				ch <- taskDoneMsg{Err: err}
				return
			}
			if !result.Success() {
				err := fmt.Errorf("just target(s) failed: %v", result.Failed)
				ch <- taskStepResultMsg{Step: 0, Err: err}
				ch <- taskDoneMsg{Err: err}
				return
			}
			ch <- taskStepResultMsg{Step: 0, Err: nil, Skipped: false}
		} else {
			ch <- taskStepResultMsg{Step: 0, Err: nil, Skipped: true}
		}
		// Step 1: Generate changelog (gather + write, with progress)
		report := func(line string) { ch <- taskStatusMsg{Line: line} }
		reportProgress := func(current, total int) { ch <- taskProgressMsg{Current: current, Total: total} }
		reportLLM := func(msg string) { ch <- taskStatusMsg{Line: msg} }
		err := generateChangelogSection(ctx, cfg, repoAbs, prev, headRef, version, outPath, prLimit, report, reportProgress, reportLLM)
		ch <- taskStepResultMsg{Step: 1, Err: err}
		ch <- taskDoneMsg{Err: err}
	})
}

// generateChangelogSection gathers source (PRs or commits) between prev and headRef, then generates
// the changelog section with the given version and writes to outPath. Used by run, release, and changelog.
// When report/reportProgress are non-nil (e.g. from TUI), progress is reported during gather.
// When reportLLM is non-nil, it is called with progress during LLM work (e.g. "Summarizing PR 3/12").
func generateChangelogSection(ctx context.Context, cfg *config.Config, repoAbs, prev, headRef, version, outPath string, prLimit int, report func(string), reportProgress func(current, total int), reportLLM func(string)) error {
	format, err := cfg.ChangelogFormat(repoAbs)
	if err != nil {
		return err
	}
	var owner, repo string
	useGitHub := cfg.GitHub != nil && cfg.GitHub.Enabled
	if useGitHub {
		owner = cfg.GitHub.Owner
		repo = cfg.GitHub.Repo
		if owner == "" || repo == "" {
			remote, err := git.RemoteOriginURL(ctx, repoAbs)
			if err != nil {
				return fmt.Errorf("github not configured and could not get remote: %w", err)
			}
			owner, repo, err = git.ParseGitHubOwnerRepo(remote)
			if err != nil {
				return err
			}
		}
	}
	src, err := gatherChangelogSource(ctx, cfg, repoAbs, prev, headRef, prLimit, report, reportProgress)
	if err != nil {
		return err
	}
	provider, model, baseURL := resolveLLMConfig(cfg)
	useLLM := provider != ""
	summarizePerPR, includeDiff, cacheLLMSummaries := resolvePerPRConfig(cfg)
	opts := changelog.GenerateOptions{
		Version:            version,
		Format:             format,
		Source:             src,
		OutputPath:         outPath,
		UseLLM:             useLLM,
		LLMProvider:        provider,
		LLMModel:           model,
		LLMBaseURL:         baseURL,
		SummarizePerPR:     summarizePerPR,
		IncludeDiff:        includeDiff,
		CacheLLMSummaries:  cacheLLMSummaries,
		LLMSummaryCacheDir: filepath.Join(repoAbs, cache.DefaultDir, "llm_pr"),
	}
	if useGitHub {
		opts.Owner = owner
		opts.Repo = repo
		opts.RepoURL = fmt.Sprintf("https://github.com/%s/%s", owner, repo)
	}
	if useLLM || summarizePerPR {
		tmpl, err := cfg.ChangelogTemplate(repoAbs)
		if err != nil {
			return fmt.Errorf("changelog template: %w", err)
		}
		opts.ChangelogWriterTemplate = tmpl
	}
	if report == nil && useLLM {
		if summarizePerPR {
			fmt.Fprintf(os.Stderr, "✓ Using LLM (%s) per-PR (include_diff=%v, cache=%v)\n", provider, includeDiff, cacheLLMSummaries)
		} else {
			fmt.Fprintf(os.Stderr, "✓ Using LLM (%s) to generate changelog section\n", provider)
		}
	}
	if useGitHub && useLLM && summarizePerPR && includeDiff && len(src.PRs) > 0 {
		token := cfg.GitHub.Token
		if token == "" {
			token = os.Getenv("GITHUB_TOKEN")
		}
		gh := github.NewClient(ctx, token, owner, repo)
		for i := range src.PRs {
			diff, err := gh.GetPRDiff(ctx, src.PRs[i].Number)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not fetch diff for PR #%d: %v\n", src.PRs[i].Number, err)
				continue
			}
			src.PRs[i].Diff = diff
		}
		opts.Source = src
	}
	if data, err := os.ReadFile(outPath); err == nil {
		opts.ExistingHead = string(data)
	}
	if reportLLM != nil {
		opts.ReportLLMProgress = reportLLM
	}
	_, err = changelog.Generate(ctx, opts)
	return err
}

// gatherChangelogSource fetches PRs or commits between prev and headRef (no LLM, no writing).
// If report is non-nil, it is called with progress messages. If reportProgress is non-nil (current, total),
// it is used during GitHub PR fetch instead of per-commit status lines (e.g. for a progress bar).
func gatherChangelogSource(ctx context.Context, cfg *config.Config, repoAbs, prev, headRef string, prLimit int, report func(string), reportProgress func(current, total int)) (changelog.Source, error) {
	var src changelog.Source
	useGitHub := cfg.GitHub != nil && cfg.GitHub.Enabled
	if useGitHub {
		owner := cfg.GitHub.Owner
		repo := cfg.GitHub.Repo
		if owner == "" || repo == "" {
			remote, err := git.RemoteOriginURL(ctx, repoAbs)
			if err != nil {
				return src, fmt.Errorf("github not configured and could not get remote: %w", err)
			}
			owner, repo, err = git.ParseGitHubOwnerRepo(remote)
			if err != nil {
				return src, err
			}
		}
		prCache := cache.NewPRCache(filepath.Join(repoAbs, cache.DefaultDir))
		if prs, ok := prCache.Get(owner, repo, prev, headRef); ok {
			src.PRs = prs
			if prLimit > 0 && len(src.PRs) > prLimit {
				src.PRs = src.PRs[:prLimit]
			}
			if report != nil {
				report("Using " + fmt.Sprintf("%d", len(src.PRs)) + " merged PR(s) from cache")
			}
		} else {
			if report != nil {
				report("Querying GitHub for merged PRs between " + prev + " and " + headRef + "...")
			}
			token := cfg.GitHub.Token
			if token == "" {
				token = os.Getenv("GITHUB_TOKEN")
			}
			gh := github.NewClient(ctx, token, owner, repo)
			var prs []github.PullRequest
			var errGH error
			if report != nil || reportProgress != nil {
				prs, errGH = gh.MergedPRsBetweenWithProgress(ctx, prev, headRef, report, reportProgress)
			} else {
				prs, errGH = gh.MergedPRsBetween(ctx, prev, headRef)
			}
			if errGH != nil {
				return src, fmt.Errorf("github merged PRs: %w", errGH)
			}
			_ = prCache.Set(owner, repo, prev, headRef, prs)
			src.PRs = prs
		}
		if prLimit > 0 && len(src.PRs) > prLimit {
			src.PRs = src.PRs[:prLimit]
			if report != nil {
				report("Limiting to " + fmt.Sprintf("%d", prLimit) + " PR(s)")
			}
		}
	} else {
		if report != nil {
			report("Reading git log between " + prev + " and " + headRef + "...")
		}
		commits, err := git.LogBetween(ctx, repoAbs, prev, headRef)
		if err != nil {
			return src, fmt.Errorf("git log: %w", err)
		}
		src.Commits = commits
		if report != nil {
			report("Found " + fmt.Sprintf("%d", len(commits)) + " commits")
		}
	}
	if report == nil {
		if len(src.PRs) > 0 {
			fmt.Fprintf(os.Stderr, "✓ Found %d merged PR(s) between %s and %s\n", len(src.PRs), prev, headRef)
		} else if len(src.Commits) > 0 {
			fmt.Fprintf(os.Stderr, "✓ Found %d commit(s) between %s and %s\n", len(src.Commits), prev, headRef)
		}
	}
	return src, nil
}

// resolveLLMConfig returns provider, model, baseURL. Empty provider means no LLM.
// Reads from changelog.llm first, then top-level llm (so either works).
func resolveLLMConfig(cfg *config.Config) (provider, model, baseURL string) {
	if cfg.Changelog != nil && cfg.Changelog.LLM != nil {
		provider = cfg.Changelog.LLM.Provider
		model = cfg.Changelog.LLM.Model
		baseURL = cfg.Changelog.LLM.BaseURL
	}
	if provider == "" && model == "" && baseURL == "" && cfg.LLM != nil {
		provider = cfg.LLM.Provider
		model = cfg.LLM.Model
		baseURL = cfg.LLM.BaseURL
	}
	if p := os.Getenv("RELEASEBOT_LLM_PROVIDER"); p != "" {
		provider = p
	}
	// Backward compat: OPENAI_API_KEY with no config => openai
	if provider == "" && os.Getenv("OPENAI_API_KEY") != "" {
		provider = changelog.ProviderOpenAI
		if model == "" {
			model = "gpt-4o-mini"
		}
	}
	// Default model per provider when using config
	if provider == changelog.ProviderOllama && model == "" {
		model = "llama3.2"
	}
	if provider == changelog.ProviderOpenAI && model == "" {
		model = "gpt-4o-mini"
	}
	if provider == changelog.ProviderAnthropic && model == "" {
		model = "claude-sonnet-4-5-20250929"
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	model = strings.TrimSpace(model)
	baseURL = strings.TrimSpace(baseURL)
	return provider, model, baseURL
}

// resolvePerPRConfig returns summarize_per_pr, include_diff, cache_llm_summaries from config.
func resolvePerPRConfig(cfg *config.Config) (summarizePerPR, includeDiff, cacheLLMSummaries bool) {
	var llm *config.LLMConfig
	if cfg.Changelog != nil && cfg.Changelog.LLM != nil {
		llm = cfg.Changelog.LLM
	} else if cfg.LLM != nil {
		llm = cfg.LLM
	}
	if llm == nil {
		return false, false, true
	}
	summarizePerPR = llm.SummarizePerPR
	includeDiff = llm.IncludeDiff
	if llm.CacheLLMSummaries != nil {
		cacheLLMSummaries = *llm.CacheLLMSummaries
	} else {
		cacheLLMSummaries = summarizePerPR // default on when per-PR is on
	}
	return summarizePerPR, includeDiff, cacheLLMSummaries
}
