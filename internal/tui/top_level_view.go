package tui

import (
	"strconv"
	"strings"
)

func (m *Model) activeTopLevel() screen {
	return m.topLevelFor(m.screen)
}

func (m *Model) topLevelFor(s screen) screen {
	switch s {
	case screenGroups, screenGroupApps:
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

func (m *Model) renderTopTabs() string {
	active := m.activeTopLevel()
	tab := func(s screen, label string) string {
		if active == s {
			return m.st.title.Render(label)
		}
		return m.st.dim.Render(label)
	}
	return m.st.title.Render("okx") + "  " +
		tab(screenApps, "1 Apps") + m.st.dim.Render(" · ") +
		tab(screenGroups, "2 Groups") + m.st.dim.Render(" · ") +
		tab(screenUsers, "3 Users")
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
		pad("GROUP", wName) + " " + padLeft("APPS", wApps) + " " +
			padLeft("MEMBERS", wMembers) + " " + pad("DESCRIPTION", wDescription)))
	b.WriteByte('\n')
	for i := m.groupTop; i < len(rows) && i < m.groupTop+h; i++ {
		group := rows[i]
		members := "—"
		if ids, ok := m.app.Index.GroupMembers[group.ID]; ok {
			members = strconv.Itoa(len(ids))
		}
		line := pad(truncate(group.Profile.Name, wName, m.gl.ellipsis), wName) + " " +
			padLeft(strconv.Itoa(m.groupAppCounts[group.ID]), wApps) + " " +
			padLeft(members, wMembers) + " " +
			truncate(group.Profile.Description, wDescription, m.gl.ellipsis)
		if i == m.groupCur {
			b.WriteString(m.st.selected.Render(m.gl.cursor + truncate(line, m.width-2, m.gl.ellipsis)))
		} else {
			b.WriteString(" " + line)
		}
		b.WriteByte('\n')
	}
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
		pad("LOGIN", wLogin) + " " + pad("NAME", wName) + " " +
			pad("STATUS", wStatus) + " " + padLeft("APPS", wApps)))
	b.WriteByte('\n')
	for i := m.userTop; i < len(rows) && i < m.userTop+h; i++ {
		user := rows[i]
		line := pad(truncate(user.Profile.Login, wLogin, m.gl.ellipsis), wLogin) + " " +
			pad(truncate(user.Name(), wName, m.gl.ellipsis), wName) + " " +
			pad(truncate(user.Status, wStatus, m.gl.ellipsis), wStatus) + " " +
			padLeft(strconv.Itoa(m.userAppCounts[user.ID]), wApps)
		if i == m.userCur {
			b.WriteString(m.st.selected.Render(m.gl.cursor + truncate(line, m.width-2, m.gl.ellipsis)))
		} else {
			b.WriteString(" " + line)
		}
		b.WriteByte('\n')
	}
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
		pad("APP", wApp) + " " + pad("STATUS", wStatus) + " " + pad("SIGN-ON", wMode)))
	b.WriteByte('\n')
	for i := m.groupAppTop; i < len(m.groupApps) && i < m.groupAppTop+h; i++ {
		app := m.groupApps[i]
		line := pad(truncate(app.Label, wApp, m.gl.ellipsis), wApp) + " " +
			pad(truncate(app.Status, wStatus, m.gl.ellipsis), wStatus) + " " +
			truncate(app.SignOnMode, wMode, m.gl.ellipsis)
		if i == m.groupAppCur {
			b.WriteString(m.st.selected.Render(m.gl.cursor + truncate(line, m.width-2, m.gl.ellipsis)))
		} else {
			b.WriteString(" " + line)
		}
		b.WriteByte('\n')
	}
	return fillHeight(b.String(), h+1)
}
