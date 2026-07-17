package tabs

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"blackeye/internal/bus"
	"blackeye/internal/config"
	"blackeye/internal/services/dmesg"
	"blackeye/internal/services/systemd"
	"blackeye/internal/ui/styles"
)

type servicesSubPanel int

const (
	servicesSubPanelUnits servicesSubPanel = iota
	servicesSubPanelDmesg
	servicesSubCount
)

// Services is the Tab 5 model.
type Services struct {
	width, height int
	cfg           config.Config

	subSystemd <-chan interface{}
	subDmesg   <-chan interface{}

	systemdSnap *systemd.Snapshot
	dmesgBatch  []dmesg.Entry

	panel      servicesSubPanel
	cursor     int
	filter     string
	filterMode bool

	// Filter state: "all" | "failed" | "running"
	unitFilter string
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
			if v, ok := m.data.(systemd.Snapshot); ok {
				s.systemdSnap = &v
			}
			cmd = listenChan(s.subSystemd, "systemd")
		case s.subDmesg:
			if v, ok := m.data.(dmesg.Snapshot); ok {
				s.dmesgBatch = append(s.dmesgBatch, v.Entries...)
				// Keep last 500 entries.
				if len(s.dmesgBatch) > 500 {
					s.dmesgBatch = s.dmesgBatch[len(s.dmesgBatch)-500:]
				}
			}
			cmd = listenChan(s.subDmesg, "dmesg")
		}
		return s, cmd
	case tea.KeyMsg:
		return s.handleKey(m)
	}
	return s, nil
}

func (s *Services) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		s.unitFilter = "running"
	}
	return s, nil
}

func (s *Services) View() string {
	var panelTabs []string
	names := [servicesSubCount]string{"Services", "Kernel Log"}
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
	}

	return styles.PanelStyle.Render(lipgloss.JoinVertical(lipgloss.Left, tabBar, content))
}

func (s *Services) viewUnits() string {
	title := styles.PanelTitleStyle.Render("  Systemd Services")
	if s.systemdSnap == nil {
		return title + "\n  Loading…"
	}
	if !s.systemdSnap.Available {
		return title + "\n\n  " + styles.TextRed.Render(s.systemdSnap.Error)
	}

	filterBar := fmt.Sprintf("  Show: [a]ll  [f]ailed  [r]unning  │  Current: %s", s.unitFilter)
	if s.filterMode {
		filterBar += "\n  Filter: " + styles.TextAccent.Render(s.filter) + "█"
	} else if s.filter != "" {
		filterBar += fmt.Sprintf("  │  Name filter: %q", s.filter)
	}

	header := styles.TableHeader.Render(fmt.Sprintf("%-42s  %-40s  %-22s  %-10s",
		"Service", "Description", "Status", "Sub-state",
	))

	var rows []string
	for i, u := range s.filteredUnits() {
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
		rows = append(rows, style.Render(fmt.Sprintf("%s%-40s  %-40s  %-22s  %-10s",
			flag,
			styles.Truncate(u.Name, 40),
			styles.Truncate(u.Description, 40),
			styles.Truncate(u.DisplayState, 22),
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

	return strings.Join([]string{title, filterBar, header, strings.Join(rows, "\n")}, "\n")
}

func (s *Services) filteredUnits() []systemd.UnitSnapshot {
	if s.systemdSnap == nil {
		return nil
	}
	var out []systemd.UnitSnapshot
	for _, u := range s.systemdSnap.Units {
		switch s.unitFilter {
		case "failed":
			if !u.Flagged {
				continue
			}
		case "running":
			if u.SubState != "running" {
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

	// Show last N lines that fit in the terminal.
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
