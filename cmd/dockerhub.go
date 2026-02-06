package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/johnewart/releasebot/internal/dockerhub"
	"github.com/spf13/cobra"
)

var (
	dockerhubWaitTimeout  time.Duration
	dockerhubWaitInterval time.Duration
)

var dockerhubCmd = &cobra.Command{
	Use:   "dockerhub",
	Short: "Check or watch for a Docker image on Docker Hub",
	Long:  `Validate that an image (e.g. myorg/myimage:v1.0 or nginx:latest) exists on Docker Hub, or watch until it becomes available.`,
}

var dockerhubCheckCmd = &cobra.Command{
	Use:   "check <image>",
	Short: "Check if an image exists on Docker Hub",
	Long:  `Exits 0 if the image exists, 1 if not. Image can be e.g. nginx:latest or myorg/myimage:v1.0.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runDockerhubCheck,
}

var dockerhubWatchCmd = &cobra.Command{
	Use:   "watch <image>",
	Short: "Watch until an image appears on Docker Hub",
	Long:  `Polls Docker Hub until the image exists or the timeout is reached. Useful after pushing an image from CI.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runDockerhubWatch,
}

func init() {
	rootCmd.AddCommand(dockerhubCmd)
	dockerhubCmd.AddCommand(dockerhubCheckCmd)
	dockerhubCmd.AddCommand(dockerhubWatchCmd)

	dockerhubWatchCmd.Flags().DurationVar(&dockerhubWaitTimeout, "timeout", 5*time.Minute, "maximum time to watch")
	dockerhubWatchCmd.Flags().DurationVar(&dockerhubWaitInterval, "interval", 5*time.Second, "poll interval")
}

func runDockerhubCheck(cmd *cobra.Command, args []string) error {
	if dryRun {
		fmt.Fprintf(os.Stderr, "[dry-run] Would check if image %s exists on Docker Hub\n", args[0])
		return nil
	}
	ctx := context.Background()
	image := args[0]
	ok, err := dockerhub.Check(ctx, image)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintf(os.Stderr, "image %s not found on Docker Hub\n", image)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "✓ Image %s is available on Docker Hub\n", image)
	return nil
}

func runDockerhubWatch(cmd *cobra.Command, args []string) error {
	if dryRun {
		fmt.Fprintf(os.Stderr, "[dry-run] Would watch for image %s on Docker Hub (timeout %s)\n", args[0], dockerhubWaitTimeout)
		return nil
	}
	ctx := context.Background()
	image := args[0]
	opts := dockerhub.WaitOptions{
		Timeout:  dockerhubWaitTimeout,
		Interval: dockerhubWaitInterval,
	}
	if err := dockerhub.Wait(ctx, image, opts); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "✓ Image %s is available on Docker Hub\n", image)
	return nil
}
