package tui

import (
	"errors"
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// selModel is mouseModel with a selection style that emits SGR, so the
// reverse-video highlight is visible in View() and can be located.
func selModel() *Model {
	m := mouseModel()
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.ANSI)
	m.st.selection = r.NewStyle().Reverse(true)
	return m
}

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

func copiedText(m *Model) string { return m.textSel.Text(m.selPanes(), m.View()) }

// span is one run of reverse-video cells in a rendered frame.
type span struct {
	y, x0, x1 int
	text      string
}

func highlighted(view string) []span {
	const on, off = "\x1b[7m", "\x1b[0m"
	var out []span
	for y, line := range strings.Split(view, "\n") {
		rest, base := line, 0
		for {
			i := strings.Index(rest, on)
			if i < 0 {
				break
			}
			x0 := base + ansi.StringWidth(ansi.Strip(rest[:i]))
			body := rest[i+len(on):]
			j := strings.Index(body, off)
			if j < 0 {
				j = len(body)
			}
			text := ansi.Strip(body[:j])
			out = append(out, span{y, x0, x0 + ansi.StringWidth(text), text})
			base = x0 + ansi.StringWidth(text)
			rest = body[j:]
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
