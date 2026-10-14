package tui

import (
	"sort"

	"github.com/keyolk/okx/internal/okta"
)

func isTopLevel(s screen) bool {
	return s == screenApps || s == screenGroups || s == screenUsers
}

func (m *Model) switchTopLevel(s screen) {
	m.screen = s
	m.filtering = false
	m.saveUI()
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
	case screenGroupMembers:
		return &m.memberFiltr, true
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
	case screenGroupMembers:
		m.memberCur, m.memberTop = 0, 0
	}
}
