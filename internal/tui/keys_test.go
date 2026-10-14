package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/keyolk/tuikit/keys"
	"github.com/keyolk/tuikit/prefs"

	"github.com/keyolk/okx/internal/cache"
	"github.com/keyolk/okx/internal/okta"
)

// keyMsgFor turns a binding's key string back into the message Bubble Tea
// would deliver, by enumerating the key types tea knows.
var namedKeys = func() map[string]tea.KeyMsg {
	out := map[string]tea.KeyMsg{}
	for t := tea.KeyType(-200); t < 200; t++ {
		if t == tea.KeyRunes {
			continue
		}
		k := tea.KeyMsg{Type: t}
		if s := k.String(); s != "" && !strings.HasPrefix(s, "KeyType(") {
			if _, dup := out[s]; !dup {
				out[s] = k
			}
		}
	}
	return out
}()

func keyMsgFor(t *testing.T, s string) tea.KeyMsg {
	t.Helper()
	if k, ok := namedKeys[s]; ok {
		return k
	}
	if r := []rune(s); len(r) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: r}
	}
	t.Fatalf("no tea.KeyMsg spells %q", s)
	return tea.KeyMsg{}
}

// The table has no duplicate keys, no key hiding a pinned quit/help, and no
// action a Korean input source cannot reach. Conflicts judges reachability
// per action, so G (with end) passes; A is declared LatinOnly.
func TestKeymapHasNoConflicts(t *testing.T) {
	for _, c := range defaultKeymap().Conflicts(allStacks...) {
		t.Error(c)
	}
}

// Every action the help page documents can be run by one of its listed keys
// on some screen. Help derived from the table cannot drift from dispatch, but
// a key shadowed everywhere would still be listed as a lie.
func TestEveryHelpActionIsReachable(t *testing.T) {
	km := defaultKeymap()
	for _, sec := range km.Help(keys.Opts{}, allStacks...) {
		for _, e := range sec.Entries {
			found := false
			for _, stack := range allStacks {
				for _, k := range e.Keys {
					if b, ok := km.Resolve(stack, keyMsgFor(t, k), keys.Opts{}); ok && b.Action == e.Action {
						found = true
					}
				}
			}
			if !found {
				t.Errorf("help lists %q (%v) but no key reaches it", e.Action, e.Keys)
			}
		}
	}
}

// The footer is derived from the table, per screen.
func TestFooterFollowsTheScreen(t *testing.T) {
	model := topLevelTestModel()
	footer := ansi.Strip(model.renderFooter())
	for _, want := range []string{"123 views", "q quit", "? help", "jk move"} {
		if !strings.Contains(footer, want) {
			t.Errorf("top-level footer %q lacks %q", footer, want)
		}
	}

	model.handleKey(keyMsg("enter")) // apps → assignments
	footer = ansi.Strip(model.renderFooter())
	for _, want := range []string{"⇥ groups", "a add user", "esc back"} {
		if !strings.Contains(footer, want) {
			t.Errorf("assignments footer %q lacks %q", footer, want)
		}
	}
	model.handleKey(keyMsg("tab"))
	footer = ansi.Strip(model.renderFooter())
	for _, want := range []string{"⇥ users", "a add group"} {
		if !strings.Contains(footer, want) {
			t.Errorf("assignments groups-tab footer %q lacks %q", footer, want)
		}
	}

	// The user-apps view is a leaf: its old footer offered "enter open",
	// which did nothing.
	model.handleKey(keyMsg("3"))
	model.handleKey(keyMsg("enter"))
	if model.screen != screenUserApps {
		t.Fatalf("setup: screen = %v", model.screen)
	}
	if footer = ansi.Strip(model.renderFooter()); strings.Contains(footer, "↵") {
		t.Errorf("user-apps footer offers enter: %q", footer)
	}

	model.busyKind = busyRefresh
	model.handleKey(keyMsg("esc"))
	if footer = ansi.Strip(model.renderFooter()); !strings.Contains(footer, "refreshing…") {
		t.Errorf("refresh hint does not say it is running: %q", footer)
	}
}

// The movement keys work the same on every list; the user-apps view used to
// lack half-page moves.
func TestHalfPageWorksOnUserApps(t *testing.T) {
	model := topLevelTestModel()
	model.revAccess = make([]cache.UserAccess, 40)
	model.screen = screenUserApps
	model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlD})
	if model.revCur != model.listHeight()/2 {
		t.Fatalf("ctrl+d on user apps: cursor = %d, want %d", model.revCur, model.listHeight()/2)
	}
}

// `a` assigns whatever the tab shows, so assigning groups stays reachable
// under a Korean input source, where A arrives as a.
func TestAssignFollowsTheTab(t *testing.T) {
	model := topLevelTestModel()
	model.handleKey(keyMsg("enter"))
	model.handleKey(keyMsg("tab"))
	model.handleKey(keyMsg("ㅁ")) // physical a
	if model.overlay != overlayPickGroup {
		t.Fatalf("a on the groups tab opened %v, want the group picker", model.overlay)
	}
}

func TestPaletteRunsActionsFromTheTable(t *testing.T) {
	model := topLevelTestModel()
	model.handleKey(keyMsg(":"))
	if model.overlay != overlayPalette {
		t.Fatalf(": opened %v, want the palette", model.overlay)
	}
	if len(model.pal.all) == 0 {
		t.Fatal("palette lists nothing")
	}
	for _, e := range model.pal.all {
		if e.Action == "palette" || e.Action == "app.assign" || e.Action == "list.down" {
			t.Errorf("palette offers %q on the apps list", e.Action)
		}
	}
	for _, r := range "open groups" {
		model.handleKey(keyMsg(string(r)))
	}
	if len(model.pal.shown) == 0 || model.pal.shown[0].Action != "view.groups" {
		t.Fatalf("query ranked %v first", model.pal.shown)
	}
	if view := ansi.Strip(model.View()); !strings.Contains(view, "command palette") {
		t.Fatalf("palette not drawn:\n%s", view)
	}
	model.handleKey(keyMsg("enter"))
	if model.overlay != overlayNone || model.screen != screenGroups {
		t.Fatalf("palette run: overlay=%v screen=%v", model.overlay, model.screen)
	}
}

// Help scrolls, and holding j past the end does not grow the offset.
func TestHelpScrollsAndClamps(t *testing.T) {
	model := topLevelTestModel()
	model.height = minHeight
	model.handleKey(keyMsg("?"))
	if view := ansi.Strip(model.View()); !strings.Contains(view, "more") {
		t.Fatalf("a cut help page does not say so:\n%s", view)
	}
	for range 500 {
		model.handleKey(keyMsg("j"))
	}
	end := model.helpOff
	if end == 0 || end >= 500 {
		t.Fatalf("help offset after holding j = %d", end)
	}
	model.handleKey(keyMsg("k"))
	if model.helpOff != end-1 {
		t.Fatalf("k after the end moved to %d, want %d", model.helpOff, end-1)
	}
	model.handleKey(keyMsg("q"))
	if model.screen != screenApps {
		t.Fatalf("q left help for %v", model.screen)
	}
}

// Lists say how many rows are cut above and below (E7).
func TestListsSayHowManyRowsAreCut(t *testing.T) {
	model := topLevelTestModel()
	for i := range 60 {
		model.apps = append(model.apps, okta.App{ID: "x" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Label: "app"})
	}
	view := ansi.Strip(model.View())
	if !strings.Contains(view, "↓ ") || strings.Contains(view, "↑ ") {
		t.Fatalf("top of a long list should show only ↓ more:\n%s", view)
	}
	model.handleKey(tea.KeyMsg{Type: tea.KeyEnd})
	view = ansi.Strip(model.View())
	if !strings.Contains(view, "↑ ") || strings.Contains(view, "↓ ") {
		t.Fatalf("bottom of a long list should show only ↑ more:\n%s", view)
	}
	if lines := strings.Split(model.View(), "\n"); len(lines) != model.height {
		t.Fatalf("view has %d lines, want %d", len(lines), model.height)
	}
}

func TestTabBadgesShowCounts(t *testing.T) {
	model := membersTestModel()
	header := ansi.Strip(model.renderHeader())
	for _, want := range []string{"1 Apps 1", "2 Groups 2", "3 Users 3"} {
		if !strings.Contains(header, want) {
			t.Errorf("header %q lacks %q", header, want)
		}
	}
	model.groupFiltr = "platform"
	if header = ansi.Strip(model.renderHeader()); !strings.Contains(header, "2 Groups 1/2") {
		t.Errorf("filtered badge missing: %q", header)
	}
}

// Tab, filters and the selected row survive a restart, by ID.
func TestUIStateRoundTrips(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	model := membersTestModel()
	model.handleKey(keyMsg("2"))
	model.handleKey(keyMsg("j")) // Platform Admins
	model.userFiltr = "bob"
	_, cmd := model.handleKey(keyMsg("ctrl+c"))
	if !isQuit(model, cmd) {
		t.Fatal("ctrl+c did not quit")
	}

	data, err := os.ReadFile(prefs.Path(appName))
	if err != nil {
		t.Fatalf("quit did not write ui.json: %v", err)
	}
	var saved uiState
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Tab != "groups" || saved.Sel["groups"] != "group-1" || saved.Filters["users"] != "bob" {
		t.Fatalf("saved state = %+v", saved)
	}

	fresh := membersTestModel()
	fresh.restoreUI(saved)
	if fresh.screen != screenGroups || fresh.userFiltr != "bob" {
		t.Fatalf("restored screen=%v userFiltr=%q", fresh.screen, fresh.userFiltr)
	}
	if got := fresh.filteredTopGroups()[fresh.groupCur].ID; got != "group-1" {
		t.Fatalf("restored selection = %s, want group-1", got)
	}
}

// A refresh that adds a row above the cursor must not move it to another app.
func TestRefreshKeepsTheSelectedApp(t *testing.T) {
	model := topLevelTestModel()
	model.handleKey(keyMsg("enter"))
	model.handleKey(keyMsg("esc"))

	snap := cache.Empty("https://example.okta.com")
	snap.Apps = []okta.App{{ID: "app-0", Label: "Aardvark"}, {ID: "app-1", Label: "Alpha Console"}}
	model.busyKind = busyRefresh
	model.Update(refreshDoneMsg{index: cache.NewIndex(snap)})
	if got := model.filteredApps()[model.appCur].ID; got != "app-1" {
		t.Fatalf("cursor moved to %s after refresh, want app-1", got)
	}
}

// keys.yaml rebinds actions; a bad file keeps the defaults and says why.
func TestUserOverrides(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := keys.ConfigPath(appName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// j moves the list, so help on j would vanish from every list screen.
	if err := os.WriteFile(path, []byte("help: [j]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if km, err := loadKeymap(); err == nil {
		t.Fatal("an override hiding pinned help was accepted")
	} else if b, _ := km.Resolve(stackTop, keyMsg("?"), keys.Opts{}); b.Action != "help" {
		t.Fatalf("a rejected override did not keep the defaults: ? → %q", b.Action)
	}
	if err := os.WriteFile(path, []byte("list.down: [n, down]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	km, err := loadKeymap()
	if err != nil {
		t.Fatal(err)
	}
	model := topLevelTestModel()
	model.km = km
	model.apps = append(model.apps, okta.App{ID: "app-2", Label: "Beta"})
	model.handleKey(keyMsg("n"))
	if model.appCur != 1 {
		t.Fatalf("rebound n did not move: cursor %d", model.appCur)
	}
	if footer := ansi.Strip(model.renderFooter()); !strings.Contains(footer, "nk move") {
		t.Errorf("footer does not show the rebound key: %q", footer)
	}
}

// Prose that names a key ("press ⇥ …", "(/ to edit)") reads it from the
// table, so a keys.yaml rebind does not leave the text naming the old key.
func TestProseNamesTheReboundKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.yaml")
	if err := os.WriteFile(path, []byte("app.tab: [ctrl+t]\nfilter: [f]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	km, err := defaultKeymap().LoadOverrides(path, allStacks...)
	if err != nil {
		t.Fatal(err)
	}
	model := topLevelTestModel()
	model.km = km
	if got := model.keyName(stackAsg, "app.tab"); got != "^t" {
		t.Errorf("app.tab is named %q, want ^t", got)
	}
	model.appFiltr = "alp"
	if footer := ansi.Strip(model.renderFooter()); !strings.Contains(footer, "(f to edit)") {
		t.Errorf("filter hint does not name the rebound key: %q", footer)
	}
}
