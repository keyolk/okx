// Package tui implements okx's interactive assignment browser.
//
// The top level has three numbered resource views. Each remains a drill-down
// stack rather than a persistent multi-panel, so narrow terminals stay useful.
//
//	1 Apps   → assignments → user apps
//	2 Groups → members / granted apps (tab) → user apps
//	3 Users  → user apps
package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/keyolk/tuikit/keys"
	"github.com/keyolk/tuikit/prefs"
	"github.com/keyolk/tuikit/toast"

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
	screenGroupMembers
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
	overlayPalette
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
	members        []memberRow
	memberCur      int
	memberTop      int
	memberFiltr    string
	membersLoading bool
	memberBack     screen
	groupAppBack   screen
	revUser        okta.User
	revAccess      []cache.UserAccess
	revCur         int
	revTop         int
	detailBack     screen
	assignmentBack screen
	helpBack       screen
	helpOff        int

	// picker and palette overlays
	pick pickerState
	pal  paletteState

	// pending mutation awaiting confirmation
	pending []plannedChange

	filtering bool
	errText   string

	// km is the one binding table (keys.go). By value: the zero Keymap is
	// safe, and tests that build a Model literal fall back to the defaults.
	km keys.Keymap
	// toasts replaces the old status string: each toast has a level, expires
	// by its own tick, and errors stay until a key after they were seen.
	toasts toast.Queue
	// queued collects the commands handlers return as a side effect of
	// pushing a toast or saving UI state; Update batches them, so a handler
	// deep in the key path does not have to thread a tea.Cmd back out.
	queued []tea.Cmd
	// saver writes UI state (tab, filters, selection) debounced, off the UI
	// loop; quit flushes it and exits on the final Saved.
	saver prefs.Saver[uiState]

	// long-running operation state
	busyKind  busyKind
	busyLabel string
	busyStart time.Time
	prog      progressState
	progCh    chan progressMsg
	progGen   int
	spinFrame int
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
		ctx:    ctx,
		app:    c,
		st:     newStyles(),
		gl:     detectGlyphs(),
		km:     defaultKeymap(),
		toasts: toast.New(1), // the footer has one status row
		saver:  prefs.NewSaver[uiState](appName, 0),
	}
	m.reloadResources()
	return m
}

func (m *Model) reloadResources() {
	// The cursor follows the item, not the row: a refresh that adds or drops
	// rows above it must not move it onto a different app/group/user.
	sel := m.topSelection()
	defer m.restoreTopSelection(sel)

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
	return m.startRefresh()
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

func (m *Model) applyCmd(changes []plannedChange, appID string) tea.Cmd {
	sink, pump := m.newProgressSink()
	ch := m.progCh
	write := func() tea.Msg {
		defer close(ch)
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
			fresh, fetchErr := m.app.FetchIndexProgress(m.ctx, sink)
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
	return tea.Batch(write, pump)
}

// ---- update ---------------------------------------------------------------

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := m.update(msg)
	// Toasts wait while a modal is up (E6): they would otherwise expire
	// unread behind it. Synced here once rather than at every overlay switch.
	switch {
	case m.overlay != overlayNone && !m.toasts.Held():
		m.toasts = toast.Hold(m.toasts)
	case m.overlay == overlayNone && m.toasts.Held():
		var rel tea.Cmd
		m.toasts, rel = toast.Release(m.toasts)
		m.queued = append(m.queued, rel)
	}
	if len(m.queued) > 0 {
		cmd = tea.Batch(append([]tea.Cmd{cmd}, m.queued...)...)
		m.queued = nil
	}
	return model, cmd
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case toast.Expired:
		m.toasts = toast.Update(m.toasts, msg)
		return m, nil

	case prefs.SaveTick:
		var cmd tea.Cmd
		m.saver, cmd = m.saver.Update(msg)
		return m, cmd

	case prefs.Saved:
		if msg.Final {
			return m, tea.Quit // the flushed UI state is on disk (or failed; quit anyway)
		}
		if msg.Err != nil {
			m.notify("prefs", "could not save UI state: "+msg.Err.Error(), toast.Warn)
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.small = msg.Width < minWidth || msg.Height < minHeight
		m.clampTopLevel()
		return m, nil

	case progressMsg:
		if msg.gen != m.progGen {
			return m, nil // a superseded fetch still draining its channel
		}
		m.prog = progressState{stage: msg.stage, done: msg.done, total: msg.total, active: true}
		return m, waitProgress(m.progCh, msg.gen)

	case progressDoneMsg:
		if msg.gen == m.progGen {
			m.prog.active = false
		}
		return m, nil

	case spinnerTickMsg:
		if m.busyKind == busyNone {
			return m, nil
		}
		m.spinFrame++
		return m, spinnerTick()

	case membersDoneMsg:
		// Fold the result into the index here, on the Update goroutine, and into
		// the index that is current now — a refresh may have replaced the one
		// that was live when the fetch started. A snapshot that already resolved
		// the group is at least as fresh, so it wins.
		if msg.err == nil && !m.app.Index.HasMembers(msg.groupID) {
			m.app.ApplyGroupMembers(msg.groupID, msg.users)
		}
		// A late arrival for a group the user already left is still worth keeping
		// in the index, but must not take over the screen.
		if msg.groupID != m.curGroup.ID {
			return m, nil
		}
		m.membersLoading = false
		if msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		m.loadMembers()
		return m, nil

	case refreshDoneMsg:
		m.endBusy()
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
		case screenGroupMembers:
			m.loadGroupApps()
			if m.app.Index.HasMembers(m.curGroup.ID) {
				m.loadMembers()
			}
		case screenUserApps:
			m.revAccess = m.app.Index.UserApps(m.revUser.ID)
		}
		m.notify("refresh", "refreshed", toast.Success)
		return m, nil

	case applyDoneMsg:
		m.endBusy()
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
			m.notify("apply", fmt.Sprintf("applied %d, %d failed: %v",
				msg.applied, msg.failed, msg.err), toast.Error)
		case msg.err != nil:
			m.notify("apply", fmt.Sprintf("applied %d; snapshot refresh failed: %v",
				msg.applied, msg.err), toast.Error)
		default:
			m.notify("apply", fmt.Sprintf("applied %d change(s)", msg.applied), toast.Success)
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) back() {
	switch m.screen {
	case screenUserApps:
		m.screen = m.detailBack
	case screenGroupApps:
		m.screen = m.groupAppBack
	case screenGroupMembers:
		m.screen = m.memberBack
		m.memberFiltr = ""
	case screenAssignments:
		m.screen = m.assignmentBack
		m.asgFiltr = ""
	case screenHelp:
		m.screen = m.helpBack
	}
}

func (m *Model) drillIntoSelected() tea.Cmd {
	if m.showGroups {
		rows := m.filteredGroups()
		if len(rows) == 0 {
			return nil
		}
		// "Who does this group actually bring in?" is the question the groups tab
		// raises, so drilling in opens the membership rather than reporting a count.
		m.curGroup = rows[m.clampIdx(m.asgCur, len(rows))].group
		m.loadGroupApps()
		m.groupAppBack = screenAssignments
		return m.openGroupMembers(screenAssignments)
	}
	rows := m.filteredAssignments()
	if len(rows) == 0 {
		return nil
	}
	sel := rows[m.clampIdx(m.asgCur, len(rows))]
	m.openUserApps(sel.User, screenAssignments)
	return nil
}

func (m *Model) planRemoveSelected() {
	if m.showGroups {
		rows := m.filteredGroups()
		if len(rows) == 0 {
			return
		}
		g := rows[m.clampIdx(m.asgCur, len(rows))]
		m.pending = []plannedChange{{
			verb: "unassign", kind: "group", id: g.group.ID, label: g.group.Profile.Name,
			warn: fmt.Sprintf("removes app access for %d group member(s)", g.members),
		}}
		m.overlay = overlayConfirm
		return
	}

	rows := m.filteredAssignments()
	if len(rows) == 0 {
		return
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
}

// notify pushes a toast. Its expiry tick is batched by Update.
func (m *Model) notify(source, text string, level toast.Level) {
	var cmd tea.Cmd
	m.toasts, cmd = toast.Push(m.toasts, toast.Toast{Source: source, Text: text, Level: level})
	if cmd != nil {
		m.queued = append(m.queued, cmd)
	}
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
	for _, s := range []screen{screenApps, screenGroups, screenUsers} {
		if l, ok := m.listFor(s); ok {
			m.clampList(l)
		}
	}
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
	// A bad keys.yaml or ui.json must not stop the TUI: keep the defaults
	// and say why, sticky, so the user sees it.
	km, err := loadKeymap()
	if err != nil {
		m.notify("keys", "keys.yaml ignored: "+err.Error(), toast.Error)
	}
	m.km = km
	saved, err := prefs.Load(appName, uiState{})
	if err != nil {
		m.notify("prefs", "ui.json ignored: "+err.Error(), toast.Warn)
	}
	m.restoreUI(saved)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
	_, err = p.Run()
	return err
}

var _ tea.Model = (*Model)(nil)
