// Package ui implements the root bubbletea model for BlackEye.
// It routes keyboard events to the active tab and renders the tab bar.
package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"blackeye/internal/bus"
	"blackeye/internal/config"
	"blackeye/internal/privilege"
	"blackeye/internal/services/alerts"
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
	TabTerminal
	TabFirewall
	TabPackages
	TabUsers
	TabAdvanced
	TabCount
)

var tabNames = [TabCount]string{
	"[1] Dashboard",
	"[2] Processes",
	"[3] Network",
	"[4] Docker",
	"[5] Services",
	"[6] Terminal",
	"[7] Firewall",
	"[8] Packages",
	"[9] Users",
	"[0] Advanced",
}

// Model is the root bubbletea model.
type Model struct {
	width     int
	height    int
	activeTab int
	tabs      [TabCount]tea.Model
	// Help modal state.
	helpOpen         bool
	helpScrollOffset int
	cfg              config.Config

	// Alert notification state.
	alertSub    <-chan interface{}
	alertSnap   *alerts.Snapshot
	alertToast  string
	alertExpiry time.Time
}

// New creates the root model and initialises all tab models.
func New(b *bus.Bus, cfg config.Config) *Model {
	m := &Model{
		cfg:      cfg,
		alertSub: b.Subscribe("alerts"),
		tabs: [TabCount]tea.Model{
			tabs.NewDashboard(b, cfg),
			tabs.NewProcess(b, cfg),
			tabs.NewNetwork(b, cfg),
			tabs.NewDocker(b, cfg),
			tabs.NewServices(b, cfg),
			tabs.NewTerminal(),
			tabs.NewFirewall(b, cfg),
			tabs.NewPackages(b, cfg),
			tabs.NewUsers(b, cfg),
			tabs.NewAdvanced(b, cfg),
		},
	}
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
	cmds = append(cmds, listenAlerts(m.alertSub))
	return tea.Batch(cmds...)
}

// alertMsg delivers an alert snapshot from the bus.
type alertMsg struct {
	snap alerts.Snapshot
}

func listenAlerts(ch <-chan interface{}) tea.Cmd {
	return func() tea.Msg {
		v := <-ch
		if snap, ok := v.(alerts.Snapshot); ok {
			return alertMsg{snap: snap}
		}
		return nil
	}
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

	case alertMsg:
		m.alertSnap = &msg.snap
		if len(msg.snap.Active) > 0 {
			// Show the highest-priority alert as a toast.
			alert := msg.snap.Active[0]
			for _, a := range msg.snap.Active {
				if a.Level == alerts.AlertCritical {
					alert = a
					break
				}
			}
			m.alertToast = alert.Message
			m.alertExpiry = time.Now().Add(10 * time.Second)
		} else {
			m.alertToast = ""
		}
		return m, listenAlerts(m.alertSub)

	case tabs.TerminalFocused:
		// Terminal reports focus change — no action needed, the terminal
		// tab's IsFocused() method is used during key routing.
		return m, nil

	case tea.KeyMsg:
		// When the terminal tab is focused, forward ALL keys to it
		// (including q, numbers, ctrl+c) — except that ctrl+c when NOT
		// focused in the terminal still quits.
		if m.activeTab == TabTerminal {
			if termTab, ok := m.tabs[TabTerminal].(*tabs.Terminal); ok && termTab.IsFocused() {
				var cmd tea.Cmd
				m.tabs[TabTerminal], cmd = m.tabs[TabTerminal].Update(msg)
				return m, cmd
			}
		}

		if m.helpOpen {
			if msg.String() == "?" || msg.String() == "q" || msg.String() == "esc" {
				m.helpOpen = false
				m.helpScrollOffset = 0
				return m, nil
			}
			switch msg.String() {
			case "up", "k":
				if m.helpScrollOffset > 0 {
					m.helpScrollOffset--
				}
			case "down", "j":
				m.helpScrollOffset++
			case "pgup":
				m.helpScrollOffset -= 10
				if m.helpScrollOffset < 0 {
					m.helpScrollOffset = 0
				}
			case "pgdown":
				m.helpScrollOffset += 10
			case "home":
				m.helpScrollOffset = 0
			case "1", "2", "3", "4", "5", "6", "7", "8", "9":
				idx := int(msg.String()[0] - '1')
				if idx < TabCount {
					m.activeTab = idx
					m.helpOpen = false
					m.helpScrollOffset = 0
				}
			case "0":
				m.activeTab = TabAdvanced
				m.helpOpen = false
				m.helpScrollOffset = 0
			}
			return m, nil
		}

		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			// Don't quit if terminal tab is active (even if not focused,
			// user might be in navigation mode and 'q' should not quit).
			if m.activeTab == TabTerminal {
				var cmd tea.Cmd
				m.tabs[m.activeTab], cmd = m.tabs[m.activeTab].Update(msg)
				return m, cmd
			}
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
		case "6":
			m.activeTab = TabTerminal
			// Auto-start shell when switching to terminal tab.
			if termTab, ok := m.tabs[TabTerminal].(*tabs.Terminal); ok {
				if !termTab.IsStarted() {
					var cmd tea.Cmd
					m.tabs[TabTerminal], cmd = m.tabs[TabTerminal].Update(msg)
					return m, cmd
				}
			}
		case "7":
			m.activeTab = TabFirewall
		case "8":
			m.activeTab = TabPackages
		case "9":
			m.activeTab = TabUsers
		case "0":
			m.activeTab = TabAdvanced
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

	// Alert toast notification.
	toast := ""
	if m.alertToast != "" && time.Now().Before(m.alertExpiry) {
		var toastParts []string
		if m.alertSnap != nil {
			for _, a := range m.alertSnap.Active {
				icon := "⚠"
				toastStyle := styles.TextYellow
				if a.Level == alerts.AlertCritical {
					icon = "🔴"
					toastStyle = styles.TextRed
				}
				toastParts = append(toastParts, toastStyle.Render(icon+" "+a.Message))
			}
		}
		if len(toastParts) > 2 {
			toastParts = toastParts[:2] // Max 2 alerts in status bar.
		}
		toast = "  │  " + strings.Join(toastParts, "  ")
	}

	right := styles.TextMuted.Render("q quit  │  ? help  │  1–9,0 tabs  ")

	width := m.width - lipgloss.Width(left) - lipgloss.Width(toast) - lipgloss.Width(right)
	if width < 0 {
		width = 0
	}
	spacer := styles.RepeatStr(" ", width)

	bar := lipgloss.NewStyle().
		Foreground(styles.ColorText).
		Background(styles.ColorNavy).
		Render(left + toast + spacer + right)
	return bar
}

func (m *Model) renderHelp() string {
	rawHelp := styles.TextBold.Render("Global Shortcuts\n") +
		"  q / Ctrl+C   Quit application\n" +
		"  1–9,0        Switch between tabs\n" +
		"  ?            Toggle this help screen\n\n" +
		styles.TextBold.Render("Dashboard Tab (1)\n") +
		"  Mouse Drag   Drag & drop panel headers to rearrange layout\n" +
		"  ↑/↓ / PgUp   Scroll dashboard view up and down\n" +
		"  Trend (2m)   Rolling 2-minute metric graphs (CPU, RAM, Net)\n\n" +
		styles.TextBold.Render("Process Tab (2)\n") +
		"  ↑/↓          Navigate process list\n" +
		"  t            Toggle Tree view (├── └──) vs Flat list\n" +
		"  s / F6       Change sort metric (CPU, Memory, I/O, PID)\n" +
		"  r            Reverse current sort order\n" +
		"  /            Filter processes by name or user\n" +
		"  ESC          Clear search filter\n" +
		"  Enter        View detailed process metadata & I/O rates\n" +
		(func() string {
			if privilege.CanKill() {
				return "  k / F9       Send signal to process (kill/terminate/sleep)\n"
			}
			return ""
		})() + "\n" +
		styles.TextBold.Render("Network Tab (3)\n") +
		"  Tab          Switch sub-panels (Interfaces, Sockets, Routes)\n" +
		"  ↑/↓          Navigate items\n" +
		"  /            Filter by interface/address/port\n\n" +
		styles.TextBold.Render("Docker Tab (4)\n") +
		"  ↑/↓          Navigate containers\n" +
		"  Enter        View container details & stats\n" +
		"  l            Tail container logs\n" +
		(func() string {
			if privilege.CanKill() {
				return "  s            Stop container (confirmation required)\n" +
					"  r            Restart container (confirmation required)\n"
			}
			return ""
		})() +
		"\n" +
		styles.TextBold.Render("Services Tab (5)\n") +
		"  Tab          Switch sub-panels (Services, Kernel Log dmesg, Unit Logs)\n" +
		"  /            Filter units by name\n" +
		"  a / f / r    Filter unit status (all / failed / running)\n" +
		"  l            View logs for highlighted unit\n" +
		"  Enter        View detailed unit metadata\n" +
		(func() string {
			if privilege.CanKill() {
				btn := "  s            Start unit (confirmation required)\n" +
					"  x            Stop unit (confirmation required)\n" +
					"  r            Restart unit (confirmation required)\n"
				if privilege.IsRoot() {
					btn += "  e            Enable unit on boot\n" +
						"  d            Disable unit on boot\n" +
						"  m            Mask / Unmask unit\n"
				}
				return btn
			}
			return ""
		})() +
		"\n" +
		styles.TextBold.Render("Terminal Tab (6)\n") +
		"  i / Enter    Focus terminal input mode (shell captures keys)\n" +
		"  Esc Esc      Exit focus mode (press Esc twice within 300ms)\n" +
		"  ↑/↓ / PgUp   Scroll output scrollback (when unfocused)\n\n" +
		styles.TextBold.Render("Firewall Tab (7)\n") +
		"  Tab          Switch sub-panels (Active Rules, Quick Actions)\n" +
		"  a            Launch Add Rule Wizard (port, action, protocol)\n" +
		"  d            Delete highlighted rule (confirmation required)\n" +
		"  e            Toggle Firewall enable/disable\n\n" +
		styles.TextBold.Render("Packages Tab (8)\n") +
		"  Tab          Switch sub-panels (Installed Packages, Search & Install, Pending Updates)\n" +
		"  /            Filter installed packages OR enter repo search query\n" +
		"  Enter        Install highlighted package (in Search sub-panel)\n" +
		"  r            Remove highlighted package (in Installed sub-panel)\n" +
		"  u            Run full system package upgrade (requires typed confirmation)\n\n" +
		styles.TextBold.Render("Users Tab (9)\n") +
		"  Tab          Switch sub-panels (User Accounts, System Groups, Sudoers Rules)\n" +
		"  h            Toggle hiding system users (UID < 1000)\n" +
		"  a            Add new user account (root required)\n" +
		"  d            Delete user account (root required + confirmation)\n\n" +
		styles.TextBold.Render("Advanced Tab (0)\n") +
		"  Tab          Switch sub-panels (Active SSH Sessions, Cron & Timers, Storage Topology)\n" +
		"  k            Terminate highlighted active SSH login session (root / CAP_KILL required)\n"

	lines := strings.Split(rawHelp, "\n")
	total := len(lines)

	viewH := m.height - 7
	if viewH < 5 {
		viewH = 5
	}

	maxOffset := total - viewH
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.helpScrollOffset > maxOffset {
		m.helpScrollOffset = maxOffset
	}
	if m.helpScrollOffset < 0 {
		m.helpScrollOffset = 0
	}

	end := m.helpScrollOffset + viewH
	if end > total {
		end = total
	}
	visibleLines := lines[m.helpScrollOffset:end]

	scrollIndicator := styles.TextMuted.Render("  ↑/↓ / PgUp/PgDn scroll guide  │  Esc/? close help")
	if maxOffset > 0 {
		scrollIndicator = styles.TextMuted.Render(fmt.Sprintf("  ↑/↓ / PgUp/PgDn scroll (+%d/%d)  │  Esc/? close help", m.helpScrollOffset, maxOffset))
	}

	content := strings.Join(visibleLines, "\n") + "\n\n" + scrollIndicator

	return styles.PanelStyle.Copy().Width(m.width - 2).Render(
		styles.PanelTitleStyle.Render("? Help — Keyboard Shortcuts & Usage Guide") + "\n\n" + content,
	)
}
