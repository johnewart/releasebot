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

	// Resolve previous tag (CLI overrides config)
	prev := prevTag
	if prev == "" {
		prev = cfg.PreviousReleaseTag
	}
	if prev == "" {
		return fmt.Errorf("previous release tag is required (--prev-tag or previous_release_tag in config)")
	}

	// 1. Validate previous tag
	if _, err := git.ValidateTag(ctx, repoAbs, prev); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "✓ Previous tag %s validated\n", prev)

	// 2. Run justfile targets if configured
	if cfg.Justfile != nil && len(cfg.Justfile.Targets) > 0 {
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

	// 3. Gather changelog source (GitHub PRs or git commits)
	format, err := cfg.ChangelogFormat(repoAbs)
	if err != nil {
		return err
	}

	outPath := "CHANGELOG.md"
	if cfg.Changelog != nil && cfg.Changelog.Output != "" {
		outPath = cfg.Changelog.Output
		if !filepath.IsAbs(outPath) {
			outPath = filepath.Join(repoAbs, outPath)
		}
	}

	var src changelog.Source
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
		prCache := cache.NewPRCache(filepath.Join(repoAbs, cache.DefaultDir))
		if prs, ok := prCache.Get(owner, repo, prev, headRef); ok {
			src.PRs = prs
			fmt.Fprintf(os.Stderr, "✓ Using %d merged PR(s) from cache (between %s and %s)\n", len(prs), prev, headRef)
			if prLimit > 0 && len(src.PRs) > prLimit {
				src.PRs = src.PRs[:prLimit]
				fmt.Fprintf(os.Stderr, "✓ Limiting to %d PR(s) (--limit)\n", prLimit)
			}
		} else {
			token := cfg.GitHub.Token
			if token == "" {
				token = os.Getenv("GITHUB_TOKEN")
			}
			gh := github.NewClient(ctx, token, owner, repo)
			prs, err := gh.MergedPRsBetween(ctx, prev, headRef)
			if err != nil {
				return fmt.Errorf("github merged PRs: %w", err)
			}
			if err := prCache.Set(owner, repo, prev, headRef, prs); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not write PR cache: %v\n", err)
			}
			src.PRs = prs
			fmt.Fprintf(os.Stderr, "✓ Found %d merged PR(s) between %s and %s (cached)\n", len(prs), prev, headRef)
		}
		if prLimit > 0 && len(src.PRs) > prLimit {
			src.PRs = src.PRs[:prLimit]
			fmt.Fprintf(os.Stderr, "✓ Limiting to %d PR(s) (--limit)\n", prLimit)
		}
	} else {
		commits, err := git.LogBetween(ctx, repoAbs, prev, headRef)
		if err != nil {
			return fmt.Errorf("git log: %w", err)
		}
		src.Commits = commits
		fmt.Fprintf(os.Stderr, "✓ Found %d commit(s) between %s and %s\n", len(commits), prev, headRef)
	}

	// Version for the new section: use headRef if it looks like a tag, else "Unreleased"
	version := "Unreleased"
	if headRef != "" && headRef != "HEAD" {
		version = headRef
	}

	// 4. Generate changelog (with LLM if configured or OPENAI_API_KEY set)
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
	// When per-PR + include_diff, fetch PR diffs (need GitHub client)
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
	if opts.UseLLM {
		if summarizePerPR {
			fmt.Fprintf(os.Stderr, "✓ Using LLM (%s) per-PR (include_diff=%v, cache=%v)\n", provider, includeDiff, cacheLLMSummaries)
		} else {
			fmt.Fprintf(os.Stderr, "✓ Using LLM (%s) to generate changelog section\n", provider)
		}
	}
	// Prepend to existing CHANGELOG if present
	if data, err := os.ReadFile(outPath); err == nil {
		opts.ExistingHead = string(data)
	}
	_, err = changelog.Generate(ctx, opts)
	if err != nil {
		return fmt.Errorf("changelog: %w", err)
	}
	fmt.Fprintf(os.Stderr, "✓ Changelog written to %s\n", outPath)
	return nil
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
