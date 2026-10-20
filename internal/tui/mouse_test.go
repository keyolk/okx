package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	okxapp "github.com/keyolk/okx/internal/app"
	"github.com/keyolk/okx/internal/cache"
	"github.com/keyolk/okx/internal/config"
	"github.com/keyolk/okx/internal/okta"
)

// at finds where View drew text, so a click lands on the cell the user sees
// rather than on a coordinate the test computed with the same arithmetic as
// the code under test.
func at(t *testing.T, m *Model, text string) (x, y int) {
	t.Helper()
	for i, line := range strings.Split(m.View(), "\n") {
		plain := ansi.Strip(line)
		if j := strings.Index(plain, text); j >= 0 {
			return ansi.StringWidth(plain[:j]), i
		}
	}
	t.Fatalf("%q is not on screen:\n%s", text, ansi.Strip(m.View()))
	return 0, 0
}

func onScreen(m *Model, text string) bool {
	return strings.Contains(ansi.Strip(m.View()), text)
}

func press(m *Model, x, y int, b tea.MouseButton) {
	m.Update(tea.MouseMsg{X: x, Y: y, Button: b, Action: tea.MouseActionPress})
}

func click(m *Model, x, y int) { press(m, x, y, tea.MouseButtonLeft) }

func wheel(m *Model, x, y int, b tea.MouseButton) { press(m, x, y, b) }

func clickText(t *testing.T, m *Model, text string) {
	t.Helper()
	x, y := at(t, m, text)
	click(m, x, y)
}

// mouseModel has five apps, four users, two groups and, on Alpha, four
// direct assignments. The clock is frozen so a double click is two clicks at
// the same instant.
func mouseModel() *Model {
	snap := cache.Empty("https://example.okta.com")
	for _, name := range []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo"} {
		snap.Apps = append(snap.Apps, okta.App{ID: "app-" + strings.ToLower(name), Label: name + " Console"})
	}
	for _, name := range []string{"ann", "bob", "cyd", "dee"} {
		u := okta.User{ID: "user-" + name, Status: "ACTIVE"}
		u.Profile.Login = name + "@example.com"
		snap.Users = append(snap.Users, u)
		snap.AppUsers["app-alpha"] = append(snap.AppUsers["app-alpha"], okta.AppUser{ID: u.ID, Scope: "USER", Status: "ACTIVE"})
	}
	for _, name := range []string{"Platform", "Support"} {
		g := okta.Group{ID: "group-" + strings.ToLower(name), Type: "OKTA_GROUP"}
		g.Profile.Name = name + " Team"
		snap.Groups = append(snap.Groups, g)
	}
	m := New(context.Background(), &okxapp.Context{
		Cfg:   config.Config{OrgURL: snap.OrgURL},
		Index: cache.NewIndex(snap),
	})
	m.width, m.height = 100, 30
	t0 := time.Unix(1000, 0)
	m.now = func() time.Time { return t0 }
	return m
}

func key(m *Model, s string) { m.handleKey(keyMsg(s)) }

func TestClickSelectsTheRowUnderThePointer(t *testing.T) {
	m := mouseModel()
	clickText(t, m, "Charlie Console")
	if m.appCur != 2 {
		t.Fatalf("appCur=%d, want Charlie (2)", m.appCur)
	}
	if m.screen != screenApps {
		t.Fatal("one click selects; it must not open the app")
	}
}

func TestSecondClickOnTheSelectedRowOpensIt(t *testing.T) {
	m := mouseModel()
	x, y := at(t, m, "Bravo Console")
	click(m, x, y)
	click(m, x, y)
	if m.screen != screenAssignments || m.curApp.ID != "app-bravo" {
		t.Fatalf("screen=%v app=%q, want Bravo's assignments", m.screen, m.curApp.ID)
	}
}

func TestSecondClickOnAnotherRowOnlySelects(t *testing.T) {
	m := mouseModel()
	clickText(t, m, "Bravo Console")
	clickText(t, m, "Delta Console")
	if m.screen != screenApps || m.appCur != 3 {
		t.Fatalf("screen=%v appCur=%d, want Apps / Delta", m.screen, m.appCur)
	}
}

func TestSlowSecondClickIsNotADoubleClick(t *testing.T) {
	m := mouseModel()
	now := time.Unix(1000, 0)
	m.now = func() time.Time { return now }
	x, y := at(t, m, "Bravo Console")
	click(m, x, y)
	now = now.Add(2 * time.Second)
	click(m, x, y)
	if m.screen != screenApps {
		t.Fatal("two clicks two seconds apart opened the app")
	}
}

// A key between two clicks breaks the pair. Click a row, j, k back to it,
// click again inside the double-click window: that is a fresh first click,
// so it selects and must not open the app.
func TestKeyBetweenClicksBreaksTheDoubleClick(t *testing.T) {
	m := mouseModel()
	x, y := at(t, m, "Bravo Console")
	click(m, x, y)
	m.Update(keyMsg("j"))
	m.Update(keyMsg("k"))
	if m.appCur != 1 {
		t.Fatalf("appCur=%d after j, k; want back on Bravo (1)", m.appCur)
	}
	click(m, x, y)
	if m.screen != screenApps {
		t.Fatalf("screen=%v: a key between the clicks must break the double click", m.screen)
	}
	if m.appCur != 1 {
		t.Fatalf("appCur=%d, want Bravo (1) still selected", m.appCur)
	}
}

func TestDoubleClickDrillsThroughEveryList(t *testing.T) {
	m := mouseModel()
	key(m, "2")
	x, y := at(t, m, "Support Team")
	click(m, x, y)
	click(m, x, y)
	if m.screen != screenGroupMembers || m.curGroup.ID != "group-support" {
		t.Fatalf("groups: screen=%v group=%q", m.screen, m.curGroup.ID)
	}

	m = mouseModel()
	key(m, "3")
	x, y = at(t, m, "cyd@example.com")
	click(m, x, y)
	click(m, x, y)
	if m.screen != screenUserApps || m.revUser.ID != "user-cyd" {
		t.Fatalf("users: screen=%v user=%q", m.screen, m.revUser.ID)
	}
}

func TestUserAppsIsALeaf(t *testing.T) {
	m := mouseModel()
	m.openUserApps(m.users[0], screenUsers)
	m.revAccess = []cache.UserAccess{{App: m.apps[0], Direct: true}}
	x, y := at(t, m, "Alpha Console")
	click(m, x, y)
	click(m, x, y)
	if m.screen != screenUserApps {
		t.Fatalf("a double click left the leaf screen: %v", m.screen)
	}
}

func TestClickOnAssignmentRowSelectsAndNeverRemoves(t *testing.T) {
	m := mouseModel()
	m.curApp = m.apps[0]
	m.assignmentBack = screenApps
	m.screen = screenAssignments
	m.loadAssignments()

	x, y := at(t, m, "cyd@example.com")
	click(m, x, y)
	if m.asgCur != 2 || m.overlay != overlayNone {
		t.Fatalf("asgCur=%d overlay=%v, want Cyd selected and no modal", m.asgCur, m.overlay)
	}
	click(m, x, y)
	if m.screen != screenUserApps || m.revUser.ID != "user-cyd" {
		t.Fatalf("a double click is the drill-in key; screen=%v", m.screen)
	}
	if m.pending != nil || m.busyKind != busyNone {
		t.Fatal("a click planned or applied a change")
	}
}

func TestWheelMovesTheSelectionThreeRows(t *testing.T) {
	m := mouseModel()
	x, y := at(t, m, "Alpha Console")
	wheel(m, x, y, tea.MouseButtonWheelDown)
	if m.appCur != 3 {
		t.Fatalf("appCur=%d after one notch down, want 3", m.appCur)
	}
	wheel(m, x, y, tea.MouseButtonWheelUp)
	if m.appCur != 0 {
		t.Fatalf("appCur=%d after one notch up, want 0", m.appCur)
	}
}

func TestWheelOutsideTheBodyIsIgnored(t *testing.T) {
	m := mouseModel()
	wheel(m, 5, m.height-1, tea.MouseButtonWheelDown) // the footer
	wheel(m, 5, 0, tea.MouseButtonWheelDown)          // the tabs
	if m.appCur != 0 {
		t.Fatalf("appCur=%d, want 0", m.appCur)
	}
}

// A long list scrolls; the row under the pointer is then not item N of the
// slice but item start+N.
func TestClickOnAScrolledListHitsTheVisibleRow(t *testing.T) {
	m := mouseModel()
	for i := range 60 {
		m.apps = append(m.apps, okta.App{ID: fmt.Sprintf("x%02d", i), Label: fmt.Sprintf("Filler %02d", i)})
	}
	key(m, "G") // bottom: the window scrolls
	if m.appTop == 0 {
		t.Fatal("test setup: list did not scroll")
	}
	label := m.apps[m.appTop+2].Label
	x, y := at(t, m, label)
	click(m, x, y)
	if got := m.apps[m.appCur].Label; got != label {
		t.Fatalf("clicked %q, selected %q", label, got)
	}
}

func TestClickOnTheMoreLineAndBlankAreasDoNothing(t *testing.T) {
	m := mouseModel()
	for i := range 60 {
		m.apps = append(m.apps, okta.App{ID: fmt.Sprintf("x%02d", i), Label: fmt.Sprintf("Filler %02d", i)})
	}
	x, y := at(t, m, "↓ ")
	click(m, x, y)
	if m.appCur != 0 {
		t.Fatalf("a click on the %q line selected row %d", "↓ more", m.appCur)
	}

	m = mouseModel() // five apps in a 25-row list: blank rows below
	click(m, 10, m.height-4)
	click(m, 10, m.height-1) // the footer
	click(m, 10, 2)          // the column header
	if m.appCur != 0 || m.screen != screenApps {
		t.Fatalf("blank-area click changed state: appCur=%d screen=%v", m.appCur, m.screen)
	}
}

func TestReleaseMotionAndOtherButtonsAreIgnored(t *testing.T) {
	m := mouseModel()
	x, y := at(t, m, "Delta Console")
	for _, msg := range []tea.MouseMsg{
		{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease},
		{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion},
		{X: x, Y: y, Button: tea.MouseButtonNone, Action: tea.MouseActionMotion},
		{X: x, Y: y, Button: tea.MouseButtonRight, Action: tea.MouseActionPress},
		{X: x, Y: y, Button: tea.MouseButtonMiddle, Action: tea.MouseActionPress},
	} {
		m.Update(msg)
	}
	if m.appCur != 0 {
		t.Fatalf("appCur=%d, want 0", m.appCur)
	}
}

func TestTabClickSwitchesView(t *testing.T) {
	m := mouseModel()
	clickText(t, m, "2 Groups")
	if m.screen != screenGroups {
		t.Fatalf("screen=%v, want Groups", m.screen)
	}
	clickText(t, m, "3 Users")
	if m.screen != screenUsers {
		t.Fatalf("screen=%v, want Users", m.screen)
	}
	clickText(t, m, "1 Apps")
	if m.screen != screenApps {
		t.Fatalf("screen=%v, want Apps", m.screen)
	}
}

func TestNarrowLayoutStillHits(t *testing.T) {
	m := mouseModel()
	m.width, m.height = minWidth, minHeight
	m.clampTopLevel()
	clickText(t, m, "Charlie Console")
	if m.appCur != 2 {
		t.Fatalf("appCur=%d, want 2", m.appCur)
	}
	clickText(t, m, "2 Groups")
	if m.screen != screenGroups {
		t.Fatalf("screen=%v, want Groups", m.screen)
	}
}

func TestTooSmallScreenIgnoresTheMouse(t *testing.T) {
	m := mouseModel()
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	click(m, 5, 3)
	wheel(m, 5, 3, tea.MouseButtonWheelDown)
	if m.appCur != 0 || m.screen != screenApps {
		t.Fatalf("appCur=%d screen=%v", m.appCur, m.screen)
	}
}

func TestFilterPromptAndApplyingOwnTheScreen(t *testing.T) {
	m := mouseModel()
	key(m, "/")
	x, y := at(t, m, "Charlie Console")
	click(m, x, y)
	wheel(m, x, y, tea.MouseButtonWheelDown)
	if m.appCur != 0 || !m.filtering {
		t.Fatalf("appCur=%d filtering=%v: the mouse reached the list while typing", m.appCur, m.filtering)
	}

	m = mouseModel()
	m.busyKind = busyApply
	click(m, x, y)
	if m.appCur != 0 {
		t.Fatal("the mouse reached the list while a change was applying")
	}
}

func TestHelpPageScrollsAndIgnoresClicks(t *testing.T) {
	m := mouseModel()
	key(m, "?")
	if m.screen != screenHelp {
		t.Fatal("? opens help")
	}
	wheel(m, 10, 10, tea.MouseButtonWheelDown)
	if m.helpOff == 0 {
		t.Fatal("the wheel did not scroll help")
	}
	click(m, 10, 10)
	if m.screen != screenHelp {
		t.Fatal("a click on the help page left it")
	}
}

// ---- overlays ----------------------------------------------------------------

func TestConfirmNeverAppliesOnClick(t *testing.T) {
	m := mouseModel()
	m.curApp = m.apps[0]
	m.assignmentBack = screenApps
	m.screen = screenAssignments
	m.loadAssignments()
	key(m, "d")
	if m.overlay != overlayConfirm || !pendingRemoves(m.pending) {
		t.Fatalf("setup: overlay=%v pending=%v", m.overlay, m.pending)
	}

	// Single and double clicks on the confirm text, the "apply" ask, and the
	// row it names: none of them may start the removal.
	for _, text := range []string{"unassign on", "user ann@example.com", "apply 1 change(s)?"} {
		x, y := at(t, m, text)
		click(m, x, y)
		click(m, x, y)
		if m.overlay != overlayConfirm || m.busyKind != busyNone || len(m.pending) == 0 {
			t.Fatalf("a click on %q changed the confirm: overlay=%v busy=%v", text, m.overlay, m.busyKind)
		}
	}
	// And the rows beneath the modal are not reachable through it.
	if m.asgCur != 0 {
		t.Fatalf("asgCur=%d: a click went through the modal", m.asgCur)
	}

	click(m, 0, m.height-1) // outside: esc
	if m.overlay != overlayNone || m.pending != nil || m.busyKind != busyNone {
		t.Fatalf("outside click: overlay=%v pending=%v busy=%v, want cancelled", m.overlay, m.pending, m.busyKind)
	}
}

func TestErrorModalOwnsTheMouse(t *testing.T) {
	m := mouseModel()
	m.setError(fmt.Errorf("boom"))
	x, y := at(t, m, "boom")
	click(m, x, y)
	if m.overlay != overlayError {
		t.Fatal("a click inside the error closed it")
	}
	wheel(m, x, y, tea.MouseButtonWheelDown)
	if m.appCur != 0 {
		t.Fatal("the wheel reached the list under the error")
	}
	click(m, 0, m.height-1)
	if m.overlay != overlayNone {
		t.Fatal("a click outside did not dismiss the error")
	}
}

func pickerModel() *Model {
	m := mouseModel()
	m.curApp = m.apps[1] // Bravo has nobody: every user is a candidate
	m.assignmentBack = screenApps
	m.screen = screenAssignments
	m.loadAssignments()
	key(m, "a")
	return m
}

func TestPickerRowClickMovesTheCursorOnly(t *testing.T) {
	m := pickerModel()
	if m.overlay != overlayPickUser {
		t.Fatalf("setup: overlay=%v", m.overlay)
	}
	clickText(t, m, "cyd@example.com")
	if m.pick.cur != 2 || m.overlay != overlayPickUser || len(m.pick.chosen) != 0 {
		t.Fatalf("cur=%d overlay=%v chosen=%v", m.pick.cur, m.overlay, m.pick.chosen)
	}
}

func TestPickerSecondClickCommitsToTheConfirmModalOnly(t *testing.T) {
	m := pickerModel()
	x, y := at(t, m, "dee@example.com")
	click(m, x, y)
	click(m, x, y)
	if m.overlay != overlayConfirm || len(m.pending) != 1 || m.pending[0].id != "user-dee" {
		t.Fatalf("overlay=%v pending=%v, want a confirm for dee", m.overlay, m.pending)
	}
	if m.busyKind != busyNone {
		t.Fatal("the picker's second click applied the change")
	}
}

// The picker shares the detector, so a key between two clicks on a picker
// row breaks the pair too: down, up back to the row, click again only
// selects, and nothing reaches the confirm modal.
func TestPickerKeyBetweenClicksBreaksTheDoubleClick(t *testing.T) {
	m := pickerModel()
	x, y := at(t, m, "cyd@example.com")
	click(m, x, y)
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.pick.cur != 2 {
		t.Fatalf("cur=%d after down, up; want back on cyd (2)", m.pick.cur)
	}
	click(m, x, y)
	if m.overlay != overlayPickUser || len(m.pending) != 0 {
		t.Fatalf("overlay=%v pending=%v: a key between the clicks must break the double click", m.overlay, m.pending)
	}
	if m.pick.cur != 2 {
		t.Fatalf("cur=%d, want cyd (2) still selected", m.pick.cur)
	}
}

func TestPickerRightClickTogglesTheRow(t *testing.T) {
	m := pickerModel()
	x, y := at(t, m, "bob@example.com")
	press(m, x, y, tea.MouseButtonRight)
	if !m.pick.chosen["user-bob"] || m.pick.cur != 1 {
		t.Fatalf("chosen=%v cur=%d", m.pick.chosen, m.pick.cur)
	}
	press(m, x, y, tea.MouseButtonRight)
	if len(m.pick.chosen) != 0 {
		t.Fatalf("a second right click did not untoggle: %v", m.pick.chosen)
	}
}

func TestPickerWheelMovesTheCursor(t *testing.T) {
	m := pickerModel()
	x, y := at(t, m, "ann@example.com")
	wheel(m, x, y, tea.MouseButtonWheelDown)
	if m.pick.cur != 3 {
		t.Fatalf("cur=%d, want 3", m.pick.cur)
	}
	wheel(m, x, y, tea.MouseButtonWheelUp)
	if m.pick.cur != 0 {
		t.Fatalf("cur=%d, want 0", m.pick.cur)
	}
}

func TestPickerOwnsTheMouse(t *testing.T) {
	m := pickerModel()
	x, y := at(t, m, "assign users to")
	click(m, x, y) // the title: inside the box, not a row
	if m.overlay != overlayPickUser || m.pick.cur != 0 {
		t.Fatal("a click on the title changed the picker")
	}
	if m.asgCur != 0 {
		t.Fatal("a click reached the list under the picker")
	}
	click(m, 0, m.height-1)
	if m.overlay != overlayNone {
		t.Fatal("a click outside did not close the picker")
	}
}

func TestPaletteRowsAndOutsideClick(t *testing.T) {
	m := mouseModel()
	key(m, ":")
	if m.overlay != overlayPalette {
		t.Fatal("setup: : opens the palette")
	}
	x, y := at(t, m, "open Groups")
	click(m, x, y)
	if m.overlay != overlayPalette || m.pal.shown[m.pal.cur].Action != "view.groups" {
		t.Fatalf("overlay=%v cur=%d: a click should only move the cursor", m.overlay, m.pal.cur)
	}
	click(m, x, y)
	if m.overlay != overlayNone || m.screen != screenGroups {
		t.Fatalf("overlay=%v screen=%v: the second click runs the row", m.overlay, m.screen)
	}

	key(m, ":")
	wheel(m, 5, 5, tea.MouseButtonWheelDown)
	if m.pal.cur != 3 {
		t.Fatalf("palette cur=%d, want 3", m.pal.cur)
	}
	click(m, 0, m.height-1)
	if m.overlay != overlayNone {
		t.Fatal("a click outside did not close the palette")
	}
}
