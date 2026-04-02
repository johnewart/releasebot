package gitflow

import (
	"testing"

	"github.com/johnewart/releasebot/internal/semver"
)

func TestParseReleaseMinor(t *testing.T) {
	maj, min, err := ParseReleaseMinor("release/1.12", "release/")
	if err != nil || maj != 1 || min != 12 {
		t.Fatalf("got %d.%d err=%v", maj, min, err)
	}
	_, _, err = ParseReleaseMinor("wrong", "release/")
	if err == nil {
		t.Fatal("want error")
	}
}

func TestParseMajorMinor(t *testing.T) {
	maj, min, err := ParseMajorMinor("v2.3")
	if err != nil || maj != 2 || min != 3 {
		t.Fatalf("got %d.%d err=%v", maj, min, err)
	}
}

func TestReleaseBranchName(t *testing.T) {
	if g, w := ReleaseBranchName("rel/", 1, 0), "rel/1.0"; g != w {
		t.Fatalf("got %q want %q", g, w)
	}
}

func TestNextReleaseMinorFromTags(t *testing.T) {
	maj, min, err := NextReleaseMinorFromTags([]string{"v1.2.3", "v0.9.0"})
	if err != nil || maj != 1 || min != 3 {
		t.Fatalf("got %d.%d err=%v", maj, min, err)
	}
	maj, min, err = NextReleaseMinorFromTags(nil)
	if err != nil || maj != 1 || min != 0 {
		t.Fatalf("empty tags: got %d.%d err=%v", maj, min, err)
	}
}

func TestLatestStablePatchOnMinor(t *testing.T) {
	v := LatestStablePatchOnMinor([]string{"v1.2.1", "v1.2.10", "v1.3.0"}, 1, 2)
	if v == nil || v.Patch != 10 {
		t.Fatalf("got %+v", v)
	}
}

func TestParseHotfixVersion(t *testing.T) {
	v, err := ParseHotfixVersion("hotfix/v1.0.1", "hotfix/")
	if err != nil || v.Major != 1 || v.Minor != 0 || v.Patch != 1 {
		t.Fatalf("got %+v err=%v", v, err)
	}
	_, err = ParseHotfixVersion("hotfix/v1.0.1", "hotfix")
	if err != nil {
		t.Fatal(err)
	}
}

func TestLogicalBranch(t *testing.T) {
	if g, w := LogicalBranch("origin", "origin/release/1.0"), "release/1.0"; g != w {
		t.Fatalf("got %q", g)
	}
}

func TestHotfixBranchName(t *testing.T) {
	s := HotfixBranchName("", semver.Version{Major: 1, Minor: 2, Patch: 3})
	if s != "hotfix/v1.2.3" {
		t.Fatalf("got %q", s)
	}
}
