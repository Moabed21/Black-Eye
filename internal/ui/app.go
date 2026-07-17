// Package ui implements the root bubbletea model for BlackEye.
// It routes keyboard events to the active tab and renders the tab bar.
package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"blackeye/internal/bus"
	"blackeye/internal/config"
	"blackeye/internal/privilege"
	"blackeye/internal/ui/styles"
	"blackeye/internal/ui/tabs"
)

const (
	minWidth  = 80
	minHeight = 24
)

// Tab index constants.
const (
	TabDashboard = iota
	TabProcess
	TabNetwork
	TabDocker
	TabServices
	TabCount
)

var tabNames = [TabCount]string{
	"[1] Dashboard",
	"[2] Processes",
	"[3] Network",
	"[4] Docker",
	"[5] Services",
}

// Model is the root bubbletea model.
type Model struct {
	width     int
	height    int
	activeTab int
	tabs      [TabCount]tea.Model
	helpOpen  bool
	cfg       config.Config
}

// New creates the root model and initialises all tab models.
func New(b *bus.Bus, cfg config.Config) *Model {
	m := &Model{cfg: cfg}
	m.tabs[TabDashboard] = tabs.NewDashboard(b, cfg)
	m.tabs[TabProcess] = tabs.NewProcess(b, cfg)
	m.tabs[TabNetwork] = tabs.NewNetwork(b, cfg)
	m.tabs[TabDocker] = tabs.NewDocker(b, cfg)
	m.tabs[TabServices] = tabs.NewServices(b, cfg)
	return m
}

// GetTab returns the tea.Model for the given tab index.
// This allows main.go to type-assert and wire services (e.g. audit) into tabs.
func (m *Model) GetTab(idx int) tea.Model {
	if idx < 0 || idx >= TabCount {
		return nil
	}
	return m.tabs[idx]
}

func (m *Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	for i := range m.tabs {
		cmds = append(cmds, m.tabs[i].Init())
	}
	return tea.Batch(cmds...)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Propagate to all tabs.
		var cmds []tea.Cmd
		for i := range m.tabs {
			var cmd tea.Cmd
			m.tabs[i], cmd = m.tabs[i].Update(msg)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case tea.KeyMsg:
		if m.helpOpen {
			if msg.String() == "?" || msg.String() == "q" || msg.String() == "esc" {
				m.helpOpen = false
				return m, nil
			}
			return m, nil
		}

		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "?":
			m.helpOpen = true
			return m, nil
		case "1":
			m.activeTab = TabDashboard
		case "2":
			m.activeTab = TabProcess
		case "3":
			m.activeTab = TabNetwork
		case "4":
			m.activeTab = TabDocker
		case "5":
			m.activeTab = TabServices
		default:
			// Delegate to active tab.
			var cmd tea.Cmd
			m.tabs[m.activeTab], cmd = m.tabs[m.activeTab].Update(msg)
			return m, cmd
		}
		return m, nil
	case tea.MouseMsg:
		var cmd tea.Cmd
		m.tabs[m.activeTab], cmd = m.tabs[m.activeTab].Update(msg)
		return m, cmd
	}

	// Forward all other messages (bus ticks, etc.) to all tabs.
	var cmds []tea.Cmd
	for i := range m.tabs {
		var cmd tea.Cmd
		m.tabs[i], cmd = m.tabs[i].Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) View() string {
	if m.width < minWidth || m.height < minHeight {
		return styles.MinSizeWarning()
	}

	if m.helpOpen {
		return m.renderHelp()
	}

	tabBar := m.renderTabBar()
	content := m.tabs[m.activeTab].View()
	statusBar := m.renderStatusBar()

	return lipgloss.JoinVertical(lipgloss.Left, tabBar, content, statusBar)
}

func (m *Model) renderTabBar() string {
	var rendered []string
	for i, name := range tabNames {
		if i == m.activeTab {
			rendered = append(rendered, styles.TabActive.Render(name))
		} else {
			rendered = append(rendered, styles.TabInactive.Render(name))
		}
	}
	bar := lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
	underline := lipgloss.NewStyle().
		Foreground(styles.ColorGoldDim).
		Render(styles.RepeatStr("─", m.width))
	return lipgloss.JoinVertical(lipgloss.Left, bar, underline)
}

func (m *Model) renderStatusBar() string {
	var privStr string
	if privilege.CanKill() {
		privStr = styles.TextRed.Render("⚡ elevated")
	} else {
		privStr = styles.TextGreen.Render("● normal")
	}
	brandName := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorGold).Render("BlackEye")
	left := fmt.Sprintf("  %s  │  %s", brandName, privStr)
	right := styles.TextMuted.Render("q quit  │  ? help  │  1–5 tabs  ")

	width := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if width < 0 {
		width = 0
	}
	spacer := styles.RepeatStr(" ", width)

	bar := lipgloss.NewStyle().
		Foreground(styles.ColorText).
		Background(styles.ColorNavy).
		Render(left + spacer + right)
	return bar
}

func (m *Model) renderHelp() string {
	return styles.PanelStyle.Render(
		styles.PanelTitleStyle.Render("? Help — Keyboard Shortcuts") + "\n\n" +
			styles.TextBold.Render("Global\n") +
			"  q / Ctrl+C   Quit\n" +
			"  1–5          Switch tab\n" +
			"  ?            Toggle this help\n\n" +
			styles.TextBold.Render("Process Tab (2)\n") +
			"  ↑/↓          Navigate\n" +
			"  s / F6       Sort processes\n" +
			"  r            Reverse sort\n" +
			"  /            Filter by name/user\n" +
			"  ESC          Clear filter\n" +
			"  Enter        Process detail\n" +
			(func() string {
				if privilege.CanKill() {
					return "  k / F9       Send signal (kill, sleep, etc)\n"
				}
				return ""
			})() + "\n" +
			styles.TextBold.Render("Network Tab (3)\n") +
			"  Tab          Switch sub-panel\n" +
			"  ↑/↓          Navigate\n" +
			"  /            Filter\n\n" +
			styles.TextBold.Render("Docker Tab (4)\n") +
			"  ↑/↓          Navigate\n" +
			"  Enter        Container detail\n" +
			"  l            View logs\n" +
			(func() string {
				if privilege.CanKill() {
					return "  r            Restart (confirm)\n" +
						"  s            Stop (confirm)\n"
				}
				return ""
			})() +
			"\n" +
			styles.TextBold.Render("Services Tab (5)\n") +
			"  Tab          Switch (services/dmesg)\n" +
			"  /            Filter\n" +
			"  a/f/r        Show all/failed/running\n",
	)
}
