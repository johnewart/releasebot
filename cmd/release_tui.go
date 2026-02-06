package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

const numReleaseSteps = 7

var releaseStepNames = [numReleaseSteps]string{
	"Just targets",
	"Generate changelog",
	"Commit & tag",
	"Push to remote",
	"Wait for workflows",
	"PyPI",
	"Docker Hub",
}

// stepResultMsg is sent after each release step completes (from doReleaseSteps reporter).
type stepResultMsg struct {
	Step    int
	Err     error
	Skipped bool
}

// releaseDoneMsg is sent when doReleaseSteps returns (success or final error).
type releaseDoneMsg struct {
	Err error
}

// dryRunStatusMsg is sent during dry-run gather to show progress (e.g. "Found 12 commits").
type dryRunStatusMsg struct {
	Line string
}

// dryRunProgressMsg is sent during GitHub PR fetch for progress bar (current, total).
type dryRunProgressMsg struct {
	Current int
	Total   int
}

// dryRunPlanMsg is sent when dry-run gather completes (plan lines to show).
type dryRunPlanMsg struct {
	Lines []string
	Err   error
}

type releaseTUI struct {
	params              *releaseParams
	ch                  chan interface{}        // stepResultMsg, releaseDoneMsg, or dryRunPlanMsg
	status              [numReleaseSteps]string // "pending" | "running" | "done" | "skipped" | "error"
	current             int
	spinner             spinner.Model
	done                bool
	finalErr            error
	dryRunMode          bool
	dryRunLines         []string
	dryRunStatusLog     []string // progress lines during gather (e.g. "Found 12 commits")
	dryRunProgressCur   int      // for progress bar (fetching PRs)
	dryRunProgressTotal int
	dryRunProgressBar   progress.Model
}

func newReleaseTUI(params *releaseParams) *releaseTUI {
	s := spinner.New()
	s.Spinner = spinner.Dot
	pg := progress.New(
		progress.WithDefaultGradient(),
		progress.WithWidth(32),
	)
	return &releaseTUI{
		params:            params,
		ch:                make(chan interface{}, 1),
		status:            [numReleaseSteps]string{},
		spinner:           s,
		dryRunProgressBar: pg,
	}
}

func (m *releaseTUI) Init() tea.Cmd {
	for i := 0; i < numReleaseSteps; i++ {
		m.status[i] = "pending"
	}
	m.dryRunMode = m.params.dryRun
	if m.params.dryRun {
		m.status[0] = "running" // show "Gathering..." as first line
		m.current = -1          // no step spinner, we show a generic "Gathering plan..."
		go m.runDryRunGather()
	} else {
		m.status[0] = "running"
		m.current = 0
		go func() {
			report := func(step int, err error, skipped bool) {
				if skipped {
					m.ch <- stepResultMsg{Step: step, Skipped: true}
					return
				}
				m.ch <- stepResultMsg{Step: step, Err: err}
			}
			err := doReleaseSteps(m.params, report)
			m.ch <- releaseDoneMsg{Err: err}
		}()
	}

	return tea.Batch(
		m.spinner.Tick,
		m.waitForMsg(),
	)
}

func (m *releaseTUI) runDryRunGather() {
	ctx := m.params.ctx
	report := func(line string) {
		m.ch <- dryRunStatusMsg{Line: line}
	}
	reportProgress := func(current, total int) {
		m.ch <- dryRunProgressMsg{Current: current, Total: total}
	}
	src, err := gatherChangelogSource(ctx, m.params.cfg, m.params.repoAbs, m.params.prev, m.params.branch, 0, report, reportProgress)
	lines := []string{}
	if err != nil {
		m.ch <- dryRunPlanMsg{Err: err}
		return
	}
	// ✅ = actually ran during dry-run; ⏭️ = would run / skipped
	lines = append(lines, "✅ Previous tag "+m.params.prev+" validated")
	if m.params.cfg.Justfile != nil && len(m.params.cfg.Justfile.Targets) > 0 {
		lines = append(lines, fmt.Sprintf("✅ Just targets completed: %v", m.params.cfg.Justfile.Targets))
	}
	if len(src.PRs) > 0 {
		lines = append(lines, fmt.Sprintf("✅ Found %d merged PR(s) between %s and %s", len(src.PRs), m.params.prev, m.params.branch))
	} else {
		lines = append(lines, fmt.Sprintf("✅ Found %d commit(s) between %s and %s", len(src.Commits), m.params.prev, m.params.branch))
	}
	lines = append(lines, "⏭️ Changelog written to "+m.params.outPathAbs)
	lines = append(lines, "⏭️ Committed and tagged "+m.params.nextTagForRef)
	lines = append(lines, "⏭️ Pushed "+m.params.branch+" to "+m.params.remote)
	lines = append(lines, "⏭️ Pushed tag "+m.params.nextTagForRef+" to "+m.params.remote)
	lines = append(lines, "⏭️ All release workflow(s) completed")
	if m.params.cfg.Release != nil && m.params.cfg.Release.PyPIPackage != "" {
		pkgVersion := strings.TrimPrefix(m.params.nextTagForRef, "v")
		lines = append(lines, fmt.Sprintf("⏭️ Package %s==%s is available on PyPI", m.params.cfg.Release.PyPIPackage, pkgVersion))
	}
	if m.params.cfg.Release != nil && m.params.cfg.Release.DockerImage != "" {
		lines = append(lines, fmt.Sprintf("⏭️ Image %s:%s is available on Docker Hub", m.params.cfg.Release.DockerImage, m.params.nextTagForRef))
	}
	lines = append(lines, "✅ Release "+m.params.nextTagForRef+" complete (dry-run)")
	m.ch <- dryRunPlanMsg{Lines: lines}
}

func (m *releaseTUI) waitForMsg() tea.Cmd {
	return func() tea.Msg {
		return <-m.ch
	}
}

func (m *releaseTUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC || msg.String() == "q" {
			return m, tea.Quit
		}
		if m.done {
			return m, tea.Quit
		}
		return m, nil
	case stepResultMsg:
		if msg.Skipped {
			m.status[msg.Step] = "skipped"
		} else if msg.Err != nil {
			m.status[msg.Step] = "error"
			m.finalErr = msg.Err
		} else {
			m.status[msg.Step] = "done"
		}
		// Next step running
		next := msg.Step + 1
		if next < numReleaseSteps && m.status[next] == "pending" {
			m.status[next] = "running"
			m.current = next
		}
		return m, tea.Batch(m.spinner.Tick, m.waitForMsg())
	case releaseDoneMsg:
		m.done = true
		m.finalErr = msg.Err
		for i := 0; i < numReleaseSteps; i++ {
			if m.status[i] == "running" {
				if msg.Err != nil {
					m.status[i] = "error"
				} else {
					m.status[i] = "done"
				}
			}
		}
		return m, nil
	case dryRunStatusMsg:
		m.dryRunStatusLog = append(m.dryRunStatusLog, msg.Line)
		return m, tea.Batch(m.spinner.Tick, m.waitForMsg())
	case dryRunProgressMsg:
		m.dryRunProgressCur = msg.Current
		m.dryRunProgressTotal = msg.Total
		return m, tea.Batch(m.spinner.Tick, m.waitForMsg())
	case dryRunPlanMsg:
		m.done = true
		m.finalErr = msg.Err
		m.dryRunLines = msg.Lines
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, tea.Batch(cmd, m.waitForMsg())
	default:
		return m, nil
	}
}

func (m *releaseTUI) View() string {
	if m.dryRunMode {
		title := " releasebot  release plan (dry-run) "
		s := "\n  " + title + "\n\n"
		if len(m.dryRunLines) > 0 {
			for i, line := range m.dryRunLines {
				prefix := "├── "
				if i == len(m.dryRunLines)-1 {
					prefix = "└── "
				}
				s += "  " + prefix + line + "\n"
			}
		} else if m.finalErr != nil {
			s += "  ✗ " + m.finalErr.Error() + "\n"
		} else {
			for _, line := range m.dryRunStatusLog {
				s += "  ✅ " + line + "\n"
			}
			if m.dryRunProgressTotal > 0 {
				pct := float64(m.dryRunProgressCur) / float64(m.dryRunProgressTotal)
				s += "  " + m.dryRunProgressBar.ViewAs(pct) + " Fetching PRs " + fmt.Sprintf("%d/%d", m.dryRunProgressCur, m.dryRunProgressTotal) + "\n"
			} else {
				s += "  " + m.spinner.View() + " "
				if len(m.dryRunStatusLog) > 0 {
					s += "..."
				} else {
					s += "Gathering changelog source between " + m.params.prev + " and " + m.params.branch + "..."
				}
				s += "\n"
			}
		}
		if m.done {
			s += "\n  Press any key to exit\n"
		}
		s += "\n"
		return s
	}

	title := fmt.Sprintf(" releasebot  releasing %s ", m.params.nextTagForRef)
	s := "\n  " + title + "\n\n"

	for i := 0; i < numReleaseSteps; i++ {
		prefix := "  "
		if i < numReleaseSteps-1 {
			prefix = "├── "
		} else {
			prefix = "└── "
		}
		var icon string
		switch m.status[i] {
		case "done":
			icon = "✅" // actually executed
		case "running":
			icon = m.spinner.View()
		case "skipped":
			icon = "⏭️" // skipped
		case "error":
			icon = "✗"
		default:
			icon = "○"
		}
		s += fmt.Sprintf("%s%s  %s\n", prefix, icon, releaseStepNames[i])
	}

	s += "\n"
	if m.done && m.finalErr != nil {
		s += "  " + m.finalErr.Error() + "\n"
	} else if m.done {
		s += "  ✅ Release " + m.params.nextTagForRef + " complete\n"
	}
	if m.done {
		s += "\n  Press any key to exit\n"
	}
	return s
}

func runReleaseTUI(params *releaseParams) error {
	// No alt screen so the final output stays in terminal scrollback after keypress.
	p := tea.NewProgram(newReleaseTUI(params))
	model, err := p.Run()
	if err != nil {
		return err
	}
	if m, ok := model.(*releaseTUI); ok && m.finalErr != nil {
		return m.finalErr
	}
	return nil
}
