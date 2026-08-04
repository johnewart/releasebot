package gitflow

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/johnewart/releasebot/internal/config"
	"github.com/johnewart/releasebot/internal/git"
)

// StatusHTMLOpts configures the git-flow status HTML HTTP handler.
type StatusHTMLOpts struct {
	RepoPath   string // absolute path to the git repository
	ConfigPath string // absolute path to .releasebot.yml (or equivalent)
}

// NewStatusHTMLHandler serves an interactive D3 HTML visualization of the flow status report.
// Optional query parameter fetch=1 or fetch=true runs git fetch --prune on the configured remote first.
func NewStatusHTMLHandler(opts StatusHTMLOpts) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		fetch := strings.EqualFold(r.URL.Query().Get("fetch"), "1") ||
			strings.EqualFold(r.URL.Query().Get("fetch"), "true")

		rep, err := buildStatusReport(r.Context(), opts, fetch)
		if err != nil {
			log.Printf("gitflow: status html: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := WriteHTML(w, rep); err != nil {
			log.Printf("gitflow: write html: %v", err)
			http.Error(w, "failed to render visualization", http.StatusInternalServerError)
		}
	})
}

func buildStatusReport(ctx context.Context, opts StatusHTMLOpts, fetch bool) (*StatusReport, error) {
	if opts.RepoPath == "" {
		return nil, fmt.Errorf("repo path is required")
	}
	if opts.ConfigPath == "" {
		return nil, fmt.Errorf("config path is required")
	}

	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return nil, err
	}
	cfg.Resolve(opts.RepoPath)
	b := cfg.BranchDefaults()

	if fetch {
		remote := resolveStatusRemote(cfg, b)
		if err := git.Fetch(ctx, opts.RepoPath, remote, true); err != nil {
			return nil, fmt.Errorf("git fetch %s: %w", remote, err)
		}
	}

	return BuildReport(ctx, opts.RepoPath, b)
}

func resolveStatusRemote(cfg *config.Config, b config.BranchingConfig) string {
	if cfg.Release != nil && cfg.Release.Remote != "" {
		return cfg.Release.Remote
	}
	if b.Prune != nil && b.Prune.Remote != "" {
		return b.Prune.Remote
	}
	return "origin"
}

// ResolveStatusHTMLOpts turns CLI repo/config paths into absolute paths for StatusHTMLOpts.
func ResolveStatusHTMLOpts(repoPath, configPath string) (StatusHTMLOpts, error) {
	repoAbs, err := filepath.Abs(repoPath)
	if err != nil {
		return StatusHTMLOpts{}, fmt.Errorf("repo path: %w", err)
	}
	cfgPath := configPath
	if !filepath.IsAbs(cfgPath) {
		cfgPath = filepath.Join(repoAbs, cfgPath)
	}
	return StatusHTMLOpts{RepoPath: repoAbs, ConfigPath: cfgPath}, nil
}
