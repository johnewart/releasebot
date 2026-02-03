package changelog

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"text/template"

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
	// Per-PR summarization: call LLM once per PR and optionally cache.
	SummarizePerPR     bool
	IncludeDiff        bool // when true, pass PR diff to LLM (only when SummarizePerPR)
	CacheLLMSummaries  bool // when true, use LLMSummaryCacheDir to cache per-PR summaries
	LLMSummaryCacheDir string
	Owner              string
	Repo               string
	// ChangelogWriterTemplate is the Go text/template for the final section when using summarize_per_pr (grouped by change_type).
	ChangelogWriterTemplate string
	RepoURL                 string // e.g. https://github.com/owner/repo for PR links
}

// Generate writes a new changelog section. If UseLLM is true, uses the LLM; otherwise formats entries with the template.
// When SummarizePerPR is true, each PR is summarized by the LLM individually (with optional diff and caching).
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
			llm, err := NewLLM(opts.LLMProvider, opts.LLMModel, opts.LLMBaseURL)
			if err != nil {
				return "", fmt.Errorf("llm: %w", err)
			}
			section, err = llm.GenerateChangelogSection(ctx, opts.Version, opts.Format, entries)
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

// generateSectionPerPR runs the LLM once per PR for structured JSON (change_type, description, pr_id), then runs the changelog writer template.
func generateSectionPerPR(ctx context.Context, opts GenerateOptions) (string, error) {
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
	for _, pr := range opts.Source.PRs {
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

	return renderChangelogWriter(opts.Version, opts.RepoURL, opts.ChangelogWriterTemplate, changes)
}

// renderChangelogWriter groups changes by change_type and executes the Go template.
func renderChangelogWriter(version, repoURL, tmplContent string, changes []*PRChange) (string, error) {
	sections := make(map[string][]TemplateEntry)
	for _, c := range changes {
		url := fmt.Sprintf("%s/pulls/%d", strings.TrimSuffix(repoURL, "/"), c.PRID)
		entry := TemplateEntry{Description: c.Description, PRID: c.PRID, URL: url}
		sections[c.ChangeType] = append(sections[c.ChangeType], entry)
	}
	data := ChangelogTemplateData{
		Version:      version,
		RepoURL:      repoURL,
		Sections:     sections,
		SectionOrder: ValidChangeTypes,
	}
	tmpl, err := template.New("changelog").Parse(tmplContent)
	if err != nil {
		return "", fmt.Errorf("parse changelog template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute changelog template: %w", err)
	}
	return buf.String(), nil
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
