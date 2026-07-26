package tabs

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	audit "blackeye/internal/services/audit"
	"blackeye/internal/bus"
	"blackeye/internal/config"
	"blackeye/internal/privilege"
	"blackeye/internal/resolver"
	"blackeye/internal/services/firewall"
	"blackeye/internal/ui/styles"
)

type firewallSubPanel int

const (
	firewallSubRules firewallSubPanel = iota
	firewallSubQuick
	firewallSubCount
)

type fwActionMsg struct {
	action string
	err    error
}

// Firewall is the Tab 7 model.
type Firewall struct {
	width, height int
	cfg           config.Config
	sub           <-chan interface{}
	fwSvc         *firewall.Service
	auditSvc      *audit.Service

	snap       *firewall.Snapshot
	panel      firewallSubPanel
	cursor     int
	statusMsg  string
	filter     string
	filterMode bool

	// Add Rule wizard state
	addMode    bool
	addPort    string
	addAction  string // "ACCEPT" | "DROP"
	addProto   string // "tcp" | "udp"
	addStep    int    // 0=port, 1=action, 2=proto

	// Delete rule confirmation state
	delMode int // 0=none, 1=confirm delete

	// Firewall backends (b key)
	backends         []firewall.FirewallBackend
	activeBackendIdx int
}

func NewFirewall(b *bus.Bus, cfg config.Config) *Firewall {
	backends := firewall.AvailableFirewallBackends()
	return &Firewall{
		cfg:       cfg,
		sub:       b.Subscribe("firewall"),
		backends:  backends,
		addAction: "ACCEPT",
		addProto:  "tcp",
	}
}

func (f *Firewall) SetFirewall(svc *firewall.Service) { f.fwSvc = svc }
func (f *Firewall) SetAudit(a *audit.Service)         { f.auditSvc = a }

func (f *Firewall) Init() tea.Cmd {
	return listenChan(f.sub, "firewall")
}

func (f *Firewall) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		f.width, f.height = m.Width, m.Height
	case busMsg:
		if m.ch == f.sub {
			if v, ok := m.data.(firewall.Snapshot); ok {
				f.snap = &v
			}
			return f, listenChan(f.sub, "firewall")
		}
	case fwActionMsg:
		if m.err != nil {
			f.statusMsg = styles.TextRed.Render(fmt.Sprintf("Firewall action failed: %v", m.err))
		} else {
			f.statusMsg = styles.TextGreen.Render(fmt.Sprintf("Firewall action completed: %s", m.action))
		}
	case tea.KeyMsg:
		return f.handleKey(m)
	}
	return f, nil
}

func (f *Firewall) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Add rule wizard
	if f.addMode {
		switch msg.String() {
		case "esc":
			f.addMode = false
			f.addPort = ""
		case "enter":
			if f.addStep == 0 {
				if strings.TrimSpace(f.addPort) != "" {
					f.addStep = 1
				}
			} else if f.addStep == 1 {
				f.addStep = 2
			} else {
				// Finish wizard
				f.addMode = false
				port := f.addPort
				action := f.addAction
				proto := f.addProto
				f.addPort = ""
				f.addStep = 0
				return f, f.doAddRule(port, action, proto)
			}
		case "tab", "right":
			if f.addStep == 1 {
				if f.addAction == "ACCEPT" {
					f.addAction = "DROP"
				} else {
					f.addAction = "ACCEPT"
				}
			} else if f.addStep == 2 {
				if f.addProto == "tcp" {
					f.addProto = "udp"
				} else {
					f.addProto = "tcp"
				}
			}
		case "backspace":
			if f.addStep == 0 && len(f.addPort) > 0 {
				f.addPort = f.addPort[:len(f.addPort)-1]
			}
		default:
			if f.addStep == 0 && len(msg.String()) == 1 && filterRe.MatchString(f.addPort+msg.String()) {
				f.addPort += msg.String()
			}
		}
		return f, nil
	}

	// Delete confirmation
	if f.delMode > 0 {
		switch msg.String() {
		case "y":
			f.delMode = 0
			rules := f.filteredRules()
			if f.cursor < len(rules) {
				ruleID := rules[f.cursor].ID
				return f, f.doDeleteRule(ruleID)
			}
		case "n", "esc":
			f.delMode = 0
		}
		return f, nil
	}

	switch msg.String() {
	case "tab":
		f.panel = (f.panel + 1) % firewallSubCount
		f.cursor = 0
	case "up", "k":
		if f.cursor > 0 {
			f.cursor--
		}
	case "down", "j":
		f.cursor++
	case "b":
		if len(f.backends) > 0 {
			f.activeBackendIdx = (f.activeBackendIdx + 1) % len(f.backends)
			f.cursor = 0
			if b := f.getActiveBackend(); b != nil {
				rules, _ := b.ListRules()
				enabled, _ := b.IsEnabled()
				f.snap = &firewall.Snapshot{
					BackendName: b.Name(),
					IsEnabled:   enabled,
					Rules:       rules,
					Available:   true,
				}
			}
		}
	case "a":
		if privilege.CanFirewall() {
			f.addMode = true
			f.addStep = 0
			f.addPort = ""
		}
	case "d":
		if privilege.CanFirewall() {
			rules := f.filteredRules()
			if f.cursor < len(rules) {
				f.delMode = 1
			}
		}
	case "e":
		if privilege.CanFirewall() {
			return f, f.toggleFirewall()
		}
	}
	return f, nil
}

func (f *Firewall) getActiveBackend() firewall.FirewallBackend {
	if len(f.backends) > 0 && f.activeBackendIdx < len(f.backends) {
		return f.backends[f.activeBackendIdx]
	}
	if f.fwSvc != nil {
		return f.fwSvc.Backend()
	}
	return nil
}

func (f *Firewall) doAddRule(port, action, proto string) tea.Cmd {
	return func() tea.Msg {
		backend := f.getActiveBackend()
		if backend == nil {
			return fwActionMsg{action: "add rule", err: fmt.Errorf("firewall backend unavailable")}
		}
		r := firewall.Rule{
			Chain:    "INPUT",
			Action:   action,
			Protocol: proto,
			Port:     port,
		}
		err := backend.AddRule(r)
		result := "success"
		if err != nil {
			result = "error: " + err.Error()
		}
		if f.auditSvc != nil {
			f.auditSvc.WriteEvent(audit.Event{
				UID: os.Geteuid(), User: resolver.ByUID(os.Geteuid()),
				Action: "firewall_add_rule", Target: fmt.Sprintf("%s/%s %s", port, proto, action), Result: result,
			})
		}
		return fwActionMsg{action: fmt.Sprintf("Add Rule %s/%s %s", port, proto, action), err: err}
	}
}

func (f *Firewall) doDeleteRule(id string) tea.Cmd {
	return func() tea.Msg {
		backend := f.getActiveBackend()
		if backend == nil {
			return fwActionMsg{action: "delete rule", err: fmt.Errorf("firewall backend unavailable")}
		}
		err := backend.DeleteRule(id)
		result := "success"
		if err != nil {
			result = "error: " + err.Error()
		}
		if f.auditSvc != nil {
			f.auditSvc.WriteEvent(audit.Event{
				UID: os.Geteuid(), User: resolver.ByUID(os.Geteuid()),
				Action: "firewall_delete_rule", Target: "Rule ID " + id, Result: result,
			})
		}
		return fwActionMsg{action: "Delete Rule ID " + id, err: err}
	}
}

func (f *Firewall) toggleFirewall() tea.Cmd {
	return func() tea.Msg {
		backend := f.getActiveBackend()
		if backend == nil {
			return fwActionMsg{action: "toggle firewall", err: fmt.Errorf("firewall backend unavailable")}
		}
		enabled, _ := backend.IsEnabled()
		var err error
		action := "enable firewall"
		if enabled {
			action = "disable firewall"
			err = backend.Disable()
		} else {
			err = backend.Enable()
		}
		result := "success"
		if err != nil {
			result = "error: " + err.Error()
		}
		if f.auditSvc != nil {
			f.auditSvc.WriteEvent(audit.Event{
				UID: os.Geteuid(), User: resolver.ByUID(os.Geteuid()),
				Action: "firewall_toggle", Target: action, Result: result,
			})
		}
		return fwActionMsg{action: action, err: err}
	}
}

func (f *Firewall) filteredRules() []firewall.Rule {
	if f.snap == nil {
		return nil
	}
	return f.snap.Rules
}

func (f *Firewall) View() string {
	var panelTabs []string
	names := [firewallSubCount]string{"Active Rules", "Quick Actions"}
	for i, name := range names {
		if firewallSubPanel(i) == f.panel {
			panelTabs = append(panelTabs, styles.TabActive.Render(name))
		} else {
			panelTabs = append(panelTabs, styles.TabInactive.Render(name))
		}
	}
	tabBar := lipgloss.JoinHorizontal(lipgloss.Top, panelTabs...)

	var content string
	switch f.panel {
	case firewallSubRules:
		content = f.viewRules()
	case firewallSubQuick:
		content = f.viewQuick()
	}

	return styles.PanelStyle.Render(lipgloss.JoinVertical(lipgloss.Left, tabBar, content))
}

func (f *Firewall) viewRules() string {
	backendName := "Unknown"
	statusStr := styles.TextRed.Render("Disabled")
	if f.snap != nil {
		backendName = strings.Title(f.snap.BackendName)
		if f.snap.IsEnabled {
			statusStr = styles.TextGreen.Render("Active")
		}
	}

	engineBar := "  Active Engine [b]: "
	for i, b := range f.backends {
		if i == f.activeBackendIdx {
			engineBar += styles.TextAccent.Render("[" + b.Name() + "] ")
		} else {
			engineBar += styles.TextMuted.Render(b.Name() + "  ")
		}
	}

	title := styles.PanelTitleStyle.Render(fmt.Sprintf("  Firewall: %s  │  Status: %s", backendName, statusStr))

	if f.snap == nil {
		return title + "\n" + engineBar + "\n  Loading firewall rules…"
	}

	rules := f.filteredRules()

	actionLine := "  Keys: [b] switch engine  [a]dd rule  [d]elete rule  [e]nable/disable toggle"
	if !privilege.CanFirewall() {
		actionLine = styles.TextMuted.Render("  (Read-only mode — CAP_NET_ADMIN or root required to edit rules)")
	}

	header := styles.TableHeader.Render(fmt.Sprintf("%-6s  %-12s  %-10s  %-10s  %-12s",
		"ID", "Chain", "Action", "Protocol", "Port",
	))

	var rows []string
	for i, r := range rules {
		style := styles.TableRow
		if i == f.cursor {
			style = styles.TableRowSelected
		}

		actStyle := styles.TextGreen
		if r.Action == "DROP" || r.Action == "REJECT" {
			actStyle = styles.TextRed
		}

		rows = append(rows, style.Render(fmt.Sprintf("%-6s  %-12s  %-10s  %-10s  %-12s",
			r.ID,
			r.Chain,
			actStyle.Render(r.Action),
			r.Protocol,
			r.Port,
		)))
	}
	if len(rows) == 0 {
		rows = append(rows, styles.TextMuted.Render("  No active firewall rules defined"))
	}

	// Add Rule Wizard dialog
	wizard := ""
	if f.addMode {
		stepTitle := "Step 1: Enter Port (e.g. 80, 443, 22): " + f.addPort + "█"
		if f.addStep == 1 {
			stepTitle = fmt.Sprintf("Step 2: Select Action [Tab to toggle]: %s (Enter to next)", styles.TextAccent.Render(f.addAction))
		} else if f.addStep == 2 {
			stepTitle = fmt.Sprintf("Step 3: Select Protocol [Tab to toggle]: %s (Enter to apply)", styles.TextAccent.Render(f.addProto))
		}
		wizard = "\n\n  " + styles.TextYellow.Render("Add Rule Wizard — ") + stepTitle
	}

	// Delete confirm
	delDialog := ""
	if f.delMode > 0 && f.cursor < len(rules) {
		delDialog = "\n\n  " + styles.TextRed.Render(fmt.Sprintf("Delete Rule ID %s (%s %s)? [y/N]", rules[f.cursor].ID, rules[f.cursor].Port, rules[f.cursor].Action))
	}

	statusLine := ""
	if f.statusMsg != "" {
		statusLine = "\n  " + f.statusMsg
	}

	return strings.Join([]string{title, engineBar, actionLine, header, strings.Join(rows, "\n"), wizard, delDialog, statusLine}, "\n")
}

func (f *Firewall) viewQuick() string {
	title := styles.PanelTitleStyle.Render("  Firewall Quick Actions")
	lines := []string{
		title,
		"",
		"  Quick Rules:",
		"    • Press 'a' in Active Rules tab to launch Add Rule Wizard",
		"    • Open web port: Add port 80/tcp or 443/tcp ACCEPT",
		"    • Open SSH port: Add port 22/tcp ACCEPT",
		"    • Block bad port: Add port X DROP",
		"",
		"  Backend detected: " + func() string {
			if f.snap != nil {
				return f.snap.BackendName
			}
			return "none"
		}(),
	}
	return strings.Join(lines, "\n")
}
