package tabs

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/sys/unix"

	"blackeye/internal/ui/styles"
)

// TerminalFocused is sent by the terminal to tell the root model whether
// it currently holds input focus (so the root can decide whether to intercept keys).
type TerminalFocused struct{ Focused bool }

// termOutputMsg delivers raw bytes from the PTY stdout to the TUI.
type termOutputMsg struct{ data string }

// termExitMsg is sent when the shell process exits.
type termExitMsg struct{ err error }

// Terminal is the Tab 6 model — an embedded PTY shell.
type Terminal struct {
	width, height int

	// PTY state.
	ptmx      *os.File
	cmd       *exec.Cmd
	focused   bool // true = keystrokes go to shell; false = navigation mode
	started   bool
	col       int  // current active column on last line

	// Display buffer.
	mu       sync.Mutex
	lines    []string // rendered output lines
	maxLines int      // scrollback capacity

	// Scroll state (when not focused).
	scrollOffset int

	// Double-Esc detection.
	lastEsc time.Time

	// Shell exit message.
	exitMsg string
}

func NewTerminal() *Terminal {
	return &Terminal{
		maxLines: 10000,
		lines:    []string{""},
		focused:  false,
	}
}

// IsFocused reports whether the terminal currently has input focus.
func (t *Terminal) IsFocused() bool {
	return t.focused
}

// IsStarted reports whether the shell process has been started.
func (t *Terminal) IsStarted() bool {
	return t.started
}

func (t *Terminal) Init() tea.Cmd {
	return nil // PTY started lazily on first view
}

func (t *Terminal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		t.width = m.Width
		t.height = m.Height
		if t.ptmx != nil {
			t.resizePTY()
		}
		// Auto-start shell on first size message.
		if !t.started && t.exitMsg == "" {
			return t, t.startShell()
		}

	case termOutputMsg:
		t.processOutput(m.data)
		return t, t.readPTY()

	case termExitMsg:
		t.focused = false
		t.started = false
		if t.ptmx != nil {
			t.ptmx.Close()
			t.ptmx = nil
		}
		t.cmd = nil
		if m.err != nil {
			t.exitMsg = fmt.Sprintf("Shell exited: %v", m.err)
		} else {
			t.exitMsg = "Shell exited."
		}
		return t, func() tea.Msg { return TerminalFocused{Focused: false} }

	case tea.KeyMsg:
		return t.handleKey(m)
	}
	return t, nil
}

func (t *Terminal) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !t.focused {
		// Navigation mode.
		switch msg.String() {
		case "i", "enter":
			if t.exitMsg != "" {
				// Shell exited — restart on enter.
				t.exitMsg = ""
				t.lines = []string{""}
				return t, t.startShell()
			}
			t.focused = true
			t.scrollOffset = 0
			return t, func() tea.Msg { return TerminalFocused{Focused: true} }
		case "up", "k":
			t.scrollOffset++
		case "down", "j":
			if t.scrollOffset > 0 {
				t.scrollOffset--
			}
		case "pgup":
			t.scrollOffset += t.height / 2
		case "pgdown":
			t.scrollOffset -= t.height / 2
			if t.scrollOffset < 0 {
				t.scrollOffset = 0
			}
		case "home":
			t.mu.Lock()
			t.scrollOffset = len(t.lines)
			t.mu.Unlock()
		case "end":
			t.scrollOffset = 0
		}
		return t, nil
	}

	// Focused mode — forward keys to PTY.
	// Double-Esc detection: two Esc presses within 300ms exits focus.
	if msg.String() == "esc" {
		now := time.Now()
		if now.Sub(t.lastEsc) < 300*time.Millisecond {
			t.focused = false
			t.lastEsc = time.Time{}
			return t, func() tea.Msg { return TerminalFocused{Focused: false} }
		}
		t.lastEsc = now
	} else {
		t.lastEsc = time.Time{}
	}

	if t.ptmx == nil {
		return t, nil
	}

	// Convert tea.KeyMsg to raw bytes for PTY.
	raw := keyToBytes(msg)
	if len(raw) > 0 {
		t.ptmx.Write(raw)
	}

	return t, nil
}

// startShell spawns a PTY with the user's shell.
func (t *Terminal) startShell() tea.Cmd {
	return func() tea.Msg {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}

		cmd := exec.Command(shell)
		cmd.Env = append(os.Environ(), "TERM=xterm-256color")

		ptmx, err := startPTY(cmd)
		if err != nil {
			return termExitMsg{err: err}
		}

		t.mu.Lock()
		t.ptmx = ptmx
		t.cmd = cmd
		t.started = true
		t.mu.Unlock()

		t.resizePTY()

		// Start the shell exit waiter in a separate goroutine.
		go func() {
			cmd.Wait()
		}()

		// Return a signal to start reading.
		return termOutputMsg{data: ""}
	}
}

// readPTY returns a command that reads from the PTY.
func (t *Terminal) readPTY() tea.Cmd {
	return func() tea.Msg {
		if t.ptmx == nil {
			return termExitMsg{err: nil}
		}
		buf := make([]byte, 4096)
		n, err := t.ptmx.Read(buf)
		if err != nil {
			if err == io.EOF {
				return termExitMsg{err: nil}
			}
			return termExitMsg{err: err}
		}
		return termOutputMsg{data: string(buf[:n])}
	}
}

// parseEscape returns the full ANSI/OSC escape sequence starting at index i.
func parseEscape(s string, i int) string {
	if i >= len(s) || s[i] != '\x1b' {
		return ""
	}
	end := i + 1
	if end < len(s) {
		switch s[end] {
		case '[':
			end++
			for end < len(s) {
				c := s[end]
				end++
				if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '~' {
					break
				}
			}
		case ']':
			end++
			for end < len(s) {
				if s[end] == '\x07' {
					end++
					break
				}
				if s[end] == '\x1b' && end+1 < len(s) && s[end+1] == '\\' {
					end += 2
					break
				}
				end++
			}
		case '(', ')', '#', '%':
			if end+1 < len(s) {
				end += 2
			} else {
				end = len(s)
			}
		default:
			end++
		}
	}
	return s[i:end]
}

// processOutput handles raw PTY output, interpreting ANSI sequences and cursor positioning.
func (t *Terminal) processOutput(data string) {
	if data == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	for i := 0; i < len(data); {
		if data[i] == '\x1b' {
			seq := parseEscape(data, i)
			if len(seq) > 0 {
				if strings.HasPrefix(seq, "\x1b]") {
					// OSC sequence (window title, CWD, etc.) — swallow completely
				} else {
					switch seq {
					case "\x1b[2J", "\x1b[3J", "\x1b[H\x1b[2J":
						t.lines = []string{""}
						t.col = 0
					case "\x1b[K", "\x1b[0K":
						if len(t.lines) > 0 {
							t.lines[len(t.lines)-1] = truncateAtCol(t.lines[len(t.lines)-1], t.col)
						}
					case "\x1b[D", "\x1b[1D":
						if t.col > 0 {
							t.col--
						}
					case "\x1b[C", "\x1b[1C":
						t.col++
					default:
						if len(t.lines) > 0 {
							t.lines[len(t.lines)-1] += seq
						}
					}
				}
				i += len(seq)
				continue
			}
		}

		r, size := utf8.DecodeRuneInString(data[i:])

		switch {
		case r == '\n':
			t.lines = append(t.lines, "")
			t.col = 0

		case r == '\r':
			t.col = 0

		case r == '\b':
			if t.col > 0 {
				t.col--
			}

		case r == '\t':
			spaces := 8 - (t.col % 8)
			for s := 0; s < spaces; s++ {
				if len(t.lines) > 0 {
					t.lines[len(t.lines)-1] = overwriteAtCol(t.lines[len(t.lines)-1], t.col, ' ')
				}
				t.col++
			}

		case r == '\a':
			// Bell — ignore.

		default:
			if len(t.lines) > 0 {
				t.lines[len(t.lines)-1] = overwriteAtCol(t.lines[len(t.lines)-1], t.col, r)
			}
			t.col++
		}
		i += size
	}

	// Trim scrollback.
	if len(t.lines) > t.maxLines {
		t.lines = t.lines[len(t.lines)-t.maxLines:]
	}
}

// visibleLen returns the number of visible (non-ANSI escape) runes in s.
func visibleLen(s string) int {
	count := 0
	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			seq := parseEscape(s, i)
			if len(seq) > 0 {
				i += len(seq)
				continue
			}
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		count++
		i += size
	}
	return count
}

// overwriteAtCol overwrites the visible rune at column col with r,
// expanding with spaces if col > visibleLen(s), preserving ANSI escape codes.
func overwriteAtCol(s string, col int, r rune) string {
	vLen := visibleLen(s)
	if col >= vLen {
		padding := col - vLen
		return s + strings.Repeat(" ", padding) + string(r)
	}

	var sb strings.Builder
	sb.Grow(len(s) + 4)
	visIdx := 0

	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			seq := parseEscape(s, i)
			if len(seq) > 0 {
				sb.WriteString(seq)
				i += len(seq)
				continue
			}
		}

		runeVal, size := utf8.DecodeRuneInString(s[i:])
		if visIdx == col {
			sb.WriteRune(r)
		} else {
			sb.WriteRune(runeVal)
		}
		visIdx++
		i += size
	}
	return sb.String()
}

// truncateAtCol keeps visible runes up to col (and trailing ANSI resets), dropping text beyond col.
func truncateAtCol(s string, col int) string {
	vLen := visibleLen(s)
	if col >= vLen {
		return s
	}

	var sb strings.Builder
	sb.Grow(len(s))
	visIdx := 0

	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			seq := parseEscape(s, i)
			if len(seq) > 0 {
				sb.WriteString(seq)
				i += len(seq)
				continue
			}
		}

		runeVal, size := utf8.DecodeRuneInString(s[i:])
		if visIdx < col {
			sb.WriteRune(runeVal)
			visIdx++
		}
		i += size
	}
	return sb.String()
}

// resizePTY sends TIOCSWINSZ to the PTY.
func (t *Terminal) resizePTY() {
	if t.ptmx == nil {
		return
	}
	// Reserve 3 lines for tab bar (2) + status bar (1), and 2 for panel border.
	rows := t.height - 5
	cols := t.width - 4
	if rows < 5 {
		rows = 5
	}
	if cols < 20 {
		cols = 20
	}
	unix.IoctlSetWinsize(int(t.ptmx.Fd()), unix.TIOCSWINSZ, &unix.Winsize{
		Row: uint16(rows),
		Col: uint16(cols),
	})
}

func (t *Terminal) View() string {
	viewH := t.height - 5 // tab bar + status bar + borders
	if viewH < 3 {
		viewH = 3
	}
	viewW := t.width - 4
	if viewW < 20 {
		viewW = 20
	}

	// Mode indicator.
	var modeStr string
	if t.focused {
		modeStr = lipgloss.NewStyle().
			Bold(true).
			Foreground(styles.ColorBg).
			Background(styles.ColorGreen).
			Padding(0, 1).
			Render(" TERMINAL ")
	} else {
		modeStr = lipgloss.NewStyle().
			Bold(true).
			Foreground(styles.ColorBg).
			Background(styles.ColorGold).
			Padding(0, 1).
			Render(" NAVIGATION ")
	}
	escHint := ""
	if t.focused {
		escHint = styles.TextMuted.Render("  Esc Esc to exit focus")
	} else {
		escHint = styles.TextMuted.Render("  i/Enter to focus  │  ↑/↓ scroll")
	}
	header := modeStr + escHint

	if !t.started && t.exitMsg == "" {
		// Not started yet — auto-start.
		return styles.PanelStyle.Render(lipgloss.JoinVertical(lipgloss.Left,
			header,
			"",
			styles.TextMuted.Render("  Starting shell…"),
			styles.TextMuted.Render("  Press i or Enter to activate the terminal."),
		))
	}

	if t.exitMsg != "" {
		return styles.PanelStyle.Render(lipgloss.JoinVertical(lipgloss.Left,
			header,
			"",
			styles.TextYellow.Render("  "+t.exitMsg),
			styles.TextMuted.Render("  Press Enter to restart the shell."),
		))
	}

	// Render terminal content.
	t.mu.Lock()
	allLines := make([]string, len(t.lines))
	copy(allLines, t.lines)
	t.mu.Unlock()

	// Truncate lines to view width.
	for i, line := range allLines {
		if runeLen(line) > viewW {
			allLines[i] = truncateRunes(line, viewW)
		}
	}

	// Apply scroll offset.
	totalLines := len(allLines)
	contentH := viewH - 2 // header + scroll indicator

	if t.scrollOffset > totalLines-contentH {
		t.scrollOffset = totalLines - contentH
	}
	if t.scrollOffset < 0 {
		t.scrollOffset = 0
	}

	endIdx := totalLines - t.scrollOffset
	startIdx := endIdx - contentH
	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx > totalLines {
		endIdx = totalLines
	}

	visible := allLines[startIdx:endIdx]

	// Pad to fill viewport.
	for len(visible) < contentH {
		visible = append(visible, "")
	}

	// Join and add a cursor block if focused and at bottom.
	content := strings.Join(visible, "\n")
	if t.focused && t.scrollOffset == 0 {
		// No visual cursor needed — the shell manages it via ANSI.
	}

	scrollInfo := ""
	if t.scrollOffset > 0 {
		scrollInfo = styles.TextMuted.Render(fmt.Sprintf("  ↑ Scroll: +%d lines", t.scrollOffset))
	}

	body := lipgloss.JoinVertical(lipgloss.Left, header, content, scrollInfo)
	return styles.PanelStyle.Copy().Width(t.width - 2).Render(body)
}

// startPTY creates a pseudoterminal and starts the command on it.
// Uses /dev/ptmx directly with ioctls — no cgo, no external libraries.
func startPTY(cmd *exec.Cmd) (*os.File, error) {
	// Open the PTY master.
	ptmx, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/ptmx: %w", err)
	}

	// Get the slave PTY number via ioctl TIOCGPTN.
	var ptsNum uint32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, ptmx.Fd(),
		uintptr(unix.TIOCGPTN), uintptr(unsafe.Pointer(&ptsNum)))
	if errno != 0 {
		ptmx.Close()
		return nil, fmt.Errorf("TIOCGPTN: %v", errno)
	}

	// Unlock the slave PTY via ioctl TIOCSPTLCK.
	var unlock int32 = 0
	_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, ptmx.Fd(),
		uintptr(unix.TIOCSPTLCK), uintptr(unsafe.Pointer(&unlock)))
	if errno != 0 {
		ptmx.Close()
		return nil, fmt.Errorf("TIOCSPTLCK: %v", errno)
	}

	ptsName := fmt.Sprintf("/dev/pts/%d", ptsNum)

	pts, err := os.OpenFile(ptsName, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		ptmx.Close()
		return nil, fmt.Errorf("open pts %s: %w", ptsName, err)
	}

	cmd.Stdin = pts
	cmd.Stdout = pts
	cmd.Stderr = pts
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
		Ctty:    0,
	}

	if err := cmd.Start(); err != nil {
		pts.Close()
		ptmx.Close()
		return nil, fmt.Errorf("start shell: %w", err)
	}

	pts.Close() // Parent doesn't need the slave side.
	return ptmx, nil
}

// keyToBytes converts a bubbletea key message to raw terminal bytes.
func keyToBytes(msg tea.KeyMsg) []byte {
	switch msg.Type {
	case tea.KeyEnter:
		return []byte{'\r'}
	case tea.KeyTab:
		return []byte{'\t'}
	case tea.KeyBackspace:
		return []byte{0x7f}
	case tea.KeyEscape:
		return []byte{0x1b}
	case tea.KeyUp:
		return []byte{0x1b, '[', 'A'}
	case tea.KeyDown:
		return []byte{0x1b, '[', 'B'}
	case tea.KeyRight:
		return []byte{0x1b, '[', 'C'}
	case tea.KeyLeft:
		return []byte{0x1b, '[', 'D'}
	case tea.KeyHome:
		return []byte{0x1b, '[', 'H'}
	case tea.KeyEnd:
		return []byte{0x1b, '[', 'F'}
	case tea.KeyDelete:
		return []byte{0x1b, '[', '3', '~'}
	case tea.KeyPgUp:
		return []byte{0x1b, '[', '5', '~'}
	case tea.KeyPgDown:
		return []byte{0x1b, '[', '6', '~'}
	case tea.KeyCtrlA:
		return []byte{0x01}
	case tea.KeyCtrlB:
		return []byte{0x02}
	case tea.KeyCtrlC:
		return []byte{0x03}
	case tea.KeyCtrlD:
		return []byte{0x04}
	case tea.KeyCtrlE:
		return []byte{0x05}
	case tea.KeyCtrlF:
		return []byte{0x06}
	case tea.KeyCtrlG:
		return []byte{0x07}
	case tea.KeyCtrlK:
		return []byte{0x0b}
	case tea.KeyCtrlL:
		return []byte{0x0c}
	case tea.KeyCtrlN:
		return []byte{0x0e}
	case tea.KeyCtrlO:
		return []byte{0x0f}
	case tea.KeyCtrlP:
		return []byte{0x10}
	case tea.KeyCtrlR:
		return []byte{0x12}
	case tea.KeyCtrlS:
		return []byte{0x13}
	case tea.KeyCtrlT:
		return []byte{0x14}
	case tea.KeyCtrlU:
		return []byte{0x15}
	case tea.KeyCtrlW:
		return []byte{0x17}
	case tea.KeyCtrlZ:
		return []byte{0x1a}
	case tea.KeySpace:
		return []byte{' '}
	case tea.KeyRunes:
		return []byte(string(msg.Runes))
	}
	// Fallback: try the string representation.
	s := msg.String()
	if len(s) > 0 {
		return []byte(s)
	}
	return nil
}

// runeLen counts the visible rune length, skipping ANSI sequences.
func runeLen(s string) int {
	count := 0
	inEsc := false
	for _, r := range s {
		if inEsc {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '~' {
				inEsc = false
			}
			continue
		}
		if r == '\x1b' {
			inEsc = true
			continue
		}
		count++
	}
	return count
}

// truncateRunes truncates a string to n visible runes, preserving ANSI sequences.
func truncateRunes(s string, n int) string {
	count := 0
	inEsc := false
	var result []rune
	for _, r := range s {
		if inEsc {
			result = append(result, r)
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '~' {
				inEsc = false
			}
			continue
		}
		if r == '\x1b' {
			inEsc = true
			result = append(result, r)
			continue
		}
		if count >= n {
			break
		}
		result = append(result, r)
		count++
	}
	return string(result)
}
