package changelog

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Valid change_type values for per-PR LLM output.
var ValidChangeTypes = []string{
	"Added", "Changed", "Developer Experience", "Deprecated", "Docs", "Removed", "Fixed", "Security",
}

// PRChange is the structured output from the per-PR LLM (JSON).
type PRChange struct {
	ChangeType  string `json:"change_type"`
	Description string `json:"description"`
	PRID        int    `json:"pr_id"`
}

// ChangeTypeAllowed returns true if s is one of the allowed change types (case-insensitive match).
func ChangeTypeAllowed(s string) bool {
	s = strings.TrimSpace(s)
	for _, t := range ValidChangeTypes {
		if strings.EqualFold(s, t) {
			return true
		}
	}
	return false
}

// NormalizeChangeType returns the canonical casing for a valid change type, or "Changed" for unknown.
func NormalizeChangeType(s string) string {
	s = strings.TrimSpace(s)
	for _, t := range ValidChangeTypes {
		if strings.EqualFold(s, t) {
			return t
		}
	}
	return "Changed"
}

// ParsePRChangeJSON parses the LLM JSON output into PRChange. prID is the actual PR number (used if JSON omits or wrong).
func ParsePRChangeJSON(raw string, prID int) (*PRChange, error) {
	raw = strings.TrimSpace(raw)
	// Strip markdown code block if present
	if strings.HasPrefix(raw, "```") {
		lines := strings.SplitN(raw, "\n", 2)
		if len(lines) > 1 {
			raw = strings.TrimSpace(lines[1])
		}
		raw = strings.TrimSuffix(raw, "```")
		raw = strings.TrimSpace(raw)
	}
	fmt.Println("raw", raw)
	var c PRChange
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return nil, fmt.Errorf("parse PR change JSON: %w", err)
	}
	if c.PRID == 0 {
		c.PRID = prID
	}
	c.ChangeType = NormalizeChangeType(c.ChangeType)
	if c.Description == "" {
		return nil, fmt.Errorf("description is required")
	}
	return &c, nil
}

// TemplateEntry is passed to the changelog template for each item (description + link).
type TemplateEntry struct {
	Description string
	PRID        int
	URL         string
}

// ChangelogTemplateData is the struct passed to the changelog writer template.
type ChangelogTemplateData struct {
	Version      string
	RepoURL      string
	Sections     map[string][]TemplateEntry // e.g. Sections["Added"], Sections["Fixed"]
	SectionOrder []string                   // order to iterate sections (e.g. Added, Changed, ...)
}
