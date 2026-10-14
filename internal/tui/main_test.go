package tui

import (
	"os"
	"testing"
)

// TestMain points HOME and the XDG dirs at a temp dir for the whole package:
// quit flushes ui.json and Run reads keys.yaml, and neither may touch the
// developer's real files or pick up their overrides.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "okx-tui-test-")
	if err != nil {
		panic(err)
	}
	for _, k := range []string{"HOME", "XDG_STATE_HOME", "XDG_CONFIG_HOME"} {
		if err := os.Setenv(k, dir); err != nil {
			panic(err)
		}
	}
	code := m.Run()
	_ = os.RemoveAll(dir) // best effort; the code is what matters
	os.Exit(code)
}
