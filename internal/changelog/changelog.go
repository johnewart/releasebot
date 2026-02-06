package changelog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/johnewart/releasebot/internal/cache"
	"github.com/johnewart/releasebot/internal/git"
	"github.com/johnewart/releasebot/internal/github"
)

// Source is either GitHub PRs or git commits.
type Source struct {
	PRs     []github.PullRequest
	Commits []git.Commit
}

// GenerateOptions configures changelog generation.
type GenerateOptions struct {
	Version      string
	Format       string
	Source       Source
	OutputPath   string
	UseLLM       bool
	LLMProvider  string
	LLMModel     string
	LLMBaseURL   string
	ExistingHead string
	// Per-PR summarization: when true, analyze each PR independently with the LLM (one call per PR → JSON),
	// then build the final changelog from that JSON (template). Reduces context/scope per call. When false,
	// feed the LLM all PRs at once in a single call (may take longer but more contextually relevant).
	SummarizePerPR     bool
	IncludeDiff        bool // when true, pass PR diff to LLM (only when SummarizePerPR)
	CacheLLMSummaries  bool // when true, use LLMSummaryCacheDir to cache per-PR summaries
	LLMSummaryCacheDir string
	Owner              string
	Repo               string
	// ChangelogWriterTemplate (or Format) is the structure/instructions for the final changelog when using summarize_per_pr.
	// The LLM receives the summarized records (not raw PRs/diffs) and this template to produce the section.
	ChangelogWriterTemplate string
	RepoURL                 string // e.g. https://github.com/owner/repo for PR links
	// ReportLLMProgress, when non-nil, is called with progress messages during LLM work (e.g. "Generating changelog section...").
	ReportLLMProgress func(message string)
	// ReportLLMProgressBar, when non-nil, is called with (current, total) during per-PR summarization for a progress bar instead of per-PR text.
	ReportLLMProgressBar func(current, total int)
}

// Generate writes a new changelog section. If UseLLM is true, uses the LLM; otherwise formats entries with the template.
// When SummarizePerPR is true: each PR is analyzed independently (LLM → JSON, cached); then the LLM is called
// once with those summarized records (description, pr_id, change_type) to generate the changelog. When false:
// all raw PRs are fed to the LLM in one call to generate the changelog.
func Generate(ctx context.Context, opts GenerateOptions) (string, error) {
	var section string
	if opts.UseLLM && opts.SummarizePerPR && len(opts.Source.PRs) > 0 {
		var err error
		section, err = generateSectionPerPR(ctx, opts)
		if err != nil {
			return "", err
		}
	} else {
		var entries string
		if len(opts.Source.PRs) > 0 {
			var b strings.Builder
			for _, pr := range opts.Source.PRs {
				b.WriteString(fmt.Sprintf("- #%d %s (@%s)\n", pr.Number, pr.Title, pr.Author))
				if pr.Body != "" {
					b.WriteString("  " + strings.ReplaceAll(strings.TrimSpace(pr.Body), "\n", "\n  ") + "\n")
				}
			}
			entries = b.String()
		} else {
			var b strings.Builder
			for _, c := range opts.Source.Commits {
				b.WriteString(fmt.Sprintf("- %s (%s)\n", c.Subject, c.SHA[:7]))
				if c.Body != "" {
					b.WriteString("  " + strings.ReplaceAll(c.Body, "\n", "\n  ") + "\n")
				}
			}
			entries = b.String()
		}

		if opts.UseLLM {
			if opts.ReportLLMProgress != nil {
				changelogName := filepath.Base(opts.OutputPath)
				if changelogName == "" {
					changelogName = "CHANGELOG.md"
				}
				opts.ReportLLMProgress(fmt.Sprintf("Combining changelog entries to create the new %s...", changelogName))
			}
			llm, err := NewLLM(opts.LLMProvider, opts.LLMModel, opts.LLMBaseURL)
			if err != nil {
				return "", fmt.Errorf("llm: %w", err)
			}
			structure := opts.ChangelogWriterTemplate
			if structure == "" {
				structure = opts.Format
			}
			section, err = llm.GenerateChangelogSection(ctx, opts.Version, structure, entries)
			if err != nil {
				return "", fmt.Errorf("generate section: %w", err)
			}
		} else {
			section = formatSectionSimple(opts.Version, opts.Format, opts.Source)
		}
	}

	section = strings.TrimSpace(section)
	if !strings.HasSuffix(section, "\n") {
		section += "\n"
	}
	full := section
	if opts.ExistingHead != "" {
		full = section + "\n" + opts.ExistingHead
	}
	if opts.OutputPath != "" {
		if err := os.WriteFile(opts.OutputPath, []byte(full), 0644); err != nil {
			return "", fmt.Errorf("write changelog: %w", err)
		}
	}
	return full, nil
}

// generateSectionPerPR analyzes each PR independently (LLM → JSON per PR, cached to file), then calls the LLM
// once with those summarized records (description, pr_id, change_type) to generate the final changelog—not raw PRs or diffs.
func generateSectionPerPR(ctx context.Context, opts GenerateOptions) (string, error) {
	if opts.ReportLLMProgress != nil {
		opts.ReportLLMProgress("Generating summaries...")
	}
	llm, err := NewLLM(opts.LLMProvider, opts.LLMModel, opts.LLMBaseURL)
	if err != nil {
		return "", fmt.Errorf("llm: %w", err)
	}
	var summaryCache *cache.LLMSummaryCache
	if opts.CacheLLMSummaries && opts.LLMSummaryCacheDir != "" {
		summaryCache = cache.NewLLMSummaryCache(opts.LLMSummaryCacheDir)
	}
	withDiff := opts.IncludeDiff

	var changes []*PRChange
	total := len(opts.Source.PRs)
	for i, pr := range opts.Source.PRs {
		if opts.ReportLLMProgressBar != nil {
			opts.ReportLLMProgressBar(i+1, total)
		} else if opts.ReportLLMProgress != nil {
			opts.ReportLLMProgress(fmt.Sprintf("Summarizing PR %d/%d", i+1, total))
		}
		metadata := fmt.Sprintf("Title: %s\nAuthor: @%s\nMerged: %s\n\nDescription:\n%s", pr.Title, pr.Author, pr.MergedAt, pr.Body)
		diff := pr.Diff

		var raw string
		if summaryCache != nil {
			if s, ok := summaryCache.Get(opts.Owner, opts.Repo, pr.Number, withDiff); ok {
				raw = s
			}
		}
		if raw == "" {
			raw, err = llm.SummarizePR(ctx, metadata, diff, pr.Number)
			if err != nil {
				return "", fmt.Errorf("summarize PR #%d: %w", pr.Number, err)
			}
			if summaryCache != nil {
				_ = summaryCache.Set(opts.Owner, opts.Repo, pr.Number, withDiff, raw)
			}
		}
		c, err := ParsePRChangeJSON(raw, pr.Number)
		if err != nil {
			return "", fmt.Errorf("parse PR #%d response: %w", pr.Number, err)
		}
		changes = append(changes, c)
	}

	// Pass summarized records (not raw PRs/diffs) to the LLM to generate the changelog section.
	if opts.ReportLLMProgress != nil {
		changelogName := filepath.Base(opts.OutputPath)
		if changelogName == "" {
			changelogName = "CHANGELOG.md"
		}
		opts.ReportLLMProgress(fmt.Sprintf("Combining changelog entries to create the new %s...", changelogName))
	}
	entries := formatSummarizedChanges(opts.RepoURL, changes)
	structure := opts.ChangelogWriterTemplate
	if structure == "" {
		structure = opts.Format
	}
	section, err := llm.GenerateChangelogSection(ctx, opts.Version, structure, entries)
	if err != nil {
		return "", fmt.Errorf("generate changelog from summaries: %w", err)
	}
	return section, nil
}

// formatSummarizedChanges returns a string representation of per-PR summaries for the LLM to turn into a changelog section.
func formatSummarizedChanges(repoURL string, changes []*PRChange) string {
	sections := make(map[string][]*PRChange)
	for _, c := range changes {
		sections[c.ChangeType] = append(sections[c.ChangeType], c)
	}
	var b strings.Builder
	base := strings.TrimSuffix(repoURL, "/")
	for _, typ := range ValidChangeTypes {
		if list := sections[typ]; len(list) > 0 {
			b.WriteString(typ + ":\n")
			for _, c := range list {
				b.WriteString(fmt.Sprintf("  - %s (#%d %s/pulls/%d)\n", c.Description, c.PRID, base, c.PRID))
			}
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func formatSectionSimple(version, format string, src Source) string {
	var b strings.Builder
	b.WriteString("## ")
	b.WriteString(version)
	b.WriteString("\n\n")
	if len(src.PRs) > 0 {
		for _, pr := range src.PRs {
			b.WriteString("- ")
			b.WriteString(pr.Title)
			b.WriteString(" (#")
			b.WriteString(fmt.Sprintf("%d", pr.Number))
			b.WriteString(") by @")
			b.WriteString(pr.Author)
			b.WriteString("\n")
		}
	} else {
		for _, c := range src.Commits {
			b.WriteString("- ")
			b.WriteString(c.Subject)
			b.WriteString(" (")
			b.WriteString(c.SHA[:7])
			b.WriteString(")\n")
		}
	}
	return b.String()
}
