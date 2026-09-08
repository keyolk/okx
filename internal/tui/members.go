package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/keyolk/okx/internal/fuzzy"
	"github.com/keyolk/okx/internal/okta"
)

// The snapshot only pre-resolves membership for groups assigned to an app, so
// most groups arrive here unresolved and are fetched on entry. Everything the
// screen shows about a member — which of this group's apps they reach, whether
// they also hold each one directly — comes from the index, no extra calls.

type memberRow struct {
	user okta.User
	// directApps counts this group's apps the user would keep if the group
	// assignment went away. It is the number that decides whether removing the
	// group actually removes anyone's access.
	directApps int
}

type membersDoneMsg struct {
	groupID string
	users   []okta.User
	err     error
}

// openGroupMembers shows curGroup's membership. back is where esc returns to;
// tab-toggling with the app list keeps whichever entry point brought us here.
func (m *Model) openGroupMembers(back screen) tea.Cmd {
	m.memberBack = back
	m.screen = screenGroupMembers
	m.memberCur, m.memberTop = 0, 0
	m.memberFiltr = ""
	m.members = m.members[:0]

	if m.app.Index.HasMembers(m.curGroup.ID) {
		m.loadMembers()
		return nil
	}
	// Unresolved: fetch just this group. Keep any in-flight refresh untouched —
	// this is a different, much smaller request.
	m.membersLoading = true
	gid := m.curGroup.ID
	return func() tea.Msg {
		users, err := m.app.LoadGroupMembers(m.ctx, gid)
		return membersDoneMsg{groupID: gid, users: users, err: err}
	}
}

func (m *Model) loadMembers() {
	users := m.app.Index.Members(m.curGroup.ID)
	m.members = make([]memberRow, 0, len(users))
	for _, u := range users {
		m.members = append(m.members, memberRow{user: u, directApps: m.directAppCount(u.ID)})
	}
	sort.Slice(m.members, func(i, j int) bool {
		return m.members[i].user.Profile.Login < m.members[j].user.Profile.Login
	})
	m.memberCur = m.clampIdx(m.memberCur, len(m.filteredMembers()))
}

// directAppCount counts the apps this group grants that the user also holds
// directly — the access that survives removing the group.
func (m *Model) directAppCount(userID string) int {
	n := 0
	for _, app := range m.groupApps {
		for _, au := range m.app.Index.AppUsers[app.ID] {
			if au.ID == userID && au.Scope == "USER" {
				n++
				break
			}
		}
	}
	return n
}

func (m *Model) handleGroupMembersKey(key string) (tea.Model, tea.Cmd) {
	rows := m.filteredMembers()
	switch key {
	case "j", "down":
		m.memberCur++
	case "k", "up":
		m.memberCur--
	case "g", "home":
		m.memberCur = 0
	case "G", "end":
		m.memberCur = len(rows) - 1
	case "ctrl+d", "pgdown":
		m.memberCur += m.listHeight() / 2
	case "ctrl+u", "pgup":
		m.memberCur -= m.listHeight() / 2
	case "h", "left":
		m.back()
		return m, nil
	case "tab":
		m.screen = screenGroupApps
		m.groupAppBack = m.memberBack
		return m, nil
	case "enter", "l", "right":
		if len(rows) == 0 {
			return m, nil
		}
		m.openUserApps(rows[m.clampIdx(m.memberCur, len(rows))].user, screenGroupMembers)
		return m, nil
	}
	m.memberCur = m.clampIdx(m.memberCur, len(rows))
	m.memberTop = scrollTo(m.memberTop, m.memberCur, m.listHeight())
	return m, nil
}

func (m *Model) filteredMembers() []memberRow {
	if m.memberFiltr == "" {
		return m.members
	}
	hay := make([]string, len(m.members))
	for i, r := range m.members {
		hay[i] = r.user.Profile.Login + " " + r.user.Profile.Email + " " + r.user.Name()
	}
	matches := fuzzy.Filter(hay, m.memberFiltr)
	out := make([]memberRow, 0, len(matches))
	for _, mt := range matches {
		out = append(out, m.members[mt.Index])
	}
	return out
}

// ---- rendering ------------------------------------------------------------

func (m *Model) renderGroupMembers() string {
	h := m.listHeight()
	if m.membersLoading {
		return m.centeredNotice(
			m.st.accent.Render("resolving members of "+m.curGroup.Profile.Name),
			m.st.dim.Render("this group was not in the snapshot; fetching it now"))
	}
	rows := m.filteredMembers()
	if len(rows) == 0 && m.memberFiltr == "" {
		return m.centeredNotice(
			m.st.dim.Render("this group has no members"),
			m.st.dim.Render("tab shows the apps it grants · esc goes back"))
	}

	const wStatus, wKeeps = 14, 6
	wLogin := clampInt(m.width/3, 20, 40)
	wName := m.width - wLogin - wStatus - wKeeps - 4
	if wName < 12 {
		wName = 12
	}

	var b strings.Builder
	b.WriteString(m.st.header.Render(
		pad("LOGIN", wLogin) + " " + pad("NAME", wName) + " " +
			pad("STATUS", wStatus) + " " + padLeft("KEEPS", wKeeps)))
	b.WriteByte('\n')

	for i := m.memberTop; i < len(rows) && i < m.memberTop+h; i++ {
		r := rows[i]
		// "KEEPS" is how many of this group's apps the member holds directly and
		// would retain if the group were unassigned. A dash reads better than 0
		// for the common "loses everything" case.
		keeps := m.st.dim.Render(padLeft("—", wKeeps))
		if r.directApps > 0 {
			keeps = m.st.success.Render(padLeft(strconv.Itoa(r.directApps), wKeeps))
		}
		line := pad(truncate(r.user.Profile.Login, wLogin, m.gl.ellipsis), wLogin) + " " +
			pad(truncate(r.user.Name(), wName, m.gl.ellipsis), wName) + " " +
			pad(truncate(r.user.Status, wStatus, m.gl.ellipsis), wStatus) + " "
		if i == m.memberCur {
			b.WriteString(m.st.selected.Render(m.gl.cursor+line) + keeps)
		} else {
			b.WriteString(" " + line + keeps)
		}
		b.WriteByte('\n')
	}
	return fillHeight(b.String(), h+1)
}

// centeredNotice renders a short message block in the list area.
func (m *Model) centeredNotice(lines ...string) string {
	h := m.listHeight() + 1
	pad := (h - len(lines)) / 2
	if pad < 0 {
		pad = 0
	}
	return fillHeight(strings.Repeat("\n", pad)+strings.Join(lines, "\n")+"\n", h)
}

// memberCountLabel renders a group's member count for the list views. An
// unresolved group shows "?" rather than "—": the latter was being read as
// "zero members", which is the bug this whole screen exists to fix.
func (m *Model) memberCountLabel(groupID string) string {
	if !m.app.Index.HasMembers(groupID) {
		return "?"
	}
	return strconv.Itoa(len(m.app.Index.GroupMembers[groupID]))
}

func (m *Model) groupMembersTitle() string {
	if m.membersLoading {
		return fmt.Sprintf("%s · resolving members", m.curGroup.Profile.Name)
	}
	return fmt.Sprintf("%s · %d member(s)", m.curGroup.Profile.Name, len(m.members))
}
