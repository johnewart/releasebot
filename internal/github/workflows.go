package github

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// WorkflowTrigger describes when a workflow runs. Used after parsing the "on" section.
type WorkflowTrigger struct {
	// Name is the workflow name (from the "name" key, or filename).
	Name string
	// Path is the path to the workflow file relative to repo root (e.g. .github/workflows/release.yml).
	Path string
	// RunsOnTagPush is true if the workflow is triggered by pushing a tag.
	RunsOnTagPush bool
	// TagPatterns are glob patterns for tags (e.g. ["v*", "release-*"]). Empty means all tags.
	// If RunsOnTagPush is true and TagPatterns is empty, the workflow runs on any tag push.
	TagPatterns []string
}

// ParseWorkflowFile parses workflow YAML and returns trigger info for tag pushes.
// data is the file contents; path is used for WorkflowTrigger.Path (e.g. .github/workflows/foo.yml).
func ParseWorkflowFile(data []byte, path string) (*WorkflowTrigger, error) {
	var doc struct {
		Name string    `yaml:"name"`
		On   yaml.Node `yaml:"on"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse workflow yaml: %w", err)
	}

	trigger := &WorkflowTrigger{
		Name: doc.Name,
		Path: path,
	}
	if trigger.Name == "" {
		trigger.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}

	runsOnPush, tagPatterns, err := parseOnNode(&doc.On)
	if err != nil {
		return nil, fmt.Errorf("parse 'on' for %s: %w", path, err)
	}
	trigger.RunsOnTagPush = runsOnPush
	trigger.TagPatterns = tagPatterns
	return trigger, nil
}

// parseOnNode interprets the "on" YAML node and returns: runsOnTagPush, tagPatterns, error.
func parseOnNode(node *yaml.Node) (runsOnTagPush bool, tagPatterns []string, err error) {
	if node == nil {
		return false, nil, nil
	}
	switch node.Kind {
	case yaml.ScalarNode:
		event := strings.ToLower(strings.TrimSpace(node.Value))
		if event == "push" {
			return true, nil, nil // push with no filters = all tags and branches
		}
		return false, nil, nil
	case yaml.SequenceNode:
		for _, n := range node.Content {
			if n.Kind == yaml.ScalarNode && strings.ToLower(strings.TrimSpace(n.Value)) == "push" {
				return true, nil, nil
			}
		}
		return false, nil, nil
	case yaml.MappingNode:
		for i := 0; i < len(node.Content); i += 2 {
			if i+1 >= len(node.Content) {
				break
			}
			keyNode := node.Content[i]
			valNode := node.Content[i+1]
			if keyNode.Kind != yaml.ScalarNode {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(keyNode.Value))
			if key != "push" {
				continue
			}
			// push: ... can be empty (no filters), or a map with tags/branches
			runs, patterns, e := parsePushConfig(valNode)
			return runs, patterns, e
		}
		return false, nil, nil
	default:
		return false, nil, nil
	}
}

// parsePushConfig interprets the value of "on.push" (can be null, or map with tags/branches).
func parsePushConfig(node *yaml.Node) (runsOnTagPush bool, tagPatterns []string, err error) {
	if node == nil || node.Kind == yaml.ScalarNode {
		// push: or push: null → runs on all push (tags and branches)
		return true, nil, nil
	}
	if node.Kind != yaml.MappingNode {
		return false, nil, nil
	}
	var hasTags, hasBranches bool
	for i := 0; i < len(node.Content); i += 2 {
		if i+1 >= len(node.Content) {
			break
		}
		keyNode := node.Content[i]
		valNode := node.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(keyNode.Value))
		switch key {
		case "tags":
			hasTags = true
			patterns, e := parseStringSequence(valNode)
			if e != nil {
				return false, nil, e
			}
			tagPatterns = patterns
		case "branches", "branches-ignore", "tags-ignore":
			if key == "branches" {
				hasBranches = true
			}
			// tags-ignore / branches-ignore don't change "runs on tag" for our purposes
		case "paths", "paths-ignore":
			// path filters apply to branch push; for tag push they're not evaluated (per GitHub docs)
		}
	}
	// If only "tags" is specified, workflow runs only on tag push (matching patterns).
	// If only "branches" is specified, workflow does not run on tag push.
	// If both or neither, workflow runs on tag push (when tags not specified, or in addition to branches).
	if hasBranches && !hasTags {
		return false, nil, nil
	}
	if hasTags && len(tagPatterns) == 0 {
		// tags: [] means no tags match (GitHub: "If you define only tags and the push has only branches, the workflow won't run")
		return false, nil, nil
	}
	return true, tagPatterns, nil
}

func parseStringSequence(node *yaml.Node) ([]string, error) {
	if node == nil {
		return nil, nil
	}
	if node.Kind == yaml.ScalarNode {
		s := strings.TrimSpace(node.Value)
		if s == "" {
			return nil, nil
		}
		return []string{s}, nil
	}
	if node.Kind != yaml.SequenceNode {
		return nil, nil
	}
	var out []string
	for _, n := range node.Content {
		if n.Kind == yaml.ScalarNode {
			out = append(out, strings.TrimSpace(n.Value))
		}
	}
	return out, nil
}

// WorkflowsDir is the default directory for workflow files under repo root.
const WorkflowsDir = ".github/workflows"

// ParseWorkflowsInRepo reads all workflow YAML files under repoRoot/.github/workflows and returns their trigger info.
func ParseWorkflowsInRepo(repoRoot string) ([]*WorkflowTrigger, error) {
	dir := filepath.Join(repoRoot, WorkflowsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read workflows dir: %w", err)
	}
	var result []*WorkflowTrigger
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := filepath.Ext(name)
		if ext != ".yml" && ext != ".yaml" {
			continue
		}
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		relPath := filepath.Join(WorkflowsDir, name)
		trigger, err := ParseWorkflowFile(data, relPath)
		if err != nil {
			return nil, err
		}
		if trigger.RunsOnTagPush {
			result = append(result, trigger)
		}
	}
	return result, nil
}

// TagMatchesPattern returns true if tag matches the GitHub Actions glob pattern.
// Supports * (no slash) and **; other glob chars (?, [...]) are passed through to path.Match where sensible.
func TagMatchesPattern(tag, pattern string) bool {
	// GitHub: * doesn't match /, ** matches multiple segments. For tag names there's no /.
	// Normalize pattern: ** for tags is treated as *.
	p := pattern
	if p == "**" || p == "*" {
		return true
	}
	// Replace ** with * for tag matching (tags don't have path segments).
	p = strings.ReplaceAll(p, "**", "*")
	matched, _ := filepath.Match(p, tag)
	return matched
}

// WorkflowsTriggeredByTag returns workflow triggers that run when the given tag is pushed.
func WorkflowsTriggeredByTag(repoRoot, tag string) ([]*WorkflowTrigger, error) {
	all, err := ParseWorkflowsInRepo(repoRoot)
	if err != nil {
		return nil, err
	}
	var out []*WorkflowTrigger
	for _, w := range all {
		if len(w.TagPatterns) == 0 {
			out = append(out, w)
			continue
		}
		for _, pat := range w.TagPatterns {
			if TagMatchesPattern(tag, pat) {
				out = append(out, w)
				break
			}
		}
	}
	return out, nil
}
