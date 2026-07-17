package tabs

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	audit "blackeye/internal/services/audit"
	"blackeye/internal/bus"
	"blackeye/internal/config"
	"blackeye/internal/privilege"
	"blackeye/internal/resolver"
	"blackeye/internal/services/process"
	"blackeye/internal/ui/styles"
)

// Validated input: only [a-zA-Z0-9._-] allowed in filters.
var filterRe = regexp.MustCompile(`^[a-zA-Z0-9._\-]*$`)

type sortColumn int

const (
	sortPID sortColumn = iota
	sortCPU
	sortMem
	sortName
)

// Process is the Tab 2 model.
type Process struct {
	width, height int
	cfg           config.Config
	sub           <-chan interface{}
	auditSvc      *audit.Service

	procs       []process.ProcessSnapshot
	sortCol     sortColumn
	sortReverse bool
	cursor      int
	filter      string
	filterMode  bool
	filterErr   string

	// Kill dialog state.
	killMode     int // 0=none, 1=SIGTERM confirm (deprecated), 2=SIGKILL type PID, 3=Signal Menu
	killTarget   *process.ProcessSnapshot
	killInput    string
	signalCursor int

	// Detail panel.
	detailMode  bool
	detailCache process.ProcessSnapshot

	// Sort Menu state.
	sortMenuMode   bool
	sortMenuCursor int
}

var sortMenuItems = []struct {
	name    string
	col     sortColumn
	reverse bool
}{
	{"CPU% (Descending)", sortCPU, false},
	{"CPU% (Ascending)", sortCPU, true},
	{"Memory (Descending)", sortMem, false},
	{"Memory (Ascending)", sortMem, true},
	{"PID (Ascending)", sortPID, false},
	{"PID (Descending)", sortPID, true},
	{"Name (Ascending)", sortName, false},
	{"Name (Descending)", sortName, true},
}

var signalMenuItems = []struct {
	name string
	sig  syscall.Signal
}{
	{"15 SIGTERM (Graceful Kill)", syscall.SIGTERM},
	{"9  SIGKILL (Force Kill)", syscall.SIGKILL},
	{"19 SIGSTOP (Suspend)", syscall.SIGSTOP},
	{"18 SIGCONT (Resume)", syscall.SIGCONT},
	{"1  SIGHUP  (Reload)", syscall.SIGHUP},
}

func NewProcess(b *bus.Bus, cfg config.Config) *Process {
	p := &Process{
		cfg:         cfg,
		sub:         b.Subscribe("process"),
		sortCol:     sortCPU,
		sortReverse: false,
	}
	return p
}

func (p *Process) SetAudit(a *audit.Service) { p.auditSvc = a }

func (p *Process) Init() tea.Cmd { return listenChan(p.sub, "process") }

func (p *Process) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = m.Width, m.Height

	case busMsg:
		if m.ch == p.sub {
			if v, ok := m.data.(process.Snapshot); ok {
				p.procs = v.Processes
				if p.cursor >= len(p.procs) && len(p.procs) > 0 {
					p.cursor = len(p.procs) - 1
				}
				if p.detailMode {
					f := p.filtered()
					if p.cursor < len(f) {
						snap := f[p.cursor]
						process.FetchDetails(&snap)
						p.detailCache = snap
					}
				}
			}
			return p, listenChan(p.sub, "process")
		}

	case tea.KeyMsg:
		return p.handleKey(m)
	}
	return p, nil
}

func (p *Process) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Kill dialog input handling.
	if p.killMode == 2 {
		switch msg.String() {
		case "enter":
			p.confirmKill()
		case "esc":
			p.killMode, p.killInput = 0, ""
		case "backspace":
			if len(p.killInput) > 0 {
				p.killInput = p.killInput[:len(p.killInput)-1]
			}
		default:
			if len(msg.String()) == 1 && msg.String() >= "0" && msg.String() <= "9" {
				p.killInput += msg.String()
			}
		}
		return p, nil
	}
	if p.killMode == 3 {
		switch msg.String() {
		case "up", "k":
			if p.signalCursor > 0 {
				p.signalCursor--
			}
		case "down", "j":
			if p.signalCursor < len(signalMenuItems)-1 {
				p.signalCursor++
			}
		case "enter":
			sig := signalMenuItems[p.signalCursor].sig
			if sig == syscall.SIGKILL {
				p.killMode = 2 // Transition to type PID confirmation
				p.killInput = ""
			} else {
				p.sendSignal(sig)
				p.killMode = 0
			}
		case "esc", "q":
			p.killMode = 0
		}
		return p, nil
	}
	if p.killMode == 1 {
		switch msg.String() {
		case "y":
			p.sendSignal(syscall.SIGTERM)
			p.killMode = 0
		default:
			p.killMode, p.killInput = 0, ""
		}
		return p, nil
	}

	// Sort menu mode.
	if p.sortMenuMode {
		switch msg.String() {
		case "up", "k":
			if p.sortMenuCursor > 0 {
				p.sortMenuCursor--
			}
		case "down", "j":
			if p.sortMenuCursor < len(sortMenuItems)-1 {
				p.sortMenuCursor++
			}
		case "enter":
			item := sortMenuItems[p.sortMenuCursor]
			p.sortCol = item.col
			p.sortReverse = item.reverse
			p.sortMenuMode = false
		case "esc", "q", "f6":
			p.sortMenuMode = false
		}
		return p, nil
	}

	// Filter mode.
	if p.filterMode {
		switch msg.String() {
		case "esc":
			p.filterMode, p.filter, p.filterErr = false, "", ""
		case "enter":
			p.filterMode = false
		case "backspace":
			if len(p.filter) > 0 {
				p.filter = p.filter[:len(p.filter)-1]
			}
		default:
			candidate := p.filter + msg.String()
			if filterRe.MatchString(candidate) {
				p.filter = candidate
				p.filterErr = ""
			} else {
				p.filterErr = fmt.Sprintf("Invalid character %q — only [a-zA-Z0-9._-] allowed", msg.String())
			}
		}
		return p, nil
	}

	// Normal mode.
	switch msg.String() {
	case "up":
		if p.cursor > 0 {
			p.cursor--
		}
	case "down", "j":
		if p.cursor < len(p.filtered())-1 {
			p.cursor++
		}
	case "s":
		p.sortCol = (p.sortCol + 1) % 4
	case "r":
		p.sortReverse = !p.sortReverse
	case "f6":
		p.sortMenuMode = !p.sortMenuMode
		if p.sortMenuMode {
			// Find current sort config to focus
			for i, item := range sortMenuItems {
				if item.col == p.sortCol && item.reverse == p.sortReverse {
					p.sortMenuCursor = i
					break
				}
			}
		}
	case "/":
		p.filterMode, p.filter = true, ""
	case "esc":
		if p.detailMode {
			p.detailMode = false
		} else {
			p.filter = ""
		}
	case "enter":
		p.detailMode = !p.detailMode
		if p.detailMode {
			f := p.filtered()
			if p.cursor < len(f) {
				snap := f[p.cursor]
				process.FetchDetails(&snap)
				p.detailCache = snap
			}
		}
	case "k", "f9":
		if privilege.CanKill() {
			p.startKill(3) // 3 = Signal menu
		}
	}
	return p, nil
}

func (p *Process) startKill(mode int) {
	f := p.filtered()
	if p.cursor < len(f) {
		snap := f[p.cursor]
		p.killTarget = &snap
		p.killMode = mode
		p.killInput = ""
		p.signalCursor = 0 // Reset signal menu cursor
	}
}

func (p *Process) sendSignal(sig syscall.Signal) {
	if p.killTarget == nil {
		return
	}
	t := p.killTarget

	// TOCTOU check: re-read /proc/<pid>/status before sending.
	if err := process.VerifyProcessName(t.PID, t.RawName); err != nil {
		p.filterErr = err.Error()
		return
	}

	proc, err := os.FindProcess(t.PID)
	result := "success"
	if err != nil {
		result = "error: " + err.Error()
	} else {
		if err = proc.Signal(sig); err != nil {
			result = "error: " + err.Error()
		}
	}

	action := "kill_process"
	switch sig {
	case syscall.SIGKILL:
		action = "kill_process_sigkill"
	case syscall.SIGSTOP:
		action = "suspend_process"
	case syscall.SIGCONT:
		action = "resume_process"
	case syscall.SIGHUP:
		action = "reload_process"
	}
	if p.auditSvc != nil {
		p.auditSvc.WriteEvent(audit.Event{
			UID: os.Geteuid(), User: resolver.ByUID(os.Geteuid()),
			Action: action, Target: t.DisplayName, PID: t.PID, Result: result,
		})
	}
}

func (p *Process) confirmKill() {
	if p.killTarget == nil {
		return
	}
	pidStr := fmt.Sprintf("%d", p.killTarget.PID)
	if strings.TrimSpace(p.killInput) == pidStr {
		p.sendSignal(syscall.SIGKILL)
	}
	p.killMode, p.killInput = 0, ""
}

func (p *Process) filtered() []process.ProcessSnapshot {
	if p.filter == "" {
		return p.sorted()
	}
	var out []process.ProcessSnapshot
	lower := strings.ToLower(p.filter)
	for _, proc := range p.sorted() {
		if strings.Contains(strings.ToLower(proc.DisplayName), lower) ||
			strings.Contains(strings.ToLower(proc.Owner), lower) {
			out = append(out, proc)
		}
	}
	return out
}

func (p *Process) sorted() []process.ProcessSnapshot {
	procs := make([]process.ProcessSnapshot, len(p.procs))
	copy(procs, p.procs)
	sort.Slice(procs, func(i, j int) bool {
		var less bool
		switch p.sortCol {
		case sortPID:
			less = procs[i].PID < procs[j].PID
		case sortCPU:
			less = procs[i].CPUPercent > procs[j].CPUPercent
		case sortMem:
			less = procs[i].MemoryMiB > procs[j].MemoryMiB
		case sortName:
			less = procs[i].DisplayName < procs[j].DisplayName
		}
		if p.sortReverse {
			return !less
		}
		return less
	})
	return procs
}

func (p *Process) View() string {
	if p.detailMode {
		return p.renderDetail(p.detailCache)
	}
	return p.renderTable()
}

func (p *Process) renderTable() string {
	// Header row.
	cols := []string{
		fmt.Sprintf("%-6s", "PID"),
		fmt.Sprintf("%-30s", "Name"),
		fmt.Sprintf("%-12s", "Owner"),
		fmt.Sprintf("%-7s", "CPU%"),
		fmt.Sprintf("%-10s", "Memory"),
		fmt.Sprintf("%-18s", "Status"),
		fmt.Sprintf("%-14s", "Started"),
	}
	header := styles.TableHeader.Render(strings.Join(cols, "  "))

	var rows []string
	for i, proc := range p.filtered() {
		line := fmt.Sprintf("%-6d  %-30s  %-12s  %6.1f%%  %9.1f MiB  %-18s  %-14s",
			proc.PID,
			styles.Truncate(proc.DisplayName, 30),
			styles.Truncate(proc.Owner, 12),
			proc.CPUPercent,
			proc.MemoryMiB,
			styles.Truncate(proc.DisplayStatus, 18),
			proc.StartedAt,
		)
		style := styles.TableRow
		if i == p.cursor {
			style = styles.TableRowSelected
		}
		rows = append(rows, style.Render(line))
	}

	// Viewport windowing based on cursor.
	viewHeight := p.height - 10
	if viewHeight > 0 && len(rows) > viewHeight {
		startIdx := p.cursor - viewHeight/2
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

	// Filter bar.
	filterBar := ""
	if p.filterMode {
		filterBar = "\n  Filter: " + styles.TextAccent.Render(p.filter) + "█"
	} else if p.filter != "" {
		filterBar = "\n  " + styles.TextMuted.Render(fmt.Sprintf("Filter: %q  (ESC to clear)", p.filter))
	}
	if p.filterErr != "" {
		filterBar += "\n  " + styles.TextRed.Render(p.filterErr)
	}

	// Kill dialog.
	killDialog := ""
	if p.killMode == 3 && p.killTarget != nil {
		var menuRows []string
		menuRows = append(menuRows, styles.TextYellow.Render(fmt.Sprintf("Send Signal to %s (PID %d):", p.killTarget.DisplayName, p.killTarget.PID)))
		for i, item := range signalMenuItems {
			if i == p.signalCursor {
				menuRows = append(menuRows, styles.TableRowSelected.Render("  > "+item.name))
			} else {
				menuRows = append(menuRows, styles.TextNormal.Render("    "+item.name))
			}
		}
		menuRows = append(menuRows, styles.TextMuted.Render("  (Use ↑/↓ to navigate, Enter to select, ESC to cancel)"))
		killDialog = "\n\n  " + strings.Join(menuRows, "\n  ")
	} else if p.killMode == 1 && p.killTarget != nil {
		killDialog = "\n\n  " + styles.TextYellow.Render(
			fmt.Sprintf("Send SIGTERM to %s (PID %d)? [y/N]", p.killTarget.DisplayName, p.killTarget.PID),
		)
	} else if p.killMode == 2 && p.killTarget != nil {
		killDialog = "\n\n  " + styles.TextRed.Render(
			fmt.Sprintf("Type PID to confirm SIGKILL for %s: %s█", p.killTarget.DisplayName, p.killInput),
		)
	}

	sortDialog := ""
	if p.sortMenuMode {
		var menuRows []string
		menuRows = append(menuRows, styles.TextYellow.Render("Sort Processes By:"))
		for i, item := range sortMenuItems {
			if i == p.sortMenuCursor {
				menuRows = append(menuRows, styles.TableRowSelected.Render("  > "+item.name))
			} else {
				menuRows = append(menuRows, styles.TextNormal.Render("    "+item.name))
			}
		}
		menuRows = append(menuRows, styles.TextMuted.Render("  (Use ↑/↓ to navigate, Enter to select, ESC to cancel)"))
		sortDialog = "\n\n  " + strings.Join(menuRows, "\n  ")
	}

	sortLabel := []string{"PID", "CPU", "Memory", "Name"}[p.sortCol]
	title := styles.PanelTitleStyle.Render(fmt.Sprintf("  Processes  │  Sort: %s %s  │  Total: %d",
		sortLabel, func() string {
			if p.sortReverse {
				return "↑"
			}
			return "↓"
		}(), len(p.procs)))

	return styles.PanelStyle.Render(
		lipgloss.JoinVertical(lipgloss.Left,
			title, header,
			strings.Join(rows, "\n"),
			filterBar, killDialog, sortDialog,
		),
	)
}

func (p *Process) renderDetail(proc process.ProcessSnapshot) string {
	lines := []string{
		styles.PanelTitleStyle.Render(fmt.Sprintf("  Process Detail: %s", proc.DisplayName)),
		fmt.Sprintf("  PID: %d  │  Owner: %s  │  Status: %s", proc.PID, proc.Owner, proc.DisplayStatus),
		fmt.Sprintf("  Started: %s  │  CPU: %.1f%%  │  Memory: %.1f MiB", proc.StartedAt, proc.CPUPercent, proc.MemoryMiB),
		"",
		fmt.Sprintf("  Command:  %s", styles.Truncate(proc.FullCmdLine, p.width-15)),
		fmt.Sprintf("  Open FDs: %d", proc.OpenFDs),
		fmt.Sprintf("  cgroup:   %s", proc.CgroupPath),
		"",
		styles.TextMuted.Render("  Press ESC or Enter to go back"),
	}
	return styles.PanelStyle.Render(strings.Join(lines, "\n"))
}
