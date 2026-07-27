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
	"blackeye/internal/services/security"
	"blackeye/internal/services/users"
	"blackeye/internal/ui/styles"
)

type userSubPanel int

const (
	userSubUsers userSubPanel = iota
	userSubGroups
	userSubSudoers
	userSubSUID
	userSubSSH
	userSubCount
)

type userOpMsg struct {
	op  string
	err error
}

// Users is the Tab 9 model.
type Users struct {
	width, height int
	cfg           config.Config
	sub           <-chan interface{}
	subSec        <-chan interface{}
	userSvc       *users.Service
	auditSvc      *audit.Service

	snap       *users.Snapshot
	secSnap    *security.Snapshot
	panel      userSubPanel
	cursor     int
	statusMsg  string
	filter     string
	filterMode bool

	// System users toggle (UID < 1000)
	hideSystemUsers bool

	// Add user wizard state
	addMode  bool
	addInput string

	// Delete user state
	delMode int // 0=none, 1=confirm delete

	// Add Sudoers rule wizard state
	addSudoMode   bool
	addSudoUser   string
	addSudoCmd    string
	addSudoNoPass bool
	addSudoStep   int // 0=user, 1=command, 2=nopass
}

func NewUsers(b *bus.Bus, cfg config.Config) *Users {
	return &Users{
		cfg:             cfg,
		sub:             b.Subscribe("users"),
		subSec:          b.Subscribe("security"),
		hideSystemUsers: true,
	}
}

func (u *Users) SetUsers(svc *users.Service) { u.userSvc = svc }
func (u *Users) SetAudit(a *audit.Service)   { u.auditSvc = a }

func (u *Users) Init() tea.Cmd {
	return tea.Batch(
		listenChan(u.sub, "users"),
		listenChan(u.subSec, "security"),
	)
}

func (u *Users) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		u.width, u.height = m.Width, m.Height
	case busMsg:
		var cmd tea.Cmd
		if m.ch == u.sub {
			if v, ok := m.data.(users.Snapshot); ok {
				u.snap = &v
			}
			cmd = listenChan(u.sub, "users")
		} else if m.ch == u.subSec {
			if v, ok := m.data.(security.Snapshot); ok {
				u.secSnap = &v
			}
			cmd = listenChan(u.subSec, "security")
		}
		return u, cmd
	case userOpMsg:
		if m.err != nil {
			u.statusMsg = styles.TextRed.Render(fmt.Sprintf("User operation failed (%s): %v", m.op, m.err))
		} else {
			u.statusMsg = styles.TextGreen.Render(fmt.Sprintf("User operation succeeded (%s)", m.op))
		}
	case tea.KeyMsg:
		return u.handleKey(m)
	}
	return u, nil
}

func (u *Users) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Add user wizard
	if u.addMode {
		switch msg.String() {
		case "esc":
			u.addMode = false
			u.addInput = ""
		case "enter":
			if strings.TrimSpace(u.addInput) != "" {
				name := u.addInput
				u.addMode = false
				u.addInput = ""
				return u, u.doAddUser(name)
			}
		case "backspace":
			if len(u.addInput) > 0 {
				u.addInput = u.addInput[:len(u.addInput)-1]
			}
		default:
			if len(msg.String()) == 1 && filterRe.MatchString(u.addInput+msg.String()) {
				u.addInput += msg.String()
			}
		}
		return u, nil
	}

	// Add Sudoers wizard
	if u.addSudoMode {
		switch msg.String() {
		case "esc":
			u.addSudoMode = false
			u.addSudoUser = ""
			u.addSudoCmd = ""
			u.addSudoStep = 0
		case "enter":
			if u.addSudoStep == 0 {
				if strings.TrimSpace(u.addSudoUser) != "" {
					u.addSudoStep = 1
				}
			} else if u.addSudoStep == 1 {
				if strings.TrimSpace(u.addSudoCmd) != "" {
					u.addSudoStep = 2
				}
			} else if u.addSudoStep == 2 {
				u.addSudoNoPass = true
				u.addSudoMode = false
				usr := u.addSudoUser
				cmd := u.addSudoCmd
				np := u.addSudoNoPass
				return u, u.doAddSudoRule(usr, cmd, np)
			}
		case "y":
			if u.addSudoStep == 2 {
				u.addSudoNoPass = true
				u.addSudoMode = false
				usr := u.addSudoUser
				cmd := u.addSudoCmd
				np := u.addSudoNoPass
				return u, u.doAddSudoRule(usr, cmd, np)
			}
		case "n":
			if u.addSudoStep == 2 {
				u.addSudoNoPass = false
				u.addSudoMode = false
				usr := u.addSudoUser
				cmd := u.addSudoCmd
				np := u.addSudoNoPass
				return u, u.doAddSudoRule(usr, cmd, np)
			}
		case "backspace":
			if u.addSudoStep == 0 && len(u.addSudoUser) > 0 {
				u.addSudoUser = u.addSudoUser[:len(u.addSudoUser)-1]
			} else if u.addSudoStep == 1 && len(u.addSudoCmd) > 0 {
				u.addSudoCmd = u.addSudoCmd[:len(u.addSudoCmd)-1]
			}
		default:
			if len(msg.String()) == 1 {
				if u.addSudoStep == 0 {
					u.addSudoUser += msg.String()
				} else if u.addSudoStep == 1 {
					u.addSudoCmd += msg.String()
				}
			}
		}
		return u, nil
	}

	// Delete confirm
	if u.delMode > 0 {
		switch msg.String() {
		case "y":
			u.delMode = 0
			usrList := u.filteredUsers()
			if u.cursor < len(usrList) {
				target := usrList[u.cursor].Username
				return u, u.doDeleteUser(target)
			}
		case "n", "esc":
			u.delMode = 0
		}
		return u, nil
	}

	if u.filterMode {
		switch msg.String() {
		case "esc", "enter":
			u.filterMode = false
		case "backspace":
			if len(u.filter) > 0 {
				u.filter = u.filter[:len(u.filter)-1]
			}
		default:
			if len(msg.String()) == 1 && filterRe.MatchString(u.filter+msg.String()) {
				u.filter += msg.String()
			}
		}
		return u, nil
	}

	switch msg.String() {
	case "tab":
		u.panel = (u.panel + 1) % userSubCount
		u.cursor = 0
	case "up", "k":
		if u.cursor > 0 {
			u.cursor--
		}
	case "down", "j":
		max := u.maxItems()
		if max > 0 && u.cursor < max-1 {
			u.cursor++
		}
	case "/":
		u.filterMode = true
		u.filter = ""
	case "h":
		u.hideSystemUsers = !u.hideSystemUsers
	case "a":
		if privilege.CanManageUsers() {
			if u.panel == userSubSudoers {
				u.addSudoMode = true
				u.addSudoStep = 0
				u.addSudoUser = ""
				u.addSudoCmd = ""
			} else {
				u.addMode = true
				u.addInput = ""
			}
		} else {
			u.statusMsg = styles.TextRed.Render("Root privileges required (run: sudo ./blackeye)")
		}
	case "d":
		if privilege.CanManageUsers() {
			if u.panel == userSubSudoers {
				if u.snap != nil && u.cursor < len(u.snap.SudoRules) {
					rule := u.snap.SudoRules[u.cursor]
					return u, u.doDeleteSudoRule(rule)
				}
			} else {
				usrList := u.filteredUsers()
				if u.cursor < len(usrList) {
					u.delMode = 1
				}
			}
		} else {
			u.statusMsg = styles.TextRed.Render("Root privileges required (run: sudo ./blackeye)")
		}
	}
	return u, nil
}

func (u *Users) maxItems() int {
	switch u.panel {
	case userSubUsers:
		return len(u.filteredUsers())
	case userSubGroups:
		if u.snap != nil {
			return len(u.snap.Groups)
		}
	case userSubSudoers:
		if u.snap != nil {
			return len(u.snap.SudoRules)
		}
	case userSubSUID:
		if u.secSnap != nil {
			return len(u.secSnap.SUIDBinaries)
		}
	case userSubSSH:
		return 5
	}
	return 0
}

func (u *Users) doAddUser(name string) tea.Cmd {
	return func() tea.Msg {
		err := users.AddUser(name)
		result := "success"
		if err != nil {
			result = "error: " + err.Error()
		}
		if u.auditSvc != nil {
			u.auditSvc.WriteEvent(audit.Event{
				UID: os.Geteuid(), User: resolver.ByUID(os.Geteuid()),
				Action: "user_add", Target: name, Result: result,
			})
		}
		return userOpMsg{op: "add user " + name, err: err}
	}
}

func (u *Users) doDeleteUser(name string) tea.Cmd {
	return func() tea.Msg {
		err := users.DeleteUser(name)
		result := "success"
		if err != nil {
			result = "error: " + err.Error()
		}
		if u.auditSvc != nil {
			u.auditSvc.WriteEvent(audit.Event{
				UID: os.Geteuid(), User: resolver.ByUID(os.Geteuid()),
				Action: "user_delete", Target: name, Result: result,
			})
		}
		return userOpMsg{op: "delete user " + name, err: err}
	}
}

func (u *Users) doAddSudoRule(targetUser, command string, noPass bool) tea.Cmd {
	return func() tea.Msg {
		err := users.AddSudoRule(targetUser, command, noPass)
		result := "success"
		if err != nil {
			result = "error: " + err.Error()
		}
		if u.auditSvc != nil {
			u.auditSvc.WriteEvent(audit.Event{
				UID: os.Geteuid(), User: resolver.ByUID(os.Geteuid()),
				Action: "sudoers_add_rule", Target: targetUser + " -> " + command, Result: result,
			})
		}
		return userOpMsg{op: "add sudoers rule for " + targetUser, err: err}
	}
}

func (u *Users) doDeleteSudoRule(rule users.SudoRule) tea.Cmd {
	return func() tea.Msg {
		err := users.DeleteSudoRule(rule)
		result := "success"
		if err != nil {
			result = "error: " + err.Error()
		}
		if u.auditSvc != nil {
			u.auditSvc.WriteEvent(audit.Event{
				UID: os.Geteuid(), User: resolver.ByUID(os.Geteuid()),
				Action: "sudoers_delete_rule", Target: rule.Source, Result: result,
			})
		}
		return userOpMsg{op: "delete sudoers rule " + rule.Source, err: err}
	}
}

func (u *Users) filteredUsers() []users.UserInfo {
	if u.snap == nil {
		return nil
	}
	var out []users.UserInfo
	for _, usr := range u.snap.Users {
		if u.hideSystemUsers && usr.UID < 1000 && usr.UID != 0 {
			continue
		}
		if u.filter != "" && !strings.Contains(strings.ToLower(usr.Username), strings.ToLower(u.filter)) {
			continue
		}
		out = append(out, usr)
	}
	return out
}

func (u *Users) View() string {
	var panelTabs []string
	names := [userSubCount]string{"Users", "Groups", "Sudoers Rules", "SUID/SGID Audit", "SSH Security Policy"}
	for i, name := range names {
		if userSubPanel(i) == u.panel {
			panelTabs = append(panelTabs, styles.TabActive.Render(name))
		} else {
			panelTabs = append(panelTabs, styles.TabInactive.Render(name))
		}
	}
	tabBar := lipgloss.JoinHorizontal(lipgloss.Top, panelTabs...)

	var content string
	switch u.panel {
	case userSubUsers:
		content = u.viewUsers()
	case userSubGroups:
		content = u.viewGroups()
	case userSubSudoers:
		content = u.viewSudoers()
	case userSubSUID:
		content = u.viewSUID()
	case userSubSSH:
		content = u.viewSSHPolicy()
	}

	return styles.PanelStyle.Render(lipgloss.JoinVertical(lipgloss.Left, tabBar, content))
}

func (u *Users) viewUsers() string {
	count := 0
	if u.snap != nil {
		count = len(u.snap.Users)
	}
	title := styles.PanelTitleStyle.Render(fmt.Sprintf("  User Accounts: %d total", count))

	if u.snap == nil {
		return title + "\n  Loading user accounts…"
	}

	usrList := u.filteredUsers()

	actionLine := fmt.Sprintf("  Keys: [h] toggle system users (hiding: %v)  [a]dd user  [d]elete user", u.hideSystemUsers)
	if !privilege.CanManageUsers() {
		actionLine = styles.TextMuted.Render("  (Read-only mode — root privilege required to add/delete users)")
	}

	header := styles.TableHeader.Render(fmt.Sprintf("%-6s  %-16s  %-20s  %-22s  %-14s", "UID", "Username", "Full Name", "Home", "Shell"))
	var rows []string
	for i, usr := range usrList {
		style := styles.TableRow
		if i == u.cursor {
			style = styles.TableRowSelected
		}
		rows = append(rows, style.Render(fmt.Sprintf("%-6d  %-16s  %-20s  %-22s  %-14s",
			usr.UID,
			styles.Truncate(usr.Username, 16),
			styles.Truncate(usr.FullName, 20),
			styles.Truncate(usr.Home, 22),
			styles.Truncate(usr.Shell, 14),
		)))
	}

	// Viewport windowing based on cursor
	viewHeight := u.height - 10
	if viewHeight > 0 && len(rows) > viewHeight {
		startIdx := u.cursor - viewHeight/2
		if startIdx < 0 {
			startIdx = 0
		}
		endIdx := startIdx + viewHeight
		if endIdx > len(rows) {
			endIdx = len(rows)
			startIdx = endIdx - viewHeight
		}
		if startIdx < 0 {
			startIdx = 0
		}
		rows = rows[startIdx:endIdx]
	}

	// Add User Dialog
	addDialog := ""
	if u.addMode {
		addDialog = "\n\n  " + styles.TextYellow.Render("Enter new username: ") + u.addInput + "█"
	}

	// Delete User Dialog
	delDialog := ""
	if u.delMode == 1 && u.cursor < len(usrList) {
		delDialog = "\n\n  " + styles.TextRed.Render(fmt.Sprintf("Delete user '%s' and home directory? [y/N]", usrList[u.cursor].Username))
	}

	statusLine := ""
	if u.statusMsg != "" {
		statusLine = "\n  " + u.statusMsg
	}

	return strings.Join([]string{title, actionLine, header, strings.Join(rows, "\n"), addDialog, delDialog, statusLine}, "\n")
}

func (u *Users) viewGroups() string {
	count := 0
	if u.snap != nil {
		count = len(u.snap.Groups)
	}
	title := styles.PanelTitleStyle.Render(fmt.Sprintf("  System Groups: %d total", count))

	if u.snap == nil {
		return title + "\n  Loading groups…"
	}

	header := styles.TableHeader.Render(fmt.Sprintf("%-6s  %-20s  %-40s", "GID", "Group Name", "Members"))
	var rows []string
	for i, g := range u.snap.Groups {
		style := styles.TableRow
		if i == u.cursor {
			style = styles.TableRowSelected
		}
		members := strings.Join(g.Members, ", ")
		if len(g.Members) == 0 {
			members = "-"
		}
		rows = append(rows, style.Render(fmt.Sprintf("%-6d  %-20s  %-40s",
			g.GID,
			styles.Truncate(g.Name, 20),
			styles.Truncate(members, 40),
		)))
	}

	// Viewport windowing based on cursor
	viewHeight := u.height - 8
	if viewHeight > 0 && len(rows) > viewHeight {
		startIdx := u.cursor - viewHeight/2
		if startIdx < 0 {
			startIdx = 0
		}
		endIdx := startIdx + viewHeight
		if endIdx > len(rows) {
			endIdx = len(rows)
			startIdx = endIdx - viewHeight
		}
		if startIdx < 0 {
			startIdx = 0
		}
		rows = rows[startIdx:endIdx]
	}

	return strings.Join([]string{title, header, strings.Join(rows, "\n")}, "\n")
}

func (u *Users) viewSudoers() string {
	count := 0
	if u.snap != nil {
		count = len(u.snap.SudoRules)
	}
	title := styles.PanelTitleStyle.Render(fmt.Sprintf("  Sudoers Rules: %d rules loaded", count))

	actionLine := "  Keys: [a]dd sudoers rule  [d]elete custom rule file"
	if !privilege.CanManageUsers() {
		actionLine = styles.TextMuted.Render("  (Read-only mode — root privilege required to add/delete sudoers rules)")
	}

	if u.snap == nil || len(u.snap.SudoRules) == 0 {
		return title + "\n" + actionLine + "\n  No custom sudoers rules found or read access denied"
	}

	header := styles.TableHeader.Render(fmt.Sprintf("%-15s  %-10s  %-10s  %-30s  %-15s", "User/Group", "Host", "RunAs", "Command", "Source"))
	var rows []string
	for i, r := range u.snap.SudoRules {
		style := styles.TableRow
		if i == u.cursor {
			style = styles.TableRowSelected
		}
		noPassBadge := ""
		if r.NoPass {
			noPassBadge = " " + styles.TextRed.Render("(NOPASSWD)")
		}
		rows = append(rows, style.Render(fmt.Sprintf("%-15s  %-10s  %-10s  %-30s  %-15s%s",
			styles.Truncate(r.User, 15),
			r.Host,
			r.RunAs,
			styles.Truncate(r.Command, 30),
			r.Source,
			noPassBadge,
		)))
	}

	// Viewport windowing based on cursor
	viewHeight := u.height - 10
	if viewHeight > 0 && len(rows) > viewHeight {
		startIdx := u.cursor - viewHeight/2
		if startIdx < 0 {
			startIdx = 0
		}
		endIdx := startIdx + viewHeight
		if endIdx > len(rows) {
			endIdx = len(rows)
			startIdx = endIdx - viewHeight
		}
		if startIdx < 0 {
			startIdx = 0
		}
		rows = rows[startIdx:endIdx]
	}

	sudoDialog := ""
	if u.addSudoMode {
		if u.addSudoStep == 0 {
			sudoDialog = "\n\n  " + styles.TextYellow.Render("Step 1/3 — Enter target User/Group (e.g. 'john' or '%admin'): ") + u.addSudoUser + "█"
		} else if u.addSudoStep == 1 {
			sudoDialog = "\n\n  " + styles.TextYellow.Render(fmt.Sprintf("Step 2/3 — Enter allowed Command path for '%s' (e.g. 'ALL' or '/usr/bin/pacman'): ", u.addSudoUser)) + u.addSudoCmd + "█"
		} else if u.addSudoStep == 2 {
			sudoDialog = "\n\n  " + styles.TextYellow.Render(fmt.Sprintf("Step 3/3 — Grant NOPASSWD access for '%s'? [y/N]", u.addSudoUser))
		}
	}

	statusLine := ""
	if u.statusMsg != "" {
		statusLine = "\n  " + u.statusMsg
	}

	return strings.Join([]string{title, actionLine, header, strings.Join(rows, "\n"), sudoDialog, statusLine}, "\n")
}

func (u *Users) viewSUID() string {
	title := styles.PanelTitleStyle.Render("  SUID / SGID Privilege Escalation Risk Audit")
	if u.secSnap == nil {
		return title + "\n  Scanning SUID binaries…"
	}

	header := styles.TableHeader.Render(fmt.Sprintf("%-36s  %-10s  %-12s  %-16s", "Executable Path", "Owner", "Mode", "Risk Assessment"))
	var rows []string
	for i, b := range u.secSnap.SUIDBinaries {
		style := styles.TableRow
		if i == u.cursor {
			style = styles.TableRowSelected
		}
		riskBadge := styles.TextGreen.Render("STANDARD")
		if b.IsRisk {
			riskBadge = styles.TextRed.Render("⚠️ UNKNOWN / PRIVESC RISK")
		}
		rows = append(rows, style.Render(fmt.Sprintf("%-36s  %-10s  %-12s  %-16s",
			styles.Truncate(b.Path, 36),
			b.Owner,
			b.Permissions,
			riskBadge,
		)))
	}
	if len(rows) == 0 {
		rows = append(rows, styles.TextMuted.Render("  No SUID/SGID binaries found"))
	}

	// Viewport windowing based on cursor
	viewHeight := u.height - 8
	if viewHeight > 0 && len(rows) > viewHeight {
		startIdx := u.cursor - viewHeight/2
		if startIdx < 0 {
			startIdx = 0
		}
		endIdx := startIdx + viewHeight
		if endIdx > len(rows) {
			endIdx = len(rows)
			startIdx = endIdx - viewHeight
		}
		if startIdx < 0 {
			startIdx = 0
		}
		rows = rows[startIdx:endIdx]
	}

	return strings.Join([]string{title, header, strings.Join(rows, "\n")}, "\n")
}

func (u *Users) viewSSHPolicy() string {
	title := styles.PanelTitleStyle.Render("  SSH Daemon Security Policy Audit (/etc/ssh/sshd_config)")
	if u.secSnap == nil || !u.secSnap.SSHConfig.Configured {
		return title + "\n  Unable to read sshd_config or default configuration active"
	}
	cfg := u.secSnap.SSHConfig

	rootStyle := styles.TextGreen
	if cfg.PermitRootLogin == "yes" {
		rootStyle = styles.TextRed
	} else if cfg.PermitRootLogin == "prohibit-password" {
		rootStyle = styles.TextYellow
	}

	pwdStyle := styles.TextGreen
	if cfg.PasswordAuthentication == "yes" {
		pwdStyle = styles.TextYellow
	}

	lines := []string{
		fmt.Sprintf("  PermitRootLogin:         %s", rootStyle.Render(cfg.PermitRootLogin)),
		fmt.Sprintf("  PasswordAuthentication:  %s", pwdStyle.Render(cfg.PasswordAuthentication)),
		fmt.Sprintf("  PubkeyAuthentication:    %s", styles.TextGreen.Render(cfg.PubkeyAuthentication)),
		fmt.Sprintf("  X11Forwarding:           %s", cfg.X11Forwarding),
		fmt.Sprintf("  MaxAuthTries:            %d", cfg.MaxAuthTries),
	}
	return title + "\n" + strings.Join(lines, "\n")
}
