package tui

import (
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/keyolk/tuikit/link"
	"github.com/keyolk/tuikit/tuitest"
)

const testAdmin = "https://example-admin.okta.com"

// The URLs topLevelTestModel's objects must link to, spelled out rather than
// built with the code under test.
const (
	wantAppURL   = testAdmin + "/admin/app/alpha/instance/app-1/"
	wantGroupURL = testAdmin + "/admin/group/group-1"
	wantUserURL  = testAdmin + "/admin/user/profile/view/user-1"
)

func adminModel() *Model {
	m := topLevelTestModel()
	m.app.Cfg.AdminURL = testAdmin
	return m
}

// stubLauncher records what Open would have handed the browser. It runs off
// the test goroutine (link.Launcher's contract), hence the mutex.
func stubLauncher(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var got []string
	old := link.Launcher
	link.Launcher = func(u string) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, u)
		return nil
	}
	t.Cleanup(func() { link.Launcher = old })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), got...)
	}
}

// hasLink reports whether the frame links exactly text (no padding, no
// marker) to exactly u.
func hasLink(frame, u, text string) bool {
	for _, l := range tuitest.Links(frame) {
		if l.URL == u && l.Text == text {
			return true
		}
	}
	return false
}

func requireLink(t *testing.T, m *Model, where, u, text string) {
	t.Helper()
	if f := m.View(); !hasLink(f, u, text) {
		t.Fatalf("%s: no link %q -> %s; links: %+v\n%s", where, text, u, tuitest.Links(f), ansi.Strip(f))
	}
}

// Every screen that names a user, group or app links the name, and the
// drilled-in screen's title links what the screen is about.
func TestNamesLinkToTheAdminConsole(t *testing.T) {
	m := adminModel()
	requireLink(t, m, "apps list", wantAppURL, "Alpha Console")

	key(m, "enter") // the app's user assignments
	requireLink(t, m, "assignments title", wantAppURL, "Alpha Console")
	requireLink(t, m, "assigned user", wantUserURL, "gavin@example.com")

	key(m, "tab") // the app's groups
	requireLink(t, m, "assigned group", wantGroupURL, "Platform Admins")

	key(m, "enter") // the group's members
	requireLink(t, m, "members title", wantGroupURL, "Platform Admins")
	requireLink(t, m, "member", wantUserURL, "gavin@example.com")

	key(m, "tab") // the apps the group grants
	requireLink(t, m, "group apps title", wantGroupURL, "Platform Admins")
	requireLink(t, m, "group app", wantAppURL, "Alpha Console")

	key(m, "tab")
	key(m, "enter") // the member's apps
	if m.screen != screenUserApps {
		t.Fatalf("screen = %v, want user apps", m.screen)
	}
	requireLink(t, m, "user apps title", wantUserURL, "gavin@example.com")
	requireLink(t, m, "user app", wantAppURL, "Alpha Console")

	key(m, "2")
	requireLink(t, m, "groups list", wantGroupURL, "Platform Admins")
	key(m, "3")
	requireLink(t, m, "users list", wantUserURL, "gavin@example.com")
}

// The raw escape sits right around the label, and the selected row (which
// lipgloss restyles) keeps it too.
func TestLinkEscapeHugsTheLabel(t *testing.T) {
	m := adminModel()
	want := "\x1b]8;;" + wantAppURL + "\x1b\\"
	f := m.View()
	i := strings.Index(f, want)
	if i < 0 {
		t.Fatalf("no OSC 8 for %s in the frame", wantAppURL)
	}
	if rest := ansi.Strip(f[i+len(want):]); !strings.HasPrefix(rest, "Alpha Console") {
		t.Fatalf("link opens on %q, want the label", rest[:min(len(rest), 20)])
	}
}

// `o` hands the browser exactly the selected item's admin URL, on every list.
func TestOOpensTheSelectedAdminPage(t *testing.T) {
	launched := stubLauncher(t)
	m := adminModel()
	steps := []struct {
		keys []string
		want string
	}{
		{nil, wantAppURL},
		{[]string{"enter"}, wantUserURL},       // assignments, users tab
		{[]string{"tab"}, wantGroupURL},        // assignments, groups tab
		{[]string{"enter"}, wantUserURL},       // group members
		{[]string{"tab"}, wantAppURL},          // group apps
		{[]string{"tab", "enter"}, wantAppURL}, // user apps
		{[]string{"2"}, wantGroupURL},          // groups list
		{[]string{"3"}, wantUserURL},           // users list
	}
	for i, s := range steps {
		for _, k := range s.keys {
			key(m, k)
		}
		_, cmd := m.handleKey(keyMsg("o"))
		if cmd == nil {
			t.Fatalf("step %d (%v): o returned no command", i, s.keys)
		}
		msg, ok := cmd().(link.OpenedMsg)
		if !ok || msg.URL != s.want || msg.Err != nil {
			t.Fatalf("step %d (%v): o -> %+v, want %s", i, s.keys, msg, s.want)
		}
		if got := launched(); len(got) != i+1 || got[i] != s.want {
			t.Fatalf("step %d: launcher got %v, want last %s", i, got, s.want)
		}
		m.Update(msg)
		if !onScreen(m, "opened "+s.want) {
			t.Fatalf("step %d: no 'opened' toast:\n%s", i, ansi.Strip(m.View()))
		}
	}
}

// A custom domain without adminUrl: nothing is linked anywhere, and `o`
// launches nothing but says which setting fixes it.
func TestNoAdminURLMeansNoLinksAndOSaysWhy(t *testing.T) {
	launched := stubLauncher(t)
	m := topLevelTestModel() // AdminURL empty
	for _, k := range []string{"", "enter", "tab", "enter", "tab", "2", "3"} {
		if k != "" {
			key(m, k)
		}
		if f := m.View(); strings.Contains(f, "\x1b]8;") {
			t.Fatalf("after %q: OSC 8 without an admin URL: %+v", k, tuitest.Links(f))
		}
	}
	_, cmd := m.handleKey(keyMsg("o"))
	if cmd != nil {
		cmd()
	}
	if got := launched(); len(got) != 0 {
		t.Fatalf("launcher ran with %v", got)
	}
	if !onScreen(m, "okta.client.adminUrl") {
		t.Fatalf("no toast naming okta.client.adminUrl:\n%s", ansi.Strip(m.View()))
	}
}

// An app without a name has no admin page address: its row is not linked and
// `o` on it opens nothing and says so.
func TestItemWithoutURLIsNotLinkedOrOpened(t *testing.T) {
	launched := stubLauncher(t)
	m := adminModel()
	m.app.Index.Apps[0].Name = ""
	m.reloadResources()
	if links := tuitest.Links(m.View()); len(links) != 0 {
		t.Fatalf("unnamed app is linked: %+v", links)
	}
	_, cmd := m.handleKey(keyMsg("o"))
	if cmd != nil {
		t.Fatalf("o on a URL-less app returned a command (%+v)", cmd())
	}
	if got := launched(); len(got) != 0 {
		t.Fatalf("launcher ran with %v", got)
	}
	if !onScreen(m, "no link here") {
		t.Fatalf("no toast:\n%s", ansi.Strip(m.View()))
	}
}

// adminUrl is validated at load, but a model built some other way must still
// never link or launch a javascript: or file: URL.
func TestNonHTTPAdminURLIsNeverLinkedOrLaunched(t *testing.T) {
	for _, bad := range []string{"javascript:alert(1)", "file:///etc"} {
		launched := stubLauncher(t)
		m := adminModel()
		m.app.Cfg.AdminURL = bad
		if f := m.View(); strings.Contains(f, "\x1b]8;") {
			t.Fatalf("%s: linked: %+v", bad, tuitest.Links(f))
		}
		_, cmd := m.handleKey(keyMsg("o"))
		if cmd != nil {
			if msg, ok := cmd().(link.OpenedMsg); ok && !msg.Refused() {
				t.Fatalf("%s: not refused: %+v", bad, msg)
			}
		}
		if got := launched(); len(got) != 0 {
			t.Fatalf("%s: launcher ran with %v", bad, got)
		}
	}
}

// The footer, help page and palette read the binding table, so `o` shows up
// in all three.
func TestOpenLinkIsInFooterHelpAndPalette(t *testing.T) {
	m := adminModel()
	m.width = 140
	if !onScreen(m, "o admin") {
		t.Fatalf("footer does not offer o:\n%s", ansi.Strip(m.View()))
	}
	key(m, "?")
	if !onScreen(m, "admin console") {
		t.Fatalf("help does not list o:\n%s", ansi.Strip(m.View()))
	}
	key(m, "esc")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	found := false
	for _, e := range m.pal.all {
		if e.Action == link.Action {
			found = true
		}
	}
	if !found {
		t.Fatal("palette has no link.open entry")
	}
}
