package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/keyolk/okx/internal/okta"
)

// isQuit reports whether a returned command is tea.Quit, by running it and
// checking for the QuitMsg. tea.Quit is a plain func, so it cannot be compared
// directly.
func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestNormalizeCJKKeyMapsJamoByPhysicalPosition(t *testing.T) {
	for jamo, want := range map[string]string{
		"ㅂ": "q", "ㅁ": "a", "ㅋ": "z", "ㅓ": "j", "ㅏ": "k",
		"ㅃ": "Q", "ㄲ": "R",
	} {
		if got := normalizeCJKKey(keyMsg(jamo)).String(); got != want {
			t.Errorf("normalizeCJKKey(%q) = %q, want %q", jamo, got, want)
		}
	}
}

func TestNormalizeCJKKeyLeavesEverythingElseAlone(t *testing.T) {
	// Latin keys, digits, and composed syllables pass through: a composed
	// syllable only reaches the TUI as committed text, never as a shortcut.
	for _, k := range []string{"q", "R", "0", "가"} {
		if got := normalizeCJKKey(keyMsg(k)).String(); got != k {
			t.Errorf("normalizeCJKKey(%q) = %q, want it unchanged", k, got)
		}
	}
	for _, in := range []tea.KeyMsg{{Type: tea.KeyEnter}, {Type: tea.KeyCtrlC}, {Type: tea.KeyEsc}} {
		if got := normalizeCJKKey(in); got.String() != in.String() {
			t.Errorf("normalizeCJKKey(%q) = %q, want it unchanged", in.String(), got.String())
		}
	}
}

func TestNormalizeCJKKeyLeavesPasteAndAltAlone(t *testing.T) {
	paste := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'ㅂ'}, Paste: true}
	if got := normalizeCJKKey(paste); got.String() != paste.String() {
		t.Errorf("pasted jamo was rewritten to %q", got.String())
	}
	alt := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'ㅂ'}, Alt: true}
	if got := normalizeCJKKey(alt); got.String() != alt.String() {
		t.Errorf("alt chord was rewritten to %q", got.String())
	}
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
	if !isQuit(cmd) {
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
		if m.pick.query != "ㅁ" {
			t.Fatalf("picker query = %q, want the jamo verbatim", m.pick.query)
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
			if !isQuit(cmd) {
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
	if isQuit(cmd) {
		t.Fatal("esc quit the program; it should only close the picker")
	}
	if m.overlay != overlayNone {
		t.Fatal("esc did not close the picker")
	}
}
