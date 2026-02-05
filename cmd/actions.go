package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/johnewart/releasebot/internal/config"
	"github.com/johnewart/releasebot/internal/git"
	"github.com/johnewart/releasebot/internal/github"
	"github.com/spf13/cobra"
)

var (
	actionsTag          string
	actionsPollInterval time.Duration
	actionsWaitTimeout  time.Duration
	actionsWaitAll      bool
)

var actionsCmd = &cobra.Command{
	Use:   "actions",
	Short: "List, wait for, and show status of GitHub Actions for a tag",
	Long: `List workflow runs triggered for a specific tag (e.g. after pushing a release tag),
wait until all runs complete, or show a brief status summary. Requires GitHub config
or GITHUB_TOKEN and a repo with remote origin.`,
}

var actionsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all workflow runs for a tag",
	Long:  `List GitHub Actions workflow runs that were triggered for the given tag (by commit SHA).`,
	RunE:  runActionsList,
}

var actionsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status summary of workflow runs for a tag",
	Long:  `Print a one-line summary of run states (e.g. "3 runs: 2 success, 1 in progress").`,
	RunE:  runActionsStatus,
}

var actionsWaitCmd = &cobra.Command{
	Use:   "wait",
	Short: "Wait for all workflow runs for a tag to complete",
	Long:  `Poll until every workflow run for the tag has completed, then print final status. Exits 0 if all succeeded, 1 if any failed or timed out.`,
	RunE:  runActionsWait,
}

var actionsWorkflowsCmd = &cobra.Command{
	Use:   "workflows",
	Short: "List workflows that run when a release tag is pushed",
	Long:  `Parse .github/workflows/*.yml and list which workflows are triggered by pushing the given tag (based on "on.push.tags" and "on: push").`,
	RunE:  runActionsWorkflows,
}

func init() {
	rootCmd.AddCommand(actionsCmd)
	actionsCmd.AddCommand(actionsListCmd, actionsStatusCmd, actionsWaitCmd, actionsWorkflowsCmd)

	actionsCmd.PersistentFlags().StringVar(&actionsTag, "tag", "", "git tag to list/wait for (e.g. v1.0.0); required")

	actionsWaitCmd.Flags().DurationVar(&actionsWaitTimeout, "timeout", 30*time.Minute, "maximum time to wait for runs to complete")
	actionsWaitCmd.Flags().DurationVar(&actionsPollInterval, "poll-interval", 15*time.Second, "interval between status checks")
	actionsWaitCmd.Flags().BoolVar(&actionsWaitAll, "all", false, "wait for all workflow runs for the tag; if false, wait only for workflows that run on tag push (from .github/workflows)")
}

func actionsClientAndSHA(ctx context.Context) (*github.Client, string, error) {
	repoAbs, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, "", fmt.Errorf("repo path: %w", err)
	}
	if actionsTag == "" {
		return nil, "", fmt.Errorf("--tag is required")
	}

	configPath := cfgFile
	if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(repoAbs, configPath)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, "", err
	}
	cfg.Resolve(repoAbs)

	var owner, repo string
	if cfg.GitHub != nil && cfg.GitHub.Owner != "" && cfg.GitHub.Repo != "" {
		owner = cfg.GitHub.Owner
		repo = cfg.GitHub.Repo
	} else {
		remote, err := git.RemoteOriginURL(ctx, repoAbs)
		if err != nil {
			return nil, "", fmt.Errorf("could not get remote: %w", err)
		}
		owner, repo, err = git.ParseGitHubOwnerRepo(remote)
		if err != nil {
			return nil, "", err
		}
	}

	token := ""
	if cfg.GitHub != nil && cfg.GitHub.Token != "" {
		token = cfg.GitHub.Token
	}
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token == "" {
		return nil, "", fmt.Errorf("GitHub token required: set GITHUB_TOKEN or github.token in config")
	}

	sha, err := git.RevParse(ctx, repoAbs, actionsTag)
	if err != nil {
		return nil, "", fmt.Errorf("resolve tag %q to SHA: %w", actionsTag, err)
	}

	client := github.NewClient(ctx, token, owner, repo)
	return client, sha, nil
}

// workflowStatusSymbol returns an npm-style symbol for status/conclusion.
func workflowStatusSymbol(status, conclusion string) string {
	if status != "completed" {
		return "⏳" // in progress / queued
	}
	switch conclusion {
	case "success":
		return "✓"
	case "failure", "cancelled", "timed_out", "action_required":
		return "✗"
	default:
		return "○"
	}
}

// printWorkflowTree prints runs in npm-style tree format to w (one line per run).
func printWorkflowTree(w *os.File, runs []*github.WorkflowRun, tag string) {
	if len(runs) == 0 {
		return
	}
	fmt.Fprintf(w, "\nreleasebot@ tag %s\n", tag)
	for i, r := range runs {
		status := r.GetStatus()
		conclusion := r.GetConclusion()
		if conclusion == "" {
			conclusion = status
		}
		sym := workflowStatusSymbol(status, conclusion)
		prefix := "├── "
		if i == len(runs)-1 {
			prefix = "└── "
		}
		fmt.Fprintf(w, "%s%s  %s #%d  %s\n", prefix, r.GetName(), sym, r.GetRunNumber(), r.GetHTMLURL())
	}
	fmt.Fprintln(w)
}

func runActionsList(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	client, sha, err := actionsClientAndSHA(ctx)
	if err != nil {
		return err
	}

	runs, err := client.ListWorkflowRunsForCommit(ctx, sha)
	if err != nil {
		return err
	}

	if len(runs) == 0 {
		fmt.Fprintf(os.Stderr, "No workflow runs found for tag %s (commit %s)\n", actionsTag, sha[:7])
		return nil
	}

	printWorkflowTree(os.Stdout, runs, actionsTag)
	return nil
}

func runActionsStatus(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	client, sha, err := actionsClientAndSHA(ctx)
	if err != nil {
		return err
	}

	runs, err := client.ListWorkflowRunsForCommit(ctx, sha)
	if err != nil {
		return err
	}

	if len(runs) == 0 {
		fmt.Fprintf(os.Stdout, "No workflow runs for tag %s (commit %s)\n", actionsTag, sha[:7])
		return nil
	}

	var success, failed, inProgress int
	for _, r := range runs {
		switch r.GetStatus() {
		case "completed":
			if r.GetConclusion() == "success" {
				success++
			} else {
				failed++
			}
		default:
			inProgress++
		}
	}

	// npm-style one-liner: symbols then counts
	parts := []string{fmt.Sprintf("%d run(s)", len(runs))}
	if success > 0 {
		parts = append(parts, fmt.Sprintf("✓ %d success", success))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("✗ %d failed", failed))
	}
	if inProgress > 0 {
		parts = append(parts, fmt.Sprintf("⏳ %d in progress", inProgress))
	}
	fmt.Fprintln(os.Stdout, strings.Join(parts, "  ")+".")
	return nil
}

func runActionsWait(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	client, sha, err := actionsClientAndSHA(ctx)
	if err != nil {
		return err
	}

	// When not --all, resolve repo and get workflows that run on this tag; we'll wait only on those.
	var tagPushTriggers []*github.WorkflowTrigger
	if !actionsWaitAll {
		repoAbs, absErr := filepath.Abs(repoPath)
		if absErr == nil {
			tagPushTriggers, _ = github.WorkflowsTriggeredByTag(repoAbs, actionsTag)
		}
	}

	deadline := time.Now().Add(actionsWaitTimeout)
	var runs []*github.WorkflowRun

	for time.Now().Before(deadline) {
		runs, err = client.ListWorkflowRunsForCommit(ctx, sha)
		if err != nil {
			return err
		}

		// Wait only on tag-push (publish) workflows when we have triggers from .github/workflows
		waitedRuns := runs
		if len(tagPushTriggers) > 0 {
			waitedRuns = github.RunsForTagPushWorkflows(runs, tagPushTriggers)
		}

		if len(waitedRuns) == 0 {
			if len(tagPushTriggers) > 0 {
				fmt.Fprintf(os.Stderr, "No runs yet for tag-push workflows; waiting... (next check in %s)\n", actionsPollInterval)
			} else {
				fmt.Fprintf(os.Stderr, "No workflow runs found for tag %s (commit %s); waiting...\n", actionsTag, sha[:7])
			}
			time.Sleep(actionsPollInterval)
			continue
		}

		// Require a run for each expected workflow when in publish-only mode, then all must be finished
		allSeen := true
		if len(tagPushTriggers) > 0 && len(waitedRuns) < len(tagPushTriggers) {
			allSeen = false
		}
		if allSeen && github.AllRunsFinished(waitedRuns) {
			runs = waitedRuns
			break
		}

		fmt.Fprintf(os.Stderr, "Waiting for workflows... (next check in %s)\n", actionsPollInterval)
		printWorkflowTree(os.Stderr, waitedRuns, actionsTag)
		time.Sleep(actionsPollInterval)
	}

	// Recompute waitedRuns for final messages (runs may have been set to waitedRuns on break)
	waitedRuns := runs
	if len(tagPushTriggers) > 0 {
		waitedRuns = github.RunsForTagPushWorkflows(runs, tagPushTriggers)
	}
	if len(waitedRuns) == 0 {
		if len(tagPushTriggers) > 0 {
			fmt.Fprintf(os.Stderr, "No workflow runs for tag-push workflows found for tag %s before timeout\n", actionsTag)
		} else {
			fmt.Fprintf(os.Stderr, "No workflow runs found for tag %s before timeout\n", actionsTag)
		}
		os.Exit(1)
	}

	if !github.AllRunsFinished(waitedRuns) {
		fmt.Fprintf(os.Stderr, "Timeout waiting for workflow runs to complete for tag %s\n", actionsTag)
		os.Exit(1)
	}

	if github.AnyRunFailed(waitedRuns) {
		fmt.Fprintf(os.Stderr, "One or more workflow runs failed for tag %s\n", actionsTag)
		printWorkflowTree(os.Stderr, waitedRuns, actionsTag)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "✓ All %d workflow run(s) completed successfully for tag %s\n", len(waitedRuns), actionsTag)
	printWorkflowTree(os.Stderr, waitedRuns, actionsTag)
	return nil
}

func runActionsWorkflows(cmd *cobra.Command, args []string) error {
	repoAbs, err := filepath.Abs(repoPath)
	if err != nil {
		return fmt.Errorf("repo path: %w", err)
	}
	var triggers []*github.WorkflowTrigger
	if actionsTag != "" {
		triggers, err = github.WorkflowsTriggeredByTag(repoAbs, actionsTag)
	} else {
		triggers, err = github.ParseWorkflowsInRepo(repoAbs)
	}
	if err != nil {
		return err
	}
	if len(triggers) == 0 {
		if actionsTag != "" {
			fmt.Fprintf(os.Stdout, "No workflows in .github/workflows run on tag %s\n", actionsTag)
		} else {
			fmt.Fprintf(os.Stdout, "No workflows in .github/workflows are triggered by tag push\n")
		}
		return nil
	}
	fmt.Fprintf(os.Stdout, "\nreleasebot@ %s\n", repoAbs)
	if actionsTag != "" {
		fmt.Fprintf(os.Stdout, "Workflows triggered by tag %s:\n", actionsTag)
	} else {
		fmt.Fprintf(os.Stdout, "Workflows that run on tag push:\n")
	}
	for i, w := range triggers {
		prefix := "├── "
		if i == len(triggers)-1 {
			prefix = "└── "
		}
		patterns := strings.Join(w.TagPatterns, ", ")
		if patterns == "" {
			patterns = "*"
		}
		fmt.Fprintf(os.Stdout, "%s%s  (%s)  tags: %s\n", prefix, w.Name, w.Path, patterns)
	}
	fmt.Fprintln(os.Stdout)
	return nil
}
