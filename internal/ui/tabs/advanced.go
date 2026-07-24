package tabs

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	audit "blackeye/internal/services/audit"
	"blackeye/internal/bus"
	"blackeye/internal/config"
	"blackeye/internal/privilege"
	"blackeye/internal/resolver"
	"blackeye/internal/services/advanced"
	"blackeye/internal/ui/styles"
)

type advSubPanel int

const (
	advSubSSH advSubPanel = iota
	advSubCron
	advSubStorage
	advSubCount
)

type advOpMsg struct {
	op  string
	err error
}

// Advanced is the Tab 0 model.
type Advanced struct {
	width, height int
	cfg           config.Config
	sub           <-chan interface{}
	advSvc        *advanced.Service
	auditSvc      *audit.Service

	snap      *advanced.Snapshot
	panel     advSubPanel
	cursor    int
	statusMsg string

	// Terminate SSH session confirm
	termMode int // 0=none, 1=confirm kill session
}

func NewAdvanced(b *bus.Bus, cfg config.Config) *Advanced {
	return &Advanced{
		cfg: cfg,
		sub: b.Subscribe("advanced"),
	}
}

func (a *Advanced) SetAdvanced(svc *advanced.Service) { a.advSvc = svc }
func (a *Advanced) SetAudit(audit *audit.Service)     { a.auditSvc = audit }

func (a *Advanced) Init() tea.Cmd {
	return listenChan(a.sub, "advanced")
}

func (a *Advanced) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = m.Width, m.Height
	case busMsg:
		if m.ch == a.sub {
			if v, ok := m.data.(advanced.Snapshot); ok {
				a.snap = &v
			}
			return a, listenChan(a.sub, "advanced")
		}
	case advOpMsg:
		if m.err != nil {
			a.statusMsg = styles.TextRed.Render(fmt.Sprintf("Operation failed (%s): %v", m.op, m.err))
		} else {
			a.statusMsg = styles.TextGreen.Render(fmt.Sprintf("Operation completed (%s)", m.op))
		}
	case tea.KeyMsg:
		return a.handleKey(m)
	}
	return a, nil
}

func (a *Advanced) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if a.termMode > 0 {
		switch msg.String() {
		case "y":
			a.termMode = 0
			if a.snap != nil && a.cursor < len(a.snap.SSHSessions) {
				sess := a.snap.SSHSessions[a.cursor]
				return a, a.doKillSession(sess)
			}
		case "n", "esc":
			a.termMode = 0
		}
		return a, nil
	}

	switch msg.String() {
	case "tab":
		a.panel = (a.panel + 1) % advSubCount
		a.cursor = 0
	case "up":
		if a.cursor > 0 {
			a.cursor--
		}
	case "down", "j":
		a.cursor++
	case "k", "x":
		if a.panel == advSubSSH && privilege.CanKill() {
			if a.snap != nil && a.cursor < len(a.snap.SSHSessions) {
				a.termMode = 1
			}
		}
	}
	return a, nil
}

func (a *Advanced) doKillSession(sess advanced.SSHSession) tea.Cmd {
	return func() tea.Msg {
		var err error
		if sess.PID > 0 {
			if proc, pErr := os.FindProcess(sess.PID); pErr == nil {
				err = proc.Signal(syscall.SIGHUP)
			} else {
				err = pErr
			}
		} else {
			err = fmt.Errorf("session PID unavailable")
		}

		result := "success"
		if err != nil {
			result = "error: " + err.Error()
		}
		if a.auditSvc != nil {
			a.auditSvc.WriteEvent(audit.Event{
				UID: os.Geteuid(), User: resolver.ByUID(os.Geteuid()),
				Action: "terminate_ssh_session", Target: fmt.Sprintf("%s on %s (%s)", sess.User, sess.TTY, sess.FromIP), Result: result,
			})
		}
		return advOpMsg{op: "terminate SSH session " + sess.TTY, err: err}
	}
}

func (a *Advanced) View() string {
	var panelTabs []string
	names := [advSubCount]string{"SSH Sessions", "Cron & Timers", "Storage Topology"}
	for i, name := range names {
		if advSubPanel(i) == a.panel {
			panelTabs = append(panelTabs, styles.TabActive.Render(name))
		} else {
			panelTabs = append(panelTabs, styles.TabInactive.Render(name))
		}
	}
	tabBar := lipgloss.JoinHorizontal(lipgloss.Top, panelTabs...)

	var content string
	switch a.panel {
	case advSubSSH:
		content = a.viewSSH()
	case advSubCron:
		content = a.viewCron()
	case advSubStorage:
		content = a.viewStorage()
	}

	return styles.PanelStyle.Render(lipgloss.JoinVertical(lipgloss.Left, tabBar, content))
}

func (a *Advanced) viewSSH() string {
	count := 0
	if a.snap != nil {
		count = len(a.snap.SSHSessions)
	}
	title := styles.PanelTitleStyle.Render(fmt.Sprintf("  Active SSH Login Sessions: %d active", count))

	if a.snap == nil {
		return title + "\n  Loading active sessions…"
	}

	actionLine := "  Keys: [k] terminate highlighted session"
	if !privilege.CanKill() {
		actionLine = styles.TextMuted.Render("  (Read-only mode — CAP_KILL or root required to terminate sessions)")
	}

	header := styles.TableHeader.Render(fmt.Sprintf("%-15s  %-10s  %-20s  %-20s  %-8s",
		"User", "TTY", "Source IP", "Login Time", "PID",
	))

	var rows []string
	for i, sess := range a.snap.SSHSessions {
		style := styles.TableRow
		if i == a.cursor {
			style = styles.TableRowSelected
		}
		rows = append(rows, style.Render(fmt.Sprintf("%-15s  %-10s  %-20s  %-20s  %-8d",
			styles.Truncate(sess.User, 15),
			sess.TTY,
			sess.FromIP,
			sess.LoginTime,
			sess.PID,
		)))
	}
	if len(rows) == 0 {
		rows = append(rows, styles.TextMuted.Render("  No active SSH sessions detected"))
	}

	termDialog := ""
	if a.termMode > 0 && a.cursor < len(a.snap.SSHSessions) {
		s := a.snap.SSHSessions[a.cursor]
		termDialog = "\n\n  " + styles.TextRed.Render(fmt.Sprintf("Terminate SSH session for %s on %s (%s)? [y/N]", s.User, s.TTY, s.FromIP))
	}

	statusLine := ""
	if a.statusMsg != "" {
		statusLine = "\n  " + a.statusMsg
	}

	return strings.Join([]string{title, actionLine, header, strings.Join(rows, "\n"), termDialog, statusLine}, "\n")
}

func (a *Advanced) viewCron() string {
	count := 0
	if a.snap != nil {
		count = len(a.snap.CronEntries)
	}
	title := styles.PanelTitleStyle.Render(fmt.Sprintf("  Scheduled Cron Jobs & Timers: %d entries", count))

	if a.snap == nil || len(a.snap.CronEntries) == 0 {
		return title + "\n  No scheduled cron entries found"
	}

	header := styles.TableHeader.Render(fmt.Sprintf("%-10s  %-15s  %-35s  %-15s", "User", "Schedule", "Command", "Source"))
	var rows []string
	for i, c := range a.snap.CronEntries {
		style := styles.TableRow
		if i == a.cursor {
			style = styles.TableRowSelected
		}
		rows = append(rows, style.Render(fmt.Sprintf("%-10s  %-15s  %-35s  %-15s",
			c.User,
			c.Schedule,
			styles.Truncate(c.Command, 35),
			c.Source,
		)))
	}
	return strings.Join([]string{title, header, strings.Join(rows, "\n")}, "\n")
}

func (a *Advanced) viewStorage() string {
	count := 0
	if a.snap != nil {
		count = len(a.snap.Volumes)
	}
	title := styles.PanelTitleStyle.Render(fmt.Sprintf("  Storage & Volume Topology: %d volumes detected", count))

	if a.snap == nil || len(a.snap.Volumes) == 0 {
		return title + "\n  No custom LVM or RAID storage volumes detected"
	}

	header := styles.TableHeader.Render(fmt.Sprintf("%-25s  %-20s  %-10s  %-20s", "Volume Name", "Type", "Size", "Device Path"))
	var rows []string
	for i, v := range a.snap.Volumes {
		style := styles.TableRow
		if i == a.cursor {
			style = styles.TableRowSelected
		}
		rows = append(rows, style.Render(fmt.Sprintf("%-25s  %-20s  %-10s  %-20s",
			v.Name,
			v.Type,
			v.Size,
			v.MountPoint,
		)))
	}
	return strings.Join([]string{title, header, strings.Join(rows, "\n")}, "\n")
}
