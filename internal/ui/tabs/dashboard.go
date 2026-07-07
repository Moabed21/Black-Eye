package tabs

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"blackeye/internal/bus"
	"blackeye/internal/config"
	"blackeye/internal/resolver"
	"blackeye/internal/services/cpu"
	"blackeye/internal/services/disk"
	iosvc "blackeye/internal/services/io"
	"blackeye/internal/services/memory"
	"blackeye/internal/services/network"
	"blackeye/internal/services/swap"
	"blackeye/internal/services/sysinfo"
	"blackeye/internal/services/thermal"
	"blackeye/internal/ui/styles"
)

// Dashboard is the Tab 1 model. It subscribes to:
// cpu, memory, swap, disk, io, network, thermal, sysinfo
type Dashboard struct {
	width  int
	height int
	cfg    config.Config
	bus    *bus.Bus

	// Latest snapshots from each service.
	cpuSnap     *cpu.Snapshot
	memSnap     *memory.Snapshot
	swapSnap    *swap.Snapshot
	diskSnap    *disk.Snapshot
	ioSnap      *iosvc.Snapshot
	netSnap     *network.Snapshot
	thermalSnap *thermal.Snapshot
	sysSnap     *sysinfo.Snapshot

	// Bus subscriptions.
	subCPU     <-chan interface{}
	subMem     <-chan interface{}
	subSwap    <-chan interface{}
	subDisk    <-chan interface{}
	subIO      <-chan interface{}
	subNet     <-chan interface{}
	subThermal <-chan interface{}
	subSys     <-chan interface{}

	// Scroll state.
	scrollOffset int
}

func NewDashboard(b *bus.Bus, cfg config.Config) *Dashboard {
	d := &Dashboard{bus: b, cfg: cfg}
	d.subCPU = b.Subscribe("cpu")
	d.subMem = b.Subscribe("memory")
	d.subSwap = b.Subscribe("swap")
	d.subDisk = b.Subscribe("disk")
	d.subIO = b.Subscribe("io")
	d.subNet = b.Subscribe("network")
	d.subThermal = b.Subscribe("thermal")
	d.subSys = b.Subscribe("sysinfo")
	return d
}

func (d *Dashboard) Init() tea.Cmd {
	return d.listenAll()
}

func (d *Dashboard) listenAll() tea.Cmd {
	return func() tea.Msg {
		select {
		case v := <-d.subCPU:
			return busMsg{"cpu", v}
		case v := <-d.subMem:
			return busMsg{"memory", v}
		case v := <-d.subSwap:
			return busMsg{"swap", v}
		case v := <-d.subDisk:
			return busMsg{"disk", v}
		case v := <-d.subIO:
			return busMsg{"io", v}
		case v := <-d.subNet:
			return busMsg{"network", v}
		case v := <-d.subThermal:
			return busMsg{"thermal", v}
		case v := <-d.subSys:
			return busMsg{"sysinfo", v}
		}
	}
}

func (d *Dashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		d.width = m.Width
		d.height = m.Height
	case busMsg:
		switch m.topic {
		case "cpu":
			if v, ok := m.data.(cpu.Snapshot); ok {
				d.cpuSnap = &v
			}
		case "memory":
			if v, ok := m.data.(memory.Snapshot); ok {
				d.memSnap = &v
			}
		case "swap":
			if v, ok := m.data.(swap.Snapshot); ok {
				d.swapSnap = &v
			}
		case "disk":
			if v, ok := m.data.(disk.Snapshot); ok {
				d.diskSnap = &v
			}
		case "io":
			if v, ok := m.data.(iosvc.Snapshot); ok {
				d.ioSnap = &v
			}
		case "network":
			if v, ok := m.data.(network.Snapshot); ok {
				d.netSnap = &v
			}
		case "thermal":
			if v, ok := m.data.(thermal.Snapshot); ok {
				d.thermalSnap = &v
			}
		case "sysinfo":
			if v, ok := m.data.(sysinfo.Snapshot); ok {
				d.sysSnap = &v
			}
		}
		return d, d.listenAll()
	case tea.KeyMsg:
		switch m.String() {
		case "up", "k":
			d.scrollOffset--
		case "down", "j":
			d.scrollOffset++
		case "pgup":
			d.scrollOffset -= d.height / 2
		case "pgdown":
			d.scrollOffset += d.height / 2
		case "home":
			d.scrollOffset = 0
		}
	}
	return d, nil
}

func (d *Dashboard) View() string {
	if d.width == 0 {
		return styles.TextMuted.Render("  Waiting for data…")
	}

	var sections []string

	// System Info panel.
	sections = append(sections, d.renderSysInfo())
	// CPU panel.
	sections = append(sections, d.renderCPU())
	// Memory + Swap panel.
	sections = append(sections, d.renderMemory())
	// Disk panel.
	sections = append(sections, d.renderDisk())
	// Network panel.
	sections = append(sections, d.renderNetwork())

	content := lipgloss.JoinVertical(lipgloss.Left, sections...)

	// Implement manual scrolling by splitting into lines and slicing.
	lines := strings.Split(content, "\n")
	
	// The root app model has a 2-line tab bar and 1-line status bar.
	// We reserve an extra line for safety padding.
	viewHeight := d.height - 4
	if viewHeight <= 0 {
		return content
	}

	maxScroll := len(lines) - viewHeight
	if maxScroll < 0 {
		maxScroll = 0
	}

	if d.scrollOffset > maxScroll {
		d.scrollOffset = maxScroll
	}
	if d.scrollOffset < 0 {
		d.scrollOffset = 0
	}

	endIdx := d.scrollOffset + viewHeight
	if endIdx > len(lines) {
		endIdx = len(lines)
	}

	visibleLines := lines[d.scrollOffset:endIdx]
	
	// Add scroll indicator if we're not seeing everything.
	if maxScroll > 0 {
		indicator := fmt.Sprintf("  Scroll: %d/%d (Use ↑/↓ or PgUp/PgDn)", d.scrollOffset, maxScroll)
		visibleLines = append(visibleLines, styles.TextMuted.Render(indicator))
	}

	return strings.Join(visibleLines, "\n")
}

func (d *Dashboard) renderSysInfo() string {
	title := styles.PanelTitleStyle.Render("  System Info")
	if d.sysSnap == nil {
		return styles.PanelStyle.Render(title + "\n  Loading…")
	}
	s := d.sysSnap
	line1 := fmt.Sprintf("  Hostname: %s  │  Uptime: %s  │  Kernel: %s",
		styles.TextAccent.Render(s.Hostname), s.Uptime, s.KernelVersion)
	line2 := fmt.Sprintf("  CPU: %s  │  Cores: %d  │  Threads: %d",
		s.CPUModel, s.CPUCores, s.CPUThreads)
	line3 := fmt.Sprintf("  Load avg:  1m: %.2f  │  5m: %.2f  │  15m: %.2f",
		s.LoadAvg1, s.LoadAvg5, s.LoadAvg15)
	return styles.PanelStyle.Render(title + "\n" + line1 + "\n" + line2 + "\n" + line3)
}

func (d *Dashboard) renderCPU() string {
	title := styles.PanelTitleStyle.Render("  CPU")
	if d.cpuSnap == nil {
		return styles.PanelStyle.Render(title + "\n  Loading…")
	}
	s := d.cpuSnap
	warn := d.cfg.Alerts.CPUWarning
	crit := d.cfg.Alerts.CPUCritical
	barW := 30

	totalBar := styles.Bar(s.TotalPercent, barW, warn, crit)
	totalLine := fmt.Sprintf("  Total:  %s  %s",
		styles.Colorize(fmt.Sprintf("%5.1f%%", s.TotalPercent), s.TotalPercent, warn, crit),
		totalBar)

	var coreLines []string
	for i, p := range s.CorePercent {
		bar := styles.Bar(p, 20, warn, crit)
		coreLines = append(coreLines, fmt.Sprintf("  Core%-2d: %s  %s",
			i, styles.Colorize(fmt.Sprintf("%5.1f%%", p), p, warn, crit), bar))
	}

	tempLine := ""
	if d.thermalSnap != nil && len(d.thermalSnap.Zones) > 0 {
		z := d.thermalSnap.Zones[0]
		tc := resolver.FormatTemp(z.TempCelsius)
		tempLine = "\n  Temperature: " + styles.ColorizeTemp(tc, z.TempCelsius,
			d.cfg.Alerts.TempWarning, d.cfg.Alerts.TempCritical)
	}

	body := title + "\n" + totalLine + "\n" + strings.Join(coreLines, "\n") + tempLine
	return styles.PanelStyle.Render(body)
}

func (d *Dashboard) renderMemory() string {
	title := styles.PanelTitleStyle.Render("  Memory")
	var lines []string

	warn := d.cfg.Alerts.MemoryWarning
	crit := 90.0

	if d.memSnap != nil {
		m := d.memSnap
		bar := styles.Bar(m.UsedPercent, 30, warn, crit)
		lines = append(lines,
			fmt.Sprintf("  RAM:  %s / %s  %s  %s",
				styles.Colorize(resolver.FormatBytes(uint64(m.UsedGiB*1024*1024*1024)), m.UsedPercent, warn, crit),
				resolver.FormatBytes(uint64(m.TotalGiB*1024*1024*1024)),
				bar,
				styles.Colorize(fmt.Sprintf("%.0f%%", m.UsedPercent), m.UsedPercent, warn, crit),
			),
			fmt.Sprintf("  Buffers: %s  │  Cache: %s  │  Available: %s",
				resolver.FormatBytes(uint64(m.BuffersGiB*1024*1024*1024)),
				resolver.FormatBytes(uint64(m.CachedGiB*1024*1024*1024)),
				resolver.FormatBytes(uint64(m.AvailableGiB*1024*1024*1024)),
			),
		)
	} else {
		lines = append(lines, "  Loading…")
	}

	if d.swapSnap != nil {
		s := d.swapSnap
		bar := styles.Bar(s.UsedPercent, 30, warn, crit)
		lines = append(lines,
			fmt.Sprintf("  Swap: %s / %s  %s  %s",
				resolver.FormatBytes(uint64(s.UsedGiB*1024*1024*1024)),
				resolver.FormatBytes(uint64(s.TotalGiB*1024*1024*1024)),
				bar,
				styles.Colorize(fmt.Sprintf("%.0f%%", s.UsedPercent), s.UsedPercent, warn, crit),
			),
		)
	}

	return styles.PanelStyle.Render(title + "\n" + strings.Join(lines, "\n"))
}

func (d *Dashboard) renderDisk() string {
	title := styles.PanelTitleStyle.Render("  Disk")
	if d.diskSnap == nil {
		return styles.PanelStyle.Render(title + "\n  Loading…")
	}
	warn := d.cfg.Alerts.DiskWarning
	crit := 95.0

	var lines []string
	for _, dk := range d.diskSnap.Disks {
		bar := styles.Bar(dk.UsedPercent, 20, warn, crit)
		ioLine := ""
		if d.ioSnap != nil {
			for _, dev := range d.ioSnap.Devices {
				if strings.Contains(dk.Device, dev.Device) {
					ioLine = fmt.Sprintf("  I/O: ↓ %.1f MB/s  ↑ %.1f MB/s", dev.ReadMBs, dev.WriteMBs)
					break
				}
			}
		}
		inodeLine := fmt.Sprintf("  Inodes: %s used  %.0f%%",
			resolver.FormatBytes(dk.InodesTotal-dk.InodesFree),
			dk.InodesPercent)

		lines = append(lines, fmt.Sprintf("  %s\n    %s / %s  %s  %s%s%s",
			styles.TextAccent.Render(dk.DisplayMount),
			styles.Colorize(resolver.FormatBytes(uint64(dk.UsedGiB*1024*1024*1024)), dk.UsedPercent, warn, crit),
			resolver.FormatBytes(uint64(dk.TotalGiB*1024*1024*1024)),
			bar,
			styles.Colorize(fmt.Sprintf("%.0f%%", dk.UsedPercent), dk.UsedPercent, warn, crit),
			ioLine,
			inodeLine,
		))
	}

	if len(lines) == 0 {
		lines = append(lines, "  No mounted filesystems found")
	}
	return styles.PanelStyle.Render(title + "\n" + strings.Join(lines, "\n"))
}

func (d *Dashboard) renderNetwork() string {
	title := styles.PanelTitleStyle.Render("  Network")
	if d.netSnap == nil {
		return styles.PanelStyle.Render(title + "\n  Loading…")
	}
	var lines []string
	for _, iface := range d.netSnap.Ifaces {
		errStr := ""
		if iface.RxErrors > 0 || iface.TxErrors > 0 {
			errStr = styles.TextRed.Render(fmt.Sprintf("  ⚠ errors: rx=%d tx=%d", iface.RxErrors, iface.TxErrors))
		}
		lines = append(lines, fmt.Sprintf("  %s  ↓ %s  ↑ %s%s",
			styles.TextAccent.Render(iface.DisplayName),
			resolver.FormatRate(iface.RxMBs*1024*1024),
			resolver.FormatRate(iface.TxMBs*1024*1024),
			errStr,
		))
	}
	if len(lines) == 0 {
		lines = append(lines, "  No active interfaces")
	}
	return styles.PanelStyle.Render(title + "\n" + lipgloss.JoinVertical(lipgloss.Left, lines...))
}
