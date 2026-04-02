package gitflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/johnewart/releasebot/internal/config"
	"github.com/johnewart/releasebot/internal/git"
)

// StatusReport summarizes integration branches and flow lines.
type StatusReport struct {
	Remote         string         `json:"remote"`
	Main           RefStatus      `json:"main"`
	Develop        RefStatus      `json:"develop"`
	IntegrateDrift IntegrateDrift `json:"integrate_drift"`
	Lines          []LineStatus   `json:"lines"`
	Issues         []string       `json:"issues"`
}

// RefStatus is a resolved branch or tag tip.
type RefStatus struct {
	Name    string `json:"name"`
	RefUsed string `json:"ref_used,omitempty"`
	SHA     string `json:"sha,omitempty"`
	Missing bool   `json:"missing"`
}

// IntegrateDrift compares main and develop.
type IntegrateDrift struct {
	CommitsMainNotInDevelop   int `json:"commits_main_not_in_develop"`
	CommitsDevelopNotInMain int `json:"commits_develop_not_in_main"`
}

// LineStatus is a release/* or hotfix/* line vs main/develop.
type LineStatus struct {
	Kind             string `json:"kind"`
	Name             string `json:"name"`
	RefUsed          string `json:"ref_used,omitempty"`
	TipSHA           string `json:"tip_sha,omitempty"`
	AheadMain        int    `json:"ahead_of_main"`
	BehindMain       int    `json:"behind_main"`
	AheadDevelop     int    `json:"ahead_of_develop"`
	BehindDevelop    int    `json:"behind_develop"`
	CommitsNotInMain int    `json:"commits_not_in_main"`
}

// BuildReport collects branch and drift information. Pass resolved BranchDefaults from config.
func BuildReport(ctx context.Context, repoPath string, b config.BranchingConfig) (*StatusReport, error) {
	remote := "origin"
	if b.Prune != nil && b.Prune.Remote != "" {
		remote = b.Prune.Remote
	}
	rep := &StatusReport{Remote: remote}

	mainSHA, mainRef, err := git.ResolveFirst(ctx, repoPath, b.Main, remote+"/"+b.Main)
	if err != nil {
		rep.Main = RefStatus{Name: b.Main, Missing: true}
		rep.Issues = append(rep.Issues, fmt.Sprintf("cannot resolve main branch %q (tried %s, %s/%s): %v", b.Main, b.Main, remote, b.Main, err))
	} else {
		rep.Main = RefStatus{Name: b.Main, RefUsed: mainRef, SHA: mainSHA, Missing: false}
	}

	devSHA, devRef, err := git.ResolveFirst(ctx, repoPath, b.Develop, remote+"/"+b.Develop)
	if err != nil {
		rep.Develop = RefStatus{Name: b.Develop, Missing: true}
		rep.Issues = append(rep.Issues, fmt.Sprintf("cannot resolve develop branch %q: %v", b.Develop, err))
	} else {
		rep.Develop = RefStatus{Name: b.Develop, RefUsed: devRef, SHA: devSHA, Missing: false}
	}

	if !rep.Main.Missing && !rep.Develop.Missing {
		nMainNotDev, err := git.CommitCount(ctx, repoPath, devRef, mainRef)
		if err == nil {
			rep.IntegrateDrift.CommitsMainNotInDevelop = nMainNotDev
		}
		nDevNotMain, err := git.CommitCount(ctx, repoPath, mainRef, devRef)
		if err == nil {
			rep.IntegrateDrift.CommitsDevelopNotInMain = nDevNotMain
		}
		if rep.IntegrateDrift.CommitsMainNotInDevelop > 0 {
			rep.Issues = append(rep.Issues, fmt.Sprintf("%d commit(s) on %s not reachable from %s (merge or rebase needed)", rep.IntegrateDrift.CommitsMainNotInDevelop, b.Main, b.Develop))
		}
	}

	branches, err := git.ListBranches(ctx, repoPath)
	if err != nil {
		return rep, err
	}

	byLogical := make(map[string][]git.BranchInfo)
	for _, br := range branches {
		logical := LogicalBranch(remote, br.Name)
		if logical == br.Name && !strings.Contains(br.Name, "/") {
			// local branch without remote prefix
		}
		rel := releasePrefixMatch(logical, b.ReleasePrefix)
		hf := hotfixPrefixMatch(logical, b.HotfixPrefix)
		if !rel && !hf {
			continue
		}
		byLogical[logical] = append(byLogical[logical], br)
	}

	var keys []string
	for k := range byLogical {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	mainCmp := mainRef
	devCmp := devRef
	if rep.Main.Missing {
		mainCmp = ""
	}
	if rep.Develop.Missing {
		devCmp = ""
	}

	for _, logical := range keys {
		refName, sha := pickRef(remote, logical, byLogical[logical])
		if refName == "" {
			continue
		}
		ls := LineStatus{Name: logical, RefUsed: refName, TipSHA: sha}
		isRelease := releasePrefixMatch(logical, b.ReleasePrefix)
		if isRelease {
			ls.Kind = "release"
		} else {
			ls.Kind = "hotfix"
		}
		if mainCmp != "" {
			ahead, behind, err := git.AheadBehind(ctx, repoPath, refName, mainCmp)
			if err == nil {
				ls.AheadMain, ls.BehindMain = ahead, behind
			}
			n, err := git.CommitCount(ctx, repoPath, mainCmp, refName)
			if err == nil {
				ls.CommitsNotInMain = n
				if n > 0 {
					rep.Issues = append(rep.Issues, fmt.Sprintf("%s: %d commit(s) not in %s", logical, n, b.Main))
				}
			}
		}
		if devCmp != "" {
			ahead, behind, err := git.AheadBehind(ctx, repoPath, refName, devCmp)
			if err == nil {
				ls.AheadDevelop, ls.BehindDevelop = ahead, behind
			}
		}
		rep.Lines = append(rep.Lines, ls)
	}

	return rep, nil
}

func releasePrefixMatch(logical, prefix string) bool {
	p := strings.TrimSpace(prefix)
	if p == "" {
		p = "release"
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return strings.HasPrefix(logical, p) && len(logical) > len(p)
}

func hotfixPrefixMatch(logical, prefix string) bool {
	p := strings.TrimSpace(prefix)
	if p == "" {
		p = "hotfix"
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return strings.HasPrefix(logical, p) && len(logical) > len(p)
}

func pickRef(remote, logical string, infos []git.BranchInfo) (refName, sha string) {
	// Prefer local logical name, then remote/logical
	for _, try := range []string{logical, remote + "/" + logical} {
		for _, bi := range infos {
			if bi.Name == try {
				return bi.Name, bi.SHA
			}
		}
	}
	if len(infos) > 0 {
		return infos[0].Name, infos[0].SHA
	}
	return "", ""
}

// WriteJSON writes the report as JSON (pretty) to w.
func WriteJSON(w io.Writer, rep *StatusReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rep)
}

// WriteMermaid writes a simple Mermaid flowchart for main, develop, and lines.
func WriteMermaid(w io.Writer, rep *StatusReport) {
	fmt.Fprintf(w, "flowchart LR\n")
	fmt.Fprintf(w, "  main[%s]\n", safeMermaidID(rep.Main.Name))
	fmt.Fprintf(w, "  develop[%s]\n", safeMermaidID(rep.Develop.Name))
	for _, ln := range rep.Lines {
		id := strings.ReplaceAll(ln.Name, "/", "_")
		id = strings.ReplaceAll(id, ".", "_")
		fmt.Fprintf(w, "  %s[%s]\n", id, safeMermaidID(ln.Name))
		fmt.Fprintf(w, "  develop --> %s\n", id)
		fmt.Fprintf(w, "  main --> %s\n", id)
	}
}

func safeMermaidID(s string) string {
	s = strings.ReplaceAll(s, "(", "")
	s = strings.ReplaceAll(s, ")", "")
	if s == "" {
		return "unnamed"
	}
	return s
}

// PrintTextTable prints a human-readable summary to stdout.
func PrintTextTable(rep *StatusReport) {
	w := os.Stdout
	fmt.Fprintf(w, "Remote: %s\n", rep.Remote)
	if rep.Main.Missing {
		fmt.Fprintf(w, "%s: missing\n", rep.Main.Name)
	} else {
		short := rep.Main.SHA
		if len(short) > 7 {
			short = short[:7]
		}
		fmt.Fprintf(w, "%s: %s (%s)\n", rep.Main.Name, short, rep.Main.RefUsed)
	}
	if rep.Develop.Missing {
		fmt.Fprintf(w, "%s: missing\n", rep.Develop.Name)
	} else {
		short := rep.Develop.SHA
		if len(short) > 7 {
			short = short[:7]
		}
		fmt.Fprintf(w, "%s: %s (%s)\n", rep.Develop.Name, short, rep.Develop.RefUsed)
	}
	fmt.Fprintf(w, "Drift: commits on main not in develop: %d; commits on develop not in main: %d\n",
		rep.IntegrateDrift.CommitsMainNotInDevelop, rep.IntegrateDrift.CommitsDevelopNotInMain)
	fmt.Fprintln(w, "Lines:")
	for _, ln := range rep.Lines {
		tip := ln.TipSHA
		if len(tip) > 7 {
			tip = tip[:7]
		}
		fmt.Fprintf(w, "  [%s] %s tip=%s ahead/behind %s: %d/%d  %s: %d/%d  notInMain=%d\n",
			ln.Kind, ln.Name, tip, rep.Main.Name, ln.AheadMain, ln.BehindMain,
			rep.Develop.Name, ln.AheadDevelop, ln.BehindDevelop, ln.CommitsNotInMain)
	}
	if len(rep.Issues) > 0 {
		fmt.Fprintln(w, "Issues:")
		for _, i := range rep.Issues {
			fmt.Fprintf(w, "  - %s\n", i)
		}
	}
}
