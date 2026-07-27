// Package ui implements the root bubbletea model for BlackEye.
// It routes keyboard events to the active tab and renders the tab bar.
package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"blackeye/internal/bus"
	"blackeye/internal/config"
	"blackeye/internal/privilege"
	"blackeye/internal/services/alerts"
	"blackeye/internal/services/diagnostics"
	"blackeye/internal/services/exporter"
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

var wizardMetrics = []struct {
	Key  string
	Name string
	Unit string
	Min  float64
	Max  float64
}{
	{Key: "cpu", Name: "CPU Utilization", Unit: "%", Min: 1, Max: 100},
	{Key: "ram", Name: "RAM Utilization", Unit: "%", Min: 1, Max: 100},
	{Key: "swap", Name: "Swap Utilization", Unit: "%", Min: 1, Max: 100},
	{Key: "disk", Name: "Root Disk Usage", Unit: "%", Min: 1, Max: 100},
	{Key: "temp", Name: "Core Temperature", Unit: "°C", Min: 20, Max: 120},
	{Key: "auth_failures", Name: "Auth Failure Count", Unit: "attempts", Min: 1, Max: 1000},
}

var wizardOps = []string{"> (Greater than)", ">= (Greater or equal)", "< (Less than)", "== (Equals)"}

// Model is the root bubbletea model.
type Model struct {
	width     int
	height    int
	activeTab int
	tabs      [TabCount]tea.Model
	// Help modal state.
	helpOpen         bool
	helpViewMode     int // 0 = Active Tab Context Help, 1 = Global Cheat Sheet
	helpScrollOffset int
	cfg              config.Config

	// Alert notification state.
	alertSub    <-chan interface{}
	alertSnap   *alerts.Snapshot
	alertToast  string
	alertExpiry time.Time

	// v1.2.4 Modal & Sampling State
	diagOpen       bool
	alertModalOpen bool
	samplingMode   int // 0=Turbo (1s), 1=Balanced (2s), 2=Eco (5s)

	// Fault-Preventative Custom Alert Rule Wizard State
	ruleWizardOpen   bool
	ruleWizardStep   int // 0 = Select Metric, 1 = Select Operator, 2 = Enter Value, 3 = Enter Label
	ruleMetricIdx    int
	ruleOpIdx        int
	ruleValInput     string
	ruleLabelInput   string
	ruleWizardErrMsg string
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

// SetActiveTab sets the initial active tab by index.
func (m *Model) SetActiveTab(idx int) {
	if idx >= 0 && idx < TabCount {
		m.activeTab = idx
	}
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

	case tabs.JumpToTabMsg:
		if msg.TabIndex >= 0 && msg.TabIndex < TabCount {
			m.activeTab = msg.TabIndex
			if receiver, ok := m.tabs[msg.TabIndex].(tabs.PayloadReceiver); ok {
				receiver.ReceivePayload(msg.Payload)
			}
		}
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
			if msg.String() == "g" {
				m.helpViewMode = (m.helpViewMode + 1) % 2
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

		// When active tab is capturing text/numeric input (e.g. typing PIDs, ports, usernames, filters),
		// forward ALL keypresses directly to it instead of switching tabs or quitting.
		if capturer, ok := m.tabs[m.activeTab].(tabs.InputCapturer); ok && capturer.IsInputActive() {
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			var cmd tea.Cmd
			m.tabs[m.activeTab], cmd = m.tabs[m.activeTab].Update(msg)
			return m, cmd
		}

		if m.ruleWizardOpen {
			switch msg.String() {
			case "esc":
				m.ruleWizardOpen = false
				return m, nil
			case "up", "k":
				if m.ruleWizardStep == 0 && m.ruleMetricIdx > 0 {
					m.ruleMetricIdx--
				} else if m.ruleWizardStep == 1 && m.ruleOpIdx > 0 {
					m.ruleOpIdx--
				}
			case "down", "j":
				if m.ruleWizardStep == 0 && m.ruleMetricIdx < len(wizardMetrics)-1 {
					m.ruleMetricIdx++
				} else if m.ruleWizardStep == 1 && m.ruleOpIdx < len(wizardOps)-1 {
					m.ruleOpIdx++
				}
			case "backspace":
				if m.ruleWizardStep == 2 && len(m.ruleValInput) > 0 {
					m.ruleValInput = m.ruleValInput[:len(m.ruleValInput)-1]
					m.ruleWizardErrMsg = ""
				} else if m.ruleWizardStep == 3 && len(m.ruleLabelInput) > 0 {
					m.ruleLabelInput = m.ruleLabelInput[:len(m.ruleLabelInput)-1]
				}
			case "enter":
				if m.ruleWizardStep == 0 {
					m.ruleWizardStep = 1
				} else if m.ruleWizardStep == 1 {
					m.ruleWizardStep = 2
				} else if m.ruleWizardStep == 2 {
					val, err := strconv.ParseFloat(m.ruleValInput, 64)
					met := wizardMetrics[m.ruleMetricIdx]
					if err != nil || val < met.Min || val > met.Max {
						m.ruleWizardErrMsg = fmt.Sprintf("⚠️ Fault Preventer: Value must be a valid number between %.0f and %.0f %s", met.Min, met.Max, met.Unit)
						return m, nil
					}
					m.ruleWizardErrMsg = ""
					m.ruleWizardStep = 3
				} else if m.ruleWizardStep == 3 {
					met := wizardMetrics[m.ruleMetricIdx]
					val, _ := strconv.ParseFloat(m.ruleValInput, 64)
					opStr := []string{">", ">=", "<", "=="}[m.ruleOpIdx]
					label := strings.TrimSpace(m.ruleLabelInput)
					if label == "" {
						label = fmt.Sprintf("%s %s %.0f%s", met.Name, opStr, val, met.Unit)
					}

					newRule := config.CustomRule{
						ID:       fmt.Sprintf("rule_%d", time.Now().Unix()),
						Metric:   met.Key,
						Operator: opStr,
						Value:    val,
						Severity: "warning",
						Label:    label,
					}
					if err := newRule.Validate(); err != nil {
						m.ruleWizardErrMsg = "⚠️ Rule validation error: " + err.Error()
						return m, nil
					}
					m.cfg.Alerts.CustomRules = append(m.cfg.Alerts.CustomRules, newRule)
					m.ruleWizardOpen = false
					m.alertToast = "Custom alert rule created: " + label
					m.alertExpiry = time.Now().Add(4 * time.Second)
					return m, nil
				}
			default:
				if len(msg.String()) == 1 {
					ch := msg.String()[0]
					if m.ruleWizardStep == 2 && (ch >= '0' && ch <= '9' || ch == '.') {
						m.ruleValInput += msg.String()
						m.ruleWizardErrMsg = ""
					} else if m.ruleWizardStep == 3 && (ch >= ' ' && ch <= '~') {
						m.ruleLabelInput += msg.String()
					}
				}
			}
			return m, nil
		}

		if m.diagOpen || m.alertModalOpen {
			if msg.String() == "esc" || msg.String() == "q" {
				m.diagOpen = false
				m.alertModalOpen = false
				return m, nil
			}
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
		case "h", "H":
			if m.activeTab != TabUsers {
				m.diagOpen = !m.diagOpen
				return m, nil
			}
		case "t", "T":
			if m.activeTab != TabProcess {
				themeName := styles.CycleTheme()
				m.alertToast = "Theme switched: " + themeName
				m.alertExpiry = time.Now().Add(3 * time.Second)
				return m, nil
			}
		case "a", "A":
			if m.activeTab != TabDocker && m.activeTab != TabFirewall && m.activeTab != TabUsers {
				m.alertModalOpen = !m.alertModalOpen
				return m, nil
			}
		case "r", "R":
			if m.activeTab != TabProcess && m.activeTab != TabDocker && m.activeTab != TabServices && m.activeTab != TabPackages {
				m.samplingMode = (m.samplingMode + 1) % 3
				modes := []string{"Turbo (1s)", "Balanced (2s)", "Eco (5s)"}
				m.alertToast = "Sampling mode: " + modes[m.samplingMode]
				m.alertExpiry = time.Now().Add(3 * time.Second)
				return m, nil
			}
		case "c", "C":
			if m.alertModalOpen {
				m.ruleWizardOpen = true
				m.ruleWizardStep = 0
				m.ruleMetricIdx = 0
				m.ruleOpIdx = 0
				m.ruleValInput = ""
				m.ruleLabelInput = ""
				m.ruleWizardErrMsg = ""
				return m, nil
			}
		case "e", "E":
			if m.activeTab != TabFirewall && m.activeTab != TabServices {
				data := map[string]interface{}{
					"timestamp": time.Now(),
					"activeTab": tabNames[m.activeTab],
					"alerts":    m.alertSnap,
				}
				if path, err := exporter.ExportJSONSnapshot(data, ""); err == nil {
					m.alertToast = "JSON Snapshot saved to " + path
					m.alertExpiry = time.Now().Add(4 * time.Second)
				}
				return m, nil
			}
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

	tabBar := m.renderTabBar()
	content := m.tabs[m.activeTab].View()
	statusBar := m.renderStatusBar()

	tabH := lipgloss.Height(tabBar)
	statusH := lipgloss.Height(statusBar)
	availH := m.height - tabH - statusH

	if m.helpOpen {
		helpDrawer := m.renderHelpDrawer()
		helpH := lipgloss.Height(helpDrawer)
		mainAvailH := availH - helpH

		if mainAvailH > 0 {
			lines := strings.Split(content, "\n")
			if len(lines) > mainAvailH {
				lines = lines[:mainAvailH]
				content = strings.Join(lines, "\n")
			}
		}
		content = lipgloss.JoinVertical(lipgloss.Left, content, helpDrawer)
	} else if m.ruleWizardOpen {
		content = m.renderRuleWizardModal()
	} else if m.diagOpen {
		content = m.renderDiagnosticsModal()
	} else if m.alertModalOpen {
		content = m.renderAlertModal()
	} else {
		if availH > 0 {
			lines := strings.Split(content, "\n")
			if len(lines) > availH {
				lines = lines[:availH]
				content = strings.Join(lines, "\n")
			}
		}
	}

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

	samplingStr := []string{"🚀 1s", "⚖ 2s", "🍃 5s"}[m.samplingMode]
	right := styles.TextMuted.Render(fmt.Sprintf("%s  │  q quit  │  ? help  │  1–9,0 tabs  ", samplingStr))

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
func (m *Model) renderHelpDrawer() string {
	tabTitle := tabNames[m.activeTab]
	tabBtn := styles.TabInactive.Render("Context Help (" + tabTitle + ") [g]")
	globalBtn := styles.TabInactive.Render("Global Cheat Sheet [g]")

	if m.helpViewMode == 0 {
		tabBtn = styles.TabActive.Render("Context Help (" + tabTitle + ") [g]")
	} else {
		globalBtn = styles.TabActive.Render("Global Cheat Sheet [g]")
	}

	modeBar := lipgloss.JoinHorizontal(lipgloss.Top, "  ", tabBtn, "   ", globalBtn)

	var helpContent string
	if m.helpViewMode == 0 {
		helpContent = m.getTabHelpText(m.activeTab)
	} else {
		helpContent = m.getGlobalHelpText()
	}

	lines := strings.Split(helpContent, "\n")
	total := len(lines)
	viewH := m.height - 14
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

	scrollInfo := ""
	if maxOffset > 0 {
		scrollInfo = fmt.Sprintf("  │  Line %d/%d (↓/↑ or j/k scroll)", m.helpScrollOffset+1, total)
	}
	footer := styles.TextMuted.Render("  [g] toggle mode  │  Esc/? close help" + scrollInfo)

	boxWidth := m.width - 4
	if boxWidth < 40 {
		boxWidth = 40
	}

	card := lipgloss.JoinVertical(lipgloss.Left,
		modeBar,
		"",
		strings.Join(visibleLines, "\n"),
		"",
		footer,
	)

	return styles.PanelStyle.Copy().
		Width(boxWidth).
		BorderForeground(styles.ColorGold).
		Render(card)
}

func (m *Model) getTabHelpText(idx int) string {
	switch idx {
	case TabDashboard:
		return styles.PanelTitleStyle.Render("  [1] Dashboard — Contextual Help & Controls") + "\n\n" +
			styles.TextBold.Render("Key Indicators & Telemetry:\n") +
			"  • Security Risk Score: Dynamic 0–100% risk score matrix combining firewall, root SSH & brute-force IPs\n" +
			"  • Rolling Sparklines: 2-minute rolling trend graphs for CPU%, RAM%, and network throughput\n" +
			"  • Drag & Drop Panels: Click and drag panel headers to dynamically reorder dashboard layout\n\n" +
			styles.TextBold.Render("Navigation:\n") +
			"  • ↑/↓ / PgUp/PgDn: Scroll dashboard view when content exceeds window height"

	case TabProcess:
		return styles.PanelTitleStyle.Render("  [2] Processes — Contextual Help & Controls") + "\n\n" +
			styles.TextBold.Render("Process Management Keys:\n") +
			"  • ↑/↓ / j/k: Navigate process list\n" +
			"  • t: Toggle Tree view (├── └── process hierarchy) vs Flat list\n" +
			"  • s / F6: Open Sort Menu (CPU%, Memory%, I/O Rate, PID)\n" +
			"  • r: Reverse current sort order\n" +
			"  • /: Filter processes by name or user (ESC to clear)\n" +
			"  • Enter: View detailed process metadata, open FDs, and live I/O rates\n" +
			"  • k / F9: Open Process Signal Menu (SIGTERM 15, SIGKILL 9, SIGSTOP 19)"

	case TabNetwork:
		return styles.PanelTitleStyle.Render("  [3] Network & Sockets — Contextual Help & Controls") + "\n\n" +
			styles.TextBold.Render("Sub-Panels & Security Badges:\n") +
			"  • Tab: Cycle sub-panels (Interfaces, Listening Services, Active Connections, Routing Table)\n" +
			"  • ⚠️ UNENCRYPTED: Red warning badge on unencrypted listener ports (HTTP 80, FTP 21, Telnet 23, POP3 110)\n" +
			"  • 🌐 WAN vs 🔒 LAN: Automatic remote IP scope classification\n\n" +
			styles.TextBold.Render("Incident Response Shortcuts:\n") +
			"  • f: (Listeners) Jump directly to Tab 7 (Firewall) with pre-filled DROP rule wizard for highlighted port\n" +
			"  • k: (Connections) Send SIGTERM to process PID owning highlighted socket"

	case TabDocker:
		return styles.PanelTitleStyle.Render("  [4] Docker Containers — Contextual Help & Controls") + "\n\n" +
			styles.TextBold.Render("Container Lifecycle Keys:\n") +
			"  • ↑/↓: Navigate containers\n" +
			"  • Enter: View container metadata, volume mounts, and redacted env vars\n" +
			"  • l: Tail live container stdout/stderr logs\n" +
			"  • a: Start stopped container (requires confirmation)\n" +
			"  • s: Stop container (requires confirmation)\n" +
			"  • r: Restart container\n" +
			"  • p: Pause / Unpause container\n" +
			"  • d: Remove container (force removal)"

	case TabServices:
		return styles.PanelTitleStyle.Render("  [5] Services & Kernel Logs — Contextual Help & Controls") + "\n\n" +
			styles.TextBold.Render("Unit Controls & Logs:\n") +
			"  • Tab: Cycle sub-panels (Systemd Services, Kernel dmesg logs, Unit Logs)\n" +
			"  • a / f / r: Filter unit status (all / failed / running)\n" +
			"  • l: View journalctl logs for highlighted unit\n" +
			"  • s: Start unit  │  x: Stop unit  │  r: Restart unit\n" +
			"  • e: Enable on boot  │  d: Disable on boot  │  m: Mask unit"

	case TabTerminal:
		return styles.PanelTitleStyle.Render("  [6] Embedded Terminal — Contextual Help & Controls") + "\n\n" +
			styles.TextBold.Render("Pseudoterminal (PTY) Modes:\n") +
			"  • i / Enter: Focus terminal input mode (shell captures all keypresses)\n" +
			"  • Esc Esc: Double-press Esc within 300ms to exit input mode\n" +
			"  • PTY Job Control: Full support for sudo, htop, vim, starship, and job signals"

	case TabFirewall:
		return styles.PanelTitleStyle.Render("  [7] Firewall Manager — Contextual Help & Controls") + "\n\n" +
			styles.TextBold.Render("Firewall Engine & Rule Keys:\n") +
			"  • b: Engine Switcher — cycle live between ufw, firewalld, iptables, and nftables\n" +
			"  • a: Launch Interactive Add Rule Wizard (port, action, protocol)\n" +
			"  • d: Delete highlighted firewall rule (confirmation required)\n" +
			"  • e: Toggle Firewall enable/disable state"

	case TabPackages:
		return styles.PanelTitleStyle.Render("  [8] Package Manager — Contextual Help & Controls") + "\n\n" +
			styles.TextBold.Render("Package Manager Keys:\n") +
			"  • b: Helper Switcher — cycle between pacman, yay, flatpak, snap, apt, dnf, etc.\n" +
			"  • c: Category Filter — System Core (🛡️), User App (👤), Library (📦), Dev (🛠️)\n" +
			"  • /: Search packages in repositories (Enter to execute search)\n" +
			"  • Enter: Install highlighted package\n" +
			"  • r: Remove highlighted package (prompts red System Core removal warning banner)\n" +
			"  • u: System Upgrade — run full system upgrade (requires typing 'UPDATE ALL')"

	case TabUsers:
		return styles.PanelTitleStyle.Render("  [9] Users & Security — Contextual Help & Controls") + "\n\n" +
			styles.TextBold.Render("Security Sub-Panels & Keys:\n") +
			"  • Tab: Cycle sub-panels (Users, Groups, Sudoers, SUID/SGID Audit, SSH Policy)\n" +
			"  • h: Toggle hiding system accounts (UID < 1000)\n" +
			"  • a: (Users) Add user account  │  (Sudoers) Launch 3-step Sudoers Rule Wizard\n" +
			"  • d: (Users) Delete user account  │  (Sudoers) Delete custom Sudoers drop-in rule\n" +
			"  • visudo check: Automatic 'visudo -c' syntax validation before applying sudoers rules"

	case TabAdvanced:
		return styles.PanelTitleStyle.Render("  [0] Advanced Platform — Contextual Help & Controls") + "\n\n" +
			styles.TextBold.Render("SSH Sessions & Incident Response:\n") +
			"  • Tab: Cycle sub-panels (Active SSH Sessions, Cron & Timers, Storage Topology)\n" +
			"  • k: Terminate active SSH login session PID\n" +
			"  • b: Instant Firewall IP Ban — jump to Tab 7 (Firewall) with pre-filled DROP rule for session remote IP"
	}
	return m.getGlobalHelpText()
}

func (m *Model) getGlobalHelpText() string {
	rawHelp := styles.TextBold.Render("Global Shortcuts\n") +
		"  q / Ctrl+C   Quit application\n" +
		"  1–9,0        Switch between tabs\n" +
		"  ?            Toggle this help screen\n" +
		"  H            Run System Health Diagnostics Report (PASS/WARN/FAIL)\n" +
		"  T            Cycle live Color Theme (Classic, Cyberpunk, Dracula, Matrix, Slate)\n" +
		"  A            View System Warning & Alert History Log (Max 50)\n" +
		"  R            Toggle sampling mode (1s Turbo, 2s Balanced, 5s Eco)\n" +
		"  E            Dump complete JSON system snapshot to file\n\n" +
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
		"  /            Filter by interface/address/port\n" +
		"  f            Jump to Firewall tab to create rule for highlighted port\n" +
		"  k            Send SIGTERM to process PID owning highlighted socket\n\n" +
		styles.TextBold.Render("Docker Tab (4)\n") +
		"  ↑/↓          Navigate containers\n" +
		"  Enter        View container details & stats\n" +
		"  l            Tail container logs\n" +
		(func() string {
			if privilege.HasDockerAccess() {
				return "  a            Start stopped container (confirmation required)\n" +
					"  s            Stop container (confirmation required)\n" +
					"  r            Restart container (confirmation required)\n" +
					"  p            Pause / Unpause container\n" +
					"  d            Remove container (force, confirmation required)\n"
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
		"  b            Switch active firewall engine (ufw / firewalld / iptables / nftables)\n" +
		"  a            Launch Add Rule Wizard (port, action, protocol)\n" +
		"  d            Delete highlighted rule (confirmation required)\n" +
		"  e            Toggle Firewall enable/disable\n\n" +
		styles.TextBold.Render("Packages Tab (8)\n") +
		"  Tab          Switch sub-panels (Installed Packages, Search & Install, Pending Updates)\n" +
		"  /            Filter installed packages OR enter repo search query\n" +
		"  c            Cycle category filter (System Core, User Apps, Libraries, Dev)\n" +
		"  b            Switch package manager / helper (pacman, yay, flatpak, snap, apt, etc.)\n" +
		"  Enter        Install highlighted package (in Search sub-panel)\n" +
		"  r            Remove highlighted package (in Installed sub-panel)\n" +
		"  u            Run full system package upgrade (requires typed confirmation)\n" +
		"  ESC          Dismiss execution log output modal\n\n" +
		styles.TextBold.Render("Users & Security Tab (9)\n") +
		"  Tab          Switch sub-panels (Users, Groups, Sudoers, SUID/SGID Audit, SSH Policy)\n" +
		"  h            Toggle hiding system users (UID < 1000)\n" +
		"  a            Add user account OR launch Add Sudoers Rule Wizard (root required)\n" +
		"  d            Delete user account OR delete custom Sudoers drop-in rule (root required)\n\n" +
		styles.TextBold.Render("Advanced Tab (0)\n") +
		"  Tab          Switch sub-panels (Active SSH Sessions, Cron & Timers, Storage Topology)\n" +
		"  k            Terminate highlighted active SSH login session (root / CAP_KILL required)\n" +
		"  b            Instant Firewall IP Ban for highlighted SSH session remote IP\n"

	return rawHelp
}

func (m *Model) renderDiagnosticsModal() string {
	report := diagnostics.RunDiagnostics(nil, nil)

	header := styles.PanelTitleStyle.Render("  🩺 System Health Diagnostics Report (PASS: " +
		fmt.Sprintf("%d", report.PassCount) + " │ WARN: " +
		fmt.Sprintf("%d", report.WarnCount) + " │ FAIL: " +
		fmt.Sprintf("%d", report.FailCount) + ")")

	var rows []string
	rows = append(rows, styles.TableHeader.Render(fmt.Sprintf("  %-18s  %-32s  %-8s  %-30s", "Category", "Audit Item", "Status", "Details & Remediation")))

	for _, item := range report.Items {
		statusBadge := styles.TextGreen.Render("[PASS]")
		if item.Status == diagnostics.StatusWarn {
			statusBadge = styles.TextYellow.Render("[WARN]")
		} else if item.Status == diagnostics.StatusFail {
			statusBadge = styles.TextRed.Render("[FAIL]")
		}
		rows = append(rows, fmt.Sprintf("  %-18s  %-32s  %-8s  %-30s",
			styles.Truncate(item.Category, 18),
			styles.Truncate(item.Name, 32),
			statusBadge,
			styles.Truncate(item.Detail+" — "+item.Tip, 30),
		))
	}

	footer := styles.TextMuted.Render("  Press Esc or q to close diagnostics report")

	card := lipgloss.JoinVertical(lipgloss.Left,
		header,
		"",
		strings.Join(rows, "\n"),
		"",
		footer,
	)

	boxWidth := m.width - 4
	if boxWidth < 40 {
		boxWidth = 40
	}

	return styles.PanelStyle.Copy().
		Width(boxWidth).
		BorderForeground(styles.ColorGold).
		Render(card)
}

func (m *Model) renderAlertModal() string {
	header := styles.PanelTitleStyle.Render("  🚨 System Warning & Alert History Log (Max 50)")

	var rows []string
	rows = append(rows, styles.TableHeader.Render(fmt.Sprintf("  %-12s  %-10s  %-18s  %-40s", "Time", "Level", "Source", "Message")))

	if m.alertSnap != nil && len(m.alertSnap.History) > 0 {
		for _, a := range m.alertSnap.History {
			levelBadge := styles.TextYellow.Render("WARNING")
			if a.Level == alerts.AlertCritical {
				levelBadge = styles.TextRed.Render("CRITICAL")
			}
			timeStr := a.Timestamp.Format("15:04:05")
			rows = append(rows, fmt.Sprintf("  %-12s  %-10s  %-18s  %-40s",
				timeStr,
				levelBadge,
				styles.Truncate(a.Source, 18),
				styles.Truncate(a.Message, 40),
			))
		}
	} else {
		rows = append(rows, styles.TextGreen.Render("  No threshold warnings recorded"))
	}

	footer := styles.TextMuted.Render("  Press Esc or q to close alert log drawer  │  Press 'c' to launch Custom Alert Rule Wizard")

	card := lipgloss.JoinVertical(lipgloss.Left,
		header,
		"",
		strings.Join(rows, "\n"),
		"",
		footer,
	)

	boxWidth := m.width - 4
	if boxWidth < 40 {
		boxWidth = 40
	}

	return styles.PanelStyle.Copy().
		Width(boxWidth).
		BorderForeground(styles.ColorGold).
		Render(card)
}

func (m *Model) renderRuleWizardModal() string {
	header := styles.PanelTitleStyle.Render("  🛡️ Fault-Preventative Custom Alert Rule Wizard")

	stepTitles := []string{
		"Step 1/4: Select Metric to Monitor",
		"Step 2/4: Select Comparison Operator",
		"Step 3/4: Enter Threshold Numeric Value",
		"Step 4/4: Enter Custom Alert Label / Description",
	}

	var rows []string
	rows = append(rows, styles.TextBold.Render("  "+stepTitles[m.ruleWizardStep]))
	rows = append(rows, "")

	if m.ruleWizardStep == 0 {
		for i, met := range wizardMetrics {
			if i == m.ruleMetricIdx {
				rows = append(rows, styles.TableRowSelected.Render(fmt.Sprintf("  > %s (%s)", met.Name, met.Unit)))
			} else {
				rows = append(rows, styles.TextNormal.Render(fmt.Sprintf("    %s (%s)", met.Name, met.Unit)))
			}
		}
	} else if m.ruleWizardStep == 1 {
		for i, op := range wizardOps {
			if i == m.ruleOpIdx {
				rows = append(rows, styles.TableRowSelected.Render("  > "+op))
			} else {
				rows = append(rows, styles.TextNormal.Render("    "+op))
			}
		}
	} else if m.ruleWizardStep == 2 {
		met := wizardMetrics[m.ruleMetricIdx]
		rows = append(rows, fmt.Sprintf("  Target Metric: %s", styles.TextAccent.Render(met.Name)))
		rows = append(rows, fmt.Sprintf("  Valid Range:   %.0f to %.0f %s", met.Min, met.Max, met.Unit))
		rows = append(rows, "")
		rows = append(rows, fmt.Sprintf("  Enter Threshold Value: %s█", styles.TextAccent.Render(m.ruleValInput)))
	} else if m.ruleWizardStep == 3 {
		met := wizardMetrics[m.ruleMetricIdx]
		opStr := []string{">", ">=", "<", "=="}[m.ruleOpIdx]
		rows = append(rows, fmt.Sprintf("  Rule Condition: %s %s %s %s", met.Name, opStr, m.ruleValInput, met.Unit))
		rows = append(rows, "")
		rows = append(rows, fmt.Sprintf("  Enter Custom Label: %s█", styles.TextAccent.Render(m.ruleLabelInput)))
		rows = append(rows, styles.TextMuted.Render("  (Leave empty for auto-generated label)"))
	}

	if m.ruleWizardErrMsg != "" {
		rows = append(rows, "")
		rows = append(rows, styles.TextRed.Render("  "+m.ruleWizardErrMsg))
	}

	footer := styles.TextMuted.Render("  Use ↑/↓ to navigate  │  Enter to proceed  │  ESC to cancel")

	card := lipgloss.JoinVertical(lipgloss.Left,
		header,
		"",
		strings.Join(rows, "\n"),
		"",
		footer,
	)

	boxWidth := m.width - 4
	if boxWidth < 50 {
		boxWidth = 50
	}

	return styles.PanelStyle.Copy().
		Width(boxWidth).
		BorderForeground(styles.ColorGold).
		Render(card)
}
