package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/keyolk/tuikit/toast"
)

// toastText is every queued toast's text, for asserting on what the status row
// would show.
func toastText(m *Model) string {
	var parts []string
	for _, t := range m.toasts.Items() {
		parts = append(parts, t.Text)
	}
	return strings.Join(parts, " | ")
}

// A toast left over from an earlier action used to outrank the filter prompt,
// so typing after "/" showed nothing.
func TestFilterPromptOutranksStaleStatus(t *testing.T) {
	model := topLevelTestModel()
	model.notify("apply", "applied 1 change(s)", toast.Success)
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

// A toast is drawn in the status row with a level prefix, so severity survives
// NO_COLOR.
func TestToastIsDrawnInTheStatusRow(t *testing.T) {
	model := topLevelTestModel()
	model.notify("apply", "boom", toast.Error)
	footer := model.renderFooter()
	if !strings.Contains(footer, "boom") {
		t.Fatalf("toast missing from the footer:\n%s", footer)
	}
	if !strings.Contains(footer, model.toastStyles().Prefix(toast.Error)) {
		t.Fatalf("toast has no severity prefix:\n%s", footer)
	}
}

// An error stays until a key after it was on screen long enough to read;
// the key that was already in flight when it landed does not dismiss it.
func TestErrorToastClearsOnKeyAfterDwell(t *testing.T) {
	model := topLevelTestModel()
	now := time.Now()
	model.toasts = toast.WithClock(model.toasts, func() time.Time { return now })
	model.notify("apply", "applied 0, 1 failed", toast.Error)

	model.handleKey(keyMsg("j"))
	if model.toasts.Len() != 1 {
		t.Fatal("an error was dismissed by a key pressed before it could be read")
	}
	now = now.Add(time.Second)
	model.handleKey(keyMsg("j"))
	if model.toasts.Len() != 0 {
		t.Fatalf("error survived a key after it was read: %q", toastText(model))
	}
}

// A transient toast expires by its own tick; an older tick of the same source
// cannot cut a newer toast short.
func TestToastExpiresByID(t *testing.T) {
	model := topLevelTestModel()

	model.notify("refresh", "first", toast.Info)
	first := model.toasts.Items()[0].ID()
	model.notify("refresh", "second", toast.Info)

	if _, ok := model.toasts.ExpiredMsg(first); ok {
		t.Fatal("the replaced toast still has a live tick")
	}
	second := model.toasts.Items()[0].ID()
	msg, ok := model.toasts.ExpiredMsg(second)
	if !ok {
		t.Fatal("the current toast has no tick")
	}
	model.Update(msg)
	if model.toasts.Len() != 0 {
		t.Fatalf("matching expiry left %q", toastText(model))
	}
}

func TestUpdateArmsToastExpiry(t *testing.T) {
	model := topLevelTestModel()
	model.busyKind = busyRefresh
	_, cmd := model.Update(keyMsg("R")) // "already refreshing"
	if !strings.Contains(toastText(model), "already refreshing") {
		t.Fatalf("setup: no toast was pushed: %q", toastText(model))
	}
	if cmd == nil {
		t.Fatal("a toast was pushed without an expiry command")
	}
}

// Toasts wait while a modal is up: they would otherwise expire unseen behind it.
func TestToastsHoldWhileAnOverlayIsOpen(t *testing.T) {
	model := topLevelTestModel()
	model.handleKey(keyMsg("enter")) // apps → assignments
	model.Update(keyMsg("a"))        // picker
	if !model.toasts.Held() {
		t.Fatal("toasts not held while the picker is open")
	}
	model.Update(keyMsg("esc"))
	if model.toasts.Held() {
		t.Fatal("toasts still held after the picker closed")
	}
}
