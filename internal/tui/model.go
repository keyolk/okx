// Package tui implements okx's interactive assignment browser.
//
// The top level has three numbered resource views. Each remains a drill-down
// stack rather than a persistent multi-panel, so narrow terminals stay useful.
//
//	1 Apps   → assignments → user apps
//	2 Groups → granted apps
//	3 Users  → user apps
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
	screenGroups
	screenUsers
	screenAssignments
	screenGroupApps
	screenUserApps
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

	// numbered top-level resource views
	apps           []okta.App
	appCur         int
	appTop         int
	appFiltr       string
	groups         []okta.Group
	groupCur       int
	groupTop       int
	groupFiltr     string
	users          []okta.User
	userCur        int
	userTop        int
	userFiltr      string
	groupAppCounts map[string]int
	userAppCounts  map[string]int

	// current app's assignments
	curApp      okta.App
	assignments []cache.Assignment
	asgCur      int
	asgTop      int
	asgFiltr    string
	// showGroups switches the assignment pane between users and assigned groups.
	showGroups bool
	appGroups  []groupRow

	// group and user detail views
	curGroup       okta.Group
	groupApps      []okta.App
	groupAppCur    int
	groupAppTop    int
	revUser        okta.User
	revAccess      []cache.UserAccess
	revCur         int
	revTop         int
	detailBack     screen
	assignmentBack screen
	helpBack       screen

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
	m.reloadResources()
	return m
}

func (m *Model) reloadResources() {
	m.apps = append(m.apps[:0], m.app.Index.Apps...)
	sort.Slice(m.apps, func(i, j int) bool { return m.apps[i].Label < m.apps[j].Label })
	m.groups = append(m.groups[:0], m.app.Index.Groups...)
	sort.Slice(m.groups, func(i, j int) bool {
		return m.groups[i].Profile.Name < m.groups[j].Profile.Name
	})
	m.users = append(m.users[:0], m.app.Index.Users...)
	sort.Slice(m.users, func(i, j int) bool {
		return m.users[i].Profile.Login < m.users[j].Profile.Login
	})

	m.groupAppCounts = make(map[string]int, len(m.groups))
	m.userAppCounts = make(map[string]int, len(m.users))
	for appID, groups := range m.app.Index.AppGroups {
		if _, ok := m.app.Index.App(appID); !ok {
			continue
		}
		for _, group := range groups {
			m.groupAppCounts[group.ID]++
		}
	}
	for appID, users := range m.app.Index.AppUsers {
		if _, ok := m.app.Index.App(appID); !ok {
			continue
		}
		for _, user := range users {
			m.userAppCounts[user.ID]++
		}
	}
	m.clampTopLevel()
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

// Init implements tea.Model. A stale snapshot remains immediately usable while
// its replacement is fetched by Bubble Tea rather than blocking startup.
func (m *Model) Init() tea.Cmd {
	if !m.app.NeedsRefresh {
		return nil
	}
	m.busy = "refreshing"
	return m.refreshCmd()
}

// ---- messages -------------------------------------------------------------

type refreshDoneMsg struct {
	index *cache.Index
	err   error
}
type applyDoneMsg struct {
	applied int
	failed  int
	index   *cache.Index
	err     error
}

func (m *Model) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		index, err := m.app.FetchIndex(m.ctx, true)
		return refreshDoneMsg{index: index, err: err}
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

		var index *cache.Index
		if applied > 0 {
			// Never leave a now-wrong snapshot looking fresh if the follow-up
			// fetch fails. A successful save supersedes an earlier delete error.
			invalidateErr := m.app.Invalidate()
			fresh, fetchErr := m.app.FetchIndex(m.ctx, true)
			if fetchErr != nil {
				if firstErr == nil {
					if invalidateErr != nil {
						firstErr = fmt.Errorf("invalidate cache: %v; refetch snapshot: %w", invalidateErr, fetchErr)
					} else {
						firstErr = fetchErr
					}
				}
			} else {
				index = fresh
			}
		}
		return applyDoneMsg{applied: applied, failed: failed, index: index, err: firstErr}
	}
}

// ---- update ---------------------------------------------------------------

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.small = msg.Width < minWidth || msg.Height < minHeight
		m.clampTopLevel()
		return m, nil

	case refreshDoneMsg:
		m.busy = ""
		if msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		m.app.Index = msg.index
		m.app.NeedsRefresh = false
		m.reloadResources()
		switch m.screen {
		case screenAssignments:
			if a, ok := m.app.Index.App(m.curApp.ID); ok {
				m.curApp = a
			}
			if m.assignmentBack == screenGroupApps {
				m.loadGroupApps()
			}
			m.loadAssignments()
		case screenGroupApps:
			m.loadGroupApps()
		case screenUserApps:
			m.revAccess = m.app.Index.UserApps(m.revUser.ID)
		}
		m.setStatus("refreshed", false)
		return m, nil

	case applyDoneMsg:
		m.busy = ""
		m.app.NeedsRefresh = msg.applied > 0 && msg.index == nil
		if msg.index != nil {
			m.app.Index = msg.index
		}
		m.reloadResources()
		if m.screen == screenAssignments {
			if m.assignmentBack == screenGroupApps {
				m.loadGroupApps()
			}
			m.loadAssignments()
		}
		switch {
		case msg.err != nil && msg.applied == 0:
			m.setError(msg.err)
		case msg.failed > 0:
			m.setStatus(fmt.Sprintf("%s applied %d, %d failed: %v",
				m.gl.fail, msg.applied, msg.failed, msg.err), true)
		case msg.err != nil:
			m.setStatus(fmt.Sprintf("%s applied %d; snapshot refresh failed: %v",
				m.gl.fail, msg.applied, msg.err), true)
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

	// A running mutation owns the screen so a stray keypress cannot queue a
	// second write against stale state. Refresh is read-only: keep the numbered
	// resource views available while the first snapshot is loading.
	if m.busy != "" {
		if key == "q" || key == "ctrl+c" {
			return m, tea.Quit
		}
		if m.busy == "refreshing" {
			switch key {
			case "1":
				m.switchTopLevel(screenApps)
			case "2":
				m.switchTopLevel(screenGroups)
			case "3":
				m.switchTopLevel(screenUsers)
			}
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
	case "1":
		m.switchTopLevel(screenApps)
		return m, nil
	case "2":
		m.switchTopLevel(screenGroups)
		return m, nil
	case "3":
		m.switchTopLevel(screenUsers)
		return m, nil
	case "q", "ctrl+c":
		if !isTopLevel(m.screen) {
			m.back()
			return m, nil
		}
		return m, tea.Quit
	case "?":
		if m.screen == screenHelp {
			m.screen = m.helpBack
		} else {
			m.helpBack = m.screen
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
		if _, ok := m.currentFilter(); ok {
			m.filtering = true
		}
		return m, nil
	}

	switch m.screen {
	case screenApps:
		return m.handleAppsKey(key)
	case screenGroups:
		return m.handleGroupsKey(key)
	case screenUsers:
		return m.handleUsersKey(key)
	case screenAssignments:
		return m.handleAssignmentsKey(key)
	case screenGroupApps:
		return m.handleGroupAppsKey(key)
	case screenUserApps:
		return m.handleUserAppsKey(key)
	case screenHelp:
		m.screen = m.helpBack
		return m, nil
	}
	return m, nil
}

func (m *Model) back() {
	switch m.screen {
	case screenUserApps:
		m.screen = m.detailBack
	case screenGroupApps:
		m.screen = screenGroups
	case screenAssignments:
		m.screen = m.assignmentBack
		m.asgFiltr = ""
	case screenHelp:
		m.screen = m.helpBack
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
		m.assignmentBack = screenApps
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
	m.openUserApps(sel.User, screenAssignments)
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
	target, ok := m.currentFilter()
	if !ok {
		m.filtering = false
		return m, nil
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
	m.resetCurrentCursor()
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

func (m *Model) clampTopLevel() {
	m.appCur = m.clampIdx(m.appCur, len(m.filteredApps()))
	m.appTop = scrollTo(m.appTop, m.appCur, m.listHeight())
	m.groupCur = m.clampIdx(m.groupCur, len(m.filteredTopGroups()))
	m.groupTop = scrollTo(m.groupTop, m.groupCur, m.listHeight())
	m.userCur = m.clampIdx(m.userCur, len(m.filteredTopUsers()))
	m.userTop = scrollTo(m.userTop, m.userCur, m.listHeight())
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
