package github

import (
	"testing"
)

func TestParseWorkflowFile_OnPush(t *testing.T) {
	yaml := `name: Release
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps: []
`
	trigger, err := ParseWorkflowFile([]byte(yaml), ".github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !trigger.RunsOnTagPush {
		t.Error("expected RunsOnTagPush true for on: push")
	}
	if trigger.Name != "Release" {
		t.Errorf("name: got %q", trigger.Name)
	}
	if len(trigger.TagPatterns) != 0 {
		t.Errorf("expected no tag patterns, got %v", trigger.TagPatterns)
	}
}

func TestParseWorkflowFile_OnPushTags(t *testing.T) {
	yaml := `name: Release
on:
  push:
    tags:
      - 'v*'
      - 'release-*'
jobs: {}
`
	trigger, err := ParseWorkflowFile([]byte(yaml), ".github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !trigger.RunsOnTagPush {
		t.Error("expected RunsOnTagPush true")
	}
	if len(trigger.TagPatterns) != 2 {
		t.Fatalf("expected 2 tag patterns, got %v", trigger.TagPatterns)
	}
	if trigger.TagPatterns[0] != "v*" || trigger.TagPatterns[1] != "release-*" {
		t.Errorf("tag patterns: %v", trigger.TagPatterns)
	}
}

func TestParseWorkflowFile_OnPushBranchesOnly(t *testing.T) {
	yaml := `name: CI
on:
  push:
    branches: [main]
jobs: {}
`
	trigger, err := ParseWorkflowFile([]byte(yaml), ".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	if trigger.RunsOnTagPush {
		t.Error("expected RunsOnTagPush false when only branches specified")
	}
}

func TestParseWorkflowFile_OnArrayWithPush(t *testing.T) {
	yaml := `on: [push, pull_request]
jobs: {}
`
	trigger, err := ParseWorkflowFile([]byte(yaml), ".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !trigger.RunsOnTagPush {
		t.Error("expected RunsOnTagPush true for on: [push, pull_request]")
	}
}

func TestTagMatchesPattern(t *testing.T) {
	tests := []struct {
		tag     string
		pattern string
		want    bool
	}{
		{"v1.0.0", "v*", true},
		{"v1.0.0", "v*.*.*", true},
		{"release-1", "release-*", true},
		{"foo", "v*", false},
		{"v1.0.0", "*", true},
		{"any", "**", true},
	}
	for _, tt := range tests {
		got := TagMatchesPattern(tt.tag, tt.pattern)
		if got != tt.want {
			t.Errorf("TagMatchesPattern(%q, %q) = %v, want %v", tt.tag, tt.pattern, got, tt.want)
		}
	}
}
