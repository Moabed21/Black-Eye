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
	"blackeye/internal/services/packages"
	"blackeye/internal/ui/styles"
)

type pkgSubPanel int

const (
	pkgSubInstalled pkgSubPanel = iota
	pkgSubSearch
	pkgSubUpdates
	pkgSubCount
)

type pkgOpMsg struct {
	op  string
	out string
	err error
}

type searchResultMsg struct {
	results []packages.Package
	err     error
}

// Packages is the Tab 8 model.
type Packages struct {
	width, height int
	cfg           config.Config
	sub           <-chan interface{}
	pkgSvc        *packages.Service
	auditSvc      *audit.Service

	snap       *packages.Snapshot
	panel      pkgSubPanel
	cursor     int
	statusMsg  string
	filter     string
	filterMode bool

	// Search state
	searchQuery   string
	searchResults []packages.Package
	searchLoading bool

	// Operation dialog state
	opMode   int // 0=none, 1=install confirm, 2=remove confirm, 3=update all confirm
	opTarget string
	opInput  string
}

func NewPackages(b *bus.Bus, cfg config.Config) *Packages {
	return &Packages{
		cfg: cfg,
		sub: b.Subscribe("packages"),
	}
}

func (p *Packages) SetPackages(svc *packages.Service) { p.pkgSvc = svc }
func (p *Packages) SetAudit(a *audit.Service)        { p.auditSvc = a }

func (p *Packages) Init() tea.Cmd {
	return listenChan(p.sub, "packages")
}

func (p *Packages) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = m.Width, m.Height
	case busMsg:
		if m.ch == p.sub {
			if v, ok := m.data.(packages.Snapshot); ok {
				p.snap = &v
			}
			return p, listenChan(p.sub, "packages")
		}
	case searchResultMsg:
		p.searchLoading = false
		if m.err == nil {
			p.searchResults = m.results
		}
	case pkgOpMsg:
		if m.err != nil {
			p.statusMsg = styles.TextRed.Render(fmt.Sprintf("Package operation failed (%s): %v", m.op, m.err))
		} else {
			p.statusMsg = styles.TextGreen.Render(fmt.Sprintf("Package operation succeeded (%s)", m.op))
		}
	case tea.KeyMsg:
		return p.handleKey(m)
	}
	return p, nil
}

func (p *Packages) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Confirmation dialog
	if p.opMode > 0 {
		switch msg.String() {
		case "y":
			mode := p.opMode
			target := p.opTarget
			p.opMode = 0
			if mode == 3 {
				// Require typed "UPDATE ALL" for system update
				if p.opInput == "UPDATE ALL" {
					p.opInput = ""
					return p, p.doUpgradeAll()
				}
				p.statusMsg = styles.TextRed.Render("Type 'UPDATE ALL' to confirm system update")
				return p, nil
			} else if mode == 1 {
				return p, p.doInstall(target)
			} else if mode == 2 {
				return p, p.doRemove(target)
			}
		case "n", "esc":
			p.opMode = 0
			p.opInput = ""
		default:
			if p.opMode == 3 && len(msg.String()) == 1 {
				p.opInput += strings.ToUpper(msg.String())
			}
		}
		return p, nil
	}

	if p.filterMode {
		switch msg.String() {
		case "esc":
			p.filterMode = false
		case "enter":
			p.filterMode = false
			if p.panel == pkgSubSearch && p.filter != "" {
				p.searchQuery = p.filter
				p.searchLoading = true
				return p, p.doSearch(p.searchQuery)
			}
		case "backspace":
			if len(p.filter) > 0 {
				p.filter = p.filter[:len(p.filter)-1]
			}
		default:
			if len(msg.String()) == 1 && filterRe.MatchString(p.filter+msg.String()) {
				p.filter += msg.String()
			}
		}
		return p, nil
	}

	switch msg.String() {
	case "tab":
		p.panel = (p.panel + 1) % pkgSubCount
		p.cursor = 0
	case "up", "k":
		if p.cursor > 0 {
			p.cursor--
		}
	case "down", "j":
		p.cursor++
	case "/":
		p.filterMode = true
		p.filter = ""
	case "r":
		if p.panel == pkgSubInstalled && privilege.CanPackageManage() {
			pkgs := p.filteredInstalled()
			if p.cursor < len(pkgs) {
				p.opMode = 2
				p.opTarget = pkgs[p.cursor].Name
			}
		}
	case "enter":
		if p.panel == pkgSubSearch && privilege.CanPackageManage() {
			if p.cursor < len(p.searchResults) {
				p.opMode = 1
				p.opTarget = p.searchResults[p.cursor].Name
			}
		}
	case "u":
		if privilege.CanPackageManage() {
			p.opMode = 3
			p.opInput = ""
		}
	}
	return p, nil
}

func (p *Packages) doSearch(query string) tea.Cmd {
	return func() tea.Msg {
		if p.pkgSvc == nil || p.pkgSvc.Backend() == nil {
			return searchResultMsg{err: fmt.Errorf("package manager unavailable")}
		}
		results, err := p.pkgSvc.Backend().Search(query)
		return searchResultMsg{results: results, err: err}
	}
}

func (p *Packages) doInstall(name string) tea.Cmd {
	return func() tea.Msg {
		if p.pkgSvc == nil || p.pkgSvc.Backend() == nil {
			return pkgOpMsg{op: "install", err: fmt.Errorf("package manager unavailable")}
		}
		out, err := p.pkgSvc.Backend().Install(name)
		result := "success"
		if err != nil {
			result = "error: " + err.Error()
		}
		if p.auditSvc != nil {
			p.auditSvc.WriteEvent(audit.Event{
				UID: os.Geteuid(), User: resolver.ByUID(os.Geteuid()),
				Action: "package_install", Target: name, Result: result,
			})
		}
		return pkgOpMsg{op: "install " + name, out: out, err: err}
	}
}

func (p *Packages) doRemove(name string) tea.Cmd {
	return func() tea.Msg {
		if p.pkgSvc == nil || p.pkgSvc.Backend() == nil {
			return pkgOpMsg{op: "remove", err: fmt.Errorf("package manager unavailable")}
		}
		out, err := p.pkgSvc.Backend().Remove(name)
		result := "success"
		if err != nil {
			result = "error: " + err.Error()
		}
		if p.auditSvc != nil {
			p.auditSvc.WriteEvent(audit.Event{
				UID: os.Geteuid(), User: resolver.ByUID(os.Geteuid()),
				Action: "package_remove", Target: name, Result: result,
			})
		}
		return pkgOpMsg{op: "remove " + name, out: out, err: err}
	}
}

func (p *Packages) doUpgradeAll() tea.Cmd {
	return func() tea.Msg {
		if p.pkgSvc == nil || p.pkgSvc.Backend() == nil {
			return pkgOpMsg{op: "system update", err: fmt.Errorf("package manager unavailable")}
		}
		out, err := p.pkgSvc.Backend().UpgradeAll()
		result := "success"
		if err != nil {
			result = "error: " + err.Error()
		}
		if p.auditSvc != nil {
			p.auditSvc.WriteEvent(audit.Event{
				UID: os.Geteuid(), User: resolver.ByUID(os.Geteuid()),
				Action: "package_system_update", Target: "all", Result: result,
			})
		}
		return pkgOpMsg{op: "system update", out: out, err: err}
	}
}

func (p *Packages) filteredInstalled() []packages.Package {
	if p.snap == nil {
		return nil
	}
	if p.filter == "" {
		return p.snap.Installed
	}
	var out []packages.Package
	lower := strings.ToLower(p.filter)
	for _, pkg := range p.snap.Installed {
		if strings.Contains(strings.ToLower(pkg.Name), lower) || strings.Contains(strings.ToLower(pkg.Description), lower) {
			out = append(out, pkg)
		}
	}
	return out
}

func (p *Packages) View() string {
	var panelTabs []string
	names := [pkgSubCount]string{"Installed Packages", "Search & Install", "Pending Updates"}
	for i, name := range names {
		if pkgSubPanel(i) == p.panel {
			panelTabs = append(panelTabs, styles.TabActive.Render(name))
		} else {
			panelTabs = append(panelTabs, styles.TabInactive.Render(name))
		}
	}
	tabBar := lipgloss.JoinHorizontal(lipgloss.Top, panelTabs...)

	var content string
	switch p.panel {
	case pkgSubInstalled:
		content = p.viewInstalled()
	case pkgSubSearch:
		content = p.viewSearch()
	case pkgSubUpdates:
		content = p.viewUpdates()
	}

	return styles.PanelStyle.Render(lipgloss.JoinVertical(lipgloss.Left, tabBar, content))
}

func (p *Packages) viewInstalled() string {
	backendName := "Unknown"
	count := 0
	if p.snap != nil {
		backendName = strings.Title(p.snap.BackendName)
		count = p.snap.InstalledCount
	}

	title := styles.PanelTitleStyle.Render(fmt.Sprintf("  Package Manager: %s  │  Installed: %d", backendName, count))

	if p.snap == nil {
		return title + "\n  Loading package list…"
	}

	pkgs := p.filteredInstalled()
	actionLine := "  Keys: [/] filter list  [r]emove package  [u]pgrade all"
	if !privilege.CanPackageManage() {
		actionLine = styles.TextMuted.Render("  (Read-only mode — root privilege required to install/remove packages)")
	}

	filterBar := ""
	if p.filterMode {
		filterBar = "\n  Filter: " + styles.TextAccent.Render(p.filter) + "█"
	} else if p.filter != "" {
		filterBar = fmt.Sprintf("\n  Filter: %q", p.filter)
	}

	header := styles.TableHeader.Render(fmt.Sprintf("%-25s  %-20s  %-10s  %-35s",
		"Package", "Version", "Arch", "Description",
	))

	var rows []string
	for i, pkg := range pkgs {
		style := styles.TableRow
		if i == p.cursor {
			style = styles.TableRowSelected
		}
		rows = append(rows, style.Render(fmt.Sprintf("%-25s  %-20s  %-10s  %-35s",
			styles.Truncate(pkg.Name, 25),
			styles.Truncate(pkg.Version, 20),
			pkg.Arch,
			styles.Truncate(pkg.Description, 35),
		)))
	}
	if len(rows) == 0 {
		rows = append(rows, styles.TextMuted.Render("  No matching packages found"))
	}

	// Viewport windowing based on cursor
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

	opDialog := ""
	if p.opMode == 2 {
		opDialog = "\n\n  " + styles.TextRed.Render(fmt.Sprintf("Remove package %s? [y/N]", p.opTarget))
	} else if p.opMode == 3 {
		opDialog = "\n\n  " + styles.TextYellow.Render("Type 'UPDATE ALL' to confirm full system upgrade: "+p.opInput+"█")
	}

	statusLine := ""
	if p.statusMsg != "" {
		statusLine = "\n  " + p.statusMsg
	}

	return strings.Join([]string{title, actionLine, filterBar, header, strings.Join(rows, "\n"), opDialog, statusLine}, "\n")
}

func (p *Packages) viewSearch() string {
	title := styles.PanelTitleStyle.Render("  Search Package Repositories")
	inputLine := "  Press [/] to enter search query: " + styles.TextAccent.Render(p.searchQuery)
	if p.filterMode {
		inputLine = "  Search Query: " + styles.TextAccent.Render(p.filter) + "█  (Press Enter to search)"
	}

	if p.searchLoading {
		return title + "\n" + inputLine + "\n\n  Searching package repositories…"
	}

	header := styles.TableHeader.Render(fmt.Sprintf("%-30s  %-15s  %-40s", "Package", "Version", "Description"))
	var rows []string
	for i, pkg := range p.searchResults {
		style := styles.TableRow
		if i == p.cursor {
			style = styles.TableRowSelected
		}
		rows = append(rows, style.Render(fmt.Sprintf("%-30s  %-15s  %-40s",
			styles.Truncate(pkg.Name, 30),
			styles.Truncate(pkg.Version, 15),
			styles.Truncate(pkg.Description, 40),
		)))
	}
	if len(rows) == 0 {
		rows = append(rows, styles.TextMuted.Render("  No search results (press / to search)"))
	}

	opDialog := ""
	if p.opMode == 1 {
		opDialog = "\n\n  " + styles.TextYellow.Render(fmt.Sprintf("Install package %s? [y/N]", p.opTarget))
	}

	return strings.Join([]string{title, inputLine, header, strings.Join(rows, "\n"), opDialog}, "\n")
}

func (p *Packages) viewUpdates() string {
	count := 0
	if p.snap != nil {
		count = p.snap.UpgradableCount
	}
	title := styles.PanelTitleStyle.Render(fmt.Sprintf("  Pending Updates: %d packages available", count))
	if count == 0 {
		return title + "\n\n  " + styles.TextGreen.Render("✔ All system packages are up to date!")
	}
	return title + "\n  Press 'u' to perform full system update (requires typed confirmation)."
}
