package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/keyolk/okx/internal/okta"
)

// View implements tea.Model.
func (m *Model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.small {
		return m.renderTooSmall()
	}

	var body string
	switch m.screen {
	case screenApps:
		body = m.renderApps()
	case screenAssignments:
		body = m.renderAssignments()
	case screenUserApps:
		body = m.renderUserApps()
	case screenHelp:
		body = m.renderHelp()
	}

	base := lipgloss.JoinVertical(lipgloss.Left, m.renderHeader(), body, m.renderFooter())
	if m.overlay == overlayNone {
		return base
	}
	return m.renderOverlay(base)
}

func (m *Model) renderTooSmall() string {
	msg := fmt.Sprintf("terminal too small\nneed at least %d×%d, have %d×%d",
		minWidth, minHeight, m.width, m.height)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		m.st.warn.Render(msg))
}

// ---- chrome ---------------------------------------------------------------

func (m *Model) renderHeader() string {
	var left, right string
	switch m.screen {
	case screenApps:
		left = m.st.title.Render("okx") + m.st.dim.Render("  apps")
		right = m.st.dim.Render(fmt.Sprintf("%d apps · %s",
			len(m.apps), shortOrg(m.app.Cfg.OrgURL)))
	case screenAssignments:
		tab := "users"
		if m.showGroups {
			tab = "groups"
		}
		left = m.st.title.Render(m.curApp.Label) + m.st.dim.Render("  "+tab)
		direct := 0
		for _, a := range m.assignments {
			if a.Direct {
				direct++
			}
		}
		right = m.st.dim.Render(fmt.Sprintf("%d users (%d direct) · %d groups",
			len(m.assignments), direct, len(m.appGroups)))
	case screenUserApps:
		left = m.st.title.Render(m.revUser.Profile.Login) +
			m.st.dim.Render("  "+m.revUser.Name())
		right = m.st.dim.Render(fmt.Sprintf("%d app(s) · %s", len(m.revAccess), m.revUser.Status))
	case screenHelp:
		left = m.st.title.Render("okx") + m.st.dim.Render("  help")
	}

	line := padBetween(left, right, m.width)
	sep := m.st.dim.Render(strings.Repeat("─", m.width))
	if m.gl.check == asciiGlyphs.check {
		sep = m.st.dim.Render(strings.Repeat("-", m.width))
	}
	return line + "\n" + sep
}

func (m *Model) renderFooter() string {
	// Status line (transient) sits above the hints so the hints never move.
	var status string
	switch {
	case m.busy != "":
		status = m.st.info.Render(m.busy + "…")
	case m.status != "" && m.statusErr:
		status = m.st.err.Render(m.status)
	case m.status != "":
		status = m.st.success.Render(m.status)
	case m.filtering:
		target := m.appFiltr
		if m.screen == screenAssignments {
			target = m.asgFiltr
		}
		status = m.st.filter.Render("/" + target + "▏")
	default:
		filter := m.appFiltr
		if m.screen == screenAssignments {
			filter = m.asgFiltr
		}
		if filter != "" {
			status = m.st.dim.Render("filter: ") + m.st.filter.Render(filter) +
				m.st.dim.Render("  (esc to clear)")
		}
	}

	hints := m.st.footer.Render(strings.Join(m.hints(), m.st.dim.Render(" · ")))
	return status + "\n" + truncate(hints, m.width, m.gl.ellipsis)
}

func (m *Model) hints() []string {
	switch m.screen {
	case screenApps:
		return []string{"j/k move", "enter open", "/ filter", "R refresh", "? help", "q quit"}
	case screenAssignments:
		if m.showGroups {
			return []string{"j/k move", "tab users", "A add group", "d remove", "/ filter", "esc back"}
		}
		return []string{"j/k move", "tab groups", "a add user", "d remove", "enter user apps", "esc back"}
	case screenUserApps:
		return []string{"j/k move", "esc back", "? help", "q quit"}
	default:
		return []string{"any key to close"}
	}
}

// ---- app list -------------------------------------------------------------

func (m *Model) renderApps() string {
	rows := m.filteredApps()
	h := m.listHeight()

	// Column widths: label takes what's left after the fixed-width numerics.
	const wUsers, wDirect, wGroups, wMode = 6, 7, 7, 16
	wLabel := m.width - wUsers - wDirect - wGroups - wMode - 4
	if wLabel < 12 {
		wLabel = 12
	}

	var b strings.Builder
	b.WriteString(m.st.header.Render(
		pad("APP", wLabel) + " " + padLeft("USERS", wUsers) + " " +
			padLeft("DIRECT", wDirect) + " " + padLeft("GROUPS", wGroups) + " " +
			pad("SIGN-ON", wMode)))
	b.WriteByte('\n')

	for i := m.appTop; i < len(rows) && i < m.appTop+h; i++ {
		a := rows[i]
		users := len(m.app.Index.AppUsers[a.ID])
		direct := 0
		for _, au := range m.app.Index.AppUsers[a.ID] {
			if au.Scope == "USER" {
				direct++
			}
		}
		groups := len(m.app.Index.AppGroups[a.ID])

		label := truncate(a.Label, wLabel, m.gl.ellipsis)
		line := pad(label, wLabel) + " " +
			padLeft(strconv.Itoa(users), wUsers) + " " +
			padLeft(strconv.Itoa(direct), wDirect) + " " +
			padLeft(strconv.Itoa(groups), wGroups) + " " +
			pad(truncate(a.SignOnMode, wMode, m.gl.ellipsis), wMode)

		if i == m.appCur {
			b.WriteString(m.st.selected.Render(m.gl.cursor + truncate(line, m.width-2, m.gl.ellipsis)))
		} else {
			b.WriteString(" " + line)
		}
		b.WriteByte('\n')
	}
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
		pad("LOGIN", wLogin) + " " + pad("STATUS", wStatus) + " " + pad("ACCESS VIA", wVia)))
	b.WriteByte('\n')

	for i := m.asgTop; i < len(rows) && i < m.asgTop+h; i++ {
		a := rows[i]
		// Marker doubles the direct/group signal so it survives monochrome.
		marker := m.st.warn.Render("G")
		if a.Direct {
			marker = m.st.success.Render("D")
		}
		via := accessVia(a.Direct, a.ViaGroups)
		line := pad(truncate(a.User.Profile.Login, wLogin-2, m.gl.ellipsis), wLogin-2) + " " +
			pad(truncate(a.Status, wStatus, m.gl.ellipsis), wStatus) + " " +
			truncate(via, wVia, m.gl.ellipsis)

		prefix := " "
		if i == m.asgCur {
			prefix = m.gl.cursor
			b.WriteString(m.st.selected.Render(prefix) + marker + " " + m.st.selected.Render(line))
		} else {
			b.WriteString(prefix + marker + " " + line)
		}
		b.WriteByte('\n')
	}
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
		pad("GROUP", wName) + " " + padLeft("MEMBERS", wMembers) + "  " + pad("ID", 22)))
	b.WriteByte('\n')

	for i := m.asgTop; i < len(rows) && i < m.asgTop+h; i++ {
		g := rows[i]
		line := pad(truncate(g.group.Profile.Name, wName, m.gl.ellipsis), wName) + " " +
			padLeft(strconv.Itoa(g.members), wMembers) + "  " +
			m.st.dim.Render(g.group.ID)
		if i == m.asgCur {
			b.WriteString(m.st.selected.Render(m.gl.cursor + line))
		} else {
			b.WriteString(" " + line)
		}
		b.WriteByte('\n')
	}
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
		pad("APP", wApp) + " " + pad("STATUS", 14) + " " + pad("ACCESS VIA", wVia)))
	b.WriteByte('\n')

	for i := m.revTop; i < len(m.revAccess) && i < m.revTop+h; i++ {
		a := m.revAccess[i]
		marker := m.st.warn.Render("G")
		if a.Direct {
			marker = m.st.success.Render("D")
		}
		line := pad(truncate(a.App.Label, wApp-2, m.gl.ellipsis), wApp-2) + " " +
			pad(truncate(a.Status, 14, m.gl.ellipsis), 14) + " " +
			truncate(accessVia(a.Direct, a.ViaGroups), wVia, m.gl.ellipsis)
		prefix := " "
		if i == m.revCur {
			b.WriteString(m.st.selected.Render(m.gl.cursor) + marker + " " + m.st.selected.Render(line))
			continue
		}
		b.WriteString(prefix + marker + " " + line + "\n")
	}
	return fillHeight(b.String(), h+1)
}

func (m *Model) renderHelp() string {
	sections := []struct {
		title string
		keys  [][2]string
	}{
		{"navigation", [][2]string{
			{"j / k, ↓ / ↑", "move"},
			{"g / G", "top / bottom"},
			{"ctrl+d / ctrl+u", "half page"},
			{"enter, l", "drill in"},
			{"esc, h", "back"},
			{"q", "back, or quit at the app list"},
		}},
		{"app view", [][2]string{
			{"tab", "switch between assigned users and groups"},
			{"a", "assign users (fuzzy picker, tab to multi-select)"},
			{"A", "assign groups"},
			{"d", "remove the selected assignment"},
			{"enter", "on a user: show every app that user has"},
		}},
		{"general", [][2]string{
			{"/", "filter the current list"},
			{"R", "refresh the snapshot from Okta"},
			{"?", "toggle this help"},
		}},
		{"reading the list", [][2]string{
			{"D", "direct assignment — d removes it"},
			{"G", "access comes from a group — remove the group instead"},
			{"direct + group", "d leaves group access intact"},
		}},
	}

	var b strings.Builder
	for _, s := range sections {
		b.WriteString(m.st.accent.Render(s.title) + "\n")
		for _, k := range s.keys {
			b.WriteString("  " + pad(m.st.info.Render(k[0]), 32) + m.st.dim.Render(k[1]) + "\n")
		}
		b.WriteByte('\n')
	}
	return fillHeight(b.String(), m.listHeight()+1)
}

// ---- overlays -------------------------------------------------------------

func (m *Model) renderOverlay(base string) string {
	var box string
	switch m.overlay {
	case overlayPickUser, overlayPickGroup:
		box = m.renderPicker()
	case overlayConfirm:
		box = m.renderConfirm()
	case overlayError:
		box = m.st.modalWarn.Render(
			m.st.err.Render("error") + "\n\n" +
				wrap(m.errText, minInt(m.width-8, 76)) + "\n\n" +
				m.st.dim.Render("esc to dismiss"))
	}
	if box == "" {
		return base
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m *Model) renderPicker() string {
	p := &m.pick
	w := clampInt(m.width-10, 40, 76)
	h := m.pickerHeight()

	title := "assign users to " + m.curApp.Label
	if p.kind == "group" {
		title = "assign groups to " + m.curApp.Label
	}

	var b strings.Builder
	b.WriteString(m.st.title.Render(truncate(title, w, m.gl.ellipsis)) + "\n")
	b.WriteString(m.st.filter.Render("/"+p.query) + m.st.dim.Render("▏") + "\n")
	b.WriteString(m.st.dim.Render(strings.Repeat("─", w)) + "\n")

	if len(p.matches) == 0 {
		b.WriteString(m.st.dim.Render("  no match") + "\n")
	}
	for i := p.top; i < len(p.matches) && i < p.top+h; i++ {
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
		line := pad(truncate(label, wLabel, m.gl.ellipsis), wLabel) + " "
		if it.already != "" {
			line += m.st.warn.Render(truncate(desc, w-wLabel-6, m.gl.ellipsis))
		} else {
			line += m.st.dim.Render(truncate(desc, w-wLabel-6, m.gl.ellipsis))
		}

		if i == p.cur {
			b.WriteString(m.st.selected.Render(m.gl.cursor) + mark + " " + line + "\n")
		} else {
			b.WriteString("  " + mark + " " + line + "\n")
		}
	}

	b.WriteString(m.st.dim.Render(strings.Repeat("─", w)) + "\n")
	sel := ""
	if len(p.chosen) > 0 {
		sel = m.st.success.Render(fmt.Sprintf("%d selected  ", len(p.chosen)))
	}
	b.WriteString(sel + m.st.footer.Render("tab select · enter confirm · esc cancel"))

	return m.st.modal.Width(w).Render(b.String())
}

func (m *Model) renderConfirm() string {
	w := clampInt(m.width-10, 40, 76)

	var b strings.Builder
	verb := "apply"
	if len(m.pending) > 0 {
		verb = m.pending[0].verb
	}
	b.WriteString(m.st.title.Render(verb+" on "+truncate(m.curApp.Label, w-16, m.gl.ellipsis)) + "\n\n")

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
		b.WriteString(truncate(line, w, m.gl.ellipsis) + "\n")
		if ch.noop != "" {
			b.WriteString(m.st.dim.Render("    "+wrapIndent(ch.noop, w-4, "    ")) + "\n")
		}
		if ch.warn != "" {
			b.WriteString(m.st.warn.Render("  ! "+wrapIndent(ch.warn, w-4, "    ")) + "\n")
		}
	}

	b.WriteString("\n")
	if applicable == 0 {
		b.WriteString(m.st.dim.Render("nothing to apply · esc to close"))
		return m.st.modal.Width(w).Render(b.String())
	}
	b.WriteString(m.st.footer.Render(
		fmt.Sprintf("apply %d change(s)?  ", applicable)) +
		m.st.success.Render("y") + m.st.dim.Render(" / ") + m.st.err.Render("n"))

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

// pad right-pads s to w display cells.
func pad(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// padLeft left-pads s to w display cells.
func padLeft(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return strings.Repeat(" ", d) + s
	}
	return s
}

// padBetween places left and right at the edges of a w-cell line.
func padBetween(left, right string, w int) string {
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return truncate(left, w, "…")
	}
	return left + strings.Repeat(" ", gap) + right
}

// truncate cuts s to w display cells, appending the ellipsis glyph.
func truncate(s string, w int, ellipsis string) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	ew := lipgloss.Width(ellipsis)
	if w <= ew {
		return ellipsis[:0]
	}
	// Walk runes accumulating cell width, since a CJK rune is two cells.
	var b strings.Builder
	used := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if used+rw > w-ew {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	return b.String() + ellipsis
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

func wrap(s string, w int) string {
	if w < 8 {
		w = 8
	}
	var out []string
	for len(s) > w {
		cut := strings.LastIndex(s[:w], " ")
		if cut <= 0 {
			cut = w
		}
		out = append(out, s[:cut])
		s = strings.TrimLeft(s[cut:], " ")
	}
	out = append(out, s)
	return strings.Join(out, "\n")
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

