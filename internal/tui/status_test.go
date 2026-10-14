package tui

import (
	"strings"
	"testing"
)

// A status left over from an earlier action used to outrank the filter prompt,
// so typing after "/" showed nothing.
func TestFilterPromptOutranksStaleStatus(t *testing.T) {
	model := topLevelTestModel()
	model.setStatus("applied 1 change(s)", false)
	model.filtering = true
	model.appFiltr = "alp"

	footer := model.renderFooter()
	if !strings.Contains(footer, "/alp") {
		t.Fatalf("filter prompt hidden by status:\n%s", footer)
	}
}

// The filter prompt also stays visible while a refresh runs; typing a filter
// during a refresh is allowed, so its echo must be too.
func TestFilterPromptOutranksRefreshProgress(t *testing.T) {
	model := topLevelTestModel()
	model.busyKind = busyRefresh
	model.filtering = true
	model.appFiltr = "alp"

	if footer := model.renderFooter(); !strings.Contains(footer, "/alp") {
		t.Fatalf("filter prompt hidden by progress:\n%s", footer)
	}
}

func TestStatusClearsOnNextKey(t *testing.T) {
	model := topLevelTestModel()
	model.setStatus("refreshed", false)
	model.handleKey(keyMsg("j"))
	if model.status != "" {
		t.Fatalf("status survived a keypress: %q", model.status)
	}
}

// Setting a status through Update arms an expiry; only the matching tick
// clears it, so a newer status is not cut short by an older timer.
func TestStatusExpiresByID(t *testing.T) {
	model := topLevelTestModel()

	model.setStatus("first", false)
	first := model.statusID
	model.setStatus("second", false)

	model.Update(statusExpireMsg{id: first})
	if model.status != "second" {
		t.Fatalf("a stale expiry cleared the newer status: %q", model.status)
	}
	model.Update(statusExpireMsg{id: model.statusID})
	if model.status != "" {
		t.Fatalf("matching expiry left status %q", model.status)
	}
}

func TestUpdateArmsStatusExpiry(t *testing.T) {
	model := topLevelTestModel()
	model.busyKind = busyRefresh
	_, cmd := model.Update(keyMsg("R")) // "already refreshing"
	if model.status == "" {
		t.Fatal("setup: no status was set")
	}
	if cmd == nil {
		t.Fatal("a new status was set without an expiry command")
	}
}
