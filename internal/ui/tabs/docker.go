package tabs

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	audit "blackeye/internal/services/audit"
	"blackeye/internal/bus"
	"blackeye/internal/config"
	"blackeye/internal/privilege"
	"blackeye/internal/resolver"
	dockersvc "blackeye/internal/services/docker"
	"blackeye/internal/ui/styles"
)

type dockerActionMsg struct {
	action string
	err    error
}

type dockerLogsMsg struct {
	lines []string
	err   error
}

// Docker is the Tab 4 model.
type Docker struct {
	width, height int
	cfg           config.Config
	sub           <-chan interface{}
	auditSvc      *audit.Service
	dockerSvc     *dockersvc.Service

	snap       *dockersvc.Snapshot
	cursor     int
	detailMode bool
	logMode    bool

	// Scroll state for detail/log panels
	detailScroll int
	logLines     []string
	logLoading   bool
	logError     string
	statusMsg    string

	// Action dialog.
	actionMode   int // 0=none 1=stop confirm 2=restart confirm
	actionTarget string
}

func NewDocker(b *bus.Bus, cfg config.Config) *Docker {
	d := &Docker{cfg: cfg, sub: b.Subscribe("docker")}
	return d
}

func (d *Docker) SetAudit(a *audit.Service)   { d.auditSvc = a }
func (d *Docker) SetDocker(s *dockersvc.Service) { d.dockerSvc = s }

func (d *Docker) Init() tea.Cmd { return listenChan(d.sub, "docker") }

func (d *Docker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		d.width, d.height = m.Width, m.Height
	case busMsg:
		if m.ch == d.sub {
			if v, ok := m.data.(dockersvc.Snapshot); ok {
				d.snap = &v
			}
			return d, listenChan(d.sub, "docker")
		}
	case dockerActionMsg:
		if m.err != nil {
			d.statusMsg = styles.TextRed.Render("Action failed: " + m.err.Error())
		} else {
			d.statusMsg = styles.TextGreen.Render("Action completed: " + m.action)
		}
	case dockerLogsMsg:
		d.logLoading = false
		if m.err != nil {
			d.logError = m.err.Error()
			d.logLines = nil
		} else {
			d.logError = ""
			d.logLines = m.lines
		}
	case tea.KeyMsg:
		return d.handleKey(m)
	}
	return d, nil
}

func (d *Docker) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if d.actionMode > 0 {
		switch msg.String() {
		case "y":
			mode := d.actionMode
			d.actionMode = 0
			return d, d.doActionCmd(mode)
		case "n", "esc":
			d.actionMode = 0
		}
		return d, nil
	}

	if d.detailMode || d.logMode {
		switch msg.String() {
		case "up":
			d.detailScroll--
		case "down", "j":
			d.detailScroll++
		case "pgup":
			d.detailScroll -= d.height / 2
		case "pgdown":
			d.detailScroll += d.height / 2
		case "home":
			d.detailScroll = 0
		case "esc", "enter", "q":
			d.detailMode, d.logMode = false, false
			d.detailScroll = 0
			d.logLines = nil
			d.logError = ""
			d.logLoading = false
		}
		return d, nil
	}

	switch msg.String() {
	case "up":
		if d.cursor > 0 {
			d.cursor--
		}
	case "down", "j":
		if d.snap != nil && d.cursor < len(d.snap.Containers)-1 {
			d.cursor++
		}
	case "enter":
		d.detailMode = true
		d.detailScroll = 0
	case "l":
		if d.snap == nil || d.cursor >= len(d.snap.Containers) {
			return d, nil
		}
		d.logMode = true
		d.detailScroll = 0
		d.logLines = nil
		d.logError = ""
		d.logLoading = true
		return d, d.fetchLogsCmd()
	case "r":
		if privilege.HasDockerAccess() {
			d.startAction(2)
		}
	case "s":
		if privilege.HasDockerAccess() {
			d.startAction(1)
		}
	}
	return d, nil
}

func (d *Docker) startAction(mode int) {
	if d.snap == nil || d.cursor >= len(d.snap.Containers) {
		return
	}
	d.actionMode = mode
	d.actionTarget = d.snap.Containers[d.cursor].FullID
}

func (d *Docker) doActionCmd(mode int) tea.Cmd {
	if d.dockerSvc == nil || d.snap == nil || d.cursor >= len(d.snap.Containers) {
		return nil
	}
	c := d.snap.Containers[d.cursor]
	action := "stop_container"
	if mode == 2 {
		action = "restart_container"
	}
	id := c.FullID

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		var err error
		switch mode {
		case 1:
			err = d.dockerSvc.StopContainer(ctx, id)
		case 2:
			err = d.dockerSvc.RestartContainer(ctx, id)
		}

		result := "success"
		if err != nil {
			result = "error: " + err.Error()
		}
		if d.auditSvc != nil {
			d.auditSvc.WriteEvent(audit.Event{
				UID: os.Geteuid(), User: resolver.ByUID(os.Geteuid()),
				Action: action, Target: c.Name, ID: id, Result: result,
			})
		}
		return dockerActionMsg{action: action, err: err}
	}
}

func (d *Docker) fetchLogsCmd() tea.Cmd {
	if d.dockerSvc == nil || d.snap == nil || d.cursor >= len(d.snap.Containers) {
		return func() tea.Msg {
			return dockerLogsMsg{err: fmt.Errorf("docker service unavailable")}
		}
	}
	id := d.snap.Containers[d.cursor].FullID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		text, err := d.dockerSvc.ContainerLogs(ctx, id, 200)
		if err != nil {
			return dockerLogsMsg{err: err}
		}
		if text == "" {
			return dockerLogsMsg{lines: []string{styles.TextMuted.Render("  (no log output)")}}
		}
		return dockerLogsMsg{lines: strings.Split(text, "\n")}
	}
}

func (d *Docker) View() string {
	if d.snap != nil && !d.snap.Available {
		return styles.PanelStyle.Render(
			styles.PanelTitleStyle.Render("  Docker") + "\n\n" +
				styles.TextRed.Render("  Docker is not available:\n\n") +
				"  " + strings.ReplaceAll(d.snap.Error, "\n", "\n  "),
		)
	}

	if d.logMode && d.snap != nil && d.cursor < len(d.snap.Containers) {
		return d.renderLogs(d.snap.Containers[d.cursor])
	}

	if d.detailMode && d.snap != nil && d.cursor < len(d.snap.Containers) {
		return d.renderDetail(d.snap.Containers[d.cursor])
	}

	return d.renderTable()
}

func (d *Docker) renderTable() string {
	title := styles.PanelTitleStyle.Render("  Docker Containers")
	if d.snap == nil {
		return styles.PanelStyle.Render(title + "\n  Loading…")
	}

	header := styles.TableHeader.Render(fmt.Sprintf("%-13s  %-24s  %-32s  %-22s  %-7s  %-16s",
		"ID", "Name", "Image", "Status", "CPU%", "Memory",
	))

	var rows []string
	for i, c := range d.snap.Containers {
		icon := resolver.DockerStatusIcon(strings.Split(c.DisplayStatus, " ")[0])
		style := styles.TableRow
		if i == d.cursor {
			style = styles.TableRowSelected
		}
		if strings.Contains(c.DisplayStatus, "dead") || strings.Contains(c.DisplayStatus, "failed") {
			style = styles.TableRowFlagged
		}
		rows = append(rows, style.Render(fmt.Sprintf("%-13s  %-24s  %-32s  %-22s  %6.1f%%  %-16s",
			c.ID, styles.Truncate(c.Name, 24), styles.Truncate(c.Image, 32),
			icon+" "+styles.Truncate(c.DisplayStatus, 20),
			c.CPUPercent, c.MemDisplay,
		)))
	}
	if len(rows) == 0 {
		rows = append(rows, styles.TextMuted.Render("  No containers found"))
	}

	viewHeight := d.height - 10
	if viewHeight > 0 && len(rows) > viewHeight {
		startIdx := d.cursor - viewHeight/2
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

	actionDialog := ""
	if d.actionMode == 1 {
		actionDialog = "\n\n  " + styles.TextYellow.Render("Stop container? [y/N]")
	} else if d.actionMode == 2 {
		actionDialog = "\n\n  " + styles.TextYellow.Render("Restart container? [y/N]")
	}

	statusLine := ""
	if d.statusMsg != "" {
		statusLine = "\n  " + d.statusMsg
	}

	return styles.PanelStyle.Render(
		lipgloss.JoinVertical(lipgloss.Left,
			title, header, strings.Join(rows, "\n"), actionDialog, statusLine,
		),
	)
}

func (d *Docker) renderDetail(c dockersvc.ContainerInfo) string {
	var lines []string
	lines = append(lines,
		styles.PanelTitleStyle.Render(fmt.Sprintf("  Container: %s  (%s)", c.Name, c.DisplayStatus)),
		fmt.Sprintf("  ID:      %s", styles.Truncate(c.FullID, 17)),
		fmt.Sprintf("  Image:   %s", c.Image),
		fmt.Sprintf("  CPU:     %.2f%%   Memory: %s", c.CPUPercent, c.MemDisplay),
		fmt.Sprintf("  Uptime:  %s", c.Uptime),
		"",
	)

	if len(c.Ports) > 0 {
		lines = append(lines, styles.SectionTitle.Render("  Port Mappings"))
		for _, p := range c.Ports {
			lines = append(lines, "    "+p)
		}
		lines = append(lines, "")
	}

	if len(c.Mounts) > 0 {
		lines = append(lines, styles.SectionTitle.Render("  Volume Mounts"))
		for _, m := range c.Mounts {
			lines = append(lines, "    "+m)
		}
		lines = append(lines, "")
	}

	if c.NetworkInfo != "" {
		lines = append(lines, styles.SectionTitle.Render("  Networks"))
		lines = append(lines, "    "+c.NetworkInfo, "")
	}

	if len(c.EnvVars) > 0 {
		lines = append(lines, styles.SectionTitle.Render("  Environment Variables"))
		for _, e := range c.EnvVars {
			if e.Redacted {
				lines = append(lines, fmt.Sprintf("    %s = %s", e.Key, styles.TextMuted.Render("[REDACTED]")))
			} else {
				lines = append(lines, fmt.Sprintf("    %s = %s", e.Key, e.Value))
			}
		}
	}

	lines = append(lines, "", styles.TextMuted.Render("  Press ESC to go back"))
	return d.renderScrollable(lines)
}

func (d *Docker) renderLogs(c dockersvc.ContainerInfo) string {
	lines := []string{
		styles.PanelTitleStyle.Render(fmt.Sprintf("  Logs: %s", c.Name)),
		"",
	}
	if d.logLoading {
		lines = append(lines, styles.TextMuted.Render("  Loading logs…"))
	} else if d.logError != "" {
		lines = append(lines, styles.TextRed.Render("  Error: "+d.logError))
	} else {
		for _, line := range d.logLines {
			lines = append(lines, "  "+styles.Truncate(line, d.width-4))
		}
	}
	lines = append(lines, "", styles.TextMuted.Render("  Press ESC to go back"))
	return d.renderScrollable(lines)
}

func (d *Docker) renderScrollable(lines []string) string {
	viewHeight := d.height - 4
	if viewHeight <= 0 {
		return styles.PanelStyle.Render(strings.Join(lines, "\n"))
	}

	maxScroll := len(lines) - viewHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if d.detailScroll > maxScroll {
		d.detailScroll = maxScroll
	}
	if d.detailScroll < 0 {
		d.detailScroll = 0
	}

	endIdx := d.detailScroll + viewHeight
	if endIdx > len(lines) {
		endIdx = len(lines)
	}
	visibleLines := lines[d.detailScroll:endIdx]

	if maxScroll > 0 {
		indicator := fmt.Sprintf("  Scroll: %d/%d (Use ↑/↓ or PgUp/PgDn)", d.detailScroll, maxScroll)
		visibleLines = append(visibleLines, styles.TextMuted.Render(indicator))
	}

	return styles.PanelStyle.Render(strings.Join(visibleLines, "\n"))
}
