package cli

import (
	"os"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
)

// lipgloss v2 always emits ANSI; nothing downsamples for us. So color is gated
// on stdout actually being a terminal (plus the standard NO_COLOR / TERM=dumb
// escape hatches) -- otherwise a piped or CI stdout would carry raw escape
// codes. Plain glyphs still print; only the color is dropped.
func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	return term.IsTerminal(os.Stdout.Fd())
}

var (
	okStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green
	noStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // red
	warnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow
)

func colorize(s string, style lipgloss.Style) string {
	if colorEnabled() {
		return style.Render(s)
	}
	return s
}

func glyphOK() string   { return colorize("✓", okStyle) }
func glyphNo() string   { return colorize("✗", noStyle) }
func glyphWarn() string { return colorize("⚠", warnStyle) }

// stdinIsTTY reports whether stdin is an interactive terminal. destroy uses it
// to decide between prompting for confirmation and refusing (a piped/CI stdin
// can't answer a prompt, so reading would hang forever).
func stdinIsTTY() bool {
	return term.IsTerminal(os.Stdin.Fd())
}
