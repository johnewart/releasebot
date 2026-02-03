package just

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Runner runs justfile recipes by invoking the just binary.
// The just binary must be installed and on PATH when using this package.
func Runner(workingDir string, targets []string) (*RunnerResult, error) {
	if len(targets) == 0 {
		return &RunnerResult{}, nil
	}
	absDir, err := filepath.Abs(workingDir)
	if err != nil {
		return nil, fmt.Errorf("resolve working dir: %w", err)
	}
	justfile := filepath.Join(absDir, "justfile")
	if _, err := os.Stat(justfile); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("justfile not found in %s", absDir)
		}
		return nil, fmt.Errorf("justfile: %w", err)
	}
	var failed []string
	for _, target := range targets {
		cmd := exec.CommandContext(context.Background(), "just", target)
		cmd.Dir = absDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = os.Environ()
		if err := cmd.Run(); err != nil {
			failed = append(failed, target)
			return &RunnerResult{Failed: failed, Err: err}, nil
		}
	}
	return &RunnerResult{}, nil
}

// RunnerResult holds the result of running just targets.
type RunnerResult struct {
	Failed []string
	Err    error
}

// Success returns true if all targets succeeded.
func (r *RunnerResult) Success() bool {
	return len(r.Failed) == 0 && r.Err == nil
}
