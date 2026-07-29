// Package styles defines the BlackEye color palette and lipgloss styles.
// Imported by both ui (app shell) and ui/tabs (tab renderers) to avoid
// circular dependencies.
//
// Brand palette: Navy Blue #1E4174 + Gold #DDA94B
package styles

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ── Brand Colors ──────────────────────────────────────────────────────────
var (
	ColorNavy      = lipgloss.Color("#1E4174") // primary brand — deep navy
	ColorGold      = lipgloss.Color("#DDA94B") // secondary brand — warm gold
	ColorNavyLight = lipgloss.Color("#2B5EA3") // lighter navy for hover / active
	ColorNavyDark  = lipgloss.Color("#142D52") // darker navy for backgrounds
	ColorGoldLight = lipgloss.Color("#E8C56D") // lighter gold for highlights
	ColorGoldDim   = lipgloss.Color("#B8882F") // muted gold for borders
)

// ── Semantic Colors (derived from brand) ──────────────────────────────────
var (
	ColorAccent  = ColorGold                   // primary accent = gold
	ColorGreen   = lipgloss.Color("#5CB870")   // success — soft green
	ColorYellow  = ColorGoldLight              // warning — light gold
	ColorRed     = lipgloss.Color("#E05252")   // critical/error — warm red
	ColorOrange  = lipgloss.Color("#E0943A")   // degraded — amber-orange
	ColorMuted   = lipgloss.Color("#5A6E8A")   // inactive text — blue-gray
	ColorBg      = lipgloss.Color("#0D1B2E")   // near-black navy background
	ColorSurface = ColorNavyDark               // panel background = dark navy
	ColorBorder  = lipgloss.Color("#2A4570")   // border = medium navy
	ColorText    = lipgloss.Color("#D4DDE8")   // primary text — warm white
	ColorSubtext = lipgloss.Color("#8A9BB5")   // secondary text — steel blue
)

// ── Tab Bar ───────────────────────────────────────────────────────────────
var (
	TabActive = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorGold).
			Background(ColorNavy).
			BorderBottom(true).
			BorderForeground(ColorGold).
			Padding(0, 2)

	TabInactive = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Padding(0, 2)
)

// ── Panels ────────────────────────────────────────────────────────────────
var (
	PanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Padding(0, 1)

	PanelTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorGold).
			MarginBottom(1)

	SectionTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorNavyLight)
)

// ── Text ──────────────────────────────────────────────────────────────────
var (
	TextNormal = lipgloss.NewStyle().Foreground(ColorText)
	TextMuted  = lipgloss.NewStyle().Foreground(ColorMuted)
	TextGreen  = lipgloss.NewStyle().Foreground(ColorGreen)
	TextYellow = lipgloss.NewStyle().Foreground(ColorGoldLight)
	TextRed    = lipgloss.NewStyle().Foreground(ColorRed)
	TextOrange = lipgloss.NewStyle().Foreground(ColorOrange)
	TextAccent = lipgloss.NewStyle().Foreground(ColorGold)
	TextBold   = lipgloss.NewStyle().Bold(true).Foreground(ColorText)
	TextNavy   = lipgloss.NewStyle().Foreground(ColorNavyLight)
)

// ── Tables ────────────────────────────────────────────────────────────────
var (
	TableHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorGold).
			Background(ColorNavyDark).
			BorderBottom(true).
			BorderForeground(ColorGoldDim)

	TableRow = lipgloss.NewStyle().Foreground(ColorText)

	TableRowSelected = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorBg).
				Background(ColorGold)

	TableRowFlagged = lipgloss.NewStyle().
			Foreground(ColorRed).
			Bold(true)
)

// ── Usage Bars ────────────────────────────────────────────────────────────

// Bar renders a horizontal usage bar of given width.
// pct should be 0.0–100.0. warn and crit are thresholds.
// Uses gold for OK, amber for warn, red for critical.
func Bar(pct float64, width int, warn, crit float64) string {
	if width <= 0 {
		width = 20
	}
	filled := int(pct / 100.0 * float64(width))
	if filled > width {
		filled = width
	}
	empty := width - filled

	// Color gradient: navy-tinted green → gold → red
	color := ColorGreen
	if pct >= crit {
		color = ColorRed
	} else if pct >= warn {
		color = ColorGold
	}

	filledStyle := lipgloss.NewStyle().Foreground(color)
	emptyStyle := lipgloss.NewStyle().Foreground(ColorBorder)

	bar := filledStyle.Render(RepeatStr("█", filled)) +
		emptyStyle.Render(RepeatStr("░", empty))
	return bar
}

// RepeatStr repeats string s n times.
func RepeatStr(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(s, n)
}

// Colorize returns text colored by percentage threshold.
func Colorize(text string, pct, warn, crit float64) string {
	if pct >= crit {
		return TextRed.Render(text)
	}
	if pct >= warn {
		return TextAccent.Render(text) // gold for warning
	}
	return TextGreen.Render(text)
}

// ColorizeTemp returns text colored by temperature threshold.
func ColorizeTemp(text string, celsius, warn, crit float64) string {
	if celsius >= crit {
		return TextRed.Render(text)
	}
	if celsius >= warn {
		return TextAccent.Render(text) // gold for warm
	}
	return TextGreen.Render(text)
}

// MinSizeWarning returns the warning shown when terminal is too small.
func MinSizeWarning() string {
	return lipgloss.NewStyle().
		Foreground(ColorGold).
		Bold(true).
		Render("⚠  Terminal too small. Please resize to at least 80×24 columns.")
}

// Truncate shortens a string to n visible characters with an ellipsis.
// Uses rune slicing to avoid cutting multi-byte UTF-8 characters mid-sequence.
func Truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(runes[:n-1]) + "…"
}

// FmtPercent formats a float64 percentage with one decimal place.
func FmtPercent(p float64) string {
	return fmt.Sprintf("%.1f%%", p)
}
