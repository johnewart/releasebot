package semver

import "testing"

func TestDebugNext(t *testing.T) {
	tags := []string{"1.2.4rc0"}
	got := NextFromTags(tags, true, false, false, false)
	if got != "1.2.4rc1" {
		t.Errorf("NextFromTags(%v, true, false) = %q, want 1.2.4rc1", tags, got)
	}
}
