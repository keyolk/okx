package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// truncate is applied to whole styled lines. It must cut by display cells and
// keep escape sequences intact, never leaving half an SGR code behind.
func TestTruncateKeepsEscapeSequencesIntact(t *testing.T) {
	styled := "\x1b[31mred text here\x1b[0m and plain"
	got := truncate(styled, 8, "…")
	if w := lipgloss.Width(got); w != 8 {
		t.Fatalf("width = %d, want 8: %q", w, got)
	}
	if ansi.Strip(got) != "red tex…" {
		t.Fatalf("visible text = %q, want %q", ansi.Strip(got), "red tex…")
	}
	// Stripping must leave no ESC: a broken sequence would survive Strip.
	if strings.ContainsRune(ansi.Strip(got), '\x1b') {
		t.Fatalf("truncate split an escape sequence: %q", got)
	}
}

func TestTruncateCountsWideRunes(t *testing.T) {
	got := truncate("가나다라마바사", 7, "…")
	if w := lipgloss.Width(got); w > 7 {
		t.Fatalf("width = %d > 7: %q", w, got)
	}
}

// wrap used to slice bytes, splitting Hangul mid-rune.
func TestWrapDoesNotSplitRunes(t *testing.T) {
	got := wrap("그룹 멤버를 가져오지 못했습니다 다시 시도하세요", 10)
	for _, line := range strings.Split(got, "\n") {
		if !utf8Valid(line) {
			t.Fatalf("wrap produced invalid UTF-8: %q", line)
		}
		if w := lipgloss.Width(line); w > 10 {
			t.Fatalf("line %q is %d cells, want <= 10", line, w)
		}
	}
}

func utf8Valid(s string) bool { return strings.ToValidUTF8(s, "�") == s }
