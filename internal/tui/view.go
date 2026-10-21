package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/keyolk/tuikit/input"
	"github.com/keyolk/tuikit/keys"
	"github.com/keyolk/tuikit/render"
	"github.com/keyolk/tuikit/selection"
	"github.com/keyolk/tuikit/toast"

	"github.com/keyolk/okx/internal/okta"
)

// View implements tea.Model: the frame with the drag selection on top.
func (m *Model) View() string {
	return m.textSel.Render(m.frame(), m.selPanes(), selection.DefaultStyle())
}

// frame is View without the selection highlight: the selection reads it to
// copy panes that have no Lines, and the highlight must not be copied along.
func (m *Model) frame() string {
	if m.width == 0 {
		return ""
	}
	if m.small {
		return m.renderTooSmall()
	}

	var body string
	switch {
	case m.refreshing() && m.app.Index.Empty() && isTopLevel(m.screen):
		body = m.renderLoadingBody()
	default:
		body = m.renderScreen()
	}

	frame := lipgloss.JoinVertical(lipgloss.Left, m.renderHeader(), body, m.renderFooter())
	if m.overlay != overlayNone {
		frame = m.renderOverlay(frame)
	}
	return frame
}

func (m *Model) renderScreen() string {
	var body string
	switch m.screen {
	case screenApps:
		body = m.renderApps()
	case screenGroups:
		body = m.renderTopGroups()
	case screenUsers:
		body = m.renderTopUsers()
	case screenAssignments:
		body = m.renderAssignments()
	case screenGroupApps:
		body = m.renderGroupApps()
	case screenGroupMembers:
		body = m.renderGroupMembers()
	case screenUserApps:
		body = m.renderUserApps()
	case screenHelp:
		body = m.renderHelp()
	}
	return body
}

func (m *Model) renderTooSmall() string {
	msg := fmt.Sprintf("terminal too small\nneed at least %d×%d, have %d×%d",
		minWidth, minHeight, m.width, m.height)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		m.st.warn.Render(msg))
}

// ---- chrome ---------------------------------------------------------------

func (m *Model) renderHeader() string {
	left := m.renderTopTabs()
	// The title names what a drilled-in screen is about: name links to its
	// admin page (when known), rest is the plain " · N apps" after it.
	var u, name, rest string
	switch m.screen {
	case screenApps, screenGroups, screenUsers:
		// The counts moved into the tab badges, where all three are visible.
		rest = shortOrg(m.app.Cfg.OrgURL)
	case screenAssignments:
		tab := "users"
		if m.showGroups {
			tab = "groups"
		}
		u, name, rest = m.appURL(m.curApp), m.curApp.Label, " · "+tab
	case screenGroupApps:
		u, name, rest = m.groupURL(m.curGroup), m.curGroup.Profile.Name, fmt.Sprintf(" · %d apps", len(m.groupApps))
	case screenGroupMembers:
		u, name, rest = m.groupURL(m.curGroup), m.curGroup.Profile.Name, m.groupMembersSuffix()
	case screenUserApps:
		u, name, rest = m.userURL(m.revUser), m.revUser.Profile.Login, fmt.Sprintf(" · %d apps", len(m.revAccess))
	case screenHelp:
		rest = "help"
	}

	rightWidth := m.width - lipgloss.Width(left) - 1
	if rightWidth < 0 {
		rightWidth = 0
	}
	right := m.linkTitle(u, name, rest, rightWidth)
	line := padBetween(left, m.st.dim.Render(right), m.width)
	sep := m.st.dim.Render(strings.Repeat("─", m.width))
	if m.gl.check == asciiGlyphs.check {
		sep = m.st.dim.Render(strings.Repeat("-", m.width))
	}
	return line + "\n" + sep
}

func (m *Model) renderFooter() string {
	// The status row sits above the hints so the hints never move. The filter
	// prompt comes first: it is the only echo of what the user is typing, and
	// a toast or a refresh progress line used to hide it.
	var status string
	switch {
	case m.filtering:
		if f, ok := m.filterInput(); ok {
			status = m.st.filter.Render("/") + f.View(m.width-1, true, m.inputStyle())
		}
	case m.busyKind != busyNone:
		status = m.renderProgress()
	case m.toasts.Len() > 0 && !m.toasts.Held():
		status = toast.View(m.toasts, m.width, m.toastStyles())
	default:
		if filter, ok := m.currentFilter(); ok && *filter != "" {
			status = m.st.dim.Render("filter: ") + m.st.filter.Render(*filter) +
				m.st.dim.Render("  ("+m.keyName(m.active(), "filter")+" to edit)")
		}
	}
	return status + "\n" + m.footerFor(m.active(), m.width)
}

// footerFor is the hint line for a context stack, derived from the binding
// table: what it shows is exactly what the keys do there.
func (m *Model) footerFor(stack []keys.Ctx, width int) string {
	return m.km.Or(defaultKeymap()).Footer(stack, width, m.keyOpts(), m.footerStyle())
}

// inputStyle draws every text field: the filter, the picker query and the
// palette query.
func (m *Model) inputStyle() input.Style {
	return input.Style{Text: m.st.filter, Placeholder: m.st.dim}
}

func (m *Model) toastStyles() toast.Styles {
	return toast.Styles{Info: m.st.info, Success: m.st.success, Warn: m.st.warn, Error: m.st.err,
		ASCII: m.gl.check == asciiGlyphs.check}
}

// ---- app list -------------------------------------------------------------

func (m *Model) renderApps() string {
	rows := m.filteredApps()
	h := m.listHeight()

	// Column widths: label takes what's left after the fixed-width numerics.
	const wUsers, wDirect, wGroups, wMode = 6, 7, 7, 16
	// 4 gaps between the five columns plus the cursor cell every row starts
	// with; without the cursor cell each row was one cell wider than the
	// terminal and wrapped, shifting every row below it.
	wLabel := m.width - wUsers - wDirect - wGroups - wMode - 5
	if wLabel < 12 {
		wLabel = 12
	}

	var b strings.Builder
	b.WriteString(m.st.header.Render(
		render.PadRight("APP", wLabel) + " " + render.PadLeft("USERS", wUsers) + " " +
			render.PadLeft("DIRECT", wDirect) + " " + render.PadLeft("GROUPS", wGroups) + " " +
			render.PadRight("SIGN-ON", wMode)))
	b.WriteByte('\n')

	m.listLines(&b, len(rows), m.appCur, m.appTop, h, func(i int, sel bool) string {
		a := rows[i]
		users := len(m.app.Index.AppUsers[a.ID])
		direct := 0
		for _, au := range m.app.Index.AppUsers[a.ID] {
			if au.Scope == "USER" {
				direct++
			}
		}
		groups := len(m.app.Index.AppGroups[a.ID])

		line := m.linkCell(m.appURL(a), a.Label, wLabel) + " " +
			render.PadLeft(strconv.Itoa(users), wUsers) + " " +
			render.PadLeft(strconv.Itoa(direct), wDirect) + " " +
			render.PadLeft(strconv.Itoa(groups), wGroups) + " " +
			render.PadRight(render.TruncateMarker(a.SignOnMode, wMode, m.gl.ellipsis), wMode)

		if sel {
			return m.st.selected.Render(m.gl.cursor + render.TruncateMarker(line, m.width-2, m.gl.ellipsis))
		}
		return " " + line
	})
	return fillHeight(b.String(), h+1)
}

// ---- assignment list ------------------------------------------------------

func (m *Model) renderAssignments() string {
	if m.showGroups {
		return m.renderAppGroups()
	}
	rows := m.filteredAssignments()
	h := m.listHeight()

	const wStatus = 14
	wLogin := clampInt(m.width/3, 16, 34)
	wVia := m.width - wLogin - wStatus - 4
	if wVia < 12 {
		wVia = 12
	}

	var b strings.Builder
	b.WriteString(m.st.header.Render(
		render.PadRight("LOGIN", wLogin) + " " + render.PadRight("STATUS", wStatus) + " " + render.PadRight("ACCESS VIA", wVia)))
	b.WriteByte('\n')

	m.listLines(&b, len(rows), m.asgCur, m.asgTop, h, func(i int, sel bool) string {
		a := rows[i]
		// Marker doubles the direct/group signal so it survives monochrome.
		marker := m.st.warn.Render("G")
		if a.Direct {
			marker = m.st.success.Render("D")
		}
		via := accessVia(a.Direct, a.ViaGroups)
		line := m.linkCell(m.userURL(a.User), a.User.Profile.Login, wLogin-2) + " " +
			render.PadRight(render.TruncateMarker(a.Status, wStatus, m.gl.ellipsis), wStatus) + " " +
			render.TruncateMarker(via, wVia, m.gl.ellipsis)

		if sel {
			return m.st.selected.Render(m.gl.cursor) + marker + " " + m.st.selected.Render(line)
		}
		return " " + marker + " " + line
	})
	return fillHeight(b.String(), h+1)
}

func (m *Model) renderAppGroups() string {
	rows := m.filteredGroups()
	h := m.listHeight()

	const wMembers = 9
	wName := m.width - wMembers - 26
	if wName < 16 {
		wName = 16
	}

	var b strings.Builder
	b.WriteString(m.st.header.Render(
		render.PadRight("GROUP", wName) + " " + render.PadLeft("MEMBERS", wMembers) + "  " + render.PadRight("ID", 22)))
	b.WriteByte('\n')

	m.listLines(&b, len(rows), m.asgCur, m.asgTop, h, func(i int, sel bool) string {
		g := rows[i]
		line := m.linkCell(m.groupURL(g.group), g.group.Profile.Name, wName) + " " +
			render.PadLeft(strconv.Itoa(g.members), wMembers) + "  " +
			m.st.dim.Render(g.group.ID)
		if sel {
			return m.st.selected.Render(m.gl.cursor + line)
		}
		return " " + line
	})
	return fillHeight(b.String(), h+1)
}

func (m *Model) renderUserApps() string {
	h := m.listHeight()
	wApp := clampInt(m.width/3, 16, 36)
	wVia := m.width - wApp - 18
	if wVia < 12 {
		wVia = 12
	}

	var b strings.Builder
	b.WriteString(m.st.header.Render(
		render.PadRight("APP", wApp) + " " + render.PadRight("STATUS", 14) + " " + render.PadRight("ACCESS VIA", wVia)))
	b.WriteByte('\n')

	m.listLines(&b, len(m.revAccess), m.revCur, m.revTop, h, func(i int, sel bool) string {
		a := m.revAccess[i]
		marker := m.st.warn.Render("G")
		if a.Direct {
			marker = m.st.success.Render("D")
		}
		line := m.linkCell(m.appURL(a.App), a.App.Label, wApp-2) + " " +
			render.PadRight(render.TruncateMarker(a.Status, 14, m.gl.ellipsis), 14) + " " +
			render.TruncateMarker(accessVia(a.Direct, a.ViaGroups), wVia, m.gl.ellipsis)
		if sel {
			return m.st.selected.Render(m.gl.cursor) + marker + " " + m.st.selected.Render(line)
		}
		return " " + marker + " " + line
	})
	return fillHeight(b.String(), h+1)
}

// helpSections documents every screen, the current one first, from the same
// table dispatch reads; the legend that explains the list columns follows.
func (m *Model) helpSections() []keys.Section {
	stacks := append([][]keys.Ctx{stackFor(m.helpBack)}, allStacks...)
	secs := m.km.Or(defaultKeymap()).Help(m.keyOpts(), stacks...)
	return append(secs, legendSections...)
}

// legendSections explain what the lists show. They are not actions, so they
// carry no Action and are not part of the binding table.
var legendSections = []keys.Section{{
	Title: "reading the lists",
	Entries: []keys.Entry{
		{Keys: []string{"D"}, Help: "direct assignment — d removes it"},
		{Keys: []string{"G"}, Help: "access comes from a group — remove the group instead"},
		{Keys: []string{"direct + group"}, Help: "d leaves group access intact"},
		{Keys: []string{"KEEPS"}, Help: "apps the member also holds directly; unassigning the group leaves them"},
		{Keys: []string{"? in MEMBERS"}, Help: "membership not in the snapshot yet — open the group to resolve it"},
	},
	Notes: []string{"Browsing stays live during a refresh; assign and remove wait for it to land."},
}}

func (m *Model) helpRows() int { return m.listHeight() + 1 }

// scrollHelp moves the help page and stores the offset RenderHelp clamps, so
// holding j past the end does not grow it.
func (m *Model) scrollHelp(delta int) {
	_, m.helpOff = keys.RenderHelp(m.helpSections(), max(m.width, 1), m.helpRows(), m.helpOff+delta, keys.HelpStyle{})
}

func (m *Model) renderHelp() string {
	lines, _ := keys.RenderHelp(m.helpSections(), m.width, m.helpRows(), m.helpOff, m.helpStyle())
	return fillHeight(strings.Join(lines, "\n")+"\n", m.helpRows())
}

// ---- overlays -------------------------------------------------------------

func (m *Model) renderOverlay(base string) string {
	box := m.overlayBox()
	if box == "" {
		return base
	}
	// Over the screen, not instead of it (E8): the list being acted on stays.
	return render.Center(base, box, m.width, m.height)
}

// overlayBox draws the modal for the open overlay; the mouse handler measures
// the same string to know where the box landed.
func (m *Model) overlayBox() string {
	switch m.overlay {
	case overlayPickUser, overlayPickGroup:
		return m.renderPicker()
	case overlayConfirm:
		return m.renderConfirm()
	case overlayError:
		return m.st.modalWarn.Render(
			m.st.err.Render("error") + "\n\n" +
				wrap(m.errText, minInt(m.width-8, 76)) + "\n\n" +
				m.footerFor(stackError, minInt(m.width-8, 76)))
	case overlayPalette:
		return m.renderPalette()
	}
	return ""
}

func (m *Model) renderPicker() string {
	p := &m.pick
	w := clampInt(m.width-10, 40, 76)
	inner := w - 2 // modal padding; a full-width rule used to wrap onto a second line
	h := m.pickerHeight()

	title := "assign users to " + m.curApp.Label
	if p.kind == "group" {
		title = "assign groups to " + m.curApp.Label
	}

	var b strings.Builder
	b.WriteString(m.st.title.Render(render.TruncateMarker(title, w, m.gl.ellipsis)) + "\n")
	b.WriteString(m.st.filter.Render("/") + p.query.View(inner-1, true, m.inputStyle()) + "\n")
	b.WriteString(m.st.dim.Render(strings.Repeat("─", inner)) + "\n")

	if len(p.matches) == 0 {
		b.WriteString(m.st.dim.Render("  no match") + "\n")
	}
	m.listLines(&b, len(p.matches), p.cur, p.top, h, func(i int, sel bool) string {
		it := p.items[p.matches[i].Index]

		mark := m.gl.uncheck
		if p.chosen[it.id] {
			mark = m.st.checked.Render(m.gl.check)
		}

		label := it.label
		desc := it.desc
		if it.already != "" {
			desc = it.already
		}
		wLabel := clampInt(w/2, 14, 40)
		line := render.PadRight(render.TruncateMarker(label, wLabel, m.gl.ellipsis), wLabel) + " "
		if it.already != "" {
			line += m.st.warn.Render(render.TruncateMarker(desc, w-wLabel-6, m.gl.ellipsis))
		} else {
			line += m.st.dim.Render(render.TruncateMarker(desc, w-wLabel-6, m.gl.ellipsis))
		}

		if sel {
			return m.st.selected.Render(m.gl.cursor) + mark + " " + line
		}
		return "  " + mark + " " + line
	})

	b.WriteString(m.st.dim.Render(strings.Repeat("─", inner)) + "\n")
	// The keys are in the footer below the frame; the box says what is chosen.
	sel := m.st.dim.Render(fmt.Sprintf("%d match(es)", len(p.matches)))
	if len(p.chosen) > 0 {
		sel = m.st.success.Render(fmt.Sprintf("%d selected", len(p.chosen)))
	}
	b.WriteString(sel)

	return m.st.modal.Width(w).Render(b.String())
}

func (m *Model) renderConfirm() string {
	w := clampInt(m.width-10, 40, 76)

	var b strings.Builder
	verb := "apply"
	if len(m.pending) > 0 {
		verb = m.pending[0].verb
	}
	b.WriteString(m.st.title.Render(verb+" on "+render.TruncateMarker(m.curApp.Label, w-16, m.gl.ellipsis)) + "\n\n")

	applicable := 0
	for _, ch := range m.pending {
		sign := m.st.success.Render(m.gl.plus)
		if ch.verb == "unassign" {
			sign = m.st.err.Render(m.gl.minus)
		}
		line := sign + " " + m.st.dim.Render(ch.kind+" ") + ch.label
		if ch.noop != "" {
			line += m.st.dim.Render("  (skip)")
		} else {
			applicable++
		}
		b.WriteString(render.TruncateMarker(line, w, m.gl.ellipsis) + "\n")
		if ch.noop != "" {
			b.WriteString(m.st.dim.Render("    "+wrapIndent(ch.noop, w-4, "    ")) + "\n")
		}
		if ch.warn != "" {
			b.WriteString(m.st.warn.Render("  ! "+wrapIndent(ch.warn, w-4, "    ")) + "\n")
		}
	}

	b.WriteString("\n")
	if applicable == 0 {
		b.WriteString(m.st.dim.Render("nothing to apply  ") + m.footerFor(stackConfirm, w-18))
		return m.st.modal.Width(w).Render(b.String())
	}
	ask := m.st.footer.Render(fmt.Sprintf("apply %d change(s)?  ", applicable))
	b.WriteString(ask + m.footerFor(stackConfirm, w-lipgloss.Width(ask)))

	style := m.st.modal
	for _, ch := range m.pending {
		if ch.verb == "unassign" && ch.noop == "" {
			style = m.st.modalWarn
			break
		}
	}
	return style.Width(w).Render(b.String())
}

// ---- text helpers ---------------------------------------------------------

// accessVia renders why a principal has an app: "direct", the group names, or
// both. The wording matches `okx show` so the TUI and CLI never disagree.
func accessVia(direct bool, groups []okta.Group) string {
	var parts []string
	if direct {
		parts = append(parts, "direct")
	}
	for _, g := range groups {
		parts = append(parts, "group:"+g.Profile.Name)
	}
	if len(parts) == 0 {
		return "group (unresolved)"
	}
	return strings.Join(parts, ", ")
}

func shortOrg(url string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://")
	if i := strings.Index(s, "."); i > 0 {
		return s[:i]
	}
	return s
}

// padBetween places left and right at the edges of a w-cell line.
func padBetween(left, right string, w int) string {
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return render.Truncate(left, w)
	}
	return left + strings.Repeat(" ", gap) + right
}

// fillHeight pads a block to exactly n lines so the footer never shifts as
// the list shortens.
func fillHeight(s string, n int) string {
	lines := strings.Count(s, "\n")
	if strings.HasSuffix(s, "\n") {
		s = strings.TrimSuffix(s, "\n")
		lines--
	}
	for i := lines; i < n-1; i++ {
		s += "\n"
	}
	return s
}

// wrap word-wraps s to w display cells. Byte slicing split multi-byte runes
// (Korean names in error text) and counted bytes, not cells.
func wrap(s string, w int) string {
	if w < 8 {
		w = 8
	}
	return ansi.Wrap(s, w, "")
}

func wrapIndent(s string, w int, indent string) string {
	return strings.ReplaceAll(wrap(s, w), "\n", "\n"+indent)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
