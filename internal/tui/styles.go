package tui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Semantic color tokens. Render paths reference these, never a raw hex
// literal, so that theming and NO_COLOR can be handled in one place.
var (
	cAccent  = lipgloss.Color("#A78BFA") // selection, titles
	cInfo    = lipgloss.Color("#38BDF8") // identifiers, counts
	cSuccess = lipgloss.Color("#22C55E") // direct assignment, applied
	cWarn    = lipgloss.Color("#F59E0B") // group-derived, caveats
	cError   = lipgloss.Color("#EF4444") // failures, destructive
	cDim     = lipgloss.Color("#9CA3AF") // secondary metadata
	cBorder  = lipgloss.Color("#4B5563")
	cFocus   = lipgloss.Color("#38BDF8")
)

// styles holds every style used in the render path, built once against the
// terminal's detected color profile.
type styles struct {
	title     lipgloss.Style
	subtitle  lipgloss.Style
	dim       lipgloss.Style
	accent    lipgloss.Style
	info      lipgloss.Style
	success   lipgloss.Style
	warn      lipgloss.Style
	err       lipgloss.Style
	selected  lipgloss.Style
	header    lipgloss.Style
	footer    lipgloss.Style
	filter    lipgloss.Style
	modal     lipgloss.Style
	modalWarn lipgloss.Style
	checked   lipgloss.Style
}

func newStyles() *styles {
	// Rendering to stderr keeps profile detection working when stdout is
	// redirected, and matches where Bubble Tea writes.
	r := lipgloss.NewRenderer(os.Stderr)
	s := &styles{
		title:    r.NewStyle().Bold(true).Foreground(cAccent),
		subtitle: r.NewStyle().Foreground(cDim),
		dim:      r.NewStyle().Foreground(cDim),
		accent:   r.NewStyle().Foreground(cAccent),
		info:     r.NewStyle().Foreground(cInfo),
		success:  r.NewStyle().Foreground(cSuccess),
		warn:     r.NewStyle().Foreground(cWarn),
		err:      r.NewStyle().Foreground(cError),
		selected: r.NewStyle().Bold(true).Foreground(cAccent),
		header:   r.NewStyle().Bold(true).Foreground(cDim),
		footer:   r.NewStyle().Foreground(cDim),
		filter:   r.NewStyle().Foreground(cInfo),
		checked:  r.NewStyle().Foreground(cSuccess),
		modal: r.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cFocus).
			Padding(0, 1),
		modalWarn: r.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cWarn).
			Padding(0, 1),
	}
	return s
}

// glyphs are swapped for ASCII when the terminal or locale can't be trusted
// with box-drawing and check marks (plain SSH sessions, Windows consoles).
type glyphSet struct {
	check    string
	uncheck  string
	cursor   string
	bullet   string
	plus     string
	minus    string
	ok       string
	fail     string
	ellipsis string
	barFull  string
	barEmpty string
	spinner  []string
}

var unicodeGlyphs = glyphSet{
	check: "◉", uncheck: "○", cursor: "▸", bullet: "·",
	plus: "+", minus: "−", ok: "✓", fail: "✗", ellipsis: "…",
	barFull: "━", barEmpty: "┄",
	spinner: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
}

var asciiGlyphs = glyphSet{
	check: "[x]", uncheck: "[ ]", cursor: ">", bullet: "-",
	plus: "+", minus: "-", ok: "ok", fail: "!!", ellipsis: "...",
	barFull: "#", barEmpty: ".",
	spinner: []string{"|", "/", "-", "\\"},
}

func detectGlyphs() glyphSet {
	if os.Getenv("OKX_ASCII") != "" {
		return asciiGlyphs
	}
	for _, v := range []string{os.Getenv("LC_ALL"), os.Getenv("LC_CTYPE"), os.Getenv("LANG")} {
		if v != "" {
			if containsUTF8(v) {
				return unicodeGlyphs
			}
			return asciiGlyphs
		}
	}
	return asciiGlyphs
}

func containsUTF8(s string) bool {
	for i := 0; i+4 <= len(s); i++ {
		seg := s[i : i+4]
		if seg == "UTF-" || seg == "utf-" || seg == "UTF8" || seg == "utf8" {
			return true
		}
	}
	return false
}
