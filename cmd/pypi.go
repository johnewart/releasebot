package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/johnewart/releasebot/internal/pypi"
	"github.com/spf13/cobra"
)

var (
	pypiWaitTimeout  time.Duration
	pypiWaitInterval time.Duration
)

var pypiCmd = &cobra.Command{
	Use:   "pypi",
	Short: "Check or wait for a Python package on PyPI",
	Long:  `Validate that a package (e.g. my-package or my-package==1.0.0) exists on PyPI, or wait until it becomes available.`,
}

var pypiCheckCmd = &cobra.Command{
	Use:   "check <package> [version]",
	Short: "Check if a package exists on PyPI",
	Long:  `Exits 0 if the package exists (and optionally the given version). Example: releasebot pypi check my-package 1.0.0`,
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runPypiCheck,
}

var pypiWaitCmd = &cobra.Command{
	Use:   "wait <package> [version]",
	Short: "Wait for a package to appear on PyPI",
	Long:  `Polls PyPI until the package (and optional version) exists or the timeout is reached. Useful after publishing from CI.`,
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runPypiWait,
}

func init() {
	rootCmd.AddCommand(pypiCmd)
	pypiCmd.AddCommand(pypiCheckCmd)
	pypiCmd.AddCommand(pypiWaitCmd)

	pypiWaitCmd.Flags().DurationVar(&pypiWaitTimeout, "timeout", 5*time.Minute, "maximum time to wait")
	pypiWaitCmd.Flags().DurationVar(&pypiWaitInterval, "interval", 5*time.Second, "poll interval")
}

func runPypiCheck(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	name := args[0]
	version := ""
	if len(args) == 2 {
		version = args[1]
	}
	ok, err := pypi.Check(ctx, name, version)
	if err != nil {
		return err
	}
	if !ok {
		ref := name
		if version != "" {
			ref = name + "==" + version
		}
		fmt.Fprintf(os.Stderr, "package %s not found on PyPI\n", ref)
		os.Exit(1)
	}
	ref := name
	if version != "" {
		ref = name + "==" + version
	}
	fmt.Fprintf(os.Stderr, "✓ Package %s is available on PyPI\n", ref)
	return nil
}

func runPypiWait(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	name := args[0]
	version := ""
	if len(args) == 2 {
		version = args[1]
	}
	opts := pypi.WaitOptions{
		Timeout:  pypiWaitTimeout,
		Interval: pypiWaitInterval,
	}
	if err := pypi.Wait(ctx, name, version, opts); err != nil {
		return err
	}
	ref := name
	if version != "" {
		ref = name + "==" + version
	}
	fmt.Fprintf(os.Stderr, "✓ Package %s is available on PyPI\n", ref)
	return nil
}
