package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/florianleibert/flobot/internal/executor"
	"github.com/florianleibert/flobot/internal/store"
)

// TestVerdictSurvivesServerRestart is the M7 acceptance check at the surface
// level: a verdict posted to one server is readable from a second server built
// over the same home directory, i.e. it survived a restart.
func TestVerdictSurvivesServerRestart(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	bin, err := executor.WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	st := store.New()
	st.SetPath(filepath.Join(home, ".flobot", "state.json"))
	srv := New(st, WithExec(&executor.Exec{Bin: bin}), WithHome(home))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{
		"name": "Judge", "role": "coder", "route": "glm",
	})
	run := postJSON(t, ts, "/api/v1/runs", map[string]any{
		"bot_id": bot["id"], "prompt": "hello", "intent": "ask",
	})
	runID, _ := run["id"].(string)
	waitEvents(t, ts, runID)

	res := postJSON(t, ts, "/api/v1/runs/"+runID+"/verdict", map[string]any{
		"verdict": "good", "note": "clean answer",
	})
	got, _ := res["verdict"].(map[string]any)
	if got["verdict"] != "good" {
		t.Fatalf("verdict = %v", res)
	}

	// Restart: a fresh server over the same home must still see the label.
	st2 := store.New()
	st2.SetPath(filepath.Join(home, ".flobot", "state.json"))
	srv2 := New(st2, WithExec(&executor.Exec{Bin: bin}), WithHome(home))
	ts2 := httptest.NewServer(srv2.Handler())
	t.Cleanup(ts2.Close)

	list := getJSON(t, ts2, "/api/v1/eval/verdicts")
	rows, _ := list["verdicts"].([]any)
	if len(rows) != 1 {
		t.Fatalf("verdicts after restart = %v", list)
	}
	row, _ := rows[0].(map[string]any)
	if row["run_id"] != runID || row["verdict"] != "good" || row["note"] != "clean answer" {
		t.Fatalf("row = %v", row)
	}
}

// TestVerdictRejectsBadValue: a label that did not stick must be visible.
func TestVerdictRejectsBadValue(t *testing.T) {
	srv := New(store.New(), WithHome(t.TempDir()))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	resp, err := http.Post(ts.URL+"/api/v1/runs/run-x/verdict", "application/json",
		strings.NewReader(`{"verdict":"meh"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestReplayReproducesStoredRuns is the M7 replay acceptance check over the real
// store: every finished run's recorded status is reproduced from its events.
func TestReplayReproducesStoredRuns(t *testing.T) {
	dir := t.TempDir()
	bin, err := executor.WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(store.New(), WithExec(&executor.Exec{Bin: bin}), WithHome(t.TempDir()))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{
		"name": "Replayer", "role": "coder", "route": "glm",
	})
	for i := 0; i < 3; i++ {
		run := postJSON(t, ts, "/api/v1/runs", map[string]any{
			"bot_id": bot["id"], "prompt": "hello", "intent": "ask",
		})
		id, _ := run["id"].(string)
		waitEvents(t, ts, id)
	}

	res := getJSON(t, ts, "/api/v1/eval/replay")
	total := int(res["total"].(float64))
	if total < 3 {
		t.Fatalf("replayed %d runs: %v", total, res)
	}
	if int(res["reproduced"].(float64)) != total {
		t.Fatalf("reproduced %v of %v: %v", res["reproduced"], res["total"], res["results"])
	}
}
