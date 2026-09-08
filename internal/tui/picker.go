package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/keyolk/okx/internal/cache"
	"github.com/keyolk/okx/internal/fuzzy"
	"github.com/keyolk/okx/internal/okta"
)

// pickerState drives the assign overlay: a fuzzy-filtered, multi-select list
// of users or groups not yet assigned to the current app.
type pickerState struct {
	kind    string // "user" or "group"
	query   string
	items   []pickItem
	matches []fuzzy.Match
	cur     int
	top     int
	chosen  map[string]bool
}

type pickItem struct {
	id    string
	label string // what fuzzy-matches and what gets displayed
	desc  string
	// already is set when the candidate already has some form of access; the
	// picker shows it rather than hiding it, so "why isn't X in the list" never
	// becomes a question.
	already string
}

func (m *Model) openPicker(kind overlay) {
	p := pickerState{chosen: map[string]bool{}}

	if kind == overlayPickGroup {
		p.kind = "group"
		assigned := map[string]bool{}
		for _, ag := range m.app.Index.AppGroups[m.curApp.ID] {
			assigned[ag.ID] = true
		}
		for _, g := range m.app.Index.Groups {
			it := pickItem{id: g.ID, label: g.Profile.Name, desc: g.Profile.Description}
			if assigned[g.ID] {
				it.already = "assigned"
			}
			p.items = append(p.items, it)
		}
		sort.Slice(p.items, func(i, j int) bool { return p.items[i].label < p.items[j].label })
	} else {
		p.kind = "user"
		byID := map[string]cache.Assignment{}
		for _, a := range m.app.Index.AppAssignments(m.curApp.ID) {
			byID[a.User.ID] = a
		}
		for _, u := range m.app.Index.Users {
			it := pickItem{id: u.ID, label: u.Profile.Login, desc: u.Name()}
			if a, ok := byID[u.ID]; ok {
				if a.Direct {
					it.already = "direct"
				} else {
					it.already = "via " + groupList(a.ViaGroups)
				}
			}
			p.items = append(p.items, it)
		}
		sort.Slice(p.items, func(i, j int) bool { return p.items[i].label < p.items[j].label })
	}

	p.refilter()
	m.pick = p
	m.overlay = kind
}

func (p *pickerState) refilter() {
	labels := make([]string, len(p.items))
	for i, it := range p.items {
		labels[i] = it.label
	}
	// Match against the description too, so "Gavin" finds gavin.jeong@… even
	// when the login is an initials-style address.
	if p.query != "" {
		p.matches = p.matches[:0]
		seen := map[int]bool{}
		for _, m := range fuzzy.Filter(labels, p.query) {
			p.matches = append(p.matches, m)
			seen[m.Index] = true
		}
		descs := make([]string, len(p.items))
		for i, it := range p.items {
			descs[i] = it.desc
		}
		for _, m := range fuzzy.Filter(descs, p.query) {
			if !seen[m.Index] && p.items[m.Index].desc != "" {
				// Description hits rank below label hits.
				m.Score -= 64
				p.matches = append(p.matches, m)
			}
		}
		sort.SliceStable(p.matches, func(i, j int) bool { return p.matches[i].Score > p.matches[j].Score })
	} else {
		p.matches = p.matches[:0]
		for i := range p.items {
			p.matches = append(p.matches, fuzzy.Match{Index: i})
		}
	}
	if p.cur >= len(p.matches) {
		p.cur = len(p.matches) - 1
	}
	if p.cur < 0 {
		p.cur = 0
	}
	p.top = 0
}

func (p *pickerState) selected() (pickItem, bool) {
	if p.cur < 0 || p.cur >= len(p.matches) {
		return pickItem{}, false
	}
	return p.items[p.matches[p.cur].Index], true
}

func (m *Model) handleOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch m.overlay {
	case overlayError:
		if key == "esc" || key == "enter" || key == "q" {
			m.overlay = overlayNone
			m.errText = ""
		}
		return m, nil

	case overlayConfirm:
		switch key {
		case "y", "Y", "enter":
			pending := m.pending
			m.pending = nil
			m.overlay = overlayNone
			applicable := 0
			for _, ch := range pending {
				if ch.noop == "" {
					applicable++
				}
			}
			if applicable == 0 {
				m.setStatus("nothing to do", false)
				return m, nil
			}
			m.busyKind = busyApply
			m.busyLabel = fmt.Sprintf("applying %d change(s)", applicable)
			m.status = ""
			return m, m.applyCmd(pending, m.curApp.ID)
		case "n", "N", "esc", "q":
			m.pending = nil
			m.overlay = overlayNone
			m.setStatus("aborted", false)
		}
		return m, nil

	case overlayPickUser, overlayPickGroup:
		return m.handlePickerKey(msg)
	}
	return m, nil
}

func (m *Model) handlePickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := &m.pick
	h := m.pickerHeight()

	switch msg.String() {
	case "esc":
		m.overlay = overlayNone
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "down", "ctrl+n":
		p.cur++
	case "up", "ctrl+p":
		p.cur--
	case "pgdown":
		p.cur += h / 2
	case "pgup":
		p.cur -= h / 2
	case "tab":
		// Toggle without moving: multi-select is the point of this overlay.
		if it, ok := p.selected(); ok {
			if p.chosen[it.id] {
				delete(p.chosen, it.id)
			} else {
				p.chosen[it.id] = true
			}
		}
	case "enter":
		return m.commitPicker()
	case "backspace":
		if r := []rune(p.query); len(r) > 0 {
			p.query = string(r[:len(r)-1])
			p.refilter()
		}
	case "ctrl+u":
		p.query = ""
		p.refilter()
	default:
		if msg.Type == tea.KeyRunes {
			p.query += string(msg.Runes)
			p.refilter()
		} else if msg.String() == " " {
			p.query += " "
			p.refilter()
		}
	}

	if p.cur < 0 {
		p.cur = 0
	}
	if p.cur >= len(p.matches) {
		p.cur = len(p.matches) - 1
	}
	if p.cur < 0 {
		p.cur = 0
	}
	p.top = scrollTo(p.top, p.cur, h)
	return m, nil
}

// commitPicker turns the picker selection into a pending change set and hands
// off to the confirm modal. Enter with nothing toggled uses the row under the
// cursor, which is what a single-target assign should take.
func (m *Model) commitPicker() (tea.Model, tea.Cmd) {
	p := &m.pick
	var targets []pickItem
	if len(p.chosen) > 0 {
		for _, it := range p.items {
			if p.chosen[it.id] {
				targets = append(targets, it)
			}
		}
	} else if it, ok := p.selected(); ok {
		targets = append(targets, it)
	}
	if len(targets) == 0 {
		m.overlay = overlayNone
		return m, nil
	}

	var changes []plannedChange
	for _, it := range targets {
		ch := plannedChange{verb: "assign", kind: p.kind, id: it.id, label: it.label}
		switch {
		case p.kind == "group" && it.already == "assigned":
			ch.noop = "already assigned"
		case p.kind == "user" && it.already == "direct":
			ch.noop = "already assigned directly"
		case p.kind == "user" && it.already != "":
			ch.warn = "already has access " + it.already + "; adding a direct assignment too"
		}
		changes = append(changes, ch)
	}
	m.pending = changes
	m.overlay = overlayConfirm
	return m, nil
}

func (m *Model) pickerHeight() int {
	h := m.height - 8
	if h < 3 {
		return 3
	}
	if h > 18 {
		return 18
	}
	return h
}

// ---- filtering on the main screens ----------------------------------------

func (m *Model) filteredApps() []okta.App {
	if m.appFiltr == "" {
		return m.apps
	}
	out := make([]okta.App, 0, len(m.apps))
	for _, a := range m.apps {
		if m.app.AppMatches(a.ID, m.appFiltr) {
			out = append(out, a)
		}
	}
	return out
}

func (m *Model) filteredTopGroups() []okta.Group {
	if m.groupFiltr == "" {
		return m.groups
	}
	hay := make([]string, len(m.groups))
	for i, g := range m.groups {
		hay[i] = g.Profile.Name + " " + g.Profile.Description + " " + g.ID
	}
	matches := fuzzy.Filter(hay, m.groupFiltr)
	out := make([]okta.Group, 0, len(matches))
	for _, match := range matches {
		out = append(out, m.groups[match.Index])
	}
	return out
}

func (m *Model) filteredTopUsers() []okta.User {
	if m.userFiltr == "" {
		return m.users
	}
	hay := make([]string, len(m.users))
	for i, u := range m.users {
		hay[i] = u.Profile.Login + " " + u.Profile.Email + " " + u.Name() + " " + u.ID
	}
	matches := fuzzy.Filter(hay, m.userFiltr)
	out := make([]okta.User, 0, len(matches))
	for _, match := range matches {
		out = append(out, m.users[match.Index])
	}
	return out
}

func (m *Model) filteredAssignments() []cache.Assignment {
	if m.asgFiltr == "" {
		return m.assignments
	}
	// Match on "login name group1 group2" so filtering by a group name narrows
	// to the users that group brings in — the common audit question.
	hay := make([]string, len(m.assignments))
	for i, a := range m.assignments {
		var b strings.Builder
		b.WriteString(a.User.Profile.Login)
		b.WriteByte(' ')
		b.WriteString(a.User.Name())
		for _, g := range a.ViaGroups {
			b.WriteByte(' ')
			b.WriteString(g.Profile.Name)
		}
		hay[i] = b.String()
	}
	matches := fuzzy.Filter(hay, m.asgFiltr)
	out := make([]cache.Assignment, 0, len(matches))
	for _, mt := range matches {
		out = append(out, m.assignments[mt.Index])
	}
	return out
}

func (m *Model) filteredGroups() []groupRow {
	if m.asgFiltr == "" {
		return m.appGroups
	}
	names := make([]string, len(m.appGroups))
	for i, g := range m.appGroups {
		names[i] = g.group.Profile.Name
	}
	matches := fuzzy.Filter(names, m.asgFiltr)
	out := make([]groupRow, 0, len(matches))
	for _, mt := range matches {
		out = append(out, m.appGroups[mt.Index])
	}
	return out
}

var _ = okta.Group{}
