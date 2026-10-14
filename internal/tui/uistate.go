package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/keyolk/tuikit/render"
)

// uiState is the UI state worth keeping across restarts (E12), written to
// $XDG_STATE_HOME/okx/ui.json by prefs.Saver: the last top-level tab, each
// tab's filter, and each tab's selected row by ID — an ID, not an index, so a
// snapshot that gained rows still lands on the same app/group/user.
type uiState struct {
	Tab     string            `json:"tab,omitempty"`
	Filters map[string]string `json:"filters,omitempty"`
	Sel     map[string]string `json:"sel,omitempty"`
}

var tabNames = map[screen]string{screenApps: "apps", screenGroups: "groups", screenUsers: "users"}

func (m *Model) uiState() uiState {
	s := uiState{
		Tab:     tabNames[m.activeTopLevel()],
		Filters: map[string]string{},
		Sel:     map[string]string{},
	}
	for sc, name := range tabNames {
		if f := m.topFilter(sc); f != "" {
			s.Filters[name] = f
		}
	}
	for sc, id := range m.topSelection() {
		s.Sel[tabNames[sc]] = id
	}
	return s
}

// saveUI records the current state; the write is debounced and runs off the
// UI loop, so holding j writes once after the keys stop.
func (m *Model) saveUI() {
	var cmd tea.Cmd
	m.saver, cmd = m.saver.Save(m.uiState())
	if cmd != nil {
		m.queued = append(m.queued, cmd)
	}
}

// restoreUI applies saved state at startup. Unknown tabs and IDs that are no
// longer in the snapshot are ignored.
func (m *Model) restoreUI(s uiState) {
	for sc, name := range tabNames {
		if s.Tab == name {
			m.screen = sc
		}
		if f, ok := s.Filters[name]; ok {
			*m.topFilterPtr(sc) = f
		}
	}
	sel := map[screen]string{}
	for sc, name := range tabNames {
		if id := s.Sel[name]; id != "" {
			sel[sc] = id
		}
	}
	m.restoreTopSelection(sel)
}

func (m *Model) topFilterPtr(s screen) *string {
	switch s {
	case screenGroups:
		return &m.groupFiltr
	case screenUsers:
		return &m.userFiltr
	}
	return &m.appFiltr
}

func (m *Model) topFilter(s screen) string { return *m.topFilterPtr(s) }

// topSelection is the ID under each top-level cursor.
func (m *Model) topSelection() map[screen]string {
	out := map[screen]string{}
	if rows := m.filteredApps(); len(rows) > 0 {
		out[screenApps] = rows[m.clampIdx(m.appCur, len(rows))].ID
	}
	if rows := m.filteredTopGroups(); len(rows) > 0 {
		out[screenGroups] = rows[m.clampIdx(m.groupCur, len(rows))].ID
	}
	if rows := m.filteredTopUsers(); len(rows) > 0 {
		out[screenUsers] = rows[m.clampIdx(m.userCur, len(rows))].ID
	}
	return out
}

// restoreTopSelection moves each top-level cursor back onto its ID. An ID
// that vanished keeps the cursor's slot, clamped.
func (m *Model) restoreTopSelection(sel map[screen]string) {
	if id, ok := sel[screenApps]; ok {
		rows := m.filteredApps()
		for i := range rows {
			if rows[i].ID == id {
				m.appCur = i
				break
			}
		}
	}
	if id, ok := sel[screenGroups]; ok {
		rows := m.filteredTopGroups()
		for i := range rows {
			if rows[i].ID == id {
				m.groupCur = i
				break
			}
		}
	}
	if id, ok := sel[screenUsers]; ok {
		rows := m.filteredTopUsers()
		for i := range rows {
			if rows[i].ID == id {
				m.userCur = i
				break
			}
		}
	}
	m.clampTopLevel()
}

// listLines writes a list's visible rows into b. When they do not all fit,
// the last row says how many are cut on each side — `↓ 22 more`, `↑ 3 ↓ 19
// more` (E7) — instead of the list ending silently. One line for both keeps
// the first row under the column header rather than below a blank marker.
// height is every row the list may use, the marker included.
func (m *Model) listLines(b *strings.Builder, total, cur, top, height int, row func(i int, sel bool) string) {
	rows := windowRows(total, height)
	start, end, _ := render.Window(total, rows, cur, top)
	for i := start; i < end; i++ {
		b.WriteString(row(i, i == cur) + "\n")
	}
	if rows < height {
		// More collapses both counts into its second line below height 3.
		_, line := render.More(start, total-end, 2)
		b.WriteString(m.st.dim.Render(" "+line) + "\n")
	}
}
