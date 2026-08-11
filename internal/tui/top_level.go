package tui

import (
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/keyolk/okx/internal/okta"
)

func isTopLevel(s screen) bool {
	return s == screenApps || s == screenGroups || s == screenUsers
}

func (m *Model) switchTopLevel(s screen) {
	m.screen = s
	m.filtering = false
	m.status = ""
	m.statusErr = false
}

func (m *Model) handleGroupsKey(key string) (tea.Model, tea.Cmd) {
	rows := m.filteredTopGroups()
	switch key {
	case "j", "down":
		m.groupCur++
	case "k", "up":
		m.groupCur--
	case "g", "home":
		m.groupCur = 0
	case "G", "end":
		m.groupCur = len(rows) - 1
	case "ctrl+d", "pgdown":
		m.groupCur += m.listHeight() / 2
	case "ctrl+u", "pgup":
		m.groupCur -= m.listHeight() / 2
	case "enter", "l", "right":
		if len(rows) == 0 {
			return m, nil
		}
		m.curGroup = rows[m.clampIdx(m.groupCur, len(rows))]
		m.loadGroupApps()
		m.screen = screenGroupApps
		return m, nil
	}
	m.groupCur = m.clampIdx(m.groupCur, len(rows))
	m.groupTop = scrollTo(m.groupTop, m.groupCur, m.listHeight())
	return m, nil
}

func (m *Model) handleUsersKey(key string) (tea.Model, tea.Cmd) {
	rows := m.filteredTopUsers()
	switch key {
	case "j", "down":
		m.userCur++
	case "k", "up":
		m.userCur--
	case "g", "home":
		m.userCur = 0
	case "G", "end":
		m.userCur = len(rows) - 1
	case "ctrl+d", "pgdown":
		m.userCur += m.listHeight() / 2
	case "ctrl+u", "pgup":
		m.userCur -= m.listHeight() / 2
	case "enter", "l", "right":
		if len(rows) == 0 {
			return m, nil
		}
		m.openUserApps(rows[m.clampIdx(m.userCur, len(rows))], screenUsers)
		return m, nil
	}
	m.userCur = m.clampIdx(m.userCur, len(rows))
	m.userTop = scrollTo(m.userTop, m.userCur, m.listHeight())
	return m, nil
}

func (m *Model) handleGroupAppsKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "j", "down":
		m.groupAppCur++
	case "k", "up":
		m.groupAppCur--
	case "g", "home":
		m.groupAppCur = 0
	case "G", "end":
		m.groupAppCur = len(m.groupApps) - 1
	case "ctrl+d", "pgdown":
		m.groupAppCur += m.listHeight() / 2
	case "ctrl+u", "pgup":
		m.groupAppCur -= m.listHeight() / 2
	case "h", "left":
		m.back()
		return m, nil
	case "enter", "l", "right":
		if len(m.groupApps) == 0 {
			return m, nil
		}
		m.curApp = m.groupApps[m.clampIdx(m.groupAppCur, len(m.groupApps))]
		m.assignmentBack = screenGroupApps
		m.screen = screenAssignments
		m.showGroups = true
		m.loadAssignments()
		return m, nil
	}
	m.groupAppCur = m.clampIdx(m.groupAppCur, len(m.groupApps))
	m.groupAppTop = scrollTo(m.groupAppTop, m.groupAppCur, m.listHeight())
	return m, nil
}

func (m *Model) loadGroupApps() {
	m.groupApps = m.groupApps[:0]
	for _, app := range m.app.Index.Apps {
		if m.app.AppHasGroup(app.ID, m.curGroup.ID) {
			m.groupApps = append(m.groupApps, app)
		}
	}
	sort.Slice(m.groupApps, func(i, j int) bool {
		return m.groupApps[i].Label < m.groupApps[j].Label
	})
	m.groupAppCur, m.groupAppTop = 0, 0
}

func (m *Model) openUserApps(user okta.User, back screen) {
	m.revUser = user
	m.revAccess = m.app.Index.UserApps(user.ID)
	m.revCur, m.revTop = 0, 0
	m.detailBack = back
	m.screen = screenUserApps
}

func (m *Model) currentFilter() (*string, bool) {
	switch m.screen {
	case screenApps:
		return &m.appFiltr, true
	case screenGroups:
		return &m.groupFiltr, true
	case screenUsers:
		return &m.userFiltr, true
	case screenAssignments:
		return &m.asgFiltr, true
	default:
		return nil, false
	}
}

func (m *Model) resetCurrentCursor() {
	switch m.screen {
	case screenApps:
		m.appCur, m.appTop = 0, 0
	case screenGroups:
		m.groupCur, m.groupTop = 0, 0
	case screenUsers:
		m.userCur, m.userTop = 0, 0
	case screenAssignments:
		m.asgCur, m.asgTop = 0, 0
	}
}
