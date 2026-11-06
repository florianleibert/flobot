// Package desktop tests the Electron shell's source. The shell is plain
// JavaScript with no test runner of its own, so the invariants that decide
// whether a finished run reaches the user are asserted against the source
// here, next to the Go tests that already pin the web assets.
package desktop

import (
	"os"
	"strings"
	"testing"
)

func readSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// TestRunEndCountsEveryRun pins that the badge counts a run even when the
// window is focused.
//
// The badge means "something finished that you have not looked at". A focused
// window does not mean you watched the run: you may be on the Queue tab, or
// the Code panel may be scrolled away. Dropping the event on focus left the
// badge at zero for runs the user never saw, which is the one thing the count
// exists to prevent. Only the banner is suppressed when focused.
func TestRunEndCountsEveryRun(t *testing.T) {
	src := readSource(t, "main.js")

	start := strings.Index(src, "ipcMain.on('flobot:run-end'")
	if start < 0 {
		t.Fatal("flobot:run-end handler not found in main.js")
	}
	body := src[start:]
	if end := strings.Index(body, "\n});"); end > 0 {
		body = body[:end]
	}

	count := strings.Index(body, "setUnseen(unseenRuns + 1)")
	focus := strings.Index(body, "win.isFocused()")
	if count < 0 {
		t.Fatal("run-end handler never increments the unseen count; the badge stays at zero")
	}
	if focus < 0 {
		t.Fatal("run-end handler lost its focus check; it would banner while you are watching")
	}
	if count > focus {
		t.Errorf("run-end increments the badge only after the focus check; " +
			"a focused window returns early and the run is never counted")
	}
}

// TestPreloadForwardsRunEnd pins the one channel the renderer needs. Without
// it the bridge is present but inert, and notifyRunDone silently falls back to
// the browser API — no coalescing, no badge.
func TestPreloadForwardsRunEnd(t *testing.T) {
	src := readSource(t, "preload.js")
	for _, needle := range []string{"notifyRunEnd", "flobot:run-end"} {
		if !strings.Contains(src, needle) {
			t.Errorf("preload.js does not reference %q; the renderer cannot reach the main process", needle)
		}
	}
}
