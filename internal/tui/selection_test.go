package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/keyolk/tuikit/keys"
	"github.com/keyolk/tuikit/selection"
	"github.com/keyolk/tuikit/tuitest"
)

// selModel is mouseModel. selection.DefaultStyle draws reverse video on a
// renderer that keeps it whatever profile the test process detects, so the
// highlight shows in View() without a test-only style.
func selModel() *Model { return mouseModel() }

func mouseAt(m *Model, x, y int, b tea.MouseButton, a tea.MouseAction) tea.Cmd {
	_, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Button: b, Action: a})
	return cmd
}

// drag presses at (x0,y0), moves to (x1,y1) and releases there, returning
// the release's command.
func drag(m *Model, x0, y0, x1, y1 int) tea.Cmd {
	mouseAt(m, x0, y0, tea.MouseButtonLeft, tea.MouseActionPress)
	mouseAt(m, x1, y1, tea.MouseButtonLeft, tea.MouseActionMotion)
	return mouseAt(m, x1, y1, tea.MouseButtonNone, tea.MouseActionRelease)
}

func copiedText(m *Model) string { return m.textSel.Text(m.selPanes(), m.frame()) }

// span is one run of reverse-video cells in a rendered frame, located by
// finding the run's text on its stripped line.
type span struct {
	y, x0, x1 int
	text      string
}

func highlighted(view string) []span {
	var out []span
	for y, line := range strings.Split(view, "\n") {
		plain := ansi.Strip(line)
		from := 0
		for _, run := range tuitest.Reversed(line) {
			i := strings.Index(plain[from:], run)
			if i < 0 {
				continue
			}
			x0 := ansi.StringWidth(plain[:from+i])
			out = append(out, span{y, x0, x0 + ansi.StringWidth(run), run})
			from += i + len(run)
		}
	}
	return out
}

func TestDragStaysInsideTheListBody(t *testing.T) {
	m := selModel()
	x, y := at(t, m, "Charlie Console")
	fx, fy := at(t, m, "refresh") // the footer, below the list body
	_, hy := at(t, m, "APP")      // the column header: the body's first row
	if cmd := drag(m, x, y, fx+3, fy); cmd == nil {
		t.Fatal("a finished drag must return the copy command")
	}
	got := copiedText(m)
	if !strings.HasPrefix(got, "Charlie Console") || !strings.Contains(got, "Echo Console") {
		t.Fatalf("copy %q should run from Charlie through Echo", got)
	}
	if strings.Contains(got, "refresh") || strings.Contains(got, "views") {
		t.Fatalf("copy %q leaked the footer", got)
	}
	spans := highlighted(m.View())
	if len(spans) == 0 {
		t.Fatal("the selection is not drawn")
	}
	for _, s := range spans {
		if s.y >= fy-1 || s.y < hy {
			t.Fatalf("highlight %+v left the list body (rows %d..%d)", s, hy, fy-2)
		}
	}
}

func TestDragUpStopsAtTheColumnHeader(t *testing.T) {
	m := selModel()
	x, y := at(t, m, "Bravo Console")
	drag(m, x, y, 0, 0) // onto the tabs
	got := copiedText(m)
	if strings.Contains(got, "okx") || strings.Contains(got, "Groups") || strings.Contains(got, "─") {
		t.Fatalf("copy %q leaked the header", got)
	}
	if !strings.HasPrefix(got, "APP") {
		t.Fatalf("copy %q should start at the column header", got)
	}
}

func TestPressWithoutMotionIsStillAClick(t *testing.T) {
	m := selModel()
	x, y := at(t, m, "Charlie Console")
	mouseAt(m, x, y, tea.MouseButtonLeft, tea.MouseActionPress)
	mouseAt(m, x, y, tea.MouseButtonNone, tea.MouseActionRelease)
	if m.appCur != 2 {
		t.Fatalf("appCur=%d, want Charlie (2): a still press is the app's click", m.appCur)
	}
	if m.textSel.Active() || len(highlighted(m.View())) != 0 {
		t.Fatal("a click without motion must not leave a selection")
	}
}

func TestSecondClickOnARowIsStillOpenNotAWordPick(t *testing.T) {
	m := selModel()
	x, y := at(t, m, "Alpha Console")
	mouseAt(m, x, y, tea.MouseButtonLeft, tea.MouseActionPress)
	mouseAt(m, x, y, tea.MouseButtonNone, tea.MouseActionRelease)
	mouseAt(m, x, y, tea.MouseButtonLeft, tea.MouseActionPress)
	if m.screen != screenAssignments {
		t.Fatalf("screen=%v: a list keeps second click = list.open", m.screen)
	}
	if m.textSel.Active() {
		t.Fatal("a list pane must not pick a word on a double click")
	}
}

func TestKeyClearsTheSelection(t *testing.T) {
	m := selModel()
	x, y := at(t, m, "Bravo Console")
	drag(m, x, y, x+5, y)
	if !m.textSel.Active() {
		t.Fatal("setup: the drag should leave a selection")
	}
	m.Update(keyMsg("j"))
	if m.textSel.Active() || len(highlighted(m.View())) != 0 {
		t.Fatal("a key must clear the selection")
	}
}

func TestDragPastTheHelpBottomScrollsAndKeepsScrolledOutLines(t *testing.T) {
	m := selModel()
	m.height = 16 // the help is longer than the page
	key(m, "?")
	x, y := at(t, m, "navigation")
	_, marker := at(t, m, "more")
	shown := strings.Count(ansi.Strip(m.View()), "\n")
	mouseAt(m, x, y, tea.MouseButtonLeft, tea.MouseActionPress)
	for range 5 {
		mouseAt(m, x+60, marker, tea.MouseButtonLeft, tea.MouseActionMotion)
	}
	if m.helpOff != 5 {
		t.Fatalf("helpOff=%d after five drags onto the marker, want 5", m.helpOff)
	}
	if cmd := mouseAt(m, x+60, marker, tea.MouseButtonNone, tea.MouseActionRelease); cmd == nil {
		t.Fatal("release must return the copy command")
	}
	got := copiedText(m)
	if !strings.HasPrefix(got, "navigation") {
		t.Fatalf("copy %q must keep the scrolled-out first line", got)
	}
	if n := strings.Count(got, "\n") + 1; n <= shown-5 {
		t.Fatalf("copy has %d lines; it must span more than one page", n)
	}
	if strings.Contains(got, "more") {
		t.Fatalf("copy %q includes the overflow marker", got)
	}
}

func TestErrorModalTextIsSelectableInsideItsBorder(t *testing.T) {
	m := selModel()
	m.setError(errors.New("listing users failed: request id 00ab12cd34 timed out"))
	x, y := at(t, m, "listing users")
	drag(m, x+3, y, 0, y) // out through the left border onto the list
	got := copiedText(m)
	if got != "list" {
		t.Fatalf("copy %q, want the modal text up to the press only", got)
	}
	if m.overlay != overlayError {
		t.Fatal("a drag inside the modal must not dismiss it")
	}
	for _, s := range highlighted(m.View()) {
		if s.x0 < x || strings.ContainsAny(s.text, "│╭╰") {
			t.Fatalf("highlight %+v left the modal content", s)
		}
	}
}

func TestDoubleClickInTheErrorModalPicksAWord(t *testing.T) {
	m := selModel()
	m.setError(errors.New("listing users failed: request id 00ab12cd34 timed out"))
	x, y := at(t, m, "00ab12cd34")
	mouseAt(m, x+2, y, tea.MouseButtonLeft, tea.MouseActionPress)
	mouseAt(m, x+2, y, tea.MouseButtonNone, tea.MouseActionRelease)
	if cmd := mouseAt(m, x+2, y, tea.MouseButtonLeft, tea.MouseActionPress); cmd == nil {
		t.Fatal("a word pick copies")
	}
	if got := copiedText(m); got != "00ab12cd34" {
		t.Fatalf("word pick = %q", got)
	}
}

func TestListUnderAModalIsNotSelectable(t *testing.T) {
	m := selModel()
	x, y := at(t, m, "Echo Console") // left of where the modal lands
	m.setError(errors.New("boom"))
	drag(m, x, y, x+8, y+1)
	if m.textSel.Active() || len(highlighted(m.View())) != 0 {
		t.Fatal("the list under a modal must not be selectable")
	}
}

// A resize can leave helpOff past the end of the page (RenderHelp clamps it
// when drawing). A drag past the top must still scroll up one line from what
// is on screen: the app sets the offset the selection reports, it does not
// add the scroll to its own stale value.
func TestDragPastTheHelpTopScrollsFromAStaleOffset(t *testing.T) {
	m := selModel()
	m.height = 16 // the help is longer than the page
	key(m, "?")
	m.helpOff = 1 << 10 // past the end, as a taller terminal leaves it
	_, drawn := keysRenderedOffset(m)
	if drawn == 0 {
		t.Fatal("setup: help must be longer than the page")
	}
	first := strings.Split(ansi.Strip(m.View()), "\n")[headerLines]
	x, y := at(t, m, strings.TrimSpace(first))
	mouseAt(m, x, y, tea.MouseButtonLeft, tea.MouseActionPress)
	mouseAt(m, x, 0, tea.MouseButtonLeft, tea.MouseActionMotion) // onto the tabs, above the page
	if m.helpOff != drawn-1 {
		t.Fatalf("helpOff=%d after a drag past the top, want %d (one above the drawn page)", m.helpOff, drawn-1)
	}
	if _, now := keysRenderedOffset(m); now != drawn-1 {
		t.Fatalf("the page did not move: drawn offset %d, want %d", now, drawn-1)
	}
}

// keysRenderedOffset is the page and offset renderHelp draws with
// (RenderHelp clamps helpOff).
func keysRenderedOffset(m *Model) ([]string, int) {
	return keys.RenderHelp(m.helpSections(), m.width, m.helpRows(), m.helpOff, keys.HelpStyle{})
}

// A drag resting on the "… more" marker keeps scrolling through the
// selection's auto-scroll ticks; the terminal sends no motion meanwhile.
func TestDragRestingPastTheHelpBottomAutoScrolls(t *testing.T) {
	m := selModel()
	m.height = 16
	key(m, "?")
	x, y := at(t, m, "navigation")
	_, marker := at(t, m, "more")
	mouseAt(m, x, y, tea.MouseButtonLeft, tea.MouseActionPress)
	cmd := mouseAt(m, x+60, marker, tea.MouseButtonLeft, tea.MouseActionMotion)
	if m.helpOff != 1 {
		t.Fatalf("helpOff=%d after one drag onto the marker, want 1", m.helpOff)
	}
	for want := 2; want <= 3; want++ {
		var tick tea.Msg
		for _, msg := range tuitest.Drain(cmd, 2*selection.AutoScrollInterval) {
			if _, ok := msg.(selection.AutoScrollMsg); ok {
				tick = msg
			}
		}
		if tick == nil {
			t.Fatalf("no auto-scroll tick while the drag rests past the edge (want offset %d)", want)
		}
		_, cmd = m.Update(tick)
		if m.helpOff != want {
			t.Fatalf("helpOff=%d after an auto-scroll tick, want %d", m.helpOff, want)
		}
	}
}

// NO_COLOR asks for no color; reverse video is not a color, and without it a
// selection would be invisible.
func TestSelectionStaysVisibleUnderNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := selModel()
	x, y := at(t, m, "Bravo Console")
	drag(m, x, y, x+4, y)
	if got := tuitest.Reversed(m.View()); len(got) != 1 || got[0] != "Bravo" {
		t.Fatalf("reverse runs under NO_COLOR = %q, want [Bravo]", got)
	}
}

// The copy reaches the clipboard through selection.Clipboard and comes back
// as the toast text the kit words.
func TestReleaseCopiesThroughTheClipboardSeam(t *testing.T) {
	var got []string
	old := selection.Clipboard
	selection.Clipboard = func(s string) error { got = append(got, s); return nil }
	t.Cleanup(func() { selection.Clipboard = old })
	m := selModel()
	x, y := at(t, m, "Bravo Console")
	for _, msg := range tuitest.Drain(drag(m, x, y, x+4, y), 0) {
		m.Update(msg)
	}
	if len(got) != 1 || got[0] != "Bravo" {
		t.Fatalf("clipboard got %q, want [Bravo]", got)
	}
	if footer := ansi.Strip(m.renderFooter()); !strings.Contains(footer, "copied 5 chars") {
		t.Fatalf("footer %q, want the copy note", footer)
	}
}
