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
	"blackeye/internal/services/dmesg"
	"blackeye/internal/services/initsys"
	"blackeye/internal/services/systemd"
	"blackeye/internal/ui/styles"
)

type servicesSubPanel int

const (
	servicesSubPanelUnits servicesSubPanel = iota
	servicesSubPanelDmesg
	servicesSubPanelLogs
	servicesSubCount
)

// serviceActionMsg reports the result of a unit action.
type serviceActionMsg struct {
	action string
	unit   string
	err    error
}

type unitLogsLoadedMsg struct {
	unit string
	logs []initsys.LogEntry
	err  error
}

// Services is the Tab 5 model.
type Services struct {
	width, height int
	cfg           config.Config

	subSystemd <-chan interface{}
	subDmesg   <-chan interface{}

	systemdSnap *systemd.Snapshot
	initsysSnap *initsys.Snapshot
	dmesgBatch  []dmesg.Entry

	panel      servicesSubPanel
	cursor     int
	filter     string
	filterMode bool

	// Filter state: "all" | "failed" | "running"
	unitFilter string

	// Unit action state.
	systemdSvc *systemd.Service
	initsysSvc *initsys.Service
	auditSvc   *audit.Service
	actionMode int // 0=none, 1=start, 2=stop, 3=restart, 4=enable, 5=disable, 6=mask
	actionUnit string
	statusMsg  string

	// Detail view state.
	detailMode bool

	// Unit Logs view state.
	selectedUnit string
	unitLogs     []initsys.LogEntry
	logsLoading  bool
	logsErr      string
}

func NewServices(b *bus.Bus, cfg config.Config) *Services {
	s := &Services{
		cfg:        cfg,
		subSystemd: b.Subscribe("systemd"),
		subDmesg:   b.Subscribe("dmesg"),
		unitFilter: "all",
	}
	return s
}

func (s *Services) SetSystemd(svc *systemd.Service) { s.systemdSvc = svc }
func (s *Services) SetInitSys(svc *initsys.Service) { s.initsysSvc = svc }
func (s *Services) SetAudit(a *audit.Service)     { s.auditSvc = a }

func (s *Services) Init() tea.Cmd {
	return tea.Batch(
		listenChan(s.subSystemd, "systemd"),
		listenChan(s.subDmesg, "dmesg"),
	)
}

func (s *Services) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = m.Width, m.Height
	case busMsg:
		var cmd tea.Cmd
		switch m.ch {
		case s.subSystemd:
			if v, ok := m.data.(initsys.Snapshot); ok {
				s.initsysSnap = &v
			} else if v, ok := m.data.(systemd.Snapshot); ok {
				s.systemdSnap = &v
			}
			cmd = listenChan(s.subSystemd, "systemd")
		case s.subDmesg:
			if v, ok := m.data.(dmesg.Snapshot); ok {
				s.dmesgBatch = append(s.dmesgBatch, v.Entries...)
				if len(s.dmesgBatch) > 500 {
					s.dmesgBatch = s.dmesgBatch[len(s.dmesgBatch)-500:]
				}
			}
			cmd = listenChan(s.subDmesg, "dmesg")
		}
		return s, cmd
	case serviceActionMsg:
		if m.err != nil {
			s.statusMsg = styles.TextRed.Render(fmt.Sprintf("Action failed: %s — %v", m.action, m.err))
		} else {
			s.statusMsg = styles.TextGreen.Render(fmt.Sprintf("Action completed: %s %s", m.action, m.unit))
		}
	case unitLogsLoadedMsg:
		s.logsLoading = false
		if m.err != nil {
			s.logsErr = m.err.Error()
		} else {
			s.unitLogs = m.logs
			s.logsErr = ""
		}
	case tea.KeyMsg:
		return s.handleKey(m)
	}
	return s, nil
}

func (s *Services) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Action confirmation dialog.
	if s.actionMode > 0 {
		switch msg.String() {
		case "y":
			mode := s.actionMode
			unit := s.actionUnit
			s.actionMode = 0
			return s, s.doUnitAction(mode, unit)
		case "n", "esc":
			s.actionMode = 0
		}
		return s, nil
	}

	// Detail mode.
	if s.detailMode {
		switch msg.String() {
		case "esc", "enter", "q":
			s.detailMode = false
		}
		return s, nil
	}

	if s.filterMode {
		switch msg.String() {
		case "esc", "enter":
			s.filterMode = false
		case "backspace":
			if len(s.filter) > 0 {
				s.filter = s.filter[:len(s.filter)-1]
			}
		default:
			if len(msg.String()) == 1 && filterRe.MatchString(s.filter+msg.String()) {
				s.filter += msg.String()
			}
		}
		return s, nil
	}

	switch msg.String() {
	case "tab":
		s.panel = (s.panel + 1) % servicesSubCount
		s.cursor = 0
		if s.panel == servicesSubPanelLogs && s.selectedUnit != "" {
			return s, s.fetchLogsCmd(s.selectedUnit)
		}
	case "up":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		s.cursor++
	case "/":
		s.filterMode = !s.filterMode
	case "esc":
		s.filter, s.filterMode = "", false
	case "a":
		s.unitFilter = "all"
	case "f":
		s.unitFilter = "failed"
	case "r":
		if s.panel == servicesSubPanelUnits && privilege.CanKill() {
			units := s.filteredUnits()
			if s.cursor < len(units) {
				s.actionMode = 3
				s.actionUnit = units[s.cursor].Name
			}
		} else {
			s.unitFilter = "running"
		}
	case "l":
		if s.panel == servicesSubPanelUnits {
			units := s.filteredUnits()
			if s.cursor < len(units) {
				s.selectedUnit = units[s.cursor].Name
				s.panel = servicesSubPanelLogs
				return s, s.fetchLogsCmd(s.selectedUnit)
			}
		}
	case "enter":
		if s.panel == servicesSubPanelUnits {
			s.detailMode = true
		}
	case "s":
		if s.panel == servicesSubPanelUnits && privilege.CanKill() {
			units := s.filteredUnits()
			if s.cursor < len(units) {
				s.actionMode = 1
				s.actionUnit = units[s.cursor].Name
			}
		}
	case "x":
		if s.panel == servicesSubPanelUnits && privilege.CanKill() {
			units := s.filteredUnits()
			if s.cursor < len(units) {
				s.actionMode = 2
				s.actionUnit = units[s.cursor].Name
			}
		}
	case "e":
		if s.panel == servicesSubPanelUnits && privilege.IsRoot() {
			units := s.filteredUnits()
			if s.cursor < len(units) {
				s.actionMode = 4
				s.actionUnit = units[s.cursor].Name
			}
		}
	case "d":
		if s.panel == servicesSubPanelUnits && privilege.IsRoot() {
			units := s.filteredUnits()
			if s.cursor < len(units) {
				s.actionMode = 5
				s.actionUnit = units[s.cursor].Name
			}
		}
	case "m":
		if s.panel == servicesSubPanelUnits && privilege.IsRoot() {
			units := s.filteredUnits()
			if s.cursor < len(units) {
				s.actionMode = 6
				s.actionUnit = units[s.cursor].Name
			}
		}
	}
	return s, nil
}

func (s *Services) fetchLogsCmd(unitName string) tea.Cmd {
	return func() tea.Msg {
		if s.initsysSvc == nil || s.initsysSvc.Backend() == nil {
			return unitLogsLoadedMsg{unit: unitName, err: fmt.Errorf("init backend not available")}
		}
		logs, err := s.initsysSvc.Backend().UnitLogs(unitName, 50)
		return unitLogsLoadedMsg{unit: unitName, logs: logs, err: err}
	}
}

func (s *Services) doUnitAction(mode int, unitName string) tea.Cmd {
	if s.initsysSvc == nil || s.initsysSvc.Backend() == nil {
		return func() tea.Msg {
			return serviceActionMsg{action: "error", unit: unitName, err: fmt.Errorf("init service not wired")}
		}
	}

	backend := s.initsysSvc.Backend()

	return func() tea.Msg {
		var err error
		var action string
		switch mode {
		case 1:
			action = "start_unit"
			err = backend.Start(unitName)
		case 2:
			action = "stop_unit"
			err = backend.Stop(unitName)
		case 3:
			action = "restart_unit"
			err = backend.Restart(unitName)
		case 4:
			action = "enable_unit"
			err = backend.Enable(unitName)
		case 5:
			action = "disable_unit"
			err = backend.Disable(unitName)
		case 6:
			action = "mask_unit"
			err = backend.Mask(unitName)
		}

		result := "success"
		if err != nil {
			result = "error: " + err.Error()
		}
		if s.auditSvc != nil {
			s.auditSvc.WriteEvent(audit.Event{
				UID: os.Geteuid(), User: resolver.ByUID(os.Geteuid()),
				Action: action, Target: unitName, Result: result,
			})
		}
		return serviceActionMsg{action: action, unit: unitName, err: err}
	}
}

func (s *Services) View() string {
	var panelTabs []string
	names := [servicesSubCount]string{"Services", "Kernel Log", "Unit Logs"}
	for i, name := range names {
		if servicesSubPanel(i) == s.panel {
			panelTabs = append(panelTabs, styles.TabActive.Render(name))
		} else {
			panelTabs = append(panelTabs, styles.TabInactive.Render(name))
		}
	}
	tabBar := lipgloss.JoinHorizontal(lipgloss.Top, panelTabs...)

	var content string
	switch s.panel {
	case servicesSubPanelUnits:
		content = s.viewUnits()
	case servicesSubPanelDmesg:
		content = s.viewDmesg()
	case servicesSubPanelLogs:
		content = s.viewUnitLogs()
	}

	return styles.PanelStyle.Render(lipgloss.JoinVertical(lipgloss.Left, tabBar, content))
}

func (s *Services) viewUnits() string {
	initName := "Systemd"
	if s.initsysSnap != nil && s.initsysSnap.InitName != "" {
		initName = strings.Title(s.initsysSnap.InitName)
	}
	title := styles.PanelTitleStyle.Render(fmt.Sprintf("  %s Services", initName))

	units := s.filteredUnits()

	if s.initsysSnap == nil && s.systemdSnap == nil {
		return title + "\n  Loading…"
	}

	// Detail mode.
	if s.detailMode && s.cursor < len(units) {
		return s.renderUnitDetail(units[s.cursor])
	}

	filterBar := fmt.Sprintf("  Show: [a]ll  [f]ailed  [r]unning  │  Current: %s", s.unitFilter)
	if privilege.CanKill() {
		filterBar += "  │  [s]tart  [x]stop  [r]estart  [l]ogs  [Enter] detail"
		if privilege.IsRoot() {
			filterBar += "  │  [e]nable  [d]isable  [m]ask"
		}
	}
	if s.filterMode {
		filterBar += "\n  Filter: " + styles.TextAccent.Render(s.filter) + "█"
	} else if s.filter != "" {
		filterBar += fmt.Sprintf("  │  Name filter: %q", s.filter)
	}

	header := styles.TableHeader.Render(fmt.Sprintf("%-38s  %-38s  %-20s  %-10s",
		"Service", "Description", "Status", "Sub-state",
	))

	var rows []string
	for i, u := range units {
		style := styles.TableRow
		if i == s.cursor {
			style = styles.TableRowSelected
		}
		if u.Flagged {
			style = styles.TableRowFlagged
		}
		flag := "  "
		if u.Flagged {
			flag = "⚠ "
		}
		rows = append(rows, style.Render(fmt.Sprintf("%s%-36s  %-38s  %-20s  %-10s",
			flag,
			styles.Truncate(u.Name, 36),
			styles.Truncate(u.Description, 38),
			styles.Truncate(u.DisplayState, 20),
			u.SubState,
		)))
	}
	if len(rows) == 0 {
		rows = append(rows, styles.TextMuted.Render("  No matching units"))
	}

	// Viewport windowing based on cursor.
	viewHeight := s.height - 10
	if viewHeight > 0 && len(rows) > viewHeight {
		startIdx := s.cursor - viewHeight/2
		if startIdx < 0 {
			startIdx = 0
		}
		endIdx := startIdx + viewHeight
		if endIdx > len(rows) {
			endIdx = len(rows)
			startIdx = endIdx - viewHeight
		}
		rows = rows[startIdx:endIdx]
	}

	// Action dialog.
	actionDialog := ""
	if s.actionMode > 0 {
		actionNames := map[int]string{1: "Start", 2: "Stop", 3: "Restart", 4: "Enable", 5: "Disable", 6: "Mask"}
		actionDialog = "\n\n  " + styles.TextYellow.Render(
			fmt.Sprintf("%s unit %s? [y/N]", actionNames[s.actionMode], s.actionUnit),
		)
	}

	// Status message.
	statusLine := ""
	if s.statusMsg != "" {
		statusLine = "\n  " + s.statusMsg
	}

	return strings.Join([]string{title, filterBar, header, strings.Join(rows, "\n"), actionDialog, statusLine}, "\n")
}

func (s *Services) renderUnitDetail(u initsys.Unit) string {
	lines := []string{
		styles.PanelTitleStyle.Render(fmt.Sprintf("  Unit Detail: %s", u.Name)),
		"",
		fmt.Sprintf("  Description:  %s", u.Description),
		fmt.Sprintf("  Status:       %s", u.DisplayState),
		fmt.Sprintf("  Sub-state:    %s", u.SubState),
		fmt.Sprintf("  Enabled:      %s", u.EnabledState),
		"",
	}
	if u.Flagged {
		lines = append(lines, styles.TextRed.Render("  ⚠ This unit is in a FAILED state"))
		lines = append(lines, "")
	}
	if privilege.CanKill() {
		lines = append(lines, styles.TextMuted.Render("  Actions: [s] Start  [x] Stop  [r] Restart  [l] View Logs"))
	}
	lines = append(lines, "", styles.TextMuted.Render("  Press ESC or Enter to go back"))
	return styles.PanelStyle.Render(strings.Join(lines, "\n"))
}

func (s *Services) viewUnitLogs() string {
	unitName := s.selectedUnit
	if unitName == "" {
		units := s.filteredUnits()
		if len(units) > 0 {
			unitName = units[0].Name
		}
	}
	title := styles.PanelTitleStyle.Render(fmt.Sprintf("  Logs: %s", unitName))

	if s.logsLoading {
		return title + "\n  Loading logs…"
	}
	if s.logsErr != "" {
		return title + "\n\n  " + styles.TextRed.Render(s.logsErr)
	}
	if len(s.unitLogs) == 0 {
		return title + "\n  No recent log entries for " + unitName
	}

	var rows []string
	for _, entry := range s.unitLogs {
		levelStyle := styles.TextNormal
		switch entry.Priority {
		case "err":
			levelStyle = styles.TextRed
		case "warn":
			levelStyle = styles.TextYellow
		}
		ts := entry.Timestamp.Format("15:04:05")
		rows = append(rows, fmt.Sprintf("  %s  %s",
			styles.TextMuted.Render(ts),
			levelStyle.Render(entry.Message),
		))
	}
	return title + "\n" + strings.Join(rows, "\n")
}

func (s *Services) filteredUnits() []initsys.Unit {
	var source []initsys.Unit
	if s.initsysSnap != nil {
		source = s.initsysSnap.Units
	} else if s.systemdSnap != nil {
		for _, u := range s.systemdSnap.Units {
			st := initsys.StateInactive
			if u.SubState == "running" || u.SubState == "listening" {
				st = initsys.StateActive
			} else if u.Flagged {
				st = initsys.StateFailed
			}
			source = append(source, initsys.Unit{
				Name:         u.Name,
				Description:  u.Description,
				ActiveState:  st,
				SubState:     u.SubState,
				DisplayState: u.DisplayState,
				Flagged:      u.Flagged,
			})
		}
	}

	var out []initsys.Unit
	for _, u := range source {
		switch s.unitFilter {
		case "failed":
			if !u.Flagged {
				continue
			}
		case "running":
			if u.SubState != "running" && u.ActiveState != initsys.StateActive {
				continue
			}
		}
		if s.filter != "" && !strings.Contains(strings.ToLower(u.Name), strings.ToLower(s.filter)) {
			continue
		}
		out = append(out, u)
	}
	return out
}

func (s *Services) viewDmesg() string {
	title := styles.PanelTitleStyle.Render("  Kernel Log (dmesg)")
	if len(s.dmesgBatch) == 0 {
		return title + "\n  Waiting for kernel messages…\n  (Requires read access to /dev/kmsg)"
	}

	maxLines := s.height - 8
	if maxLines < 5 {
		maxLines = 5
	}

	entries := s.dmesgBatch
	if len(entries) > maxLines {
		entries = entries[len(entries)-maxLines:]
	}

	var rows []string
	for _, e := range entries {
		levelStyle := styles.TextNormal
		switch e.Level {
		case "emerg", "alert", "crit", "err":
			levelStyle = styles.TextRed
		case "warn":
			levelStyle = styles.TextYellow
		case "notice", "info":
			levelStyle = styles.TextGreen
		case "debug":
			levelStyle = styles.TextMuted
		}
		ts := e.Timestamp.Format("15:04:05")
		levelLabel := fmt.Sprintf("%-7s", "["+e.Level+"]")
		rows = append(rows, fmt.Sprintf("  %s  %s  %s",
			styles.TextMuted.Render(ts),
			levelStyle.Render(levelLabel),
			styles.Truncate(e.Message, s.width-25),
		))
	}
	return title + "\n" + strings.Join(rows, "\n")
}
