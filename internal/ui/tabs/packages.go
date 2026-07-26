package tabs

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"blackeye/internal/bus"
	"blackeye/internal/config"
	"blackeye/internal/privilege"
	"blackeye/internal/resolver"
	audit "blackeye/internal/services/audit"
	"blackeye/internal/services/packages"
	"blackeye/internal/ui/styles"
)

var pkgFilterRe = regexp.MustCompile(`^[a-zA-Z0-9._-]*$`)

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
	results     []packages.Package
	suggestions []packages.Package
	err         error
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

	// Category filter state (c key)
	catFilterIdx int // 0=All, 1=System Core, 2=User App, 3=Library, 4=Dev

	// Helper backend selector state (b key)
	backends         []packages.PkgBackend
	activeBackendIdx int

	// Search state
	searchQuery       string
	searchResults     []packages.Package
	searchSuggestions []packages.Package
	searchLoading     bool

	// Operation dialog & result modal state
	opMode    int // 0=none, 1=install confirm, 2=remove confirm, 3=update all confirm
	opTarget  string
	opInput   string
	lastOpMsg *pkgOpMsg
}

func NewPackages(b *bus.Bus, cfg config.Config) *Packages {
	backends := packages.AvailableBackends()
	return &Packages{
		cfg:      cfg,
		sub:      b.Subscribe("packages"),
		backends: backends,
	}
}

func (p *Packages) SetPackages(svc *packages.Service) { p.pkgSvc = svc }
func (p *Packages) SetAudit(a *audit.Service)        { p.auditSvc = a }

func (p *Packages) Init() tea.Cmd {
	return listenChan(p.sub, "packages")
}

func (p *Packages) getActiveBackend() packages.PkgBackend {
	if len(p.backends) > 0 && p.activeBackendIdx < len(p.backends) {
		return p.backends[p.activeBackendIdx]
	}
	if p.pkgSvc != nil {
		return p.pkgSvc.Backend()
	}
	return nil
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
			p.searchSuggestions = m.suggestions
		}
	case pkgOpMsg:
		opMsg := m
		p.lastOpMsg = &opMsg
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
	// Dismiss result modal on ESC / Enter / Space / q
	if p.lastOpMsg != nil {
		if msg.String() == "esc" || msg.String() == "enter" || msg.String() == "q" || msg.String() == "space" {
			p.lastOpMsg = nil
		}
		return p, nil
	}

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
			if len(msg.String()) == 1 && pkgFilterRe.MatchString(p.filter+msg.String()) {
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
	case "c":
		// Cycle category filter: All -> System Core -> User App -> Library -> Dev
		p.catFilterIdx = (p.catFilterIdx + 1) % 5
		p.cursor = 0
	case "b":
		// Cycle manager backend: pacman -> yay -> flatpak -> etc.
		if len(p.backends) > 0 {
			p.activeBackendIdx = (p.activeBackendIdx + 1) % len(p.backends)
			p.cursor = 0
			if b := p.getActiveBackend(); b != nil {
				if inst, err := b.ListInstalled(); err == nil {
					if p.snap == nil {
						p.snap = &packages.Snapshot{}
					}
					p.snap.BackendName = b.Name()
					p.snap.InstalledCount = len(inst)
					p.snap.Installed = inst
				}
			}
			if p.searchQuery != "" {
				p.searchLoading = true
				return p, p.doSearch(p.searchQuery)
			}
		}
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
			targetList := p.searchResults
			if len(targetList) == 0 {
				targetList = p.searchSuggestions
			}
			if p.cursor < len(targetList) {
				p.opMode = 1
				p.opTarget = targetList[p.cursor].Name
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
		backend := p.getActiveBackend()
		if backend == nil {
			return searchResultMsg{err: fmt.Errorf("package manager unavailable")}
		}
		results, err := backend.Search(query)
		var suggestions []packages.Package

		if len(results) == 0 {
			// Search alternative backends for suggestions
			for _, alt := range p.backends {
				if alt.Name() != backend.Name() {
					altRes, _ := alt.Search(query)
					if len(altRes) > 0 {
						suggestions = append(suggestions, altRes...)
					}
				}
			}
			if p.snap != nil && len(suggestions) < 5 {
				fuzz := packages.FuzzySuggest(query, p.snap.Installed)
				suggestions = append(suggestions, fuzz...)
			}
		}
		return searchResultMsg{results: results, suggestions: suggestions, err: err}
	}
}

func (p *Packages) doInstall(name string) tea.Cmd {
	return func() tea.Msg {
		backend := p.getActiveBackend()
		if backend == nil {
			return pkgOpMsg{op: "install", err: fmt.Errorf("package manager unavailable")}
		}
		out, err := backend.Install(name)
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
		backend := p.getActiveBackend()
		if backend == nil {
			return pkgOpMsg{op: "remove", err: fmt.Errorf("package manager unavailable")}
		}
		out, err := backend.Remove(name)
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
		backend := p.getActiveBackend()
		if backend == nil {
			return pkgOpMsg{op: "system update", err: fmt.Errorf("package manager unavailable")}
		}
		out, err := backend.UpgradeAll()
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
	var source []packages.Package
	for _, pkg := range p.snap.Installed {
		cat := pkg.GetCategory()
		match := false
		switch p.catFilterIdx {
		case 1:
			match = cat == packages.CategorySystemCore
		case 2:
			match = cat == packages.CategoryUserInstalled
		case 3:
			match = cat == packages.CategoryDependency
		case 4:
			match = cat == packages.CategoryDevelopment
		default:
			match = true
		}
		if match {
			source = append(source, pkg)
		}
	}

	if p.filter == "" {
		return source
	}
	var out []packages.Package
	lower := strings.ToLower(p.filter)
	for _, pkg := range source {
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

	// Render result modal if operation feedback is available
	if p.lastOpMsg != nil {
		modalTitle := styles.TextGreen.Render("✔ Package Operation Succeeded (" + p.lastOpMsg.op + ")")
		if p.lastOpMsg.err != nil {
			modalTitle = styles.TextRed.Render("❌ Package Operation Failed (" + p.lastOpMsg.op + ")")
		}
		outText := p.lastOpMsg.out
		if outText == "" {
			if p.lastOpMsg.err != nil {
				outText = p.lastOpMsg.err.Error()
			} else {
				outText = "Operation completed with exit status 0."
			}
		}
		modalLines := []string{
			modalTitle,
			"",
			styles.TextMuted.Render("Execution Log Output:"),
			styles.TextNormal.Render(styles.Truncate(outText, 1000)),
			"",
			styles.TextMuted.Render("  (Press ESC to close result dialog)"),
		}
		modalBox := styles.PanelStyle.Copy().Width(p.width - 6).Render(strings.Join(modalLines, "\n"))
		return styles.PanelStyle.Render(lipgloss.JoinVertical(lipgloss.Left, tabBar, modalBox))
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

	catNames := [5]string{"All", "🛡️ System Core", "👤 User App", "📦 Library", "🛠️ Dev"}
	catFilterBar := "  Category [c]: "
	for i, name := range catNames {
		if i == p.catFilterIdx {
			catFilterBar += styles.TextAccent.Render("[" + name + "] ")
		} else {
			catFilterBar += styles.TextMuted.Render(name + "  ")
		}
	}

	title := styles.PanelTitleStyle.Render(fmt.Sprintf("  Package Manager: %s  │  Installed: %d", backendName, count))

	if p.snap == nil {
		return title + "\n  Loading package list…"
	}

	pkgs := p.filteredInstalled()
	actionLine := "  Keys: [/] filter  [c] category filter  [r]emove package  [u]pgrade all"
	if !privilege.CanPackageManage() {
		actionLine = styles.TextMuted.Render("  (Read-only mode — root privilege required to install/remove packages)")
	}

	filterBar := ""
	if p.filterMode {
		filterBar = "\n  Filter: " + styles.TextAccent.Render(p.filter) + "█"
	} else if p.filter != "" {
		filterBar = fmt.Sprintf("\n  Filter: %q", p.filter)
	}

	header := styles.TableHeader.Render(fmt.Sprintf("%-22s  %-14s  %-16s  %-30s",
		"Package", "Version", "Category", "Description",
	))

	var rows []string
	for i, pkg := range pkgs {
		style := styles.TableRow
		if i == p.cursor {
			style = styles.TableRowSelected
		}
		cat := pkg.GetCategory()
		catBadge := string(cat)
		switch cat {
		case packages.CategorySystemCore:
			catBadge = "🛡️ System Core"
		case packages.CategoryUserInstalled:
			catBadge = "👤 User App"
		case packages.CategoryDependency:
			catBadge = "📦 Library"
		case packages.CategoryDevelopment:
			catBadge = "🛠️ Dev"
		}
		rows = append(rows, style.Render(fmt.Sprintf("%-22s  %-14s  %-16s  %-30s",
			styles.Truncate(pkg.Name, 22),
			styles.Truncate(pkg.Version, 14),
			styles.Truncate(catBadge, 16),
			styles.Truncate(pkg.Description, 30),
		)))
	}
	if len(rows) == 0 {
		rows = append(rows, styles.TextMuted.Render("  No matching packages found"))
	}

	// Viewport windowing based on cursor
	viewHeight := p.height - 13
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
		cat := packages.ClassifyCategory(p.opTarget)
		warnBanner := ""
		if cat == packages.CategorySystemCore {
			warnBanner = styles.TextRed.Render("  ⚠️ CRITICAL WARNING: System Core package! Removal may break OS functionality!\n")
		}
		opDialog = "\n\n" + warnBanner + "  " + styles.TextRed.Render(fmt.Sprintf("Remove package %s (%s)? [y/N]", p.opTarget, cat))
	} else if p.opMode == 3 {
		opDialog = "\n\n  " + styles.TextYellow.Render("Type 'UPDATE ALL' to confirm full system upgrade: "+p.opInput+"█")
	}

	statusLine := ""
	if p.statusMsg != "" {
		statusLine = "\n  " + p.statusMsg
	}

	return strings.Join([]string{title, catFilterBar, actionLine, filterBar, header, strings.Join(rows, "\n"), opDialog, statusLine}, "\n")
}

func (p *Packages) viewSearch() string {
	backendName := "Unknown"
	if b := p.getActiveBackend(); b != nil {
		backendName = b.Name()
	}

	managerBar := "  Active Manager [b]: "
	for i, b := range p.backends {
		if i == p.activeBackendIdx {
			managerBar += styles.TextAccent.Render("[" + b.Name() + "] ")
		} else {
			managerBar += styles.TextMuted.Render(b.Name() + "  ")
		}
	}

	title := styles.PanelTitleStyle.Render(fmt.Sprintf("  Search Package Repositories (%s)", backendName))
	inputLine := "  Press [/] to enter search query  │  [b] switch helper: " + styles.TextAccent.Render(p.searchQuery)
	if p.filterMode {
		inputLine = "  Search Query: " + styles.TextAccent.Render(p.filter) + "█  (Press Enter to search)"
	}

	if p.searchLoading {
		return title + "\n" + managerBar + "\n" + inputLine + "\n\n  Searching package repositories via " + backendName + "…"
	}

	targetList := p.searchResults
	suggestionMode := false
	if len(targetList) == 0 && len(p.searchSuggestions) > 0 {
		targetList = p.searchSuggestions
		suggestionMode = true
	}

	header := styles.TableHeader.Render(fmt.Sprintf("%-30s  %-15s  %-40s", "Package", "Version", "Description"))
	var rows []string
	for i, pkg := range targetList {
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
		rows = append(rows, styles.TextMuted.Render("  No search results found (press / to search)"))
	}

	suggHeader := ""
	if suggestionMode {
		suggHeader = "\n  " + styles.TextYellow.Render("💡 No exact match in "+backendName+". Similar / alternative packages found:")
	}

	opDialog := ""
	if p.opMode == 1 {
		opDialog = "\n\n  " + styles.TextYellow.Render(fmt.Sprintf("Install package %s via %s? [y/N]", p.opTarget, backendName))
	}

	return strings.Join([]string{title, managerBar, inputLine, suggHeader, header, strings.Join(rows, "\n"), opDialog}, "\n")
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
