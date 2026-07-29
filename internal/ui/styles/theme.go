package styles

import (
	"sync"

	"github.com/charmbracelet/lipgloss"
)

type Theme struct {
	Name       string
	Navy       lipgloss.Color
	Gold       lipgloss.Color
	NavyLight  lipgloss.Color
	NavyDark   lipgloss.Color
	GoldLight  lipgloss.Color
	GoldDim    lipgloss.Color
	Green      lipgloss.Color
	Red        lipgloss.Color
	Muted      lipgloss.Color
	Bg         lipgloss.Color
	Surface    lipgloss.Color
	Border     lipgloss.Color
	Text       lipgloss.Color
}

var AvailableThemes = []Theme{
	{
		Name:      "Classic Navy & Gold",
		Navy:      lipgloss.Color("#1E4174"),
		Gold:      lipgloss.Color("#DDA94B"),
		NavyLight: lipgloss.Color("#2B5EA3"),
		NavyDark:  lipgloss.Color("#142D52"),
		GoldLight: lipgloss.Color("#E8C56D"),
		GoldDim:   lipgloss.Color("#B8882F"),
		Green:     lipgloss.Color("#5CB870"),
		Red:       lipgloss.Color("#E05252"),
		Muted:     lipgloss.Color("#5A6E8A"),
		Bg:        lipgloss.Color("#0D1B2E"),
		Surface:   lipgloss.Color("#142D52"),
		Border:    lipgloss.Color("#2A4570"),
		Text:      lipgloss.Color("#D4DDE8"),
	},
	{
		Name:      "Cyberpunk Neon",
		Navy:      lipgloss.Color("#2D006B"),
		Gold:      lipgloss.Color("#00F0FF"),
		NavyLight: lipgloss.Color("#7000FF"),
		NavyDark:  lipgloss.Color("#150038"),
		GoldLight: lipgloss.Color("#70F0FF"),
		GoldDim:   lipgloss.Color("#00A0B0"),
		Green:     lipgloss.Color("#00FF66"),
		Red:       lipgloss.Color("#FF0055"),
		Muted:     lipgloss.Color("#8050B0"),
		Bg:        lipgloss.Color("#0A001A"),
		Surface:   lipgloss.Color("#1A003E"),
		Border:    lipgloss.Color("#00F0FF"),
		Text:      lipgloss.Color("#E5E0FF"),
	},
	{
		Name:      "Dracula Dark",
		Navy:      lipgloss.Color("#6272A4"),
		Gold:      lipgloss.Color("#BD93F9"),
		NavyLight: lipgloss.Color("#8BE9FD"),
		NavyDark:  lipgloss.Color("#282A36"),
		GoldLight: lipgloss.Color("#FF79C6"),
		GoldDim:   lipgloss.Color("#954080"),
		Green:     lipgloss.Color("#50FA7B"),
		Red:       lipgloss.Color("#FF5555"),
		Muted:     lipgloss.Color("#6272A4"),
		Bg:        lipgloss.Color("#1E1F29"),
		Surface:   lipgloss.Color("#282A36"),
		Border:    lipgloss.Color("#BD93F9"),
		Text:      lipgloss.Color("#F8F8F2"),
	},
	{
		Name:      "Matrix Emerald",
		Navy:      lipgloss.Color("#003B00"),
		Gold:      lipgloss.Color("#00FF41"),
		NavyLight: lipgloss.Color("#008F11"),
		NavyDark:  lipgloss.Color("#001A00"),
		GoldLight: lipgloss.Color("#66FF8C"),
		GoldDim:   lipgloss.Color("#00A32A"),
		Green:     lipgloss.Color("#00FF41"),
		Red:       lipgloss.Color("#FF3333"),
		Muted:     lipgloss.Color("#005C12"),
		Bg:        lipgloss.Color("#000800"),
		Surface:   lipgloss.Color("#001F00"),
		Border:    lipgloss.Color("#00FF41"),
		Text:      lipgloss.Color("#D0FFD8"),
	},
	{
		Name:      "High Contrast Slate",
		Navy:      lipgloss.Color("#334155"),
		Gold:      lipgloss.Color("#F59E0B"),
		NavyLight: lipgloss.Color("#475569"),
		NavyDark:  lipgloss.Color("#1E293B"),
		GoldLight: lipgloss.Color("#FCD34D"),
		GoldDim:   lipgloss.Color("#B45309"),
		Green:     lipgloss.Color("#10B981"),
		Red:       lipgloss.Color("#EF4444"),
		Muted:     lipgloss.Color("#64748B"),
		Bg:        lipgloss.Color("#0F172A"),
		Surface:   lipgloss.Color("#1E293B"),
		Border:    lipgloss.Color("#CBD5E1"),
		Text:      lipgloss.Color("#F8FAFC"),
	},
}

var ActiveThemeIdx = 0

// themeMu protects all global color/style variables during live theme switching.
var themeMu sync.RWMutex

// CycleTheme advances to the next theme and applies it atomically.
func CycleTheme() string {
	themeMu.Lock()
	defer themeMu.Unlock()
	ActiveThemeIdx = (ActiveThemeIdx + 1) % len(AvailableThemes)
	applyThemeLocked(AvailableThemes[ActiveThemeIdx])
	return AvailableThemes[ActiveThemeIdx].Name
}

// ApplyTheme applies the given theme atomically. It is goroutine-safe.
func ApplyTheme(t Theme) {
	themeMu.Lock()
	defer themeMu.Unlock()
	applyThemeLocked(t)
}

// applyThemeLocked sets all globals — caller must hold themeMu write lock.
func applyThemeLocked(t Theme) {
	ColorNavy = t.Navy
	ColorGold = t.Gold
	ColorNavyLight = t.NavyLight
	ColorNavyDark = t.NavyDark
	ColorGoldLight = t.GoldLight
	ColorGoldDim = t.GoldDim
	ColorGreen = t.Green
	ColorRed = t.Red
	ColorMuted = t.Muted
	ColorBg = t.Bg
	ColorSurface = t.Surface
	ColorBorder = t.Border
	ColorText = t.Text

	ColorAccent = ColorGold
	ColorYellow = ColorGoldLight

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

	TextNormal = lipgloss.NewStyle().Foreground(ColorText)
	TextMuted = lipgloss.NewStyle().Foreground(ColorMuted)
	TextGreen = lipgloss.NewStyle().Foreground(ColorGreen)
	TextYellow = lipgloss.NewStyle().Foreground(ColorGoldLight)
	TextRed = lipgloss.NewStyle().Foreground(ColorRed)
	TextAccent = lipgloss.NewStyle().Foreground(ColorGold)
	TextBold = lipgloss.NewStyle().Bold(true).Foreground(ColorText)
}
