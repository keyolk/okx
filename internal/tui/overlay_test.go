package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// A modal is drawn over the screen it belongs to. It used to be placed on a
// blank canvas, hiding the list the user was acting on.
func TestOverlayKeepsBaseVisible(t *testing.T) {
	model := topLevelTestModel()
	base := model.View()
	header := strings.SplitN(base, "\n", 2)[0]

	model.setError(errors.New("boom"))
	view := model.View()

	if !strings.Contains(view, "boom") {
		t.Fatalf("overlay box missing:\n%s", view)
	}
	if got := strings.SplitN(view, "\n", 2)[0]; got != header {
		t.Errorf("header row was replaced:\n got %q\nwant %q", got, header)
	}
	lines := strings.Split(view, "\n")
	if len(lines) != model.height {
		t.Errorf("view has %d rows, want %d", len(lines), model.height)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w > model.width {
			t.Errorf("row %d is %d cells wide, over %d", i, w, model.width)
		}
	}
}

func TestSpliceCenterPreservesSurroundingCells(t *testing.T) {
	base := strings.Join([]string{"aaaaaaaaaa", "bbbbbbbbbb", "cccccccccc"}, "\n")
	got := spliceCenter(base, "XX", 10, 3)
	lines := strings.Split(got, "\n")
	if lines[0] != "aaaaaaaaaa" || lines[2] != "cccccccccc" {
		t.Fatalf("rows outside the box changed: %q", lines)
	}
	if mid := stripReset(lines[1]); mid != "bbbbXXbbbb" {
		t.Fatalf("middle row = %q, want %q", mid, "bbbbXXbbbb")
	}
}

func stripReset(s string) string { return strings.ReplaceAll(s, "\x1b[m", "") }
