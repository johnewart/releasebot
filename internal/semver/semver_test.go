package semver

import (
	"testing"
)

func TestParseTag(t *testing.T) {
	tests := []struct {
		tag     string
		wantNil bool
		major   int
		minor   int
		patch   int
		preKind string
		preNum  int
	}{
		{"v1.2.3", false, 1, 2, 3, "", 0},
		{"1.2.3", false, 1, 2, 3, "", 0},
		{"v1.2.3rc0", false, 1, 2, 3, "rc", 0},
		{"1.2.3rc1", false, 1, 2, 3, "rc", 1},
		{"v1.2.3a0", false, 1, 2, 3, "a", 0},
		{"1.0.0a2", false, 1, 0, 0, "a", 2},
		{"foo", true, 0, 0, 0, "", 0},
	}
	for _, tt := range tests {
		v := ParseTag(tt.tag)
		if tt.wantNil {
			if v != nil {
				t.Errorf("ParseTag(%q) should return nil, got %+v", tt.tag, v)
			}
			continue
		}
		if v == nil {
			t.Errorf("ParseTag(%q) = nil", tt.tag)
			continue
		}
		if v.Major != tt.major || v.Minor != tt.minor || v.Patch != tt.patch || v.PreKind != tt.preKind || v.PreNum != tt.preNum {
			t.Errorf("ParseTag(%q) = %+v, want %d.%d.%d %s%d", tt.tag, v, tt.major, tt.minor, tt.patch, tt.preKind, tt.preNum)
		}
	}
}

func TestNextFromTags_Stable(t *testing.T) {
	tests := []struct {
		tags []string
		want string
	}{
		{nil, "v1.0.0"},
		{[]string{}, "v1.0.0"},
		{[]string{"v1.2.3"}, "v1.2.4"},
		{[]string{"v1.2.3", "v1.2.4"}, "v1.2.5"},
		{[]string{"v2.0.0"}, "v2.0.1"},
	}
	for _, tt := range tests {
		got := NextFromTags(tt.tags, false, false, false, false)
		if got != tt.want {
			t.Errorf("NextFromTags(%v, false, false) = %q, want %q", tt.tags, got, tt.want)
		}
	}
}

func TestNextFromTags_RC(t *testing.T) {
	tests := []struct {
		tags []string
		want string
	}{
		{nil, "1.0.0rc0"},
		{[]string{"v1.2.3"}, "1.2.4rc0"},
		{[]string{"v1.2.3", "1.2.4rc0"}, "1.2.4rc1"},
		{[]string{"v1.2.3", "1.2.4rc0", "1.2.4rc1"}, "1.2.4rc2"},
		{[]string{"1.2.4rc0"}, "1.2.4rc1"},
	}
	for _, tt := range tests {
		got := NextFromTags(tt.tags, true, false, false, false)
		if got != tt.want {
			t.Errorf("NextFromTags(%v, true, false) = %q, want %q", tt.tags, got, tt.want)
		}
	}
}

func TestNextFromTags_Alpha(t *testing.T) {
	tests := []struct {
		tags []string
		want string
	}{
		{nil, "1.0.0a0"},
		{[]string{"v1.2.3"}, "1.2.4a0"},
		{[]string{"v1.2.3", "1.2.4a0"}, "1.2.4a1"},
		{[]string{"1.2.4a2"}, "1.2.4a3"},
	}
	for _, tt := range tests {
		got := NextFromTags(tt.tags, false, true, false, false)
		if got != tt.want {
			t.Errorf("NextFromTags(%v, false, true) = %q, want %q", tt.tags, got, tt.want)
		}
	}
}

func TestNextFromTags_Release(t *testing.T) {
	tests := []struct {
		tags []string
		want string
	}{
		{nil, "v1.0.0"},
		{[]string{"v2.77.0"}, "v2.78.0"},
		{[]string{"v2.77.3"}, "v2.78.0"},
		{[]string{"v1.2.3", "v2.77.1"}, "v2.78.0"},
	}
	for _, tt := range tests {
		got := NextFromTags(tt.tags, false, false, true, false)
		if got != tt.want {
			t.Errorf("NextFromTags(%v, release) = %q, want %q", tt.tags, got, tt.want)
		}
	}
}

func TestNextFromTags_ReleaseMajor(t *testing.T) {
	tests := []struct {
		tags []string
		want string
	}{
		{nil, "v1.0.0"},
		{[]string{"v2.77.0"}, "v3.0.0"},
		{[]string{"v2.77.3"}, "v3.0.0"},
		{[]string{"v1.2.3", "v2.77.1"}, "v3.0.0"},
	}
	for _, tt := range tests {
		got := NextFromTags(tt.tags, false, false, true, true)
		if got != tt.want {
			t.Errorf("NextFromTags(%v, release, major) = %q, want %q", tt.tags, got, tt.want)
		}
	}
}
