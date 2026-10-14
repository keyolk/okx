package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// The truncation marker follows the locale: `…` draws two cells under a CJK
// locale, so every cut line would overflow by one there.
func TestEllipsisFollowsTheLocale(t *testing.T) {
	for _, tc := range []struct{ lang, want string }{
		{"en_US.UTF-8", "…"},
		{"ko_KR.UTF-8", "~"},
		{"C", "..."},
	} {
		t.Setenv("LC_ALL", "")
		t.Setenv("LC_CTYPE", "")
		t.Setenv("LANG", tc.lang)
		t.Setenv("OKX_ASCII", "")
		if got := detectGlyphs().ellipsis; got != tc.want {
			t.Errorf("LANG=%s: ellipsis = %q, want %q", tc.lang, got, tc.want)
		}
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
