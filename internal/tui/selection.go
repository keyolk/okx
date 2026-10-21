package tui

import (
	"fmt"
	"unicode/utf8"

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

// selectionMouse feeds msg to the selection before the click handler. A press
// that does not move is left to handleMouse, so click-to-select still works;
// only what the selection consumed (a drag, its release, a word pick) skips it.
func (m *Model) selectionMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	r := m.textSel.Mouse(msg, m.selPanes(), m.View(), m.clock())
	var cmds []tea.Cmd
	if r.Scroll != 0 && r.ScrollPane == helpPaneID {
		cmds = append(cmds, m.repeat(r.Scroll, "help.down", "help.up"))
	}
	if r.Copy != "" {
		cmds = append(cmds, selection.Copy(r.Copy))
	}
	if r.Consumed {
		return m, tea.Batch(cmds...)
	}
	_, cmd := m.handleMouse(msg)
	return m, tea.Batch(append(cmds, cmd)...)
}

// copied reports the clipboard write: a copy is silent otherwise, and a
// failed one (no pbcopy, no /dev/tty) must not look like a success.
func (m *Model) copied(msg selection.CopiedMsg) {
	if msg.Err != nil {
		m.notify("copy", "copy failed: "+msg.Err.Error(), toast.Error)
		return
	}
	m.notify("copy", fmt.Sprintf("copied %d chars", utf8.RuneCountInString(msg.Text)), toast.Info)
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
	all, _ := keys.RenderHelp(sections, m.width, keys.HelpLineCount(sections, m.width), 0, m.helpStyle())
	rect := mouse.Rect{X: 0, Y: headerLines, W: m.width, H: rows}
	if len(all)-m.helpOff > rows {
		// RenderHelp spends the last row on "… N more" while lines remain
		// below. That row is not a help line, so it is left out: a drag onto
		// it scrolls instead of selecting the hidden line under the marker.
		rect.H--
	}
	return selection.Pane{ID: helpPaneID, Rect: rect, Kind: selection.Text, Lines: all, Offset: m.helpOff}
}

// overlayPanes is the open modal's content inside its border and padding,
// then its picker or palette rows. Listed last, the rows own their area.
func (m *Model) overlayPanes() []selection.Pane {
	box := m.overlayRect()
	if box.W == 0 {
		return nil
	}
	st := m.st.modal // modalWarn has the same frame
	left := st.GetBorderLeftSize() + st.GetPaddingLeft()
	top := st.GetBorderTopSize() + st.GetPaddingTop()
	content := mouse.Rect{
		X: box.X + left, Y: box.Y + top,
		W: max(0, box.W-st.GetHorizontalFrameSize()), H: max(0, box.H-st.GetVerticalFrameSize()),
	}
	panes := []selection.Pane{{ID: "modal", Rect: content, Kind: selection.Text}}
	if rows, _, _, _, _ := m.overlayRows(box); rows.H > 0 {
		// overlayRows spans the padding cells too (it hit-tests whole rows);
		// the selection keeps to the content columns.
		rows.X, rows.W = content.X, content.W
		panes = append(panes, selection.Pane{ID: "modal-rows", Rect: rows, Kind: selection.List})
	}
	return panes
}
