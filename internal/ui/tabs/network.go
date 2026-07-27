package tabs

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"blackeye/internal/bus"
	"blackeye/internal/config"
	"blackeye/internal/resolver"
	"blackeye/internal/services/network"
	"blackeye/internal/services/netstats"
	"blackeye/internal/services/ports"
	"blackeye/internal/services/routing"
	"blackeye/internal/ui/styles"
)

type networkSubPanel int

const (
	subPanelInterfaces networkSubPanel = iota
	subPanelPorts
	subPanelConnections
	subPanelRouting
	subPanelStats
	subPanelCount
)

var subPanelNames = [subPanelCount]string{
	"Interfaces", "Listeners", "Connections", "Routing", "Statistics",
}

// Network is the Tab 3 model.
type Network struct {
	width, height int
	cfg           config.Config

	subNet      <-chan interface{}
	subPorts    <-chan interface{}
	subRouting  <-chan interface{}
	subNetstats <-chan interface{}

	netSnap      *network.Snapshot
	portsSnap    *ports.PortsSnapshot
	routingSnap  *routing.Snapshot
	netstatsSnap *netstats.Snapshot

	panel      networkSubPanel
	cursor     int
	filter     string
	filterMode bool
}

func NewNetwork(b *bus.Bus, cfg config.Config) *Network {
	n := &Network{
		cfg:         cfg,
		subNet:      b.Subscribe("network"),
		subPorts:    b.Subscribe("ports"),
		subRouting:  b.Subscribe("routing"),
		subNetstats: b.Subscribe("netstats"),
	}
	return n
}

func (n *Network) Init() tea.Cmd {
	return tea.Batch(
		listenChan(n.subNet, "network"),
		listenChan(n.subPorts, "ports"),
		listenChan(n.subRouting, "routing"),
		listenChan(n.subNetstats, "netstats"),
	)
}

func (n *Network) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		n.width, n.height = m.Width, m.Height
	case busMsg:
		var cmd tea.Cmd
		switch m.ch {
		case n.subNet:
			if v, ok := m.data.(network.Snapshot); ok {
				n.netSnap = &v
			}
			cmd = listenChan(n.subNet, "network")
		case n.subPorts:
			if v, ok := m.data.(ports.PortsSnapshot); ok {
				n.portsSnap = &v
			}
			cmd = listenChan(n.subPorts, "ports")
		case n.subRouting:
			if v, ok := m.data.(routing.Snapshot); ok {
				n.routingSnap = &v
			}
			cmd = listenChan(n.subRouting, "routing")
		case n.subNetstats:
			if v, ok := m.data.(netstats.Snapshot); ok {
				n.netstatsSnap = &v
			}
			cmd = listenChan(n.subNetstats, "netstats")
		}
		return n, cmd
	case tea.KeyMsg:
		switch m.String() {
		case "tab":
			n.panel = (n.panel + 1) % subPanelCount
			n.cursor = 0
		case "up":
			if n.cursor > 0 {
				n.cursor--
			}
		case "down", "j":
			max := 0
			if n.panel == subPanelInterfaces && n.netSnap != nil {
				for _, iface := range n.netSnap.Ifaces {
					if n.filter == "" || strings.Contains(strings.ToLower(iface.DisplayName), strings.ToLower(n.filter)) {
						max++
					}
				}
			} else if n.panel == subPanelPorts && n.portsSnap != nil {
				for _, l := range n.portsSnap.Listeners {
					if n.filter == "" || strings.Contains(strings.ToLower(l.DisplayService), strings.ToLower(n.filter)) {
						max++
					}
				}
			} else if n.panel == subPanelConnections && n.portsSnap != nil {
				for _, c := range n.portsSnap.Connections {
					if n.filter == "" || strings.Contains(strings.ToLower(c.RemoteDisplay), strings.ToLower(n.filter)) || strings.Contains(strings.ToLower(c.LocalDisplay), strings.ToLower(n.filter)) || strings.Contains(strings.ToLower(c.DisplayProcess), strings.ToLower(n.filter)) {
						max++
					}
				}
			} else if n.panel == subPanelRouting && n.routingSnap != nil {
				for _, r := range n.routingSnap.Routes {
					if n.filter == "" || strings.Contains(strings.ToLower(r.Destination), strings.ToLower(n.filter)) || strings.Contains(strings.ToLower(r.DisplayIface), strings.ToLower(n.filter)) {
						max++
					}
				}
			}
			if max > 0 && n.cursor < max-1 {
				n.cursor++
			}
		case "f":
			if n.panel == subPanelPorts && n.portsSnap != nil && len(n.portsSnap.Listeners) > 0 {
				idx := 0
				for _, l := range n.portsSnap.Listeners {
					if n.filter != "" && !strings.Contains(strings.ToLower(l.DisplayService), strings.ToLower(n.filter)) {
						continue
					}
					if idx == n.cursor {
						portStr := fmt.Sprintf("%d", l.RawPort)
						protoStr := strings.ToLower(l.Protocol)
						return n, func() tea.Msg {
							return JumpToTabMsg{
								TabIndex: 6, // TabFirewall
								Payload: FirewallPrefill{
									Port:   portStr,
									Proto:  protoStr,
									Action: "DROP",
								},
							}
						}
					}
					idx++
				}
			}
		case "k":
			if n.panel == subPanelConnections && n.portsSnap != nil && len(n.portsSnap.Connections) > 0 {
				idx := 0
				for _, c := range n.portsSnap.Connections {
					if n.filter != "" && !strings.Contains(strings.ToLower(c.RemoteDisplay), strings.ToLower(n.filter)) && !strings.Contains(strings.ToLower(c.LocalDisplay), strings.ToLower(n.filter)) && !strings.Contains(strings.ToLower(c.DisplayProcess), strings.ToLower(n.filter)) {
						continue
					}
					if idx == n.cursor && c.PID > 0 {
						proc, err := os.FindProcess(c.PID)
						if err == nil {
							_ = proc.Signal(syscall.SIGTERM)
						}
						break
					}
					idx++
				}
			}
		case "/":
			n.filterMode = !n.filterMode
			if !n.filterMode {
				n.filter = ""
			}
		case "esc":
			n.filterMode, n.filter = false, ""
		default:
			if n.filterMode && len(m.String()) == 1 {
				if filterRe.MatchString(n.filter + m.String()) {
					n.filter += m.String()
					n.cursor = 0 // Reset cursor on filter change
				}
			} else if n.filterMode && m.String() == "backspace" && len(n.filter) > 0 {
				n.filter = n.filter[:len(n.filter)-1]
				n.cursor = 0 // Reset cursor on filter change
			}
		}
	}
	return n, nil
}

func (n *Network) View() string {
	// Sub-panel tab bar.
	var panelTabs []string
	for i, name := range subPanelNames {
		if networkSubPanel(i) == n.panel {
			panelTabs = append(panelTabs, styles.TabActive.Render(name))
		} else {
			panelTabs = append(panelTabs, styles.TabInactive.Render(name))
		}
	}
	tabBar := lipgloss.JoinHorizontal(lipgloss.Top, panelTabs...)

	var content string
	switch n.panel {
	case subPanelInterfaces:
		content = n.viewInterfaces()
	case subPanelPorts:
		content = n.viewListeners()
	case subPanelConnections:
		content = n.viewConnections()
	case subPanelRouting:
		content = n.viewRouting()
	case subPanelStats:
		content = n.viewStats()
	}

	filterBar := ""
	if n.filterMode {
		filterBar = "\n  Filter: " + styles.TextAccent.Render(n.filter) + "█"
	}

	return styles.PanelStyle.Render(lipgloss.JoinVertical(lipgloss.Left,
		tabBar, content, filterBar,
	))
}

func (n *Network) viewInterfaces() string {
	title := styles.PanelTitleStyle.Render("  Network Interfaces")
	if n.netSnap == nil {
		return title + "\n  Loading…"
	}
	var rows []string
	rows = append(rows, styles.TableHeader.Render(
		fmt.Sprintf("%-24s  %-13s  %-13s  %-14s  %-14s  %-8s",
			"Interface", "↓ Recv Rate", "↑ Send Rate", "Total Recv", "Total Send", "Errors"),
	))
	for _, iface := range n.netSnap.Ifaces {
		if n.filter != "" && !strings.Contains(strings.ToLower(iface.DisplayName), strings.ToLower(n.filter)) {
			continue
		}
		errStyle := styles.TextNormal
		if iface.RxErrors > 0 || iface.TxErrors > 0 {
			errStyle = styles.TextRed
		}
		errStr := fmt.Sprintf("%d/%d", iface.RxErrors, iface.TxErrors)

		rxRateStr := resolver.FormatRate(iface.RxBps)
		txRateStr := resolver.FormatRate(iface.TxBps)
		rxTotalStr := resolver.FormatBytes(iface.RxBytesTotal)
		txTotalStr := resolver.FormatBytes(iface.TxBytesTotal)

		rows = append(rows, fmt.Sprintf("%-24s  %-13s  %-13s  %-14s  %-14s  %-8s",
			styles.Truncate(iface.DisplayName, 24),
			rxRateStr,
			txRateStr,
			rxTotalStr,
			txTotalStr,
			errStyle.Render(errStr),
		))
	}
	return title + "\n" + strings.Join(rows, "\n")
}

func (n *Network) viewListeners() string {
	title := styles.PanelTitleStyle.Render("  Listening Services (press 'f' to add firewall rule)")
	if n.portsSnap == nil {
		return title + "\n  Loading…"
	}
	var rows []string
	rows = append(rows, styles.TableHeader.Render(
		fmt.Sprintf("%-32s  %-5s  %-22s  %-20s  %-12s",
			"Service", "Proto", "Listening On", "Process", "Owner"),
	))
	filteredIdx := 0
	for _, l := range n.portsSnap.Listeners {
		if n.filter != "" && !strings.Contains(strings.ToLower(l.DisplayService), strings.ToLower(n.filter)) {
			continue
		}
		style := styles.TableRow
		if filteredIdx == n.cursor {
			style = styles.TableRowSelected
		}
		if l.Flagged || l.IsUnencrypted {
			style = styles.TableRowFlagged
		}
		flag := "  "
		if l.IsUnencrypted {
			flag = styles.TextRed.Render("⚠ UNENCRYPTED ")
		} else if l.Flagged {
			flag = "⚠ "
		}
		rows = append(rows, style.Render(fmt.Sprintf("%s%-30s  %-5s  %-22s  %-20s  %-12s",
			flag,
			styles.Truncate(l.DisplayService, 30),
			l.Protocol,
			styles.Truncate(l.DisplayScope, 22),
			styles.Truncate(l.DisplayProcess, 20),
			l.Owner,
		)))
		filteredIdx++
	}
	if len(rows) == 1 {
		rows = append(rows, styles.TextMuted.Render("  No active listeners found"))
	}

	// Viewport windowing based on cursor.
	viewHeight := n.height - 12
	if viewHeight > 0 && len(rows) > viewHeight {
		header := rows[0]
		dataRows := rows[1:]
		
		startIdx := n.cursor - viewHeight/2
		if startIdx < 0 {
			startIdx = 0
		}
		endIdx := startIdx + viewHeight
		if endIdx > len(dataRows) {
			endIdx = len(dataRows)
			startIdx = endIdx - viewHeight
		}
		rows = append([]string{header}, dataRows[startIdx:endIdx]...)
	}

	return title + "\n" + strings.Join(rows, "\n")
}

func (n *Network) viewConnections() string {
	title := styles.PanelTitleStyle.Render("  Active Connections (press 'k' to terminate socket process)")
	if n.portsSnap == nil {
		return title + "\n  Loading…"
	}
	var rows []string
	rows = append(rows, styles.TableHeader.Render(
		fmt.Sprintf("%-36s  %-26s  %-10s  %-24s  %-18s",
			"Local", "Remote", "Scope", "State", "Process"),
	))
	filteredIdx := 0
	for _, c := range n.portsSnap.Connections {
		if n.filter != "" && !strings.Contains(strings.ToLower(c.RemoteDisplay), strings.ToLower(n.filter)) && !strings.Contains(strings.ToLower(c.LocalDisplay), strings.ToLower(n.filter)) && !strings.Contains(strings.ToLower(c.DisplayProcess), strings.ToLower(n.filter)) {
			continue
		}
		style := styles.TableRow
		if filteredIdx == n.cursor {
			style = styles.TableRowSelected
		}
		scopeBadge := styles.TextMuted.Render("🔒 LAN")
		if c.IsPublicWAN {
			scopeBadge = styles.TextYellow.Render("🌐 WAN")
		}
		rows = append(rows, style.Render(fmt.Sprintf("%-36s  %-26s  %-10s  %-24s  %-18s",
			styles.Truncate(c.LocalDisplay, 36),
			styles.Truncate(c.RemoteDisplay, 26),
			scopeBadge,
			styles.Truncate(c.DisplayState, 24),
			styles.Truncate(c.DisplayProcess, 18),
		)))
		filteredIdx++
	}

	// Viewport windowing based on cursor.
	viewHeight := n.height - 12
	if viewHeight > 0 && len(rows) > viewHeight {
		header := rows[0]
		dataRows := rows[1:]
		
		startIdx := n.cursor - viewHeight/2
		if startIdx < 0 {
			startIdx = 0
		}
		endIdx := startIdx + viewHeight
		if endIdx > len(dataRows) {
			endIdx = len(dataRows)
			startIdx = endIdx - viewHeight
		}
		rows = append([]string{header}, dataRows[startIdx:endIdx]...)
	}

	return title + "\n" + strings.Join(rows, "\n")
}

func (n *Network) viewRouting() string {
	title := styles.PanelTitleStyle.Render("  Routing Table")
	if n.routingSnap == nil {
		return title + "\n  Loading…"
	}
	var rows []string
	rows = append(rows, styles.TableHeader.Render(
		fmt.Sprintf("%-34s  %-16s  %-24s  %-6s  %-6s",
			"Destination", "Gateway", "Interface", "Flags", "Metric"),
	))
	for _, r := range n.routingSnap.Routes {
		rows = append(rows, fmt.Sprintf("%-34s  %-16s  %-24s  %-6s  %-6d",
			styles.Truncate(r.Destination, 34),
			r.Gateway,
			styles.Truncate(r.DisplayIface, 24),
			r.Flags, r.Metric,
		))
	}
	if len(n.routingSnap.ARP) > 0 {
		rows = append(rows, "", styles.SectionTitle.Render("  ARP Cache"))
		rows = append(rows, styles.TableHeader.Render(
			fmt.Sprintf("%-16s  %-20s  %-24s  %-10s", "IP", "MAC", "Interface", "State"),
		))
		for _, a := range n.routingSnap.ARP {
			rows = append(rows, fmt.Sprintf("%-16s  %-20s  %-24s  %-10s",
				a.IP, a.MAC, styles.Truncate(a.DisplayIface, 24), a.State,
			))
		}
	}
	return title + "\n" + strings.Join(rows, "\n")
}

func (n *Network) viewStats() string {
	title := styles.PanelTitleStyle.Render("  Network Statistics")
	if n.netstatsSnap == nil {
		return title + "\n  Loading…"
	}
	s := n.netstatsSnap
	lines := []string{
		fmt.Sprintf("  TCP Retransmitted Segments:  %d", s.TCPRetransmits),
		fmt.Sprintf("  TCP Receive Errors:          %d", s.TCPErrors),
		fmt.Sprintf("  UDP Dropped Packets:         %d", s.UDPDropped),
		fmt.Sprintf("  ICMP Errors:                 %d", s.ICMPErrors),
	}
	return title + "\n" + strings.Join(lines, "\n")
}
