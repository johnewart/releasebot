package gitflow

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/johnewart/releasebot/internal/config"
	"github.com/johnewart/releasebot/internal/semver"
)

// PruneCandidate is a remote branch short name (e.g. release/1.0) suggested for deletion.
type PruneCandidate struct {
	Branch    string `json:"branch"`
	Reason    string `json:"reason"`
	LatestTag string `json:"latest_tag,omitempty"`
}

// PruneReleaseCandidates evaluates release/* branches against retain_minors and/or max_age_days.
// tagEpoch maps tag name to committer unix time; use git.CommitterEpoch. If both rules are disabled, returns nil, false.
func PruneReleaseCandidates(b config.BranchingConfig, remoteBranches []string, tags []string, tagEpoch map[string]int64, now time.Time) ([]PruneCandidate, bool) {
	remote := "origin"
	if b.Prune != nil && b.Prune.Remote != "" {
		remote = b.Prune.Remote
	}
	retain := 0
	maxAge := 0
	if b.Prune != nil {
		retain = b.Prune.RetainMinors
		maxAge = b.Prune.MaxAgeDays
	}
	if retain <= 0 && maxAge <= 0 {
		return nil, false
	}

	prefixRel := b.ReleasePrefix
	if prefixRel == "" {
		prefixRel = "release/"
	}
	if !strings.HasSuffix(prefixRel, "/") {
		prefixRel += "/"
	}

	lts := make(map[string]struct{})
	for _, x := range b.LTSBranches {
		lts[strings.TrimSpace(x)] = struct{}{}
	}
	main := b.Main
	if main == "" {
		main = "main"
	}
	dev := b.Develop
	if dev == "" {
		dev = "develop"
	}
	for _, prot := range []string{main, dev, remote + "/" + main, remote + "/" + dev} {
		lts[strings.TrimPrefix(prot, remote+"/")] = struct{}{}
	}

	type line struct {
		short string
		maj   int
		min   int
	}
	var lines []line
	for _, br := range remoteBranches {
		short := LogicalBranch(remote, br)
		if !strings.HasPrefix(short, prefixRel) {
			continue
		}
		if _, ok := lts[short]; ok {
			continue
		}
		maj, min, err := ParseReleaseMinor(short, prefixRel)
		if err != nil {
			continue
		}
		lines = append(lines, line{short: short, maj: maj, min: min})
	}

	fromRetain := make(map[string]PruneCandidate)
	if retain > 0 && len(lines) > 0 {
		sorted := append([]line(nil), lines...)
		sort.Slice(sorted, func(i, j int) bool {
			if sorted[i].maj != sorted[j].maj {
				return sorted[i].maj > sorted[j].maj
			}
			return sorted[i].min > sorted[j].min
		})
		protected := make(map[string]struct{})
		n := retain
		if n > len(sorted) {
			n = len(sorted)
		}
		for i := 0; i < n; i++ {
			protected[sorted[i].short] = struct{}{}
		}
		for _, li := range lines {
			if _, ok := protected[li.short]; ok {
				continue
			}
			latest := latestStableTagForMinor(tags, li.maj, li.min)
			fromRetain[li.short] = PruneCandidate{
				Branch:    li.short,
				Reason:    fmt.Sprintf("not among %d newest release minors", retain),
				LatestTag: latest,
			}
		}
	}

	fromAge := make(map[string]PruneCandidate)
	if maxAge > 0 {
		cutoff := now.Add(-time.Duration(maxAge) * 24 * time.Hour).Unix()
		for _, li := range lines {
			if _, ok := lts[li.short]; ok {
				continue
			}
			latest := latestStableTagForMinor(tags, li.maj, li.min)
			if latest == "" {
				continue
			}
			epoch, ok := tagEpoch[latest]
			if !ok || epoch >= cutoff {
				continue
			}
			fromAge[li.short] = PruneCandidate{
				Branch:    li.short,
				Reason:    fmt.Sprintf("latest tag %s is older than %d days", latest, maxAge),
				LatestTag: latest,
			}
		}
	}

	merged := make(map[string]PruneCandidate)
	for k, v := range fromRetain {
		merged[k] = v
	}
	for k, v := range fromAge {
		if ex, ok := merged[k]; ok {
			ex.Reason = ex.Reason + "; " + v.Reason
			if ex.LatestTag == "" {
				ex.LatestTag = v.LatestTag
			}
			merged[k] = ex
		} else {
			merged[k] = v
		}
	}

	var out []PruneCandidate
	for _, v := range merged {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Branch < out[j].Branch })
	return out, true
}

func latestStableTagForMinor(tags []string, maj, min int) string {
	var best *semver.Version
	var bestStr string
	for _, t := range tags {
		v := semver.ParseTag(t)
		if v == nil || !v.IsStable() {
			continue
		}
		if v.Major != maj || v.Minor != min {
			continue
		}
		if best == nil || best.Less(v) {
			cp := *v
			best = &cp
			bestStr = t
		}
	}
	if best == nil {
		return ""
	}
	return bestStr
}
