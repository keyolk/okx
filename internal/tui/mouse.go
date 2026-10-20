package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/keyolk/tuikit/mouse"
	"github.com/keyolk/tuikit/render"
	"github.com/keyolk/tuikit/toast"
)

// Mouse is an augmentation of the keyboard, not a second command set: every
// gesture below resolves to an action in the binding table and runs through
// run, so a click does exactly what the key it stands for does.
//
// okx can unassign access, so one rule is stricter than the fleet default: no
// gesture applies a change. A click only moves a cursor, opens a screen, or
// opens the confirm modal the keys would open. Inside the confirm modal a
// click does nothing; only y / enter apply, and a click outside cancels.
//
//   - tab in the header: view.apps / view.groups / view.users.
//   - row: select it; a second click on the selected row is list.open.
//   - wheel over the body: three list.down / list.up (help.down / help.up on
//     the help page).
//   - picker and palette: a row click moves the cursor, a second click is
//     picker.commit / palette.run (which only opens the confirm modal), a
//     right click on a picker row is picker.toggle, a click outside is the
//     overlay's esc, the wheel is query.down / query.up.
//   - confirm and error modals: a click outside cancels / dismisses; nothing
//     inside is clickable.
//   - while a filter is being typed or a change is being applied: ignored.
func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.width == 0 || m.small || m.busyKind == busyApply {
		return m, nil
	}
	if mouse.IsLeftPress(msg) {
		// A click is a keystroke for toasts: an error seen before it is read.
		m.toasts = toast.OnKey(m.toasts)
	}
	if m.overlay != overlayNone {
		return m, m.overlayMouse(msg)
	}
	if m.filtering {
		return m, nil // clicks never type, and the filter keys own the screen
	}

	if d := mouse.Delta(msg); d != 0 {
		if !m.bodyRect().Contains(msg.X, msg.Y) {
			return m, nil
		}
		down, up := "list.down", "list.up"
		if m.screen == screenHelp {
			down, up = "help.down", "help.up"
		}
		return m, m.repeat(d, down, up)
	}
	if !mouse.IsLeftPress(msg) {
		return m, nil
	}

	for i, r := range m.tabRects() {
		if r.Contains(msg.X, msg.Y) {
			m.clicks = mouse.Clicks{}
			return m, m.run(m.topTabs()[i].action)
		}
	}
	return m, m.listClick(msg)
}

// repeat runs the down action d times, or the up action -d times.
func (m *Model) repeat(d int, down, up string) tea.Cmd {
	act := down
	if d < 0 {
		act, d = up, -d
	}
	var cmds []tea.Cmd
	for range d {
		cmds = append(cmds, m.run(act))
	}
	return tea.Batch(cmds...)
}

// listClick selects the row under the pointer; a second click on the row that
// was already selected is the drill-in key.
func (m *Model) listClick(msg tea.MouseMsg) tea.Cmd {
	l, ok := m.listFor(m.screen)
	if !ok {
		return nil
	}
	rect, start := m.listRect(l)
	hit := mouse.RowHit(rect, start, l.n, msg)
	if !hit.HasItem {
		m.clicks = mouse.Clicks{} // a blank click breaks a double
		return nil
	}
	wasSelected := *l.cur == hit.Index
	*l.cur = hit.Index
	m.clampList(l)
	if isTopLevel(m.screen) {
		m.saveUI()
	}
	var double bool
	m.clicks, double = m.clicks.Press(m.rowKey(hit.Index), m.clock())
	if double && wasSelected && m.keyEnabled("list.open") {
		return m.run("list.open")
	}
	return nil
}

// overlayMouse keeps the overlay's ownership of the mouse: nothing reaches
// the screen under it.
func (m *Model) overlayMouse(msg tea.MouseMsg) tea.Cmd {
	box := m.overlayRect()
	switch m.overlay {
	case overlayPickUser, overlayPickGroup, overlayPalette:
		if d := mouse.Delta(msg); d != 0 {
			return m.repeat(d, "query.down", "query.up")
		}
	}
	left, right := mouse.IsLeftPress(msg), mouse.IsRightPress(msg)
	if !left && !right {
		return nil
	}
	if !box.Contains(msg.X, msg.Y) {
		if !left {
			return nil
		}
		m.clicks = mouse.Clicks{}
		return m.run(overlayCancel(m.overlay))
	}

	// Inside the box only picker and palette rows respond. The confirm and
	// error modals have nothing to click: applying stays on y / enter.
	rect, total, cur, top, rows := m.overlayRows(box)
	if rect.H == 0 {
		return nil
	}
	hit := mouse.RowHit(rect, windowStart(total, rows, cur, top), total, msg)
	if !hit.HasItem {
		m.clicks = mouse.Clicks{}
		return nil
	}
	wasSelected := cur == hit.Index
	m.moveQuery(hit.Index - cur)
	if right {
		if m.overlay == overlayPalette {
			return nil
		}
		m.clicks = mouse.Clicks{}
		return m.run("picker.toggle")
	}
	var double bool
	m.clicks, double = m.clicks.Press(m.overlayRowKey(hit.Index), m.clock())
	if !double || !wasSelected {
		return nil
	}
	if m.overlay == overlayPalette {
		return m.run("palette.run")
	}
	return m.run("picker.commit")
}

// overlayCancel is the key that dismisses an overlay: what a click outside
// the box stands for.
func overlayCancel(o overlay) string {
	switch o {
	case overlayPickUser, overlayPickGroup:
		return "picker.cancel"
	case overlayPalette:
		return "palette.cancel"
	case overlayConfirm:
		return "confirm.no"
	}
	return "error.close"
}

// ---- geometry ---------------------------------------------------------------
//
// These read the numbers View draws with: headerLines, listHeight, windowRows
// and render.Window, plus the overlay box View centers.

// headerLines is the tabs line and the rule under it.
const headerLines = 2

// bodyRect is the column header and the list rows under it.
func (m *Model) bodyRect() mouse.Rect {
	return mouse.Rect{X: 0, Y: headerLines, W: m.width, H: m.listHeight() + 1}
}

// tabRects is where the header draws each tab label.
func (m *Model) tabRects() []mouse.Rect {
	x := ansi.StringWidth(headerBrand + headerGap)
	var out []mouse.Rect
	for _, t := range m.topTabs() {
		w := ansi.StringWidth(t.label)
		out = append(out, mouse.Rect{X: x, Y: 0, W: w, H: 1})
		x += w + ansi.StringWidth(tabSep)
	}
	return out
}

// listRect is where listLines draws l's rows, below the column header, and
// the first item it draws. The "more" line below the rows is not a row.
func (m *Model) listRect(l listRef) (mouse.Rect, int) {
	rows := windowRows(l.n, m.listHeight())
	return mouse.Rect{X: 0, Y: headerLines + 1, W: m.width, H: rows},
		windowStart(l.n, rows, *l.cur, *l.top)
}

func windowStart(total, rows, cur, top int) int {
	start, _, _ := render.Window(total, rows, cur, top)
	return start
}

// overlayRect is where render.Center puts the open modal.
func (m *Model) overlayRect() mouse.Rect {
	box := m.overlayBox()
	if box == "" {
		return mouse.Rect{}
	}
	w, h := 0, strings.Count(box, "\n")+1
	for _, l := range strings.Split(box, "\n") {
		w = max(w, ansi.StringWidth(l))
	}
	return mouse.Rect{X: max(0, (m.width-w)/2), Y: max(0, (m.height-h)/2), W: w, H: h}
}

// overlayRows is the rows of the picker or palette inside box: below the
// border, the title, the query line and the rule. An overlay without rows
// returns an empty rect.
func (m *Model) overlayRows(box mouse.Rect) (rect mouse.Rect, total, cur, top, rows int) {
	switch m.overlay {
	case overlayPickUser, overlayPickGroup:
		p := &m.pick
		total, cur, top, rows = len(p.matches), p.cur, p.top, m.pickerRows()
	case overlayPalette:
		p := &m.pal
		total, cur, top, rows = len(p.shown), p.cur, p.top, windowRows(len(p.shown), p.rows)
	default:
		return mouse.Rect{}, 0, 0, 0, 0
	}
	const chrome = 1 + 3 // border, then title / query / rule
	return mouse.Rect{X: box.X + 1, Y: box.Y + chrome, W: max(0, box.W-2), H: rows}, total, cur, top, rows
}

// ---- identity ---------------------------------------------------------------

// rowKey names the row at index i of the current list by its ID, so a double
// click needs the same item, not the same screen position. The screen is part
// of the key: the same ID on another screen is another row.
func (m *Model) rowKey(i int) string {
	var id string
	switch m.screen {
	case screenApps:
		id = m.filteredApps()[i].ID
	case screenGroups:
		id = m.filteredTopGroups()[i].ID
	case screenUsers:
		id = m.filteredTopUsers()[i].ID
	case screenAssignments:
		if m.showGroups {
			id = m.filteredGroups()[i].group.ID
		} else {
			id = m.filteredAssignments()[i].User.ID
		}
	case screenGroupApps:
		id = m.groupApps[i].ID
	case screenGroupMembers:
		id = m.filteredMembers()[i].user.ID
	case screenUserApps:
		id = m.revAccess[i].App.ID
	}
	return fmt.Sprintf("%d:%s", m.screen, id)
}

func (m *Model) overlayRowKey(i int) string {
	if m.overlay == overlayPalette {
		return "palette:" + m.pal.shown[i].Action
	}
	return "picker:" + m.pick.items[m.pick.matches[i].Index].id
}

func (m *Model) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}
