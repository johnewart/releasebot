package gitflow

import (
	"fmt"
	"strings"

	"github.com/johnewart/releasebot/internal/semver"
)

// NextReleaseMinorFromTags returns the MAJOR.MINOR for a new release line after the latest stable tag.
func NextReleaseMinorFromTags(tags []string) (major, minor int, err error) {
	latest := semver.LatestStableTag(tags)
	if latest == "" {
		return 1, 0, nil
	}
	v := semver.ParseTag(latest)
	if v == nil {
		return 0, 0, fmt.Errorf("parse latest stable tag %q", latest)
	}
	n := v.NextMinor()
	return n.Major, n.Minor, nil
}

// LatestStablePatchOnMinor returns the greatest stable X.Y.Z tag for the given X.Y, or nil if none.
func LatestStablePatchOnMinor(tags []string, maj, min int) *semver.Version {
	var best *semver.Version
	for _, t := range tags {
		v := semver.ParseTag(t)
		if v == nil || !v.IsStable() || v.Major != maj || v.Minor != min {
			continue
		}
		if best == nil || best.Less(v) {
			cp := *v
			best = &cp
		}
	}
	return best
}

// ParseMajorMinor parses "1.2", "v1.2", or "1.2.0" into major and minor (ignores patch if present).
func ParseMajorMinor(s string) (major, minor int, err error) {
	s = strings.TrimSpace(strings.TrimPrefix(s, "v"))
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("expected MAJOR.MINOR: %q", s)
	}
	_, err = fmt.Sscanf(parts[0]+"."+parts[1], "%d.%d", &major, &minor)
	if err != nil {
		return 0, 0, err
	}
	return major, minor, nil
}

// ParseReleaseMinor returns major.minor from a branch name after the release prefix (e.g. release/1.2 -> 1, 2).
func ParseReleaseMinor(branchName, releasePrefix string) (major, minor int, err error) {
	pfx := strings.TrimSpace(releasePrefix)
	if pfx != "" && !strings.HasSuffix(pfx, "/") {
		pfx += "/"
	}
	s := strings.TrimPrefix(branchName, pfx)
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	parts := strings.SplitN(s, ".", 3)
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("expected MAJOR.MINOR after prefix: %q", branchName)
	}
	if _, err := fmt.Sscanf(parts[0]+"."+parts[1], "%d.%d", &major, &minor); err != nil {
		return 0, 0, fmt.Errorf("parse release minor: %w", err)
	}
	return major, minor, nil
}

// ParseHotfixVersion parses the semver after hotfix prefix using semver.ParseTag (e.g. hotfix/v1.2.4).
func ParseHotfixVersion(branchName, hotfixPrefix string) (*semver.Version, error) {
	pfx := strings.TrimSpace(hotfixPrefix)
	if pfx != "" && !strings.HasSuffix(pfx, "/") {
		pfx += "/"
	}
	s := strings.TrimPrefix(branchName, pfx)
	s = strings.TrimSpace(s)
	v := semver.ParseTag(s)
	if v == nil {
		return nil, fmt.Errorf("not a semver tag after hotfix prefix: %q", branchName)
	}
	return v, nil
}

// ReleaseBranchName returns the canonical branch short name with prefix (e.g. release/2.3).
func ReleaseBranchName(prefix string, major, minor int) string {
	p := strings.TrimSpace(prefix)
	if p == "" {
		p = "release/"
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return fmt.Sprintf("%s%d.%d", p, major, minor)
}

// HotfixBranchName returns hotfix/x.y.z with optional v on version string.
func HotfixBranchName(prefix string, v semver.Version) string {
	p := strings.TrimSpace(prefix)
	if p == "" {
		p = "hotfix/"
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p + v.StringWithV()
}

// LogicalBranch strips remote/ prefix when fullName starts with remote + "/".
func LogicalBranch(remote, fullName string) string {
	if remote != "" && strings.HasPrefix(fullName, remote+"/") {
		return strings.TrimPrefix(fullName, remote+"/")
	}
	return fullName
}
