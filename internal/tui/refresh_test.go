package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/keyolk/okx/internal/cache"
)

// A refresh is read-only, so every navigation key keeps working while it runs.
// This is the regression the whole change exists for: gating the entire key
// handler on "busy" froze the UI for the length of a full-org fetch.
func TestNavigationWorksDuringRefresh(t *testing.T) {
	model := topLevelTestModel()
	model.busyKind = busyRefresh

	model.handleKey(keyMsg("2"))
	model.handleKey(keyMsg("enter"))
	if model.screen != screenGroupMembers {
		t.Fatalf("drill-down during refresh: screen = %v, want group members", model.screen)
	}
	model.handleKey(keyMsg("esc"))
	if model.screen != screenGroups {
		t.Fatalf("back during refresh: screen = %v, want groups", model.screen)
	}

	model.handleKey(keyMsg("/"))
	if !model.filtering {
		t.Fatal("/ did not open the filter during refresh")
	}
	model.handleKey(keyMsg("p"))
	if model.groupFiltr != "p" {
		t.Fatalf("filter text during refresh = %q, want %q", model.groupFiltr, "p")
	}
	model.handleKey(keyMsg("esc"))

	model.handleKey(keyMsg("?"))
	if model.screen != screenHelp {
		t.Fatalf("help during refresh: screen = %v, want help", model.screen)
	}
}

// Writes stay gated: planning a change against an index that is about to be
// replaced would apply it to rows the user never saw.
func TestAssignmentWritesAreBlockedDuringRefresh(t *testing.T) {
	for _, key := range []string{"a", "A", "d"} {
		model := topLevelTestModel()
		model.handleKey(keyMsg("enter")) // apps → assignments
		if model.screen != screenAssignments {
			t.Fatalf("setup: screen = %v, want assignments", model.screen)
		}
		model.busyKind = busyRefresh

		model.handleKey(keyMsg(key))
		if model.overlay != overlayNone {
			t.Errorf("key %q opened overlay %v during refresh", key, model.overlay)
		}
		if model.pending != nil {
			t.Errorf("key %q queued %d change(s) during refresh", key, len(model.pending))
		}
		if model.status == "" {
			t.Errorf("key %q was silently ignored; expected an explanation", key)
		}
	}
}

// A second R while one fetch is in flight must not start another.
func TestRefreshKeyIsIgnoredWhileRefreshing(t *testing.T) {
	model := topLevelTestModel()
	model.busyKind = busyRefresh
	gen := model.progGen

	_, cmd := model.handleKey(keyMsg("R"))
	if cmd != nil {
		t.Error("R started a second fetch while one was already running")
	}
	if model.progGen != gen {
		t.Errorf("progGen = %d, want %d — a new progress sink was allocated", model.progGen, gen)
	}
}

// The footer must name the stage and its position, so a multi-minute fetch is
// distinguishable from a hang.
func TestProgressFooterShowsStageAndCount(t *testing.T) {
	model := topLevelTestModel()
	model.busyKind = busyRefresh
	model.busyLabel = "refreshing snapshot"
	model.busyStart = time.Now()
	model.prog = progressState{stage: cache.StageAssignments, done: 7, total: 20, active: true}

	footer := model.renderFooter()
	for _, want := range []string{"refreshing snapshot", cache.StageAssignments, "7/20", "[4/5]", "35%"} {
		if !strings.Contains(footer, want) {
			t.Errorf("footer %q does not contain %q", footer, want)
		}
	}
}

// A stage that reports no total gets no bar rather than a fabricated one.
func TestProgressBarOmittedWithoutTotal(t *testing.T) {
	model := topLevelTestModel()
	model.prog = progressState{stage: cache.StageApps, active: true}
	if bar := model.progressBar(); bar != "" {
		t.Errorf("progressBar() = %q for a stage with no total, want empty", bar)
	}
}

// Progress reports from a superseded fetch must not overwrite the current one.
func TestStaleProgressIsDropped(t *testing.T) {
	model := topLevelTestModel()
	model.progGen = 2
	model.prog = progressState{stage: cache.StageGroups, done: 3, total: 9, active: true}

	model.Update(progressMsg{gen: 1, stage: cache.StageApps, done: 99, total: 99})
	if model.prog.stage != cache.StageGroups || model.prog.done != 3 {
		t.Fatalf("stale progress overwrote state: %#v", model.prog)
	}

	model.Update(progressDoneMsg{gen: 1})
	if !model.prog.active {
		t.Fatal("a stale done message deactivated the live progress")
	}
}

// The very first fetch has no rows; the body must say what is happening rather
// than render an empty table that reads as "this org has 0 apps".
func TestEmptyIndexShowsLoadingBody(t *testing.T) {
	model := topLevelTestModel()
	model.app.Index = cache.NewIndex(cache.Empty("https://example.okta.com"))
	model.reloadResources()
	model.busyKind = busyRefresh
	model.busyLabel = "refreshing snapshot"
	model.busyStart = time.Now()

	view := model.View()
	if !strings.Contains(view, "fetching the org snapshot") {
		t.Errorf("view during the initial fetch does not explain itself:\n%s", view)
	}
	for _, label := range []string{"1 Apps", "2 Groups", "3 Users"} {
		if !strings.Contains(view, label) {
			t.Errorf("loading view lost the %q tab", label)
		}
	}
}

// A populated stale snapshot stays on screen during its replacement — the whole
// point of AllowStale.
func TestStaleSnapshotStaysVisibleDuringRefresh(t *testing.T) {
	model := topLevelTestModel()
	model.busyKind = busyRefresh
	model.busyStart = time.Now()

	view := model.View()
	if !strings.Contains(view, "Alpha Console") {
		t.Errorf("stale rows disappeared during refresh:\n%s", view)
	}
}

func TestStageIndexCoversEveryFetchStage(t *testing.T) {
	for i, stage := range cache.Stages {
		step, of := cache.StageIndex(stage)
		if step != i+1 || of != len(cache.Stages) {
			t.Errorf("StageIndex(%q) = (%d, %d), want (%d, %d)", stage, step, of, i+1, len(cache.Stages))
		}
	}
	if step, _ := cache.StageIndex("not a stage"); step != 0 {
		t.Errorf("StageIndex on an unknown stage = %d, want 0", step)
	}
}
