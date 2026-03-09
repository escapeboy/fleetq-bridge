package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/fleetq/fleetq-bridge/internal/ipc"
)

// Tab indices
const (
	tabStatus    = 0
	tabEndpoints = 1
	tabLogs      = 2
)

var tabNames = []string{"Status", "Endpoints", "Logs"}

// Styles
var (
	colorPrimary  = lipgloss.Color("#7C3AED")
	colorMuted    = lipgloss.Color("#6B7280")
	colorSuccess  = lipgloss.Color("#10B981")
	colorError    = lipgloss.Color("#EF4444")
	colorWarning  = lipgloss.Color("#F59E0B")
	colorBorder   = lipgloss.Color("#374151")

	styleTab = lipgloss.NewStyle().
			Padding(0, 2).
			Foreground(colorMuted)

	styleTabActive = lipgloss.NewStyle().
			Padding(0, 2).
			Foreground(colorPrimary).
			Bold(true)

	styleTabBar = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(colorBorder)

	styleBold   = lipgloss.NewStyle().Bold(true)
	styleSuccess = lipgloss.NewStyle().Foreground(colorSuccess)
	styleError   = lipgloss.NewStyle().Foreground(colorError)
	styleMuted   = lipgloss.NewStyle().Foreground(colorMuted)

	styleKeyHint = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)
)

// tickMsg is sent every second for status refresh
type tickMsg time.Time

// statusMsg carries fresh status from daemon
type statusMsg struct {
	status *ipc.StatusPayload
	err    error
}

// logLine is a single log entry
type logLine struct {
	ts   time.Time
	text string
}

// Model is the root Bubble Tea model
type Model struct {
	activeTab int
	width     int
	height    int

	// Status tab
	connected bool
	status    *ipc.StatusPayload
	statusErr string
	lastPoll  time.Time

	// Logs tab
	logs     []logLine
	maxLogs  int

	// scroll offsets
	scrollEndpoints int
	scrollLogs      int
}

// New creates a new TUI model
func New() Model {
	return Model{
		activeTab: tabStatus,
		maxLogs:   500,
	}
}

// AddLog appends a line to the logs tab (safe to call from goroutines via tea.Println)
func (m *Model) AddLog(line string) {
	m.logs = append(m.logs, logLine{ts: time.Now(), text: line})
	if len(m.logs) > m.maxLogs {
		m.logs = m.logs[len(m.logs)-m.maxLogs:]
	}
}

// Init starts the tick loop
func (m Model) Init() tea.Cmd {
	return tea.Batch(tickCmd(), pollStatusCmd())
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab", "right", "l":
			m.activeTab = (m.activeTab + 1) % len(tabNames)
		case "shift+tab", "left", "h":
			m.activeTab = (m.activeTab - 1 + len(tabNames)) % len(tabNames)
		case "1":
			m.activeTab = tabStatus
		case "2":
			m.activeTab = tabEndpoints
		case "3":
			m.activeTab = tabLogs
		case "up", "k":
			m.scroll(-1)
		case "down", "j":
			m.scroll(1)
		case "g":
			m.scrollToTop()
		case "G":
			m.scrollToBottom()
		}
		return m, nil

	case tickMsg:
		return m, tea.Batch(tickCmd(), pollStatusCmd())

	case statusMsg:
		m.lastPoll = time.Now()
		if msg.err != nil {
			m.connected = false
			m.statusErr = msg.err.Error()
		} else {
			m.connected = true
			m.statusErr = ""
			m.status = msg.status
		}
		return m, nil
	}

	return m, nil
}

// View renders the full TUI
func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	tabs := m.renderTabs()
	body := m.renderBody()
	footer := m.renderFooter()

	// Reserve lines: tabBar=2, footer=1
	return lipgloss.JoinVertical(lipgloss.Left, tabs, body, footer)
}

func (m Model) renderTabs() string {
	var parts []string
	for i, name := range tabNames {
		if i == m.activeTab {
			parts = append(parts, styleTabActive.Render(name))
		} else {
			parts = append(parts, styleTab.Render(name))
		}
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	return styleTabBar.Width(m.width).Render(row)
}

func (m Model) renderBody() string {
	bodyHeight := m.height - 3 // tabbar + footer
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	var content string
	switch m.activeTab {
	case tabStatus:
		content = m.renderStatus()
	case tabEndpoints:
		content = m.renderEndpoints()
	case tabLogs:
		content = m.renderLogs(bodyHeight)
	}

	return lipgloss.NewStyle().
		Width(m.width).
		Height(bodyHeight).
		Padding(1, 2).
		Render(content)
}

func (m Model) renderStatus() string {
	var b strings.Builder

	// Connection indicator
	if m.connected {
		b.WriteString(styleSuccess.Render("● Connected") + "  ")
	} else {
		b.WriteString(styleError.Render("● Disconnected") + "  ")
	}
	if m.lastPoll.IsZero() {
		b.WriteString(styleMuted.Render("Connecting..."))
	} else {
		b.WriteString(styleMuted.Render("Last update: " + m.lastPoll.Format("15:04:05")))
	}
	b.WriteString("\n\n")

	if !m.connected {
		if m.statusErr != "" {
			b.WriteString(styleError.Render("Error: " + m.statusErr))
		} else {
			b.WriteString(styleMuted.Render("Daemon not running. Start with: fleetq-bridge daemon"))
		}
		return b.String()
	}

	if m.status == nil {
		b.WriteString(styleMuted.Render("Waiting for status..."))
		return b.String()
	}

	// Summary counts
	onlineLLMs := 0
	for _, ep := range m.status.LLMEndpoints {
		if ep.Online {
			onlineLLMs++
		}
	}
	foundAgents := 0
	for _, a := range m.status.Agents {
		if a.Found {
			foundAgents++
		}
	}

	b.WriteString(styleBold.Render("Local LLMs: "))
	b.WriteString(fmt.Sprintf("%d online / %d probed\n", onlineLLMs, len(m.status.LLMEndpoints)))

	b.WriteString(styleBold.Render("AI Agents:  "))
	b.WriteString(fmt.Sprintf("%d found / %d supported\n", foundAgents, len(m.status.Agents)))

	b.WriteString(styleBold.Render("Relay:      "))
	b.WriteString(m.status.RelayURL + "\n")

	return b.String()
}

func (m Model) renderEndpoints() string {
	if !m.connected || m.status == nil {
		return styleMuted.Render("Connect the daemon to see endpoints.")
	}

	var b strings.Builder

	b.WriteString(styleBold.Render("Local LLMs") + "\n\n")
	for _, ep := range m.status.LLMEndpoints {
		dot := styleError.Render("○")
		detail := "offline"
		if ep.Online {
			dot = styleSuccess.Render("●")
			detail = fmt.Sprintf("%d model(s)", len(ep.Models))
		}
		b.WriteString(fmt.Sprintf("  %s  %-14s %-28s %s\n", dot, ep.Name, ep.URL, styleMuted.Render(detail)))
	}

	b.WriteString("\n" + styleBold.Render("AI Agents") + "\n\n")
	for _, a := range m.status.Agents {
		dot := styleError.Render("○")
		detail := "not found"
		if a.Found {
			dot = styleSuccess.Render("●")
			detail = a.Version
		}
		b.WriteString(fmt.Sprintf("  %s  %-14s %-20s %s\n", dot, a.Key, a.Binary, styleMuted.Render(detail)))
	}

	if len(m.status.MCPServers) > 0 {
		b.WriteString("\n" + styleBold.Render("MCP Servers") + "\n\n")
		for _, s := range m.status.MCPServers {
			dot := styleError.Render("○")
			detail := "not running"
			if s.Running {
				dot = styleSuccess.Render("●")
				detail = "running"
			}
			b.WriteString(fmt.Sprintf("  %s  %-20s %s\n", dot, s.Name, styleMuted.Render(detail)))
		}
	}

	return b.String()
}

func (m Model) renderLogs(height int) string {
	if len(m.logs) == 0 {
		return styleMuted.Render("No logs yet. Daemon events will appear here.")
	}

	// Apply scroll offset from bottom
	visible := height - 2
	if visible < 1 {
		visible = 1
	}

	total := len(m.logs)
	// scrollLogs=0 means bottom (newest)
	end := total - m.scrollLogs
	if end > total {
		end = total
	}
	start := end - visible
	if start < 0 {
		start = 0
	}

	var lines []string
	for _, l := range m.logs[start:end] {
		ts := styleMuted.Render(l.ts.Format("15:04:05"))
		lines = append(lines, ts+"  "+l.text)
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderFooter() string {
	hints := []string{
		"tab/1-3: switch",
		"j/k: scroll",
		"g/G: top/bottom",
		"q: quit",
	}
	text := styleKeyHint.Render(strings.Join(hints, "  ·  "))
	return lipgloss.NewStyle().
		Width(m.width).
		BorderStyle(lipgloss.NormalBorder()).
		BorderTop(true).
		BorderForeground(colorBorder).
		Padding(0, 1).
		Render(text)
}

func (m *Model) scroll(delta int) {
	switch m.activeTab {
	case tabEndpoints:
		m.scrollEndpoints -= delta
		if m.scrollEndpoints < 0 {
			m.scrollEndpoints = 0
		}
	case tabLogs:
		m.scrollLogs += delta
		if m.scrollLogs < 0 {
			m.scrollLogs = 0
		}
		if m.scrollLogs >= len(m.logs) && len(m.logs) > 0 {
			m.scrollLogs = len(m.logs) - 1
		}
	}
}

func (m *Model) scrollToTop() {
	switch m.activeTab {
	case tabLogs:
		if len(m.logs) > 0 {
			m.scrollLogs = len(m.logs) - 1
		}
	}
}

func (m *Model) scrollToBottom() {
	m.scrollLogs = 0
	m.scrollEndpoints = 0
}

// --- commands ---

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func pollStatusCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		client, err := ipc.Dial(ctx)
		if err != nil {
			return statusMsg{err: err}
		}
		defer client.Close()
		status, err := client.GetStatus()
		return statusMsg{status: status, err: err}
	}
}
