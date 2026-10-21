package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/keyolk/tuikit/keys"
	"github.com/keyolk/tuikit/mouse"
	"github.com/keyolk/tuikit/selection"
	"github.com/keyolk/tuikit/toast"
)

// Drag selection (E14): a drag selects text inside the pane under the press
// and the release copies it, the way the mod engine selects. Mouse capture
// turned the terminal's own drag-select off, and an Okta ID or login is the
// thing most worth copying out of okx.
//
//   - the list body (column header and rows) is a List pane: a second click on
//     the selected row stays list.open, so it gets no word or line pick.
//   - the help page is a Text pane with every help line, so a drag past its
//     edge scrolls it through help.down / help.up, as the wheel does.
//   - an open modal is the only thing selectable: its content (title, query,
//     error text, planned changes) is a Text pane, and the picker or palette
//     rows inside it are a List pane on top, so their second click still
//     commits. A drag over the query never moves its cursor: the selection
//     consumes it before overlayMouse sees anything.
//
// Selection never applies anything: the copy is the only side effect.

// selectionUpdate feeds a mouse event or an auto-scroll tick to the selection
// before the click handler. A press that does not move is left to
// handleMouse, so click-to-select still works; only what the selection
// consumed (a drag, its release, a word pick) skips it.
func (m *Model) selectionUpdate(msg tea.Msg) (tea.Model, tea.Cmd) {
	r, cmd := m.textSel.Update(msg, m.selPanes, m.frame, m.clock())
	if r.Scroll != 0 && r.ScrollPane == helpPaneID {
		// Set, not add: the pane was built from the offset View drew with,
		// and a helpOff a resize left past the end would swallow a scroll up.
		m.helpOff = r.Offset
	}
	mm, ok := msg.(tea.MouseMsg)
	if r.Consumed || !ok {
		return m, cmd
	}
	_, c := m.handleMouse(mm)
	return m, tea.Batch(cmd, c)
}

// copied reports the clipboard write: a copy is silent otherwise, and a
// failed one (no pbcopy, no /dev/tty) must not look like a success.
func (m *Model) copied(msg selection.CopiedMsg) {
	level := toast.Info
	if msg.Err != nil {
		level = toast.Error
	}
	m.notify("copy", msg.Note(), level)
}

const helpPaneID = "help"

// selPanes lists the selectable regions in drawing order, from the numbers
// View and the click handler use (bodyRect, overlayRect, overlayRows).
func (m *Model) selPanes() []selection.Pane {
	if m.width == 0 || m.small {
		return nil
	}
	if m.overlay != overlayNone {
		return m.overlayPanes()
	}
	if m.screen == screenHelp {
		return []selection.Pane{m.helpPane()}
	}
	return []selection.Pane{{ID: "list", Rect: m.bodyRect(), Kind: selection.List}}
}

// helpPane is the help page with all of its lines. renderHelp draws
// RenderHelp(...)[helpOff:] from the row under the header, so the full list
// at offset 0 lines up with it row for row.
func (m *Model) helpPane() selection.Pane {
	sections := m.helpSections()
	rows := m.helpRows()
	all := keys.HelpLines(sections, m.width, m.helpStyle())
	// The offset renderHelp draws with: RenderHelp clamps helpOff, which a
	// resize can leave past the end.
	_, off := keys.RenderHelp(sections, m.width, rows, m.helpOff, keys.HelpStyle{})
	rect := mouse.Rect{X: 0, Y: headerLines, W: m.width, H: rows}
	if len(all)-off > rows {
		// RenderHelp spends the last row on "… N more" while lines remain
		// below. That row is not a help line, so it is left out: a drag onto
		// it scrolls instead of selecting the hidden line under the marker.
		rect.H--
	}
	return selection.Pane{ID: helpPaneID, Rect: rect, Kind: selection.Text, Lines: all, Offset: off}
}

// overlayPanes is the open modal's content inside its border and padding,
// then its picker or palette rows. Listed last, the rows own their area.
func (m *Model) overlayPanes() []selection.Pane {
	box := m.overlayRect()
	if box.W == 0 {
		return nil
	}
	content := box.InsetStyle(m.st.modal) // modalWarn has the same frame
	panes := []selection.Pane{{ID: "modal", Rect: content, Kind: selection.Text}}
	if rows, _, _, _, _ := m.overlayRows(box); rows.H > 0 {
		// overlayRows spans the padding cells too (it hit-tests whole rows);
		// the selection keeps to the content columns.
		rows.X, rows.W = content.X, content.W
		panes = append(panes, selection.Pane{ID: "modal-rows", Rect: rows, Kind: selection.List})
	}
	return panes
}
