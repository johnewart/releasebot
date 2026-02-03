package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/johnewart/releasebot/internal/github"
)

// DefaultDir is the default cache directory name under the repo root.
const DefaultDir = ".releasebot/cache"

// PRCache stores and loads cached merged PR data keyed by owner, repo, base ref, head ref.
type PRCache struct {
	Dir string
}

// NewPRCache returns a cache that uses dir (e.g. .releasebot/cache). Dir is created on first Set.
func NewPRCache(dir string) *PRCache {
	return &PRCache{Dir: dir}
}

// key returns a safe filename for the given range (no path separators).
func key(owner, repo, base, head string) string {
	safe := func(s string) string {
		s = strings.ReplaceAll(s, "/", "_")
		s = strings.ReplaceAll(s, ":", "_")
		s = strings.TrimSpace(s)
		if s == "" {
			s = "empty"
		}
		return s
	}
	return fmt.Sprintf("%s_%s_%s_%s.json", safe(owner), safe(repo), safe(base), safe(head))
}

// Get loads cached PRs for the given range. Returns (nil, false) on miss or error.
func (c *PRCache) Get(owner, repo, base, head string) ([]github.PullRequest, bool) {
	path := filepath.Join(c.Dir, key(owner, repo, base, head))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var prs []github.PullRequest
	if err := json.Unmarshal(data, &prs); err != nil {
		return nil, false
	}
	return prs, true
}

// Set writes PRs to the cache for the given range. Creates the cache dir if needed.
func (c *PRCache) Set(owner, repo, base, head string, prs []github.PullRequest) error {
	if err := os.MkdirAll(c.Dir, 0755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	path := filepath.Join(c.Dir, key(owner, repo, base, head))
	data, err := json.MarshalIndent(prs, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write cache: %w", err)
	}
	return nil
}
