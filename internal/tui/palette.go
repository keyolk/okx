package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/keyolk/tuikit/keys"
	"github.com/keyolk/tuikit/render"
)

// The `:` palette lists the actions the screen under it can run, straight
// from the binding table (keys.Keymap.Entries), and ranks them with the kit's
// fuzzy filter. A key the footer had no room for is found here, and an entry
// runs through the same run() as its key.

type paletteState struct {
	query string
	stack []keys.Ctx // the screen the palette was opened over
	all   []keys.Entry
	shown []keys.Entry
	cur   int
	top   int
	rows  int // match rows the box draws; set by openPalette from the height
}

func (m *Model) openPalette() {
	stack := m.active()
	// Bindings marked NoPalette (held-key steps, the palette itself) are
	// already left out by Entries.
	all := m.km.Or(defaultKeymap()).Entries(stack, m.keyOpts())
	m.pal = paletteState{stack: stack, all: all, rows: m.pickerHeight()}
	m.pal.refilter()
	m.overlay = overlayPalette
}

func (p *paletteState) refilter() {
	p.shown = keys.Filter(p.all, p.query)
	p.cur, p.top = 0, 0
}

func (p *paletteState) move(delta int) {
	p.cur = max(0, min(p.cur+delta, len(p.shown)-1))
	_, _, p.top = render.Window(len(p.shown), windowRows(len(p.shown), p.rows), p.cur, p.top)
}

// runPalette closes the palette and runs the chosen action against the
// screen it was opened over. The action is re-checked there: the palette
// lists only what was enabled when it opened, but a refresh may have landed.
func (m *Model) runPalette() tea.Cmd {
	m.overlay = overlayNone
	if m.pal.cur < 0 || m.pal.cur >= len(m.pal.shown) {
		return nil
	}
	action := m.pal.shown[m.pal.cur].Action
	if !m.keyEnabled(action) {
		return nil
	}
	return m.run(action)
}

func (m *Model) renderPalette() string {
	p := &m.pal
	w := clampInt(m.width-10, 40, 76)
	inner := w - 2 // modal padding
	names := m.keyNames()

	var b strings.Builder
	b.WriteString(m.st.title.Render("command palette") + "\n")
	b.WriteString(m.st.filter.Render(":"+p.query) + m.st.dim.Render("▏") + "\n")
	b.WriteString(m.st.dim.Render(strings.Repeat("─", inner)) + "\n")

	if len(p.shown) == 0 {
		b.WriteString(m.st.dim.Render("  no match") + "\n")
	}
	m.listLines(&b, len(p.shown), p.cur, p.top, p.rows, func(i int, sel bool) string {
		e := p.shown[i]
		ks := make([]string, len(e.Keys))
		for j, k := range e.Keys {
			ks[j] = names.Name(k)
		}
		keyCol := m.st.info.Render(pad(truncate(strings.Join(ks, "/"), 14, m.gl.ellipsis), 14))
		desc := truncate(e.Help, inner-32, m.gl.ellipsis)
		group := m.st.dim.Render(truncate(e.Group, 12, m.gl.ellipsis))
		line := pad(desc, inner-32) + " " + keyCol + " " + group
		if sel {
			return m.st.selected.Render(m.gl.cursor) + " " + line
		}
		return "  " + line
	})

	b.WriteString(m.st.dim.Render(strings.Repeat("─", inner)) + "\n")
	// The keys are in the footer below the frame.
	b.WriteString(m.st.dim.Render(fmt.Sprintf("%d/%d actions", len(p.shown), len(p.all))))
	return m.st.modal.Width(w).Render(b.String())
}
