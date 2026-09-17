package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/TheShiveshNetwork/syla/pkg/loop"
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	barStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	helpStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

// Model is the Bubble Tea model for a running loop.
type Model struct {
	engine          *loop.Engine
	viewport        viewport.Model
	status          string
	iteration       int
	elapsed         string
	budget          string
	logs            []string
	quitting        bool
	detached        bool
	confirmingAbort bool
	abortError      string
}

type eventMsg loop.Event

func New(engine *loop.Engine) Model {
	vp := viewport.New(80, 20)
	vp.SetContent("Starting syla...\n")
	return Model{
		engine:   engine,
		viewport: vp,
		status:   "running",
	}
}

func (m Model) Init() tea.Cmd {
	return waitForEvent(m.engine)
}

func waitForEvent(e *loop.Engine) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-e.Events()
		if !ok {
			return tea.Quit()
		}
		return eventMsg(ev)
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.confirmingAbort {
			switch msg.String() {
			case "y", "Y":
				if err := m.engine.Abort(); err != nil {
					m.abortError = err.Error()
				} else {
					m.status = "aborted"
					m.logs = append(m.logs, "■ aborted by user")
					m.viewport.SetContent(strings.Join(m.logs, "\n"))
					m.viewport.GotoBottom()
				}
				m.confirmingAbort = false
				return m, waitForEvent(m.engine)
			case "n", "N", "esc":
				m.confirmingAbort = false
				m.abortError = ""
				return m, nil
			}
			return m, nil
		}
		switch msg.String() {
		case "q", "ctrl+c":
			m.detached = true
			m.quitting = true
			return m, tea.Quit
		case "p":
			m.status = "paused"
			return m, nil
		case "a", "A":
			m.confirmingAbort = true
			m.abortError = ""
			return m, nil
		}
	case eventMsg:
		ev := loop.Event(msg)
		switch ev.Type {
		case "IterationStarted":
			m.iteration = ev.Iteration
			m.status = fmt.Sprintf("iteration %d", ev.Iteration)
			m.logs = append(m.logs, fmt.Sprintf("→ %s", ev.Message))
		case "IterationCompleted":
			if ev.Result != nil {
				m.logs = append(m.logs, fmt.Sprintf("✓ iter %d: %s", ev.Iteration, ev.Result.Output.Summary))
			} else if ev.Message != "" {
				m.logs = append(m.logs, ev.Message)
			}
		case "Failure":
			m.logs = append(m.logs, fmt.Sprintf("✗ %v", ev.Err))
		case "StopConditionMet":
			m.status = "completed: " + ev.Message
			m.logs = append(m.logs, "■ "+ev.Message)
		case "RateLimitWait":
			m.status = "rate-limit wait"
			m.logs = append(m.logs, "⏳ "+ev.Message)
		case "BudgetWarning":
			m.budget = ev.Message
		}
		// keep logs bounded
		if len(m.logs) > 200 {
			m.logs = m.logs[len(m.logs)-200:]
		}
		m.viewport.SetContent(strings.Join(m.logs, "\n"))
		m.viewport.GotoBottom()
		if !m.quitting {
			return m, waitForEvent(m.engine)
		}
	case tea.WindowSizeMsg:
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - 6
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	if m.confirmingAbort {
		modal := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("196")).Padding(1, 2).Render(
			"Abort session " + m.engine.RunID() + "?\n\nThis will stop the agent loop and mark the run as aborted.\n\nPress y to confirm, n/esc to cancel",
		)
		if m.abortError != "" {
			modal += "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("Error: "+m.abortError)
		}
		return modal + "\n\n" + helpStyle.Render("y: confirm abort • n/esc: cancel")
	}
	if m.quitting && m.detached {
		return helpStyle.Render("Detached. Run `syla attach "+m.engine.RunID()+"` to reattach. `syla stop "+m.engine.RunID()+"` to stop.\n")
	}
	if m.quitting {
		return "Quitting...\n"
	}
	header := titleStyle.Render(fmt.Sprintf("syla • %s • %s", m.engine.RunID(), m.status))
	bar := barStyle.Render(fmt.Sprintf("iter %d | %s | %s", m.iteration, m.elapsed, m.budget))
	help := helpStyle.Render("q: detach • p: pause • a: abort • ctrl+c: detach")
	return fmt.Sprintf("%s\n%s\n\n%s\n\n%s", header, bar, m.viewport.View(), help)
}

// Attach runs the TUI attached to the given engine. It returns when detached.
func Attach(engine *loop.Engine) error {
	m := New(engine)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

type remoteStatusMsg struct {
	iteration int
	status    string
	elapsed   string
	err       error
}

type RemoteModel struct {
	runID          string
	runDir         string
	viewport       viewport.Model
	status         string
	iteration      int
	elapsed        string
	logs           []string
	quitting       bool
	detached       bool
	confirmingAbort bool
	abortError     string
}

func NewRemote(runID, runDir string) RemoteModel {
	vp := viewport.New(80, 20)
	vp.SetContent("Connecting to daemon...\n")
	return RemoteModel{
		runID:    runID,
		runDir:   runDir,
		viewport: vp,
		status:   "connecting",
	}
}

func (m RemoteModel) Init() tea.Cmd {
	return pollRemote(m.runID, m.runDir)
}

func pollRemote(runID, runDir string) tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		// try to dial daemon, fallback to checkpoint file
		status, _ := fetchRemoteStatus(runDir)
		if status != nil {
			return remoteStatusMsg{iteration: status.Iteration, status: status.Status, elapsed: status.Elapsed}
		}
		return remoteStatusMsg{status: "unknown", err: fmt.Errorf("no status")}
	})
}

func fetchRemoteStatus(runDir string) (*remoteStatus, error) {
	// try live daemon IPC first (more up-to-date than checkpoint file)
	sock := runDir + "/ctl.sock"
	if conn, err := net.DialTimeout("unix", sock, 200*time.Millisecond); err == nil {
		req := map[string]any{"id": 1, "method": "Status"}
		b, _ := json.Marshal(req)
		b = append(b, '\n')
		_, _ = conn.Write(b)
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		scanner := bufio.NewScanner(conn)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)
		if scanner.Scan() {
			var res struct {
				Result *remoteStatus `json:"result"`
				Error  string        `json:"error"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &res); err == nil && res.Result != nil && res.Error == "" {
				_ = conn.Close()
				return res.Result, nil
			}
		}
		_ = conn.Close()
	}
	// fallback to checkpoint file
	cpPath := runDir + "/checkpoint.json"
	data, err := os.ReadFile(cpPath)
	if err != nil {
		return nil, err
	}
	var cp struct {
		Iteration int    `json:"iteration"`
		Status    string `json:"status"`
		StopReason string `json:"stop_reason"`
		StartedAt time.Time `json:"started_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, err
	}
	var elapsed string
	if cp.Status == "completed" || cp.Status == "failed" || cp.StopReason != "" {
		if !cp.UpdatedAt.IsZero() && !cp.StartedAt.IsZero() {
			elapsed = cp.UpdatedAt.Sub(cp.StartedAt).Truncate(time.Second).String()
		} else {
			elapsed = time.Since(cp.StartedAt).Truncate(time.Second).String()
		}
	} else {
		elapsed = time.Since(cp.StartedAt).Truncate(time.Second).String()
	}
	if cp.Status == "" {
		cp.Status = "running"
	}
	if cp.StopReason != "" {
		cp.Status = cp.StopReason
	}
	return &remoteStatus{Iteration: cp.Iteration, Status: cp.Status, Elapsed: elapsed}, nil
}

type remoteStatus struct {
	Iteration int
	Status    string
	Elapsed   string
}

func (m RemoteModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.confirmingAbort {
			switch msg.String() {
			case "y", "Y":
				// confirm abort
				if err := abortRemote(m.runDir); err != nil {
					m.abortError = err.Error()
				} else {
					m.status = "aborted"
					m.logs = append(m.logs, "■ aborted by user")
					m.viewport.SetContent(strings.Join(m.logs, "\n"))
					m.viewport.GotoBottom()
				}
				m.confirmingAbort = false
				return m, pollRemote(m.runID, m.runDir)
			case "n", "N", "esc":
				m.confirmingAbort = false
				m.abortError = ""
				return m, nil
			}
			// while confirming, ignore other keys
			return m, nil
		}
		switch msg.String() {
		case "q", "ctrl+c":
			m.detached = true
			m.quitting = true
			return m, tea.Quit
		case "a", "A":
			if isCompletedStatus(m.status) {
				// already completed, don't allow abort
				m.logs = append(m.logs, "Already "+m.status+", cannot abort")
				m.viewport.SetContent(strings.Join(m.logs, "\n"))
				m.viewport.GotoBottom()
				return m, nil
			}
			// show confirmation modal for abort
			m.confirmingAbort = true
			m.abortError = ""
			return m, nil
		}
	case remoteStatusMsg:
		if msg.err == nil {
			changed := msg.iteration != m.iteration || msg.status != m.status
			m.iteration = msg.iteration
			m.status = msg.status
			m.elapsed = msg.elapsed
			if changed {
				// only log on change to avoid spam
				if msg.status == "running" {
					m.logs = append(m.logs, fmt.Sprintf("→ iter %d • %s", msg.iteration, msg.elapsed))
				} else {
					m.logs = append(m.logs, fmt.Sprintf("■ %s • iter %d • %s", msg.status, msg.iteration, msg.elapsed))
				}
				if len(m.logs) > 200 {
					m.logs = m.logs[len(m.logs)-200:]
				}
				m.viewport.SetContent(strings.Join(m.logs, "\n"))
				m.viewport.GotoBottom()
			} else {
				// still update bar even if no log
				m.viewport.SetContent(strings.Join(m.logs, "\n"))
			}
			// if daemon reports completed/failed, show final but do NOT exit - just display completed in TUI
			if isCompletedStatus(msg.status) {
				m.status = msg.status
				// append final completion line once
				if len(m.logs) == 0 || !strings.Contains(m.logs[len(m.logs)-1], "completed") {
					m.logs = append(m.logs, fmt.Sprintf("✔ Run %s completed: %s after %d iterations", m.runID, msg.status, msg.iteration))
					m.viewport.SetContent(strings.Join(m.logs, "\n"))
					m.viewport.GotoBottom()
				}
				// stay in TUI and keep showing completed, do not quit
				// still poll occasionally in case status changes, but don't spam logs
				return m, pollRemote(m.runID, m.runDir)
			}
		}
		return m, pollRemote(m.runID, m.runDir)
	case tea.WindowSizeMsg:
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - 6
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func isCompletedStatus(s string) bool {
	if s == "completed" || s == "failed" || s == "aborted" {
		return true
	}
	if strings.Contains(s, "max_duration") || strings.Contains(s, "max_iterations") || strings.Contains(s, "budget") || strings.Contains(s, "no_diff") || strings.Contains(s, "aborted") {
		return true
	}
	// checkpoint's StopReason is already the status, so any non-running and not empty that is not "running" is considered completed
	if s != "running" && s != "unknown" && s != "connecting" && s != "" {
		// for custom stop conditions, treat as completed
		return true
	}
	return false
}

func (m RemoteModel) View() string {
	if m.confirmingAbort {
		modal := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("196")).Padding(1, 2).Render(
			"Abort session " + m.runID + "?\n\nThis will stop the agent loop and mark the run as aborted.\n\nPress y to confirm, n/esc to cancel",
		)
		if m.abortError != "" {
			modal += "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("Error: "+m.abortError)
		}
		return modal + "\n\n" + helpStyle.Render("y: confirm abort • n/esc: cancel")
	}
	if m.quitting {
		if m.detached {
			return helpStyle.Render(fmt.Sprintf("Detached. Run `syla attach %s` to reattach. `syla stop %s` to stop.\n", m.runID, m.runID))
		}
		return "Quitting...\n"
	}
	if isCompletedStatus(m.status) {
		start := 0
		if len(m.logs) > 5 {
			start = len(m.logs) - 5
		}
		return titleStyle.Render(fmt.Sprintf("✔ Run %s completed: %s", m.runID, m.status)) + "\n" +
			barStyle.Render(fmt.Sprintf("iter %d • %s", m.iteration, m.elapsed)) + "\n\n" +
			strings.Join(m.logs[start:], "\n") + "\n\n" +
			helpStyle.Render("Process completed • q: detach • syla status to see summary") + "\n"
	}
	header := titleStyle.Render(fmt.Sprintf("syla • %s • %s", m.runID, m.status))
	bar := barStyle.Render(fmt.Sprintf("iter %d | %s", m.iteration, m.elapsed))
	help := helpStyle.Render("q: detach • a: abort • ctrl+c: detach")
	return fmt.Sprintf("%s\n%s\n\n%s\n\n%s", header, bar, m.viewport.View(), help)
}

// AttachRemote attaches to a remote daemon via polling (fallback when not in-process).
func AttachRemote(runID, runDir string) error {
	// If no TTY (e.g. CI, non-interactive), fallback to simple polling log
	if !isTTY() {
		return attachRemoteNonTTY(runID, runDir)
	}
	m := NewRemote(runID, runDir)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	if err != nil {
		// Bubble Tea failed to open TTY, fallback to non-TTY mode
		if strings.Contains(err.Error(), "/dev/tty") || strings.Contains(err.Error(), "tty") {
			return attachRemoteNonTTY(runID, runDir)
		}
		return err
	}
	// After TUI exits, if it was due to completion, print summary to plain terminal
	if data, err := os.ReadFile(runDir + "/exit-summary.txt"); err == nil {
		fmt.Println("\n" + string(data))
	} else if data, err := os.ReadFile(runDir + "/checkpoint.json"); err == nil {
		var cp struct {
			Iteration int    `json:"iteration"`
			Status    string `json:"status"`
			StopReason string `json:"stop_reason"`
		}
		if err := json.Unmarshal(data, &cp); err == nil && isCompletedStatus(cp.Status) {
			fmt.Printf("\n✔ Run %s completed: %s after %d iterations\n", runID, cp.Status, cp.Iteration)
			if cp.StopReason != "" {
				fmt.Printf("  stop reason: %s\n", cp.StopReason)
			}
		}
	}
	return nil
}

func isTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func abortRemote(runDir string) error {
	sock := runDir + "/ctl.sock"
	conn, err := net.DialTimeout("unix", sock, 500*time.Millisecond)
	if err != nil {
		return err
	}
	defer conn.Close()
	req := map[string]any{"id": 2, "method": "Close"}
	b, _ := json.Marshal(req)
	b = append(b, '\n')
	if _, err := conn.Write(b); err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	scanner := bufio.NewScanner(conn)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	if scanner.Scan() {
		var res struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(scanner.Bytes(), &res)
		if res.Error != "" {
			return fmt.Errorf("%s", res.Error)
		}
	}
	return nil
}

func attachRemoteNonTTY(runID, runDir string) error {
	fmt.Printf("syla • %s • polling (no TTY, showing plain logs)\n", runID)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var lastIter int
	var lastStatus string
	for {
		select {
		case <-ticker.C:
			st, err := fetchRemoteStatus(runDir)
			if err != nil {
				continue
			}
			if st.Iteration != lastIter || st.Status != lastStatus {
				fmt.Printf("iter %d • %s • %s\n", st.Iteration, st.Status, st.Elapsed)
				lastIter = st.Iteration
				lastStatus = st.Status
			}
			if isCompletedStatus(st.Status) {
				fmt.Printf("\n✔ Run %s completed: %s after %d iterations\n", runID, st.Status, st.Iteration)
				if data, err := os.ReadFile(runDir + "/exit-summary.txt"); err == nil {
					fmt.Println(string(data))
				}
				fmt.Println("Process completed.")
				return nil
			}
		}
	}
}
