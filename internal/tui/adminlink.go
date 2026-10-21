package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/keyolk/tuikit/link"
	"github.com/keyolk/tuikit/render"
	"github.com/keyolk/tuikit/toast"

	"github.com/keyolk/okx/internal/okta"
)

// Admin console links (E11). Every user, group and app name is an OSC 8 link
// to its admin console page, and `o` opens the selected row's page. Both read
// the URL from config.Config, which leaves it empty for a custom domain
// without adminUrl: then nothing is linked and `o` says how to fix it.

func (m *Model) userURL(u okta.User) string   { return m.app.Cfg.UserURL(u.ID) }
func (m *Model) groupURL(g okta.Group) string { return m.app.Cfg.GroupURL(g.ID) }
func (m *Model) appURL(a okta.App) string     { return m.app.Cfg.AppURL(a.Name, a.ID) }

// linkCell is a w-cell column holding text linked to u. Only the (truncated)
// text is inside the link, not the padding, so a click on the blank part of
// the column opens nothing and the link underline stops at the name.
func (m *Model) linkCell(u, text string, w int) string {
	return render.PadRight(link.Wrap(u, render.TruncateMarker(text, w, m.gl.ellipsis)), w)
}

// linkTitle truncates name+rest to w cells and links the part that is the
// name. When the cut falls inside the name, the whole cut text is the link.
func (m *Model) linkTitle(u, name, rest string, w int) string {
	t := render.TruncateMarker(name+rest, w, m.gl.ellipsis)
	if name != "" && strings.HasPrefix(t, name) {
		return link.Wrap(u, name) + t[len(name):]
	}
	return link.Wrap(u, t)
}

// selectedAdminURL is the admin page of the row under the cursor. ok is false
// when no row is selected (an empty list, a screen without one).
func (m *Model) selectedAdminURL() (u string, ok bool) {
	pick := func(cur, n int) (int, bool) {
		if n == 0 {
			return 0, false
		}
		return m.clampIdx(cur, n), true
	}
	switch m.screen {
	case screenApps:
		rows := m.filteredApps()
		if i, ok := pick(m.appCur, len(rows)); ok {
			return m.appURL(rows[i]), true
		}
	case screenGroups:
		rows := m.filteredTopGroups()
		if i, ok := pick(m.groupCur, len(rows)); ok {
			return m.groupURL(rows[i]), true
		}
	case screenUsers:
		rows := m.filteredTopUsers()
		if i, ok := pick(m.userCur, len(rows)); ok {
			return m.userURL(rows[i]), true
		}
	case screenAssignments:
		if m.showGroups {
			rows := m.filteredGroups()
			if i, ok := pick(m.asgCur, len(rows)); ok {
				return m.groupURL(rows[i].group), true
			}
			break
		}
		rows := m.filteredAssignments()
		if i, ok := pick(m.asgCur, len(rows)); ok {
			return m.userURL(rows[i].User), true
		}
	case screenGroupApps:
		if i, ok := pick(m.groupAppCur, len(m.groupApps)); ok {
			return m.appURL(m.groupApps[i]), true
		}
	case screenGroupMembers:
		if m.membersLoading {
			break // the rows on screen are a notice, not members
		}
		rows := m.filteredMembers()
		if i, ok := pick(m.memberCur, len(rows)); ok {
			return m.userURL(rows[i].user), true
		}
	case screenUserApps:
		if i, ok := pick(m.revCur, len(m.revAccess)); ok {
			return m.appURL(m.revAccess[i].App), true
		}
	}
	return "", false
}

// openAdminLink runs `o`. A key always answers: with no URL it says why
// rather than doing nothing, and the most common why (a custom domain) names
// the setting that fixes it.
func (m *Model) openAdminLink() tea.Cmd {
	u, ok := m.selectedAdminURL()
	switch {
	case !ok:
		m.notify("link", "nothing selected", toast.Info)
		return nil
	case m.app.Cfg.AdminURL == "":
		m.notify("link", "admin console URL unknown: set okta.client.adminUrl or $OKTA_ADMIN_URL", toast.Warn)
		return nil
	case u == "":
		m.notify("link", link.OpenedMsg{Err: link.ErrNoLink}.Note(), toast.Info)
		return nil
	}
	return link.Open(u)
}

func (m *Model) linkOpened(msg link.OpenedMsg) {
	level := toast.Info
	if msg.Err != nil {
		level = toast.Warn
	}
	m.notify("link", msg.Note(), level)
}
