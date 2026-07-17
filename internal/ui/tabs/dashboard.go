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

	// Drag & Drop Layout state.
	layout     [][]string
	bounds     map[string]PanelBounds
	dragTarget string
}

type PanelBounds struct {
	ID   string
	X, Y int
	W, H int
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
	
	// Default layout: stacked vertically.
	d.layout = [][]string{
		{"sysinfo"},
		{"cpu"},
		{"memory"},
		{"disk"},
		{"network"},
	}
	d.bounds = make(map[string]PanelBounds)

	return d
}

func (d *Dashboard) Init() tea.Cmd {
	return tea.Batch(
		listenChan(d.subCPU, "cpu"),
		listenChan(d.subMem, "memory"),
		listenChan(d.subSwap, "swap"),
		listenChan(d.subDisk, "disk"),
		listenChan(d.subIO, "io"),
		listenChan(d.subNet, "network"),
		listenChan(d.subThermal, "thermal"),
		listenChan(d.subSys, "sysinfo"),
	)
}

func (d *Dashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		d.width = m.Width
		d.height = m.Height
	case busMsg:
		var cmd tea.Cmd
		switch m.ch {
		case d.subCPU:
			if v, ok := m.data.(cpu.Snapshot); ok {
				d.cpuSnap = &v
			}
			cmd = listenChan(d.subCPU, "cpu")
		case d.subMem:
			if v, ok := m.data.(memory.Snapshot); ok {
				d.memSnap = &v
			}
			cmd = listenChan(d.subMem, "memory")
		case d.subSwap:
			if v, ok := m.data.(swap.Snapshot); ok {
				d.swapSnap = &v
			}
			cmd = listenChan(d.subSwap, "swap")
		case d.subDisk:
			if v, ok := m.data.(disk.Snapshot); ok {
				d.diskSnap = &v
			}
			cmd = listenChan(d.subDisk, "disk")
		case d.subIO:
			if v, ok := m.data.(iosvc.Snapshot); ok {
				d.ioSnap = &v
			}
			cmd = listenChan(d.subIO, "io")
		case d.subNet:
			if v, ok := m.data.(network.Snapshot); ok {
				d.netSnap = &v
			}
			cmd = listenChan(d.subNet, "network")
		case d.subThermal:
			if v, ok := m.data.(thermal.Snapshot); ok {
				d.thermalSnap = &v
			}
			cmd = listenChan(d.subThermal, "thermal")
		case d.subSys:
			if v, ok := m.data.(sysinfo.Snapshot); ok {
				d.sysSnap = &v
			}
			cmd = listenChan(d.subSys, "sysinfo")
		}
		return d, cmd
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
	case tea.MouseMsg:
		if m.Action == tea.MouseActionPress && m.Button == tea.MouseButtonLeft {
			id := d.findPanelAt(m.X, m.Y)
			if id != "" {
				d.dragTarget = id
			}
		} else if m.Action == tea.MouseActionRelease && m.Button == tea.MouseButtonLeft {
			if d.dragTarget != "" {
				dropTarget := d.findPanelAt(m.X, m.Y)
				if dropTarget != "" && dropTarget != d.dragTarget {
					d.handleDrop(d.dragTarget, dropTarget, m.X, m.Y)
				}
				d.dragTarget = ""
			}
		}
	}
	return d, nil
}

func (d *Dashboard) findPanelAt(x, y int) string {
	for id, b := range d.bounds {
		if x >= b.X && x < b.X+b.W && y >= b.Y && y < b.Y+b.H {
			return id
		}
	}
	return ""
}

func (d *Dashboard) handleDrop(src, dst string, x, y int) {
	// Find bounding box of dst
	b, ok := d.bounds[dst]
	if !ok {
		return
	}
	
	// Determine drop zone (Left/Right 25%, Top/Bottom 25%, Center 50%)
	relX := x - b.X
	relY := y - b.Y
	
	action := "swap"
	if relX < b.W/4 {
		action = "left"
	} else if relX > b.W*3/4 {
		action = "right"
	} else if relY < b.H/4 {
		action = "top"
	} else if relY > b.H*3/4 {
		action = "bottom"
	}

	// Remove src from layout
	var srcStr string
	var newLayout [][]string
	for _, row := range d.layout {
		var newRow []string
		for _, col := range row {
			if col == src {
				srcStr = col
			} else {
				newRow = append(newRow, col)
			}
		}
		if len(newRow) > 0 {
			newLayout = append(newLayout, newRow)
		}
	}
	d.layout = newLayout

	if srcStr == "" {
		return // shouldn't happen
	}

	if action == "swap" {
		// Just swap in place (src becomes dst, dst becomes src). 
		// Actually, we already removed src. Let's just replace dst with src, and put dst where src was...
		// But since we removed src, that's complex. Let's just insert src before dst, and shift dst.
		// A true swap is easier if we don't remove src first.
		// For simplicity, "swap" will just place src exactly where dst is (insert before).
		action = "left"
	}

	// Insert src into new layout relative to dst
	var finalLayout [][]string
	for _, row := range d.layout {
		var newRow []string
		rowMatched := false
		for _, col := range row {
			if col == dst {
				rowMatched = true
				if action == "left" {
					newRow = append(newRow, srcStr, col)
				} else if action == "right" {
					newRow = append(newRow, col, srcStr)
				} else {
					newRow = append(newRow, col) // top or bottom handled at row level
				}
			} else {
				newRow = append(newRow, col)
			}
		}
		if rowMatched && action == "top" {
			finalLayout = append(finalLayout, []string{srcStr})
			finalLayout = append(finalLayout, newRow)
		} else if rowMatched && action == "bottom" {
			finalLayout = append(finalLayout, newRow)
			finalLayout = append(finalLayout, []string{srcStr})
		} else {
			finalLayout = append(finalLayout, newRow)
		}
	}
	d.layout = finalLayout
}

func (d *Dashboard) View() string {
	if d.width == 0 {
		return styles.TextMuted.Render("  Waiting for data…")
	}

	// Tab bar is 2 lines, status bar is 1 line.
	// We track Y relative to the terminal so mouse clicks match.
	currentY := 2 - d.scrollOffset
	d.bounds = make(map[string]PanelBounds)

	var rowsStr []string
	for _, row := range d.layout {
		var rowRenders []string
		var maxH int
		currentX := 0
		targetWidth := (d.width - 2) / len(row)
		if targetWidth < 20 {
			targetWidth = 20
		}
		

		// Pass 1: find max height
		for _, id := range row {
			var content string
			switch id {
			case "sysinfo": content = d.renderSysInfo(targetWidth, 0)
			case "cpu": content = d.renderCPU(targetWidth, 0)
			case "memory": content = d.renderMemory(targetWidth, 0)
			case "disk": content = d.renderDisk(targetWidth, 0)
			case "network": content = d.renderNetwork(targetWidth, 0)
			}
			h := lipgloss.Height(content)
			if h > maxH {
				maxH = h
			}
		}

		// Pass 2: render with max height
		for _, id := range row {
			var content string
			switch id {
			case "sysinfo": content = d.renderSysInfo(targetWidth, maxH)
			case "cpu": content = d.renderCPU(targetWidth, maxH)
			case "memory": content = d.renderMemory(targetWidth, maxH)
			case "disk": content = d.renderDisk(targetWidth, maxH)
			case "network": content = d.renderNetwork(targetWidth, maxH)
			}
			if content == "" {
				continue
			}

			w := lipgloss.Width(content)
			
			// Save bounding box for mouse events
			d.bounds[id] = PanelBounds{ID: id, X: currentX, Y: currentY, W: w, H: maxH}
			rowRenders = append(rowRenders, content)
			currentX += w
		}
		rowsStr = append(rowsStr, lipgloss.JoinHorizontal(lipgloss.Top, rowRenders...))
		currentY += maxH
	}

	content := lipgloss.JoinVertical(lipgloss.Left, rowsStr...)

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

func (d *Dashboard) renderSysInfo(targetWidth, targetHeight int) string {
	title := styles.PanelTitleStyle.Render("  System Info")
	
	style := styles.PanelStyle.Copy()
	if targetWidth > 0 {
		style = style.Width(targetWidth)
	}
	if targetHeight > 0 {
		style = style.Height(targetHeight - 2) // -2 for borders
	}

	if d.sysSnap == nil {
		return style.Render(title + "\n  Loading…")
	}
	s := d.sysSnap
	line1 := fmt.Sprintf("  Hostname: %s  │  Uptime: %s  │  Kernel: %s",
		styles.TextAccent.Render(s.Hostname), s.Uptime, s.KernelVersion)
	line2 := fmt.Sprintf("  CPU: %s  │  Cores: %d  │  Threads: %d",
		s.CPUModel, s.CPUCores, s.CPUThreads)
	line3 := fmt.Sprintf("  Load avg:  1m: %.2f  │  5m: %.2f  │  15m: %.2f",
		s.LoadAvg1, s.LoadAvg5, s.LoadAvg15)
	return style.Render(title + "\n" + line1 + "\n" + line2 + "\n" + line3)
}

func (d *Dashboard) renderCPU(targetWidth, targetHeight int) string {
	title := styles.PanelTitleStyle.Render("  CPU")
	
	style := styles.PanelStyle.Copy()
	if targetWidth > 0 {
		style = style.Width(targetWidth)
	}
	if targetHeight > 0 {
		style = style.Height(targetHeight - 2)
	}

	if d.cpuSnap == nil {
		return style.Render(title + "\n  Loading…")
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
	return style.Render(body)
}

func (d *Dashboard) renderMemory(targetWidth, targetHeight int) string {
	title := styles.PanelTitleStyle.Render("  Memory")
	
	style := styles.PanelStyle.Copy()
	if targetWidth > 0 {
		style = style.Width(targetWidth)
	}
	if targetHeight > 0 {
		style = style.Height(targetHeight - 2)
	}
	var lines []string

	warn := d.cfg.Alerts.MemoryWarning
	crit := d.cfg.Alerts.MemoryCritical

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

	return style.Render(title + "\n" + strings.Join(lines, "\n"))
}

func (d *Dashboard) renderDisk(targetWidth, targetHeight int) string {
	title := styles.PanelTitleStyle.Render("  Disk")
	
	style := styles.PanelStyle.Copy()
	if targetWidth > 0 {
		style = style.Width(targetWidth)
	}
	if targetHeight > 0 {
		style = style.Height(targetHeight - 2)
	}

	if d.diskSnap == nil {
		return style.Render(title + "\n  Loading…")
	}
	warn := d.cfg.Alerts.DiskWarning
	crit := d.cfg.Alerts.DiskCritical

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
		inodeLine := fmt.Sprintf("  Inodes: %d used  %.0f%%",
			dk.InodesTotal-dk.InodesFree,
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
	return style.Render(title + "\n" + strings.Join(lines, "\n"))
}

func (d *Dashboard) renderNetwork(targetWidth, targetHeight int) string {
	title := styles.PanelTitleStyle.Render("  Network")
	
	style := styles.PanelStyle.Copy()
	if targetWidth > 0 {
		style = style.Width(targetWidth)
	}
	if targetHeight > 0 {
		style = style.Height(targetHeight - 2)
	}

	if d.netSnap == nil {
		return style.Render(title + "\n  Loading…")
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
	return style.Render(title + "\n" + lipgloss.JoinVertical(lipgloss.Left, lines...))
}
