package tui

import (
	"github.com/keyolk/tuikit/keys"
)

// keys.go is the one binding table. Dispatch (handleKey), the footer, the `?`
// help page and the `:` palette all read it, so no surface can advertise a key
// that does something else — the hand-written hints and help this replaced had
// drifted (the user-apps footer offered "enter open", which did nothing).
//
// Users rebind actions in ~/.config/okx/keys.yaml (keys.ConfigPath):
//
//	list.down: [j, down, ctrl+j]
//	refresh: [R, ctrl+r]

const appName = "okx"

// Contexts, innermost first in every stack below.
const (
	ctxAlways   keys.Ctx = "always"   // ctrl+c: reachable from every state, text fields included
	ctxMain     keys.Ctx = "main"     // view switching and screen-wide keys, outside text fields
	ctxTop      keys.Ctx = "top"      // the three numbered resource lists
	ctxSub      keys.Ctx = "sub"      // a drilled-in screen, which can go back
	ctxList     keys.Ctx = "list"     // any list with a cursor
	ctxAsg      keys.Ctx = "app"      // an app's assignments
	ctxMembers  keys.Ctx = "members"  // a group's members
	ctxGroupApp keys.Ctx = "groupapp" // the apps a group grants
	ctxHelp     keys.Ctx = "help"
	ctxFilter   keys.Ctx = "filter" // typing a list filter
	ctxEdit     keys.Ctx = "edit"   // line editing shared by every text field
	ctxQuery    keys.Ctx = "query"  // moving through matches while typing a query
	ctxPicker   keys.Ctx = "picker"
	ctxPalette  keys.Ctx = "palette"
	ctxConfirm  keys.Ctx = "confirm"
	ctxError    keys.Ctx = "error"
	ctxApplying keys.Ctx = "applying" // a mutation owns the screen
)

var (
	stackTop      = []keys.Ctx{ctxList, ctxTop, ctxMain, ctxAlways}
	stackAsg      = []keys.Ctx{ctxAsg, ctxList, ctxSub, ctxMain, ctxAlways}
	stackMembers  = []keys.Ctx{ctxMembers, ctxList, ctxSub, ctxMain, ctxAlways}
	stackGroupApp = []keys.Ctx{ctxGroupApp, ctxList, ctxSub, ctxMain, ctxAlways}
	stackUserApps = []keys.Ctx{ctxList, ctxSub, ctxMain, ctxAlways}
	stackHelp     = []keys.Ctx{ctxHelp, ctxMain, ctxAlways}
	stackFilter   = []keys.Ctx{ctxFilter, ctxEdit, ctxAlways}
	stackPicker   = []keys.Ctx{ctxPicker, ctxQuery, ctxEdit, ctxAlways}
	stackPalette  = []keys.Ctx{ctxPalette, ctxQuery, ctxEdit, ctxAlways}
	stackConfirm  = []keys.Ctx{ctxConfirm, ctxAlways}
	stackError    = []keys.Ctx{ctxError, ctxAlways}
	stackApplying = []keys.Ctx{ctxApplying, ctxAlways}
)

// allStacks is every stack the model can be in. The conflict test and the
// user override loader both check against it.
var allStacks = [][]keys.Ctx{
	stackTop, stackAsg, stackMembers, stackGroupApp, stackUserApps, stackHelp,
	stackFilter, stackPicker, stackPalette, stackConfirm, stackError, stackApplying,
}

// Help sections. Each binding names one, so the help page groups by what the
// user is doing rather than by internal context.
const (
	grpNav     = "navigation"
	grpGeneral = "general"
	grpApp     = "app view"
	grpGroup   = "group view"
	grpFilter  = "filter"
	grpPicker  = "assign picker"
	grpPalette = "command palette"
	grpConfirm = "confirm"
)

func defaultKeymap() keys.Keymap {
	b := func(action string, ks []string, ctx keys.Ctx, group, help string, footer int) keys.Binding {
		return keys.Binding{Action: action, Keys: ks, Ctx: ctx, Group: group, Help: help, Footer: footer}
	}
	views := func(action, key, help string) keys.Binding {
		x := b(action, []string{key}, ctxMain, grpNav, help, 1)
		x.FooterGroup = "views"
		return x
	}
	move := func(action string, ks []string, help string, footer int, group string) keys.Binding {
		x := b(action, ks, ctxList, grpNav, help, footer)
		x.FooterGroup = group
		return x
	}

	quitAlways := b("quit", []string{"ctrl+c"}, ctxAlways, grpGeneral, "quit", 0)
	quitAlways.Pinned = true
	quitTop := b("quit", []string{"q"}, ctxTop, grpGeneral, "quit", 0)
	quitTop.Pinned = true
	quitApplying := b("quit", []string{"q"}, ctxApplying, grpGeneral, "quit", 0)
	quitApplying.Pinned = true
	help := b("help", []string{"?"}, ctxMain, grpGeneral, "help", 0)
	help.Pinned = true

	refresh := b("refresh", []string{"R", "ctrl+r"}, ctxMain, grpGeneral, "refresh the snapshot from Okta", 6)
	refresh.Short = "refresh"
	refresh.Labels = map[string]string{"busy": "refreshing…"}

	open := b("list.open", []string{"enter", "l", "right"}, ctxList, grpNav, "drill in", 5)
	open.Short = "open"
	open.Labels = map[string]string{
		"apps":     "assignments",
		"groups":   "members",
		"users":    "user apps",
		"asgUser":  "user apps",
		"asgGroup": "members",
		"groupApp": "assignments",
		"member":   "user apps",
	}

	asgTab := b("app.tab", []string{"tab"}, ctxAsg, grpApp, "switch between assigned users and groups", 2)
	asgTab.Short = "groups"
	asgTab.Labels = map[string]string{"groups": "users"}
	assign := b("app.assign", []string{"a"}, ctxAsg, grpApp, "assign users (fuzzy picker, tab to multi-select)", 3)
	assign.Short = "add user"
	assign.Labels = map[string]string{"groups": "add group"}

	remove := b("app.remove", []string{"d", "delete", "backspace"}, ctxAsg, grpApp, "remove the selected assignment", 3)
	remove.Short = "remove"

	back := b("back", []string{"esc", "q", "h", "left"}, ctxSub, grpNav, "back", 4)

	helpClose := b("help.close", []string{"esc", "q"}, ctxHelp, grpGeneral, "close help", 1)
	helpClose.Short = "close"
	short := func(x keys.Binding, label string) keys.Binding {
		x.Short = label
		return x
	}
	scroll := func(x keys.Binding) keys.Binding {
		x.FooterGroup = "scroll"
		return x
	}

	return keys.New(
		quitAlways, quitTop, quitApplying, help,
		views("view.apps", "1", "open Apps"),
		views("view.groups", "2", "open Groups"),
		views("view.users", "3", "open Users"),
		move("list.down", []string{"j", "down"}, "down", 2, "move"),
		move("list.up", []string{"k", "up"}, "up", 2, "move"),
		// G is unreachable under a Korean input source (shift+ㅎ is still ㅎ),
		// so end stays bound as its reachable alternate.
		move("list.top", []string{"g", "home"}, "top", 0, ""),
		move("list.bottom", []string{"G", "end"}, "bottom", 0, ""),
		move("list.pagedown", []string{"ctrl+d", "pgdown"}, "half page down", 0, ""),
		move("list.pageup", []string{"ctrl+u", "pgup"}, "half page up", 0, ""),
		open,
		back,
		short(b("filter", []string{"/"}, ctxMain, grpGeneral, "filter the current list", 5), "filter"),
		b("palette", []string{":"}, ctxMain, grpGeneral, "command palette", 0),
		refresh,

		asgTab,
		assign,
		// A is unreachable under a Korean input source; a on the groups tab
		// assigns groups too, so the action stays reachable there.
		b("app.assignGroup", []string{"A"}, ctxAsg, grpApp, "assign groups", 0),
		remove,
		short(b("members.apps", []string{"tab"}, ctxMembers, grpGroup, "show the apps this group grants", 2), "apps"),
		short(b("groupapp.members", []string{"tab"}, ctxGroupApp, grpGroup, "show this group's members", 2), "members"),

		helpClose,
		scroll(b("help.down", []string{"j", "down"}, ctxHelp, grpGeneral, "scroll help down", 2)),
		scroll(b("help.up", []string{"k", "up"}, ctxHelp, grpGeneral, "scroll help up", 2)),
		b("help.pagedown", []string{"ctrl+d", "pgdown", " "}, ctxHelp, grpGeneral, "scroll help a page", 0),
		b("help.pageup", []string{"ctrl+u", "pgup"}, ctxHelp, grpGeneral, "scroll help back a page", 0),

		b("filter.accept", []string{"enter"}, ctxFilter, grpFilter, "keep the filter", 1),
		b("filter.cancel", []string{"esc"}, ctxFilter, grpFilter, "clear the filter", 2),
		b("edit.delete", []string{"backspace"}, ctxEdit, grpFilter, "delete a character", 0),
		b("edit.clear", []string{"ctrl+u"}, ctxEdit, grpFilter, "clear the text", 0),

		b("query.down", []string{"down", "ctrl+n"}, ctxQuery, grpPicker, "next match", 0),
		b("query.up", []string{"up", "ctrl+p"}, ctxQuery, grpPicker, "previous match", 0),
		b("query.pagedown", []string{"pgdown"}, ctxQuery, grpPicker, "half page down", 0),
		b("query.pageup", []string{"pgup"}, ctxQuery, grpPicker, "half page up", 0),

		short(b("picker.toggle", []string{"tab"}, ctxPicker, grpPicker, "select / unselect", 1), "select"),
		b("picker.commit", []string{"enter"}, ctxPicker, grpPicker, "confirm", 2),
		b("picker.cancel", []string{"esc"}, ctxPicker, grpPicker, "cancel", 3),

		b("palette.run", []string{"enter"}, ctxPalette, grpPalette, "run", 1),
		b("palette.cancel", []string{"esc"}, ctxPalette, grpPalette, "cancel", 2),

		// enter opened this modal from the list and from the picker, so a
		// double-tap must not confirm a removal unseen: confirm.enter is
		// disabled whenever the change set removes anything (keyEnabled).
		b("confirm.yes", []string{"y", "Y"}, ctxConfirm, grpConfirm, "apply", 1),
		b("confirm.enter", []string{"enter"}, ctxConfirm, grpConfirm, "apply an assignment", 0),
		b("confirm.no", []string{"n", "N", "esc", "q"}, ctxConfirm, grpConfirm, "cancel", 2),
		b("error.close", []string{"esc", "enter", "q"}, ctxError, grpGeneral, "dismiss the error", 1),
	)
}

// loadKeymap applies the user's keys.yaml over the defaults. A bad file keeps
// the defaults and returns why, so a typo never stops the TUI from starting.
func loadKeymap() (keys.Keymap, error) {
	return defaultKeymap().LoadOverrides(keys.ConfigPath(appName), allStacks...)
}

// active is the context stack for the current state, innermost first.
func (m *Model) active() []keys.Ctx {
	switch {
	case m.busyKind == busyApply:
		return stackApplying
	case m.overlay == overlayError:
		return stackError
	case m.overlay == overlayConfirm:
		return stackConfirm
	case m.overlay == overlayPickUser, m.overlay == overlayPickGroup:
		return stackPicker
	case m.overlay == overlayPalette:
		return stackPalette
	case m.filtering:
		return stackFilter
	}
	return stackFor(m.screen)
}

func stackFor(s screen) []keys.Ctx {
	switch s {
	case screenAssignments:
		return stackAsg
	case screenGroupMembers:
		return stackMembers
	case screenGroupApps:
		return stackGroupApp
	case screenUserApps:
		return stackUserApps
	case screenHelp:
		return stackHelp
	}
	return stackTop
}

func (m *Model) keyOpts() keys.Opts {
	return keys.Opts{Enabled: m.keyEnabled, State: m.keyState}
}

// keyEnabled gates actions that cannot do anything on this screen. A disabled
// binding is left out of the footer and palette, and its key falls through.
//
// Writes during a refresh stay enabled on purpose: the handler explains why
// they wait, which a silently dead key would not.
func (m *Model) keyEnabled(action string) bool {
	switch action {
	case "list.open":
		return m.screen != screenUserApps // the reverse view is a leaf
	case "filter":
		_, ok := m.currentFilter()
		return ok
	case "confirm.enter":
		return !pendingRemoves(m.pending)
	case "palette":
		return m.overlay == overlayNone
	}
	return true
}

// keyState picks a Binding.Labels entry, so one key reads what it does now.
func (m *Model) keyState(action string) string {
	switch action {
	case "refresh":
		if m.refreshing() {
			return "busy"
		}
	case "app.tab", "app.assign":
		if m.showGroups {
			return "groups"
		}
	case "list.open":
		switch m.screen {
		case screenApps:
			return "apps"
		case screenGroups:
			return "groups"
		case screenUsers:
			return "users"
		case screenAssignments:
			if m.showGroups {
				return "asgGroup"
			}
			return "asgUser"
		case screenGroupApps:
			return "groupApp"
		case screenGroupMembers:
			return "member"
		}
	}
	return ""
}

// keyNames displays keys with glyphs (enter → ↵) only when the glyph set is
// Unicode, so an ASCII terminal reads words.
func (m *Model) keyNames() keys.Names {
	return keys.Names{Glyphs: m.gl.check != asciiGlyphs.check, Caret: true}
}

func (m *Model) footerStyle() keys.FooterStyle {
	return keys.FooterStyle{Key: m.st.info, Desc: m.st.footer, More: m.st.dim, Names: m.keyNames()}
}

func (m *Model) helpStyle() keys.HelpStyle {
	// Actions shows each action's name: what a user writes in keys.yaml.
	return keys.HelpStyle{Title: m.st.accent, Key: m.st.info, Desc: m.st.dim, Muted: m.st.dim,
		Names: m.keyNames(), Actions: true}
}
