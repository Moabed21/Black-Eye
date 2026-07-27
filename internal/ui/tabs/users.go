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
	"blackeye/internal/services/users"
	"blackeye/internal/ui/styles"
)

type userSubPanel int

const (
	userSubUsers userSubPanel = iota
	userSubGroups
	userSubSudoers
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
	userSvc       *users.Service
	auditSvc      *audit.Service

	snap       *users.Snapshot
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
}

func NewUsers(b *bus.Bus, cfg config.Config) *Users {
	return &Users{
		cfg:             cfg,
		sub:             b.Subscribe("users"),
		hideSystemUsers: true,
	}
}

func (u *Users) IsInputActive() bool {
	return u.addMode || u.delMode > 0 || u.filterMode
}

func (u *Users) SetUsers(svc *users.Service) { u.userSvc = svc }
func (u *Users) SetAudit(a *audit.Service)   { u.auditSvc = a }

func (u *Users) Init() tea.Cmd {
	return listenChan(u.sub, "users")
}

func (u *Users) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		u.width, u.height = m.Width, m.Height
	case busMsg:
		if m.ch == u.sub {
			if v, ok := m.data.(users.Snapshot); ok {
				u.snap = &v
			}
			return u, listenChan(u.sub, "users")
		}
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
		u.cursor++
	case "/":
		u.filterMode = true
		u.filter = ""
	case "h":
		u.hideSystemUsers = !u.hideSystemUsers
	case "a":
		if privilege.CanManageUsers() {
			u.addMode = true
			u.addInput = ""
		}
	case "d":
		if privilege.CanManageUsers() {
			usrList := u.filteredUsers()
			if u.cursor < len(usrList) {
				u.delMode = 1
			}
		}
	}
	return u, nil
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
	names := [userSubCount]string{"Users", "Groups", "Sudoers Rules"}
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

	header := styles.TableHeader.Render(fmt.Sprintf("%-6s  %-15s  %-15s  %-20s  %-15s",
		"UID", "Username", "Full Name", "Home Directory", "Shell",
	))

	var rows []string
	for i, usr := range usrList {
		style := styles.TableRow
		if i == u.cursor {
			style = styles.TableRowSelected
		}
		rows = append(rows, style.Render(fmt.Sprintf("%-6d  %-15s  %-15s  %-20s  %-15s",
			usr.UID,
			styles.Truncate(usr.Username, 15),
			styles.Truncate(usr.FullName, 15),
			styles.Truncate(usr.Home, 20),
			styles.Truncate(usr.Shell, 15),
		)))
	}
	if len(rows) == 0 {
		rows = append(rows, styles.TextMuted.Render("  No matching users found"))
	}

	// Add User Dialog
	addDialog := ""
	if u.addMode {
		addDialog = "\n\n  " + styles.TextYellow.Render("Add User — Enter username: "+u.addInput+"█  (Enter to confirm, ESC to cancel)")
	}

	// Delete User Dialog
	delDialog := ""
	if u.delMode > 0 && u.cursor < len(usrList) {
		delDialog = "\n\n  " + styles.TextRed.Render(fmt.Sprintf("Delete user account %s (UID %d)? [y/N]", usrList[u.cursor].Username, usrList[u.cursor].UID))
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

	if u.snap == nil || len(u.snap.SudoRules) == 0 {
		return title + "\n  No custom sudoers rules found or read access denied"
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
			noPassBadge = " " + styles.TextYellow.Render("(NOPASSWD)")
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
	return strings.Join([]string{title, header, strings.Join(rows, "\n")}, "\n")
}
