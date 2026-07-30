// Package tui implements okx's interactive assignment browser.
//
// Layout is a drill-down stack (k9s/a9s shape), not a persistent multi-panel:
// the workflow is "pick an app, then work inside it", and a 3-pane layout
// would waste half the width on a 7-row app list.
//
//	screenApps  → screenAssignments → overlay pickers / confirm modal
package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	okxapp "github.com/keyolk/okx/internal/app"
	"github.com/keyolk/okx/internal/cache"
	"github.com/keyolk/okx/internal/okta"
)

type screen int

const (
	screenApps screen = iota
	screenAssignments
	screenUserApps // reverse view: which apps does this user have
	screenHelp
)

// overlay is a modal layered on top of the current screen.
type overlay int

const (
	overlayNone overlay = iota
	overlayPickUser
	overlayPickGroup
	overlayConfirm
	overlayError
)

const (
	minWidth  = 60
	minHeight = 14
)

// Model is the Bubble Tea model.
type Model struct {
	ctx context.Context
	app *okxapp.Context

	st     *styles
	gl     glyphSet
	width  int
	height int
	small  bool

	screen  screen
	overlay overlay

	// app list
	apps     []okta.App
	appCur   int
	appTop   int
	appFiltr string

	// current app's assignments
	curApp      okta.App
	assignments []cache.Assignment
	asgCur      int
	asgTop      int
	asgFiltr    string
	// showGroups switches the assignment pane between users and assigned groups.
	showGroups bool
	appGroups  []groupRow

	// reverse view
	revUser   okta.User
	revAccess []cache.UserAccess
	revCur    int
	revTop    int

	// picker overlay
	pick pickerState

	// pending mutation awaiting confirmation
	pending []plannedChange

	filtering bool
	status    string
	statusErr bool
	busy      string
	errText   string
}

type groupRow struct {
	group   okta.Group
	members int
}

// plannedChange mirrors the CLI's change planning so the TUI shows the same
// caveats before applying anything.
type plannedChange struct {
	verb  string
	kind  string
	id    string
	label string
	noop  string
	warn  string
}

// New builds the model.
func New(ctx context.Context, c *okxapp.Context) *Model {
	m := &Model{
		ctx: ctx,
		app: c,
		st:  newStyles(),
		gl:  detectGlyphs(),
	}
	m.reloadApps()
	return m
}

func (m *Model) reloadApps() {
	m.apps = append(m.apps[:0], m.app.Index.Apps...)
	sort.Slice(m.apps, func(i, j int) bool { return m.apps[i].Label < m.apps[j].Label })
	m.clampApps()
}

func (m *Model) loadAssignments() {
	m.assignments = m.app.Assignments(m.curApp.ID)
	m.appGroups = m.appGroups[:0]
	for _, ag := range m.app.Index.AppGroups[m.curApp.ID] {
		g, ok := m.app.Index.Group(ag.ID)
		if !ok {
			g = okta.Group{ID: ag.ID}
			g.Profile.Name = ag.ID
		}
		m.appGroups = append(m.appGroups, groupRow{g, len(m.app.Index.GroupMembers[ag.ID])})
	}
	sort.Slice(m.appGroups, func(i, j int) bool {
		return m.appGroups[i].group.Profile.Name < m.appGroups[j].group.Profile.Name
	})
	m.asgCur, m.asgTop = 0, 0
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd { return nil }

// ---- messages -------------------------------------------------------------

type refreshDoneMsg struct{ err error }
type applyDoneMsg struct {
	applied int
	failed  int
	err     error
}

func (m *Model) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		return refreshDoneMsg{err: m.app.Refetch(m.ctx, true)}
	}
}

func (m *Model) applyCmd(changes []plannedChange, appID string) tea.Cmd {
	return func() tea.Msg {
		var applied, failed int
		var firstErr error
		for _, ch := range changes {
			if ch.noop != "" {
				continue
			}
			var err error
			switch {
			case ch.kind == "group" && ch.verb == "assign":
				_, err = m.app.Client.AssignGroup(m.ctx, appID, ch.id)
			case ch.kind == "group":
				err = m.app.Client.UnassignGroup(m.ctx, appID, ch.id)
			case ch.verb == "assign":
				_, err = m.app.Client.AssignUser(m.ctx, appID, ch.id)
			default:
				err = m.app.Client.UnassignUser(m.ctx, appID, ch.id)
			}
			if err != nil && ch.verb == "unassign" && okta.NotFound(err) {
				err = nil // already gone; the desired end state holds
			}
			if err != nil {
				failed++
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			applied++
		}
		if applied > 0 {
			// The snapshot is wrong the moment a write lands.
			if err := m.app.Refetch(m.ctx, true); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return applyDoneMsg{applied: applied, failed: failed, err: firstErr}
	}
}

// ---- update ---------------------------------------------------------------

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.small = msg.Width < minWidth || msg.Height < minHeight
		m.clampApps()
		return m, nil

	case refreshDoneMsg:
		m.busy = ""
		if msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		m.reloadApps()
		if m.screen == screenAssignments {
			if a, ok := m.app.Index.App(m.curApp.ID); ok {
				m.curApp = a
			}
			m.loadAssignments()
		}
		m.setStatus("refreshed", false)
		return m, nil

	case applyDoneMsg:
		m.busy = ""
		m.reloadApps()
		if m.screen == screenAssignments {
			m.loadAssignments()
		}
		switch {
		case msg.err != nil && msg.applied == 0:
			m.setError(msg.err)
		case msg.failed > 0:
			m.setStatus(fmt.Sprintf("%s applied %d, %d failed: %v",
				m.gl.fail, msg.applied, msg.failed, msg.err), true)
		default:
			m.setStatus(fmt.Sprintf("%s applied %d change(s)", m.gl.ok, msg.applied), false)
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// A running mutation owns the screen: swallow everything but quit so a
	// stray keypress can't queue a second write against stale state.
	if m.busy != "" {
		if key == "q" || key == "ctrl+c" {
			return m, tea.Quit
		}
		return m, nil
	}

	if m.overlay != overlayNone {
		return m.handleOverlayKey(msg)
	}
	if m.filtering {
		return m.handleFilterKey(msg)
	}

	switch key {
	case "q", "ctrl+c":
		if m.screen != screenApps {
			m.back()
			return m, nil
		}
		return m, tea.Quit
	case "?":
		if m.screen == screenHelp {
			m.screen = screenApps
		} else {
			m.screen = screenHelp
		}
		return m, nil
	case "esc":
		m.back()
		return m, nil
	case "R":
		m.busy = "refreshing"
		m.status = ""
		return m, m.refreshCmd()
	case "/":
		m.filtering = true
		return m, nil
	}

	switch m.screen {
	case screenApps:
		return m.handleAppsKey(key)
	case screenAssignments:
		return m.handleAssignmentsKey(key)
	case screenUserApps:
		return m.handleUserAppsKey(key)
	case screenHelp:
		m.screen = screenApps
		return m, nil
	}
	return m, nil
}

func (m *Model) back() {
	switch m.screen {
	case screenUserApps:
		m.screen = screenAssignments
	case screenAssignments:
		m.screen = screenApps
		m.asgFiltr = ""
	case screenHelp:
		m.screen = screenApps
	}
}

func (m *Model) handleAppsKey(key string) (tea.Model, tea.Cmd) {
	rows := m.filteredApps()
	switch key {
	case "j", "down":
		m.appCur++
	case "k", "up":
		m.appCur--
	case "g", "home":
		m.appCur = 0
	case "G", "end":
		m.appCur = len(rows) - 1
	case "ctrl+d", "pgdown":
		m.appCur += m.listHeight() / 2
	case "ctrl+u", "pgup":
		m.appCur -= m.listHeight() / 2
	case "enter", "l", "right":
		if len(rows) == 0 {
			return m, nil
		}
		m.curApp = rows[m.clampIdx(m.appCur, len(rows))]
		m.screen = screenAssignments
		m.showGroups = false
		m.loadAssignments()
		return m, nil
	}
	m.appCur = m.clampIdx(m.appCur, len(rows))
	m.appTop = scrollTo(m.appTop, m.appCur, m.listHeight())
	return m, nil
}

func (m *Model) handleAssignmentsKey(key string) (tea.Model, tea.Cmd) {
	n := m.assignmentRowCount()
	switch key {
	case "j", "down":
		m.asgCur++
	case "k", "up":
		m.asgCur--
	case "g", "home":
		m.asgCur = 0
	case "G", "end":
		m.asgCur = n - 1
	case "ctrl+d", "pgdown":
		m.asgCur += m.listHeight() / 2
	case "ctrl+u", "pgup":
		m.asgCur -= m.listHeight() / 2
	case "tab":
		m.showGroups = !m.showGroups
		m.asgCur, m.asgTop = 0, 0
		m.asgFiltr = ""
		return m, nil
	case "h", "left":
		m.back()
		return m, nil
	case "a":
		m.openPicker(overlayPickUser)
		return m, nil
	case "A":
		m.openPicker(overlayPickGroup)
		return m, nil
	case "d", "delete", "backspace":
		return m.planRemoveSelected()
	case "enter", "l", "right":
		return m.drillIntoSelected()
	}
	m.asgCur = m.clampIdx(m.asgCur, n)
	m.asgTop = scrollTo(m.asgTop, m.asgCur, m.listHeight())
	return m, nil
}

func (m *Model) handleUserAppsKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "j", "down":
		m.revCur++
	case "k", "up":
		m.revCur--
	case "g", "home":
		m.revCur = 0
	case "G", "end":
		m.revCur = len(m.revAccess) - 1
	case "h", "left":
		m.back()
		return m, nil
	}
	m.revCur = m.clampIdx(m.revCur, len(m.revAccess))
	m.revTop = scrollTo(m.revTop, m.revCur, m.listHeight())
	return m, nil
}

func (m *Model) drillIntoSelected() (tea.Model, tea.Cmd) {
	if m.showGroups {
		rows := m.filteredGroups()
		if len(rows) == 0 {
			return m, nil
		}
		// Drilling into a group shows the app list of… nothing useful; instead
		// jump the user list filtered to that group's members would need a new
		// screen. Keep it simple: report the membership count.
		g := rows[m.clampIdx(m.asgCur, len(rows))]
		m.setStatus(fmt.Sprintf("%s — %d member(s), id %s",
			g.group.Profile.Name, g.members, g.group.ID), false)
		return m, nil
	}
	rows := m.filteredAssignments()
	if len(rows) == 0 {
		return m, nil
	}
	sel := rows[m.clampIdx(m.asgCur, len(rows))]
	m.revUser = sel.User
	m.revAccess = m.app.Index.UserApps(sel.User.ID)
	m.revCur, m.revTop = 0, 0
	m.screen = screenUserApps
	return m, nil
}

func (m *Model) planRemoveSelected() (tea.Model, tea.Cmd) {
	if m.showGroups {
		rows := m.filteredGroups()
		if len(rows) == 0 {
			return m, nil
		}
		g := rows[m.clampIdx(m.asgCur, len(rows))]
		m.pending = []plannedChange{{
			verb: "unassign", kind: "group", id: g.group.ID, label: g.group.Profile.Name,
			warn: fmt.Sprintf("removes app access for %d group member(s)", g.members),
		}}
		m.overlay = overlayConfirm
		return m, nil
	}

	rows := m.filteredAssignments()
	if len(rows) == 0 {
		return m, nil
	}
	sel := rows[m.clampIdx(m.asgCur, len(rows))]
	ch := plannedChange{verb: "unassign", kind: "user",
		id: sel.User.ID, label: sel.User.Profile.Login}
	switch {
	case !sel.Direct:
		ch.noop = "only has access via " + groupList(sel.ViaGroups) +
			" — press Tab and remove the group assignment instead"
	case len(sel.ViaGroups) > 0:
		ch.warn = "still keeps access via " + groupList(sel.ViaGroups)
	}
	m.pending = []plannedChange{ch}
	m.overlay = overlayConfirm
	return m, nil
}

func (m *Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	target := &m.appFiltr
	if m.screen == screenAssignments {
		target = &m.asgFiltr
	}
	switch msg.String() {
	case "enter", "esc":
		m.filtering = false
		if msg.String() == "esc" {
			*target = ""
		}
	case "backspace":
		if r := []rune(*target); len(r) > 0 {
			*target = string(r[:len(r)-1])
		}
	case "ctrl+u":
		*target = ""
	default:
		if msg.Type == tea.KeyRunes {
			*target += string(msg.Runes)
		} else if msg.String() == " " {
			*target += " "
		}
	}
	m.appCur, m.appTop = 0, 0
	m.asgCur, m.asgTop = 0, 0
	return m, nil
}

func (m *Model) setStatus(s string, isErr bool) {
	m.status, m.statusErr = s, isErr
}

func (m *Model) setError(err error) {
	m.errText = err.Error()
	m.overlay = overlayError
}

// ---- geometry helpers -----------------------------------------------------

// listHeight is the number of data rows that fit: total minus header (2),
// column header (1), and footer (2).
func (m *Model) listHeight() int {
	h := m.height - 5
	if h < 1 {
		return 1
	}
	return h
}

func (m *Model) clampApps() {
	m.appCur = m.clampIdx(m.appCur, len(m.filteredApps()))
	m.appTop = scrollTo(m.appTop, m.appCur, m.listHeight())
}

func (m *Model) clampIdx(i, n int) int {
	if n == 0 {
		return 0
	}
	if i < 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
}

// scrollTo keeps cur visible in a window of the given height.
func scrollTo(top, cur, height int) int {
	if cur < top {
		return cur
	}
	if cur >= top+height {
		return cur - height + 1
	}
	if top < 0 {
		return 0
	}
	return top
}

func (m *Model) assignmentRowCount() int {
	if m.showGroups {
		return len(m.filteredGroups())
	}
	return len(m.filteredAssignments())
}

func groupList(gs []okta.Group) string {
	if len(gs) == 0 {
		return "a group"
	}
	var ns []string
	for _, g := range gs {
		ns = append(ns, g.Profile.Name)
	}
	return strings.Join(ns, ", ")
}

// Run starts the TUI program.
func Run(ctx context.Context, c *okxapp.Context) error {
	m := New(ctx, c)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
	_, err := p.Run()
	return err
}

var _ tea.Model = (*Model)(nil)
var _ = lipgloss.JoinVertical
