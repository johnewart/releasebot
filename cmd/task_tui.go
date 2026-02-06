package cmd

import (
	"fmt"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/johnewart/releasebot/internal/sound"
)

// taskTUI is a generic step-based TUI with optional status log and progress bar.
// The worker sends: taskStatusMsg, taskProgressMsg, taskStepResultMsg, taskDoneMsg.
type taskTUI struct {
	title         string
	stepNames     []string
	ch            chan interface{}
	status        []string // "pending" | "running" | "done" | "skipped" | "error"
	current       int
	spinner       spinner.Model
	done          bool
	finalErr      error
	statusLog     []string
	progressCur   int
	progressTot   int
	progressLabel string // e.g. "Fetching PRs" or "Summarizing PRs"
	progressBar   progress.Model
	planLines     []string // when set (e.g. dry-run), View shows plan instead of steps
}

type taskStatusMsg struct{ Line string }
type taskProgressMsg struct {
	Current int
	Total   int
	Label   string // optional, e.g. "Summarizing PRs"; empty => "Fetching PRs"
}
type taskStepResultMsg struct {
	Step    int
	Err     error
	Skipped bool
}
type taskDoneMsg struct{ Err error }
type taskPlanMsg struct{ Lines []string } // dry-run plan; when set, View shows plan instead of steps

func newTaskTUI(title string, stepNames []string) *taskTUI {
	s := spinner.New()
	s.Spinner = spinner.Dot
	pg := progress.New(progress.WithDefaultGradient(), progress.WithWidth(32))
	status := make([]string, len(stepNames))
	for i := range status {
		status[i] = "pending"
	}
	return &taskTUI{
		title:       title,
		stepNames:   stepNames,
		ch:          make(chan interface{}, 8),
		status:      status,
		spinner:     s,
		progressBar: pg,
	}
}

func (m *taskTUI) Init() tea.Cmd {
	m.status[0] = "running"
	m.current = 0
	return tea.Batch(m.spinner.Tick, m.waitCh())
}

func (m *taskTUI) waitCh() tea.Cmd {
	return func() tea.Msg { return <-m.ch }
}

func (m *taskTUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC || msg.String() == "q" {
			return m, tea.Quit
		}
		if m.done {
			return m, tea.Quit
		}
		return m, nil
	case taskStatusMsg:
		m.statusLog = append(m.statusLog, msg.Line)
		return m, tea.Batch(m.spinner.Tick, m.waitCh())
	case taskProgressMsg:
		m.progressCur = msg.Current
		m.progressTot = msg.Total
		if msg.Label != "" {
			m.progressLabel = msg.Label
		}
		return m, tea.Batch(m.spinner.Tick, m.waitCh())
	case taskStepResultMsg:
		if msg.Skipped {
			m.status[msg.Step] = "skipped"
		} else if msg.Err != nil {
			m.status[msg.Step] = "error"
			m.finalErr = msg.Err
		} else {
			m.status[msg.Step] = "done"
		}
		m.statusLog = nil
		m.progressCur, m.progressTot = 0, 0
		m.progressLabel = ""
		next := msg.Step + 1
		if next < len(m.stepNames) && m.status[next] == "pending" {
			m.status[next] = "running"
			m.current = next
		}
		return m, tea.Batch(m.spinner.Tick, m.waitCh())
	case taskPlanMsg:
		m.planLines = msg.Lines
		m.done = true
		for i := range m.status {
			if m.status[i] == "running" {
				m.status[i] = "done"
			}
		}
		sound.PlaySuccess()
		return m, nil
	case taskDoneMsg:
		m.done = true
		m.finalErr = msg.Err
		for i := range m.status {
			if m.status[i] == "running" {
				if msg.Err != nil {
					m.status[i] = "error"
				} else {
					m.status[i] = "done"
				}
			}
		}
		if msg.Err != nil {
			sound.PlayFailure()
		} else {
			sound.PlaySuccess()
		}
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, tea.Batch(cmd, m.waitCh())
	default:
		return m, nil
	}
}

func (m *taskTUI) View() string {
	s := "\n  " + m.title + "\n\n"
	if len(m.planLines) > 0 {
		for i, line := range m.planLines {
			prefix := "├── "
			if i == len(m.planLines)-1 {
				prefix = "└── "
			}
			s += "  " + prefix + line + "\n"
		}
		s += "\n"
		if m.done {
			s += "\n  Press any key to exit\n"
		}
		return s
	}
	for i := 0; i < len(m.stepNames); i++ {
		prefix := "├── "
		if i == len(m.stepNames)-1 {
			prefix = "└── "
		}
		var icon string
		switch m.status[i] {
		case "done":
			icon = "✅"
		case "running":
			icon = m.spinner.View()
		case "skipped":
			icon = "⏭️"
		case "error":
			icon = "✗"
		default:
			icon = "○"
		}
		s += fmt.Sprintf("  %s%s  %s\n", prefix, icon, m.stepNames[i])
		if m.status[i] == "running" {
			for _, line := range m.statusLog {
				s += "     ✅ " + line + "\n"
			}
			if m.progressTot > 0 {
				pct := float64(m.progressCur) / float64(m.progressTot)
				label := m.progressLabel
				if label == "" {
					label = "Fetching PRs"
				}
				s += "     " + m.progressBar.ViewAs(pct) + " " + label + " " + fmt.Sprintf("%d/%d", m.progressCur, m.progressTot) + "\n"
			} else if len(m.statusLog) == 0 {
				s += "     " + m.spinner.View() + " ...\n"
			}
		}
	}
	s += "\n"
	if m.done && m.finalErr != nil {
		s += "  " + m.finalErr.Error() + "\n"
	} else if m.done {
		s += "  ✅ Done\n"
	}
	if m.done {
		s += "\n  Press any key to exit\n"
	}
	return s
}

// RunTaskTUI runs a task TUI: starts the worker in a goroutine, then runs the Bubble Tea program.
// Worker should send taskStepResultMsg for each step, then taskDoneMsg. It may send taskStatusMsg
// and taskProgressMsg during a step (e.g. during gather). Returns the final error if any.
func RunTaskTUI(title string, stepNames []string, worker func(ch chan<- interface{})) error {
	m := newTaskTUI(title, stepNames)
	go worker(m.ch)
	p := tea.NewProgram(m)
	model, err := p.Run()
	if err != nil {
		return err
	}
	if t, ok := model.(*taskTUI); ok && t.finalErr != nil {
		return t.finalErr
	}
	return nil
}
