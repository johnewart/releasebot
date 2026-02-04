package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LLMSummaryCache stores and loads cached per-PR LLM summary strings.
// The cached value is the raw LLM response (already JSON). Key is (owner, repo, prNumber, withDiff) so toggling include_diff invalidates.
type LLMSummaryCache struct {
	Dir string
}

// NewLLMSummaryCache uses dir (e.g. .releasebot/cache/llm_pr). Dir is created on first Set.
func NewLLMSummaryCache(dir string) *LLMSummaryCache {
	return &LLMSummaryCache{Dir: dir}
}

func llmKey(owner, repo string, prNumber int, withDiff bool) string {
	safe := func(s string) string {
		s = strings.ReplaceAll(s, "/", "_")
		s = strings.TrimSpace(s)
		if s == "" {
			return "empty"
		}
		return s
	}
	suffix := "meta"
	if withDiff {
		suffix = "diff"
	}
	return fmt.Sprintf("%s_%s_%d_%s.json", safe(owner), safe(repo), prNumber, suffix)
}

// Get returns the cached summary for this PR and mode (meta-only vs with-diff). Returns ("", false) on miss.
// The returned string is the raw LLM JSON (e.g. {"change_type":"Added","description":"...","pr_id":12345}).
func (c *LLMSummaryCache) Get(owner, repo string, prNumber int, withDiff bool) (string, bool) {
	path := filepath.Join(c.Dir, llmKey(owner, repo, prNumber, withDiff))
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(data)), true
}

// Set writes the summary for this PR and mode. summary is the raw LLM JSON; it is stored as-is with no wrapper.
func (c *LLMSummaryCache) Set(owner, repo string, prNumber int, withDiff bool, summary string) error {
	if err := os.MkdirAll(c.Dir, 0755); err != nil {
		return fmt.Errorf("create llm cache dir: %w", err)
	}
	path := filepath.Join(c.Dir, llmKey(owner, repo, prNumber, withDiff))
	if err := os.WriteFile(path, []byte(summary), 0644); err != nil {
		return fmt.Errorf("write llm cache: %w", err)
	}
	return nil
}
