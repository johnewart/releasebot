package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LLMSummaryCache stores and loads cached per-PR LLM summary strings.
// Key is (owner, repo, prNumber, withDiff) so toggling include_diff invalidates.
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

// entry holds the cached summary (allows adding metadata later).
type llmCacheEntry struct {
	Summary string `json:"summary"`
}

// Get returns the cached summary for this PR and mode (meta-only vs with-diff). Returns ("", false) on miss.
func (c *LLMSummaryCache) Get(owner, repo string, prNumber int, withDiff bool) (string, bool) {
	path := filepath.Join(c.Dir, llmKey(owner, repo, prNumber, withDiff))
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var e llmCacheEntry
	if err := json.Unmarshal(data, &e); err != nil {
		return "", false
	}
	return e.Summary, true
}

// Set writes the summary for this PR and mode.
func (c *LLMSummaryCache) Set(owner, repo string, prNumber int, withDiff bool, summary string) error {
	if err := os.MkdirAll(c.Dir, 0755); err != nil {
		return fmt.Errorf("create llm cache dir: %w", err)
	}
	path := filepath.Join(c.Dir, llmKey(owner, repo, prNumber, withDiff))
	e := llmCacheEntry{Summary: summary}
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal llm cache: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write llm cache: %w", err)
	}
	return nil
}
