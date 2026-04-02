package gitflow

import (
	"testing"
	"time"

	"github.com/johnewart/releasebot/internal/config"
)

func TestPruneReleaseCandidatesRetainMinors(t *testing.T) {
	b := config.BranchingConfig{
		Main:           "main",
		Develop:        "develop",
		ReleasePrefix:  "release/",
		LTSBranches:    []string{"release/1.0"},
		Prune:          &config.PruneConfig{RetainMinors: 2, Remote: "origin"},
	}
	branches := []string{
		"origin/release/1.0",
		"origin/release/2.0",
		"origin/release/2.1",
		"origin/release/3.0",
	}
	tags := []string{"v1.0.5", "v2.0.1", "v2.1.0", "v3.0.0"}
	now := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	tagEpoch := map[string]int64{
		"v1.0.5": now.Unix(),
		"v2.0.1": now.Unix(),
		"v2.1.0": now.Unix(),
		"v3.0.0": now.Unix(),
	}
	got, ok := PruneReleaseCandidates(b, branches, tags, tagEpoch, now)
	if !ok {
		t.Fatal("expected ok")
	}
	names := make(map[string]struct{})
	for _, c := range got {
		names[c.Branch] = struct{}{}
	}
	if _, ok := names["release/1.0"]; ok {
		t.Fatal("LTS should not be pruned")
	}
	// Newest two minors by version: 3.0 and 2.1 -> release/2.0 is outside top 2
	if _, ok := names["release/2.0"]; !ok {
		t.Fatalf("expected release/2.0 in candidates: %#v", got)
	}
}

func TestPruneReleaseCandidatesMaxAge(t *testing.T) {
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	now := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	b := config.BranchingConfig{
		Main:          "main",
		Develop:       "develop",
		ReleasePrefix: "release/",
		Prune:         &config.PruneConfig{MaxAgeDays: 365, Remote: "origin"},
	}
	branches := []string{"origin/release/1.5"}
	tags := []string{"v1.5.0"}
	tagEpoch := map[string]int64{"v1.5.0": old}
	got, ok := PruneReleaseCandidates(b, branches, tags, tagEpoch, now)
	if !ok || len(got) != 1 {
		t.Fatalf("got ok=%v %#v", ok, got)
	}
}

func TestPruneNoRules(t *testing.T) {
	b := config.BranchingConfig{Prune: &config.PruneConfig{}}
	_, ok := PruneReleaseCandidates(b, nil, nil, nil, time.Now())
	if ok {
		t.Fatal("want false when no rules")
	}
}
