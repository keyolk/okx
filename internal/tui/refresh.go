package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/keyolk/okx/internal/cache"
)

// busyKind distinguishes the two long operations, because they have opposite
// safety properties: a refresh is read-only and leaves the current index intact
// until it lands, so the whole UI stays navigable; a mutation is about to
// change the very rows under the cursor, so it owns the screen.
type busyKind int

const (
	busyNone busyKind = iota
	busyRefresh
	busyApply
)

// progressState is the last progress report rendered in the footer.
type progressState struct {
	stage  string
	done   int
	total  int
	active bool
}

// progressMsg carries one cache.Progress callback into Update. gen guards
// against a late report from a superseded fetch.
type progressMsg struct {
	gen   int
	stage string
	done  int
	total int
}

// progressDoneMsg fires when a fetch closes its progress channel.
type progressDoneMsg struct{ gen int }

// spinnerTickMsg advances the spinner and the elapsed clock.
type spinnerTickMsg struct{}

const spinnerInterval = 120 * time.Millisecond

// newProgressSink allocates the channel a fetch goroutine reports into and
// returns the sink plus the command that pumps it into Update. Sends are
// non-blocking: a progress update that cannot be delivered is worth dropping,
// never worth stalling the fetch it is describing.
func (m *Model) newProgressSink() (cache.Progress, tea.Cmd) {
	m.progGen++
	gen := m.progGen
	ch := make(chan progressMsg, 16)
	m.progCh = ch
	m.busyStart = time.Now()
	m.prog = progressState{}

	sink := func(stage string, done, total int) {
		select {
		case ch <- progressMsg{gen: gen, stage: stage, done: done, total: total}:
		default:
		}
	}
	return sink, tea.Batch(waitProgress(ch, gen), spinnerTick())
}

func waitProgress(ch <-chan progressMsg, gen int) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return progressDoneMsg{gen: gen}
		}
		return msg
	}
}

func spinnerTick() tea.Cmd {
	return tea.Tick(spinnerInterval, func(time.Time) tea.Msg { return spinnerTickMsg{} })
}

// startRefresh marks the model busy and returns the fetch + progress commands.
func (m *Model) startRefresh() tea.Cmd {
	m.busyKind = busyRefresh
	m.busyLabel = "refreshing snapshot"
	m.status = ""
	m.statusErr = false
	return m.refreshCmd()
}

func (m *Model) refreshCmd() tea.Cmd {
	sink, pump := m.newProgressSink()
	ch := m.progCh
	fetch := func() tea.Msg {
		index, err := m.app.FetchIndexProgress(m.ctx, sink)
		// Every worker has returned by the time the fetch does, so no send can
		// race this close.
		close(ch)
		return refreshDoneMsg{index: index, err: err}
	}
	return tea.Batch(fetch, pump)
}

// refreshing reports whether a read-only snapshot fetch is in flight.
func (m *Model) refreshing() bool { return m.busyKind == busyRefresh }

// blockedByRefresh rejects a write while a fetch is in flight and explains why.
// Planning a change against an index that is seconds from being replaced would
// apply it against rows the user never saw.
func (m *Model) blockedByRefresh() bool {
	if !m.refreshing() {
		return false
	}
	m.setStatus("refreshing — assignment changes are available once it lands", false)
	return true
}

func (m *Model) endBusy() {
	m.busyKind = busyNone
	m.busyLabel = ""
	m.prog = progressState{}
}

// ---- rendering ------------------------------------------------------------

// renderProgress is the footer line during a long operation. It names the
// stage, its position in the pipeline, and the item count, so a multi-minute
// fetch never looks like a hang.
func (m *Model) renderProgress() string {
	spin := m.gl.spinner[m.spinFrame%len(m.gl.spinner)]
	parts := []string{m.st.info.Render(spin + " " + m.busyLabel)}

	if m.prog.active {
		label := m.prog.stage
		if step, of := cache.StageIndex(m.prog.stage); step > 0 {
			label = fmt.Sprintf("[%d/%d] %s", step, of, label)
		}
		if m.prog.total > 0 {
			label += fmt.Sprintf(" %d/%d", m.prog.done, m.prog.total)
		}
		parts = append(parts, m.st.dim.Render(label))
		if bar := m.progressBar(); bar != "" {
			parts = append(parts, bar)
		}
	}
	parts = append(parts, m.st.dim.Render(elapsed(time.Since(m.busyStart))))

	return truncate(strings.Join(parts, "  "), m.width, m.gl.ellipsis)
}

// progressBar renders the current stage's completion. Stages that report no
// total (a single list call) get no bar rather than a fake one.
func (m *Model) progressBar() string {
	if m.prog.total <= 0 {
		return ""
	}
	const width = 12
	filled := width * m.prog.done / m.prog.total
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	pct := 100 * m.prog.done / m.prog.total
	return m.st.info.Render(strings.Repeat(m.gl.barFull, filled)) +
		m.st.dim.Render(strings.Repeat(m.gl.barEmpty, width-filled)) +
		m.st.dim.Render(fmt.Sprintf(" %3d%%", pct))
}

// renderLoadingBody replaces an empty list while the first snapshot loads, so
// the initial screen says what is happening instead of showing nothing.
func (m *Model) renderLoadingBody() string {
	lines := []string{
		m.st.accent.Render("fetching the org snapshot from Okta"),
		m.st.dim.Render("a large org takes a few minutes — the footer tracks each stage"),
		m.st.dim.Render("1/2/3 switch views; lists fill in as soon as the fetch lands"),
	}

	h := m.listHeight() + 1
	block := strings.Join(lines, "\n")
	pad := (h - len(lines)) / 2
	if pad < 0 {
		pad = 0
	}
	return fillHeight(strings.Repeat("\n", pad)+block+"\n", h)
}

func elapsed(d time.Duration) string {
	s := int(d.Seconds())
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	return fmt.Sprintf("%dm%02ds", s/60, s%60)
}
