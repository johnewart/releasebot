package github

import (
	"context"
	"fmt"
	"time"

	gh "github.com/google/go-github/v60/github"
)

// WorkflowRun re-exposes go-github's WorkflowRun for callers.
type WorkflowRun = gh.WorkflowRun

// ListWorkflowRunsForCommit returns all workflow runs for the given commit SHA (e.g. the SHA a tag points to).
func (c *Client) ListWorkflowRunsForCommit(ctx context.Context, sha string) ([]*WorkflowRun, error) {
	opts := &gh.ListWorkflowRunsOptions{
		HeadSHA: sha,
		ListOptions: gh.ListOptions{
			PerPage: 100,
		},
	}
	var all []*WorkflowRun
	for {
		runs, resp, err := c.Actions.ListRepositoryWorkflowRuns(ctx, c.Owner, c.Repo, opts)
		if err != nil {
			return nil, fmt.Errorf("list workflow runs: %w", err)
		}
		all = append(all, runs.WorkflowRuns...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return all, nil
}

// GetWorkflowRun fetches a single workflow run by ID.
func (c *Client) GetWorkflowRun(ctx context.Context, runID int64) (*WorkflowRun, error) {
	run, _, err := c.Actions.GetWorkflowRunByID(ctx, c.Owner, c.Repo, runID)
	if err != nil {
		return nil, fmt.Errorf("get workflow run %d: %w", runID, err)
	}
	return run, nil
}

// WorkflowRunStatus is a simplified view of a run for status output.
type WorkflowRunStatus struct {
	ID         int64
	Name       string
	Status     string
	Conclusion string
	RunNumber  int
	HTMLURL    string
	CreatedAt  time.Time
}

// Status returns a summary for a WorkflowRun.
func WorkflowRunStatusFrom(r *WorkflowRun) WorkflowRunStatus {
	s := WorkflowRunStatus{
		ID:        r.GetID(),
		Name:      r.GetName(),
		Status:    r.GetStatus(),
		RunNumber: r.GetRunNumber(),
		HTMLURL:   r.GetHTMLURL(),
	}
	if c := r.GetConclusion(); c != "" {
		s.Conclusion = c
	}
	if t := r.CreatedAt; t != nil {
		s.CreatedAt = t.Time
	}
	return s
}

// IsFinished returns true when the run is no longer in progress or queued.
func (s WorkflowRunStatus) IsFinished() bool {
	return s.Status == "completed"
}

// IsSuccess returns true when the run completed successfully.
func (s WorkflowRunStatus) IsSuccess() bool {
	return s.Status == "completed" && s.Conclusion == "success"
}

// AllRunsFinished returns true when every run has status "completed".
func AllRunsFinished(runs []*WorkflowRun) bool {
	for _, r := range runs {
		if r.GetStatus() != "completed" {
			return false
		}
	}
	return true
}

// AnyRunFailed returns true if any run completed with a non-success conclusion.
func AnyRunFailed(runs []*WorkflowRun) bool {
	for _, r := range runs {
		if r.GetStatus() == "completed" && r.GetConclusion() != "success" && r.GetConclusion() != "" {
			return true
		}
	}
	return false
}

// RunsForTagPushWorkflows filters runs to those matching the given tag-push workflow triggers (by workflow name).
// Use this to wait only on workflows that are configured to run on tag push.
func RunsForTagPushWorkflows(runs []*WorkflowRun, triggers []*WorkflowTrigger) []*WorkflowRun {
	if len(triggers) == 0 {
		return runs
	}
	names := make(map[string]struct{})
	for _, t := range triggers {
		names[t.Name] = struct{}{}
	}
	var out []*WorkflowRun
	for _, r := range runs {
		if _, ok := names[r.GetName()]; ok {
			out = append(out, r)
		}
	}
	return out
}
