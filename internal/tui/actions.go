package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/keyolk/tuikit/input"
	"github.com/keyolk/tuikit/keys"
	"github.com/keyolk/tuikit/link"
	"github.com/keyolk/tuikit/render"
	"github.com/keyolk/tuikit/toast"
)

// handleKey resolves a key against the binding table and runs its action.
// There is one dispatcher: the per-screen switches it replaced repeated
// j/k/g/G/ctrl+d six times and had drifted (the user-apps screen lacked
// half-page moves while its footer offered an enter that did nothing).
func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Any key acknowledges a sticky error that has been on screen long
	// enough to be read; transient toasts expire on their own ticks.
	m.toasts = toast.OnKey(m.toasts)

	km := m.km.Or(defaultKeymap())
	stack := m.active()
	var (
		b  keys.Binding
		ok bool
	)
	if m.isInTextInput() {
		// The jamo IS the intended input in a text field, so Korean can be
		// typed into a filter or query; Resolve would rewrite ㅂ to q.
		b, ok = km.ResolveRaw(stack, msg, m.keyOpts())
	} else {
		b, ok = km.Resolve(stack, msg, m.keyOpts())
	}
	if !ok {
		if m.isInTextInput() {
			// Typing, pastes, cursor moves and word deletes: input.Field
			// takes every key the table does not claim.
			m.editText(func(f input.Field) input.Field {
				f, _ = f.Update(msg)
				return f
			})
		}
		return m, nil
	}
	return m, m.run(b.Action)
}

// isInTextInput reports whether keys are text being typed rather than
// shortcuts.
func (m *Model) isInTextInput() bool {
	if m.busyKind == busyApply {
		return false
	}
	switch m.overlay {
	case overlayPickUser, overlayPickGroup, overlayPalette:
		return true
	case overlayNone:
		return m.filtering
	}
	return false
}

// run performs one named action. The palette calls it too, so a palette
// entry and its key can never do different things.
func (m *Model) run(action string) tea.Cmd {
	switch action {
	case "quit":
		return m.quit()
	case "help":
		if m.screen == screenHelp {
			m.screen = m.helpBack
		} else {
			m.helpBack, m.screen, m.helpOff = m.screen, screenHelp, 0
		}
	case "help.close":
		m.screen = m.helpBack
	case "help.down":
		m.scrollHelp(1)
	case "help.up":
		m.scrollHelp(-1)
	case "help.pagedown":
		m.scrollHelp(m.listHeight() / 2)
	case "help.pageup":
		m.scrollHelp(-m.listHeight() / 2)

	case "view.apps":
		m.switchTopLevel(screenApps)
	case "view.groups":
		m.switchTopLevel(screenGroups)
	case "view.users":
		m.switchTopLevel(screenUsers)
	case "refresh":
		if m.refreshing() {
			m.notify("refresh", "already refreshing", toast.Info)
			return nil
		}
		return m.startRefresh()
	case "filter":
		m.filtering = true
	case "palette":
		m.openPalette()
	case "back":
		m.back()

	case "list.down":
		m.moveCursor(1, false)
	case "list.up":
		m.moveCursor(-1, false)
	case "list.top":
		m.moveCursor(0, true)
	case "list.bottom":
		m.moveCursor(-1, true)
	case "list.pagedown":
		m.moveCursor(m.listHeight()/2, false)
	case "list.pageup":
		m.moveCursor(-m.listHeight()/2, false)
	case "list.open":
		return m.openSelected()
	case link.Action:
		return m.openAdminLink()

	case "app.tab":
		m.showGroups = !m.showGroups
		m.asgCur, m.asgTop = 0, 0
		m.asgFiltr = ""
	case "app.assign":
		if m.blockedByRefresh() {
			return nil
		}
		if m.showGroups {
			m.openPicker(overlayPickGroup)
		} else {
			m.openPicker(overlayPickUser)
		}
	case "app.assignGroup":
		if m.blockedByRefresh() {
			return nil
		}
		m.openPicker(overlayPickGroup)
	case "app.remove":
		if m.blockedByRefresh() {
			return nil
		}
		m.planRemoveSelected()
	case "members.apps":
		m.screen = screenGroupApps
		m.groupAppBack = m.memberBack
	case "groupapp.members":
		return m.openGroupMembers(m.groupAppBack)

	case "filter.accept":
		m.filtering = false
		m.saveUI()
	case "filter.cancel":
		m.filtering = false
		m.editFilter(func(input.Field) input.Field { return input.Field{} })
	case "edit.delete":
		// Through the field, so a rebound key deletes before the cursor
		// exactly as backspace does.
		m.editText(func(f input.Field) input.Field {
			f, _ = f.Update(tea.KeyMsg{Type: tea.KeyBackspace})
			return f
		})
	case "edit.clear":
		m.editText(func(input.Field) input.Field { return input.Field{} })

	case "query.down":
		m.moveQuery(1)
	case "query.up":
		m.moveQuery(-1)
	case "query.pagedown":
		m.moveQuery(m.pickerHeight() / 2)
	case "query.pageup":
		m.moveQuery(-m.pickerHeight() / 2)

	case "picker.toggle":
		m.togglePicked()
	case "picker.commit":
		m.commitPicker()
	case "picker.cancel":
		m.overlay = overlayNone

	case "palette.run":
		return m.runPalette()
	case "palette.cancel":
		m.overlay = overlayNone

	case "confirm.yes", "confirm.enter":
		return m.confirmApply()
	case "confirm.no":
		m.confirmCancel()
	case "error.close":
		m.overlay = overlayNone
		m.errText = ""
	}
	return nil
}

// quit writes the UI state first; the final prefs.Saved quits (update).
// FlushWith takes the state as it is now, so a change no handler saved (a
// cursor moved by a refresh) is not lost.
func (m *Model) quit() tea.Cmd {
	var cmd tea.Cmd
	m.saver, cmd = m.saver.FlushWith(m.uiState())
	return cmd
}

// editText applies f to the text field that owns the keyboard and refilters
// when the text changed; a cursor move leaves the list where it is.
func (m *Model) editText(f func(input.Field) input.Field) {
	switch m.overlay {
	case overlayPickUser, overlayPickGroup:
		old := m.pick.query.Value()
		if m.pick.query = f(m.pick.query); m.pick.query.Value() != old {
			m.pick.refilter()
		}
	case overlayPalette:
		old := m.pal.query.Value()
		if m.pal.query = f(m.pal.query); m.pal.query.Value() != old {
			m.pal.refilter()
		}
	case overlayNone:
		if m.filtering {
			m.editFilter(f)
		}
	}
}

// filterInput is the field for the current screen's filter, rebuilt from
// the filter string when the two disagree (another screen's filter, a
// restored ui.json, a refresh).
func (m *Model) filterInput() (input.Field, bool) {
	target, ok := m.currentFilter()
	if !ok {
		return input.Field{}, false
	}
	if m.filterField.Value() != *target {
		return input.New(*target), true
	}
	return m.filterField, true
}

func (m *Model) editFilter(f func(input.Field) input.Field) {
	field, ok := m.filterInput()
	if !ok {
		m.filtering = false
		return
	}
	target, _ := m.currentFilter()
	m.filterField = f(field)
	if v := m.filterField.Value(); v != *target {
		*target = v
		m.resetCurrentCursor()
		m.saveUI()
	}
}

func (m *Model) moveQuery(delta int) {
	switch m.overlay {
	case overlayPickUser, overlayPickGroup:
		m.movePicker(delta)
	case overlayPalette:
		m.pal.move(delta)
	}
}

// ---- lists ------------------------------------------------------------------

// listRef addresses the cursor and scroll offset of one list, so every list
// shares one set of movement actions.
type listRef struct {
	cur, top *int
	n        int
}

func (m *Model) listFor(s screen) (listRef, bool) {
	switch s {
	case screenApps:
		return listRef{&m.appCur, &m.appTop, len(m.filteredApps())}, true
	case screenGroups:
		return listRef{&m.groupCur, &m.groupTop, len(m.filteredTopGroups())}, true
	case screenUsers:
		return listRef{&m.userCur, &m.userTop, len(m.filteredTopUsers())}, true
	case screenAssignments:
		return listRef{&m.asgCur, &m.asgTop, m.assignmentRowCount()}, true
	case screenGroupApps:
		return listRef{&m.groupAppCur, &m.groupAppTop, len(m.groupApps)}, true
	case screenGroupMembers:
		return listRef{&m.memberCur, &m.memberTop, len(m.filteredMembers())}, true
	case screenUserApps:
		return listRef{&m.revCur, &m.revTop, len(m.revAccess)}, true
	}
	return listRef{}, false
}

// moveCursor moves the current list's cursor by delta, or to an end when abs
// is set (delta 0 is the top, -1 the bottom).
func (m *Model) moveCursor(delta int, abs bool) {
	l, ok := m.listFor(m.screen)
	if !ok {
		return
	}
	switch {
	case abs && delta < 0:
		*l.cur = l.n - 1
	case abs:
		*l.cur = delta
	default:
		*l.cur += delta
	}
	m.clampList(l)
	if isTopLevel(m.screen) {
		m.saveUI()
	}
}

// clampList keeps the cursor in range and visible. The offset is state, so
// it is kept here, in Update, and View only reads it.
func (m *Model) clampList(l listRef) {
	*l.cur = m.clampIdx(*l.cur, l.n)
	_, _, *l.top = render.Window(l.n, windowRows(l.n, m.listHeight()), *l.cur, *l.top)
}

// openSelected drills into the row under the cursor.
func (m *Model) openSelected() tea.Cmd {
	switch m.screen {
	case screenApps:
		rows := m.filteredApps()
		if len(rows) == 0 {
			return nil
		}
		m.curApp = rows[m.clampIdx(m.appCur, len(rows))]
		m.assignmentBack = screenApps
		m.screen = screenAssignments
		m.showGroups = false
		m.loadAssignments()
	case screenGroups:
		rows := m.filteredTopGroups()
		if len(rows) == 0 {
			return nil
		}
		m.curGroup = rows[m.clampIdx(m.groupCur, len(rows))]
		// Members first: "who is in this group" is the question the group list
		// raises. The apps it grants are one tab away.
		m.loadGroupApps()
		m.groupAppBack = screenGroups
		return m.openGroupMembers(screenGroups)
	case screenUsers:
		rows := m.filteredTopUsers()
		if len(rows) == 0 {
			return nil
		}
		m.openUserApps(rows[m.clampIdx(m.userCur, len(rows))], screenUsers)
	case screenAssignments:
		return m.drillIntoSelected()
	case screenGroupApps:
		if len(m.groupApps) == 0 {
			return nil
		}
		m.curApp = m.groupApps[m.clampIdx(m.groupAppCur, len(m.groupApps))]
		m.assignmentBack = screenGroupApps
		m.screen = screenAssignments
		m.showGroups = true
		m.loadAssignments()
	case screenGroupMembers:
		rows := m.filteredMembers()
		if len(rows) == 0 {
			return nil
		}
		m.openUserApps(rows[m.clampIdx(m.memberCur, len(rows))].user, screenGroupMembers)
	}
	return nil
}

// windowRows is how many rows a list of total items draws in height rows:
// all of them when they fit, else one fewer to make room for the `N more`
// line (E7).
func windowRows(total, height int) int {
	if total <= height || height < 2 {
		return height
	}
	return height - 1
}
