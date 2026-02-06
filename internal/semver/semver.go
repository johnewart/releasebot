package semver

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version is a semantic version (major.minor.patch with optional prerelease rcN or aN).
type Version struct {
	Major   int
	Minor   int
	Patch   int
	PreKind string // "rc", "alpha" (or "a"), or ""
	PreNum  int    // e.g. 0 in rc0, 1 in a1
}

// Tag formats we accept: v?X.Y.Z, v?X.Y.ZrcN, v?X.Y.ZaN (X.Y.Z = digits).
var (
	stableRegex = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)
	rcRegex     = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)rc(\d+)$`)
	alphaRegex  = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)a(\d+)$`)
)

// ParseTag parses a tag string into a Version. Returns nil if the tag doesn't match.
func ParseTag(tag string) *Version {
	tag = strings.TrimSpace(tag)
	if m := rcRegex.FindStringSubmatch(tag); len(m) == 5 {
		maj, _ := strconv.Atoi(m[1])
		min, _ := strconv.Atoi(m[2])
		patch, _ := strconv.Atoi(m[3])
		n, _ := strconv.Atoi(m[4])
		return &Version{Major: maj, Minor: min, Patch: patch, PreKind: "rc", PreNum: n}
	}
	if m := alphaRegex.FindStringSubmatch(tag); len(m) == 5 {
		maj, _ := strconv.Atoi(m[1])
		min, _ := strconv.Atoi(m[2])
		patch, _ := strconv.Atoi(m[3])
		n, _ := strconv.Atoi(m[4])
		return &Version{Major: maj, Minor: min, Patch: patch, PreKind: "a", PreNum: n}
	}
	if m := stableRegex.FindStringSubmatch(tag); len(m) == 4 {
		maj, _ := strconv.Atoi(m[1])
		min, _ := strconv.Atoi(m[2])
		patch, _ := strconv.Atoi(m[3])
		return &Version{Major: maj, Minor: min, Patch: patch}
	}
	return nil
}

// Less returns true if v is less than o (v comes before o in release order).
// Stable X.Y.Z is greater than X.Y.ZrcN or X.Y.ZaN; rc0 < rc1 < stable.
func (v *Version) Less(o *Version) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	if v.Patch != o.Patch {
		return v.Patch < o.Patch
	}
	// Same base: stable > rc > alpha; then by pre number
	vStable := v.PreKind == ""
	oStable := o.PreKind == ""
	if vStable != oStable {
		return !vStable // v is prerelease, o is stable → v < o
	}
	if v.PreKind != o.PreKind {
		// rc > alpha
		return v.PreKind == "a"
	}
	return v.PreNum < o.PreNum
}

// String returns the version as a tag string (no leading 'v' for prerelease, optional for stable).
func (v Version) String() string {
	base := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.PreKind == "rc" {
		return base + fmt.Sprintf("rc%d", v.PreNum)
	}
	if v.PreKind == "a" {
		return base + fmt.Sprintf("a%d", v.PreNum)
	}
	return base
}

// StringWithV returns the version with a leading 'v' (e.g. v1.2.3).
func (v Version) StringWithV() string {
	return "v" + v.String()
}

// IsStable returns true for X.Y.Z with no prerelease.
func (v *Version) IsStable() bool {
	return v != nil && v.PreKind == ""
}

// Base returns the same version with prerelease stripped (e.g. 1.2.3rc2 → 1.2.3).
func (v *Version) Base() Version {
	return Version{Major: v.Major, Minor: v.Minor, Patch: v.Patch}
}

// NextPatch returns the next patch version (X.Y.Z+1), stable.
func (v *Version) NextPatch() Version {
	return Version{Major: v.Major, Minor: v.Minor, Patch: v.Patch + 1}
}

// NextMinor returns the next minor version (X.Y+1.0), stable.
func (v *Version) NextMinor() Version {
	return Version{Major: v.Major, Minor: v.Minor + 1, Patch: 0}
}

// NextMajor returns the next major version (X+1.0.0), stable.
func (v *Version) NextMajor() Version {
	return Version{Major: v.Major + 1, Minor: 0, Patch: 0}
}

// NextRC returns the next rc for this base: if v is 1.2.3 or 1.2.3rcN, returns 1.2.3rc(N+1) or 1.2.3rc0.
func (v *Version) NextRC(existingRCNum *int) Version {
	base := v.Base()
	if existingRCNum != nil {
		return Version{Major: base.Major, Minor: base.Minor, Patch: base.Patch, PreKind: "rc", PreNum: *existingRCNum + 1}
	}
	return Version{Major: base.Major, Minor: base.Minor, Patch: base.Patch, PreKind: "rc", PreNum: 0}
}

// NextAlpha returns the next alpha for this base.
func (v *Version) NextAlpha(existingANum *int) Version {
	base := v.Base()
	if existingANum != nil {
		return Version{Major: base.Major, Minor: base.Minor, Patch: base.Patch, PreKind: "a", PreNum: *existingANum + 1}
	}
	return Version{Major: base.Major, Minor: base.Minor, Patch: base.Patch, PreKind: "a", PreNum: 0}
}

// LatestTag returns the latest semantic version tag from the list (by version order).
// Only tags that parse as semver are considered. Returns empty string if none parse.
func LatestTag(tags []string) string {
	var max *Version
	for _, tagStr := range tags {
		p := ParseTag(tagStr)
		if p == nil {
			continue
		}
		v := *p
		if max == nil || max.Less(&v) {
			max = &v
		}
	}
	if max == nil {
		return ""
	}
	// Prefer returning the original tag string if it had a 'v' prefix
	for _, tagStr := range tags {
		if p := ParseTag(tagStr); p != nil && p.Major == max.Major && p.Minor == max.Minor && p.Patch == max.Patch && p.PreKind == max.PreKind && p.PreNum == max.PreNum {
			return tagStr
		}
	}
	if max.IsStable() {
		return max.StringWithV()
	}
	return max.String()
}

// LatestStableTag returns the latest stable (non-alpha, non-rc) semver tag from the list.
// Use this as the default "previous release" when building changelogs. Returns empty string if no stable tags exist.
func LatestStableTag(tags []string) string {
	var max *Version
	for _, tagStr := range tags {
		p := ParseTag(tagStr)
		if p == nil || !p.IsStable() {
			continue
		}
		v := *p
		if max == nil || max.Less(&v) {
			max = &v
		}
	}
	if max == nil {
		return ""
	}
	for _, tagStr := range tags {
		if p := ParseTag(tagStr); p != nil && p.IsStable() && p.Major == max.Major && p.Minor == max.Minor && p.Patch == max.Patch {
			return tagStr
		}
	}
	return max.StringWithV()
}

// NextFromTags computes the next version tag from a list of existing tags.
// If rc is true, returns X.Y.ZrcN (next rc: either X.Y.Zrc0 for next release, or rc(N+1) if X.Y.Zrc* exist).
// If alpha is true, returns X.Y.ZaN (next alpha, same logic).
// If release is true and major is false, returns the next minor version vX.(Y+1).0 (e.g. 2.78.0 after 2.77.x).
// If release is true and major is true, returns the next major version v(X+1).0.0.
// Otherwise returns the next patch version vX.Y.(Z+1).
func NextFromTags(tags []string, rc, alpha, release, major bool) string {
	var maxStable *Version
	rcBases := make(map[string]int) // "X.Y.Z" -> max rc N
	alphaBases := make(map[string]int)

	for _, tagStr := range tags {
		p := ParseTag(tagStr)
		if p == nil {
			continue
		}
		v := *p
		if v.IsStable() {
			if maxStable == nil || maxStable.Less(&v) {
				c := v
				maxStable = &c
			}
		}
		if v.PreKind == "rc" {
			key := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
			if cur, ok := rcBases[key]; !ok || v.PreNum > cur {
				rcBases[key] = v.PreNum
			}
		}
		if v.PreKind == "a" {
			key := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
			if cur, ok := alphaBases[key]; !ok || v.PreNum > cur {
				alphaBases[key] = v.PreNum
			}
		}
	}

	if rc {
		// Base = max of (next patch after max stable, each rc base from tags)
		candidates := []Version{}
		if maxStable != nil {
			candidates = append(candidates, maxStable.NextPatch())
		} else {
			candidates = append(candidates, Version{Major: 1, Minor: 0, Patch: 0})
		}
		for k := range rcBases {
			var parsed Version
			if _, err := fmt.Sscanf(k, "%d.%d.%d", &parsed.Major, &parsed.Minor, &parsed.Patch); err != nil {
				continue
			}
			candidates = append(candidates, parsed)
		}
		base := candidates[0]
		for i := 1; i < len(candidates); i++ {
			if base.Less(&candidates[i]) {
				base = candidates[i]
			}
		}
		key := fmt.Sprintf("%d.%d.%d", base.Major, base.Minor, base.Patch)
		if n, has := rcBases[key]; has {
			return base.NextRC(&n).String()
		}
		return base.NextRC(nil).String()
	}
	if alpha {
		candidates := []Version{}
		if maxStable != nil {
			candidates = append(candidates, maxStable.NextPatch())
		} else {
			candidates = append(candidates, Version{Major: 1, Minor: 0, Patch: 0})
		}
		for k := range alphaBases {
			var parsed Version
			if _, err := fmt.Sscanf(k, "%d.%d.%d", &parsed.Major, &parsed.Minor, &parsed.Patch); err != nil {
				continue
			}
			candidates = append(candidates, parsed)
		}
		base := candidates[0]
		for i := 1; i < len(candidates); i++ {
			if base.Less(&candidates[i]) {
				base = candidates[i]
			}
		}
		key := fmt.Sprintf("%d.%d.%d", base.Major, base.Minor, base.Patch)
		if n, has := alphaBases[key]; has {
			return base.NextAlpha(&n).String()
		}
		return base.NextAlpha(nil).String()
	}
	if maxStable == nil {
		return "v1.0.0"
	}
	if release {
		if major {
			return maxStable.NextMajor().StringWithV()
		}
		return maxStable.NextMinor().StringWithV()
	}
	return maxStable.NextPatch().StringWithV()
}
