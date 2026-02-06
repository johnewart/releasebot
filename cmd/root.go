package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	cfgFile  string
	repoPath string
	dryRun   bool
	prevTag  string
	headRef  string
	prLimit  int
)

var rootCmd = &cobra.Command{
	Use:   "releasebot",
	Short: "Release automation: run justfile targets, validate tags, and generate changelogs with an LLM",
	Long: `Releasebot automates release workflows:

  1. Loads configuration from .releasebot.yml (justfile targets, changelog format, GitHub settings)
  2. Validates the previous release tag in the git repository
  3. Optionally runs justfile recipes (requires 'just' on PATH when using this feature)
  4. Generates or updates CHANGELOG.md using an LLM, with data from GitHub PRs (if configured)
     or from the git commit log between the previous tag and HEAD`,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", ".releasebot.yml", "path to config file")
	rootCmd.PersistentFlags().StringVar(&repoPath, "repo", ".", "path to the git repository")
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "report what would be done without performing actions")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
