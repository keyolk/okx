package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/keyolk/tuikit/prefs"

	"github.com/keyolk/okx/internal/okta"
)

// isQuit reports whether a command ends the program. Quit flushes the UI
// state first and exits on the final prefs.Saved, so follow that one hop
// through the model, as Bubble Tea would.
func isQuit(m *Model, cmd tea.Cmd) bool {
	for range 2 {
		if cmd == nil {
			return false
		}
		msg := cmd()
		if _, ok := msg.(tea.QuitMsg); ok {
			return true
		}
		if _, ok := msg.(prefs.Saved); !ok {
			return false
		}
		// Tests call handleKey directly, which leaves the save ticks Update
		// would have batched; drop them so Update returns the quit alone.
		m.queued = nil
		_, cmd = m.Update(msg)
	}
	return false
}

// --- through the real dispatcher --------------------------------------------

// Under a Korean input source the navigation keys arrive as jamo. They have to
// keep working, or the list is unusable until the input source is switched.
func TestHangulNavigatesTheListLikeLatin(t *testing.T) {
	m := topLevelTestModel()
	m.screen = screenApps
	// The shared fixture has a single app, so j/k would have nowhere to go.
	m.apps = append(m.apps, okta.App{ID: "app-2", Label: "Beta Console", Name: "beta"})
	m.appCur = 0

	// `ㅓ` is the physical `j` key.
	m.handleKey(keyMsg("ㅓ"))
	if m.appCur != 1 {
		t.Fatalf("appCur = %d after ㅓ; want 1", m.appCur)
	}
	// `ㅏ` is the physical `k` key.
	m.handleKey(keyMsg("ㅏ"))
	if m.appCur != 0 {
		t.Fatalf("appCur = %d after ㅏ; want 0", m.appCur)
	}
}

func TestHangulSwitchesScreensLikeLatin(t *testing.T) {
	m := topLevelTestModel()
	// `ㅅ` is the physical `t`… unbound. `ㅣ` is the physical `l`, which
	// descends into the highlighted app.
	m.screen = screenApps
	m.handleKey(keyMsg("ㅣ"))
	if m.screen == screenApps {
		t.Fatal("ㅣ (physical l) did not descend out of the apps list")
	}
}

func TestHangulQuitsLikeLatinQ(t *testing.T) {
	m := topLevelTestModel()
	m.screen = screenApps // top level: q quits rather than backing out
	_, cmd := m.handleKey(keyMsg("ㅂ"))
	if !isQuit(m, cmd) {
		t.Fatal("ㅂ (physical q) did not quit")
	}
}

// A jamo typed into a text field is the intended input, so normalization must
// not reach it — a Korean group name would be unsearchable.
func TestTextInputsKeepHangulVerbatim(t *testing.T) {
	t.Run("filter", func(t *testing.T) {
		m := topLevelTestModel()
		m.screen = screenApps
		m.filtering = true
		m.appFiltr = ""
		m.handleKey(keyMsg("ㅂ"))
		if m.appFiltr != "ㅂ" {
			t.Fatalf("filter = %q, want the jamo verbatim", m.appFiltr)
		}
	})
	t.Run("picker query", func(t *testing.T) {
		m := topLevelTestModel()
		m.overlay = overlayPickGroup
		m.pick = pickerState{chosen: map[string]bool{}}
		m.handleKey(keyMsg("ㅁ"))
		if m.pick.query.Value() != "ㅁ" {
			t.Fatalf("picker query = %q, want the jamo verbatim", m.pick.query.Value())
		}
	})
}

// --- ctrl+c ------------------------------------------------------------------

// ctrl+c was bound only in the picker and on the top level, so whether it
// worked depended on which screen you were on. It now quits from everywhere.
func TestCtrlCQuitsFromEveryState(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*Model)
	}{
		{"top level", func(m *Model) { m.screen = screenApps }},
		{"drilled in", func(m *Model) { m.screen = screenAssignments }},
		{"filtering", func(m *Model) { m.filtering = true }},
		{"error overlay", func(m *Model) { m.overlay = overlayError; m.errText = "boom" }},
		{"confirm overlay", func(m *Model) { m.overlay = overlayConfirm }},
		{"picker", func(m *Model) { m.overlay = overlayPickGroup }},
		{"mid-apply", func(m *Model) { m.busyKind = busyApply }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := topLevelTestModel()
			tc.setup(m)
			_, cmd := m.handleKey(keyMsg("ctrl+c"))
			if !isQuit(m, cmd) {
				t.Fatalf("ctrl+c did not quit from %s", tc.name)
			}
		})
	}
}

// esc must still back out rather than quit, so the two exits stay distinct.
func TestEscStillBacksOutWithoutQuitting(t *testing.T) {
	m := topLevelTestModel()
	m.overlay = overlayPickGroup
	_, cmd := m.handleKey(keyMsg("esc"))
	if isQuit(m, cmd) {
		t.Fatal("esc quit the program; it should only close the picker")
	}
	if m.overlay != overlayNone {
		t.Fatal("esc did not close the picker")
	}
}

// Text fields are input.Field: the cursor moves, typing inserts at it, and a
// paste arrives whole. The hand-written field only appended and chopped.
func TestTextFieldsEditAtTheCursor(t *testing.T) {
	m := topLevelTestModel()
	m.screen = screenApps
	m.handleKey(keyMsg("/"))
	for _, k := range []string{"a", "c", "left", "b"} {
		m.handleKey(keyMsgFor(t, k))
	}
	if m.appFiltr != "abc" {
		t.Fatalf("filter after a c ← b = %q, want abc", m.appFiltr)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x\ny"), Paste: true})
	if m.appFiltr != "abx yc" {
		t.Fatalf("filter after a paste = %q, want abx yc", m.appFiltr)
	}
	m.handleKey(keyMsgFor(t, "backspace"))
	if m.appFiltr != "abx c" {
		t.Fatalf("backspace = %q, want abx c (delete before the cursor)", m.appFiltr)
	}
}
