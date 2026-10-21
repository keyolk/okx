package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/keyolk/tuikit/render"
)

func (m *Model) activeTopLevel() screen {
	return m.topLevelFor(m.screen)
}

func (m *Model) topLevelFor(s screen) screen {
	switch s {
	case screenGroups:
		return screenGroups
	case screenGroupApps:
		if m.groupAppBack == screenAssignments {
			return m.topLevelFor(screenAssignments)
		}
		return screenGroups
	case screenGroupMembers:
		// Reached from an app's groups tab, this is still an Apps-flow screen.
		if m.memberBack == screenAssignments {
			return m.topLevelFor(screenAssignments)
		}
		return screenGroups
	case screenUsers:
		return screenUsers
	case screenAssignments:
		if m.assignmentBack == screenGroupApps {
			return screenGroups
		}
	case screenUserApps:
		if m.detailBack == screenUsers {
			return screenUsers
		}
		if m.detailBack == screenGroupMembers {
			return m.topLevelFor(screenGroupMembers)
		}
		if m.detailBack == screenAssignments && m.assignmentBack == screenGroupApps {
			return screenGroups
		}
	case screenHelp:
		if m.helpBack != screenHelp {
			return m.topLevelFor(m.helpBack)
		}
	}
	return screenApps
}

// topTab is one numbered tab of the header: the screen it opens, the action
// its number key runs, and its label as drawn.
type topTab struct {
	screen screen
	action string
	label  string
}

// The header is "okx", two spaces, then the tabs joined by tabSep. Drawing
// and mouse hit testing both read these, so the click targets cannot drift
// from the labels.
const (
	headerBrand = "okx"
	headerGap   = "  "
	tabSep      = " · "
)

func (m *Model) topTabs() []topTab {
	return []topTab{
		{screenApps, "view.apps", "1 Apps " + m.tabBadge(screenApps)},
		{screenGroups, "view.groups", "2 Groups " + m.tabBadge(screenGroups)},
		{screenUsers, "view.users", "3 Users " + m.tabBadge(screenUsers)},
	}
}

func (m *Model) renderTopTabs() string {
	active := m.activeTopLevel()
	var b strings.Builder
	b.WriteString(m.st.title.Render(headerBrand) + headerGap)
	for i, t := range m.topTabs() {
		if i > 0 {
			b.WriteString(m.st.dim.Render(tabSep))
		}
		if active == t.screen {
			b.WriteString(m.st.title.Render(t.label))
		} else {
			b.WriteString(m.st.dim.Render(t.label))
		}
	}
	return b.String()
}

// tabBadge says what each tab holds without opening it: the row count, the
// matches over the total while that tab is filtered, and a spinner while a
// refresh is about to replace the counts (or, before the first snapshot,
// instead of a misleading 0).
func (m *Model) tabBadge(s screen) string {
	var total int
	switch s {
	case screenApps:
		total = len(m.apps)
	case screenGroups:
		total = len(m.groups)
	case screenUsers:
		total = len(m.users)
	}
	badge := strconv.Itoa(total)
	if m.topFilter(s) != "" {
		var shown int
		switch s {
		case screenApps:
			shown = len(m.filteredApps())
		case screenGroups:
			shown = len(m.filteredTopGroups())
		case screenUsers:
			shown = len(m.filteredTopUsers())
		}
		badge = fmt.Sprintf("%d/%d", shown, total)
	}
	if m.refreshing() {
		spin := m.gl.kit.SpinnerFrame(m.spinFrame)
		if m.app.Index.Empty() {
			return spin
		}
		badge += spin
	}
	return badge
}

func (m *Model) renderTopGroups() string {
	rows := m.filteredTopGroups()
	h := m.listHeight()
	const wApps, wMembers = 6, 9
	wName := clampInt(m.width/3, 18, 42)
	wDescription := m.width - wName - wApps - wMembers - 5
	if wDescription < 12 {
		wDescription = 12
	}

	var b strings.Builder
	b.WriteString(m.st.header.Render(
		render.PadRight("GROUP", wName) + " " + render.PadLeft("APPS", wApps) + " " +
			render.PadLeft("MEMBERS", wMembers) + " " + render.PadRight("DESCRIPTION", wDescription)))
	b.WriteByte('\n')
	m.listLines(&b, len(rows), m.groupCur, m.groupTop, h, func(i int, sel bool) string {
		group := rows[i]
		members := m.memberCountLabel(group.ID)
		line := m.linkCell(m.groupURL(group), group.Profile.Name, wName) + " " +
			render.PadLeft(strconv.Itoa(m.groupAppCounts[group.ID]), wApps) + " " +
			render.PadLeft(members, wMembers) + " " +
			render.TruncateMarker(group.Profile.Description, wDescription, m.gl.ellipsis)
		if sel {
			return m.st.selected.Render(m.gl.cursor + render.TruncateMarker(line, m.width-2, m.gl.ellipsis))
		}
		return " " + line
	})
	return fillHeight(b.String(), h+1)
}

func (m *Model) renderTopUsers() string {
	rows := m.filteredTopUsers()
	h := m.listHeight()
	const wStatus, wApps = 14, 6
	wLogin := clampInt(m.width/3, 20, 42)
	wName := m.width - wLogin - wStatus - wApps - 4
	if wName < 12 {
		wName = 12
	}

	var b strings.Builder
	b.WriteString(m.st.header.Render(
		render.PadRight("LOGIN", wLogin) + " " + render.PadRight("NAME", wName) + " " +
			render.PadRight("STATUS", wStatus) + " " + render.PadLeft("APPS", wApps)))
	b.WriteByte('\n')
	m.listLines(&b, len(rows), m.userCur, m.userTop, h, func(i int, sel bool) string {
		user := rows[i]
		line := m.linkCell(m.userURL(user), user.Profile.Login, wLogin) + " " +
			render.PadRight(render.TruncateMarker(user.Name(), wName, m.gl.ellipsis), wName) + " " +
			render.PadRight(render.TruncateMarker(user.Status, wStatus, m.gl.ellipsis), wStatus) + " " +
			render.PadLeft(strconv.Itoa(m.userAppCounts[user.ID]), wApps)
		if sel {
			return m.st.selected.Render(m.gl.cursor + render.TruncateMarker(line, m.width-2, m.gl.ellipsis))
		}
		return " " + line
	})
	return fillHeight(b.String(), h+1)
}

func (m *Model) renderGroupApps() string {
	h := m.listHeight()
	const wStatus, wMode = 14, 16
	wApp := m.width - wStatus - wMode - 3
	if wApp < 16 {
		wApp = 16
	}

	var b strings.Builder
	b.WriteString(m.st.header.Render(
		render.PadRight("APP", wApp) + " " + render.PadRight("STATUS", wStatus) + " " + render.PadRight("SIGN-ON", wMode)))
	b.WriteByte('\n')
	m.listLines(&b, len(m.groupApps), m.groupAppCur, m.groupAppTop, h, func(i int, sel bool) string {
		app := m.groupApps[i]
		line := m.linkCell(m.appURL(app), app.Label, wApp) + " " +
			render.PadRight(render.TruncateMarker(app.Status, wStatus, m.gl.ellipsis), wStatus) + " " +
			render.TruncateMarker(app.SignOnMode, wMode, m.gl.ellipsis)
		if sel {
			return m.st.selected.Render(m.gl.cursor + render.TruncateMarker(line, m.width-2, m.gl.ellipsis))
		}
		return " " + line
	})
	return fillHeight(b.String(), h+1)
}
