package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/johnewart/releasebot/internal/git"
	"github.com/johnewart/releasebot/internal/semver"
	"github.com/spf13/cobra"
)

var (
	tagNextRC      bool
	tagNextAlpha   bool
	tagNextRelease bool
	tagNextMajor   bool
	tagNextCreate  bool
)

var tagCmd = &cobra.Command{
	Use:   "tag",
	Short: "Work with release tags",
	Long:  `Generate or inspect the next semantic version tag.`,
}

var tagNextCmd = &cobra.Command{
	Use:   "next",
	Short: "Print the next release tag",
	Long: `Print the next semantic version tag based on existing git tags.

By default prints the next stable patch version (e.g. v1.2.4 after v1.2.3).

With --rc: prints the next release candidate tag (X.Y.ZrcN).
  If no X.Y.Zrc* tags exist for the next release, prints X.Y.Zrc0.
  Otherwise increments N (e.g. 1.2.3rc0 -> 1.2.3rc1).

With --alpha: same as --rc but for alpha prereleases (X.Y.ZaN).

With --release: next minor version (e.g. v2.78.0 if latest is 2.77.x).
With --release --major: next major version (e.g. v3.0.0 if latest is 2.77.x).

With --create: create the tag in the repo (annotated tag at HEAD) and print it.
With --dry-run and --create: print the tag that would be created without creating it.`,
	RunE: runTagNext,
}

func init() {
	rootCmd.AddCommand(tagCmd)
	tagCmd.AddCommand(tagNextCmd)
	tagNextCmd.Flags().BoolVar(&tagNextRC, "rc", false, "next release candidate (X.Y.ZrcN)")
	tagNextCmd.Flags().BoolVar(&tagNextAlpha, "alpha", false, "next alpha prerelease (X.Y.ZaN)")
	tagNextCmd.Flags().BoolVar(&tagNextRelease, "release", false, "next minor release (X.Y+1.0)")
	tagNextCmd.Flags().BoolVar(&tagNextMajor, "major", false, "with --release, next major version (X+1.0.0)")
	tagNextCmd.Flags().BoolVar(&tagNextCreate, "create", false, "create the tag in the repo (annotated tag at HEAD) and print it")
}

func runTagNext(cmd *cobra.Command, args []string) error {
	if tagNextRC && tagNextAlpha {
		return fmt.Errorf("cannot use both --rc and --alpha")
	}
	if (tagNextRelease || tagNextMajor) && (tagNextRC || tagNextAlpha) {
		return fmt.Errorf("cannot combine --release/--major with --rc or --alpha")
	}
	if tagNextMajor && !tagNextRelease {
		return fmt.Errorf("--major must be used with --release")
	}
	repoAbs, err := filepath.Abs(repoPath)
	if err != nil {
		return fmt.Errorf("repo path: %w", err)
	}
	ctx := context.Background()
	tags, err := git.ListTags(ctx, repoAbs)
	if err != nil {
		return err
	}
	next := semver.NextFromTags(tags, tagNextRC, tagNextAlpha, tagNextRelease, tagNextMajor)
	if tagNextCreate {
		if dryRun {
			fmt.Fprintf(os.Stderr, "[dry-run] Would create tag %s\n", next)
		} else {
			msg := "Release " + next
			if err := git.CreateTag(ctx, repoAbs, next, msg); err != nil {
				return err
			}
		}
	}
	fmt.Fprintln(os.Stdout, next)
	return nil
}
