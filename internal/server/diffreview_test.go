package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/florianleibert/flobot/internal/executor"
	"github.com/florianleibert/flobot/internal/store"
)

// gitInit creates a git repo with one committed file.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-m", "init")
}

// writeMutatingFakeBinary installs a fake flocode that edits a.txt and
// creates new.txt, so a run produces a real diff.
func writeMutatingFakeBinary(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "flocode")
	script := `#!/bin/sh
printf 'line two\n' >> a.txt
printf 'brand new\n' > new.txt
printf '%s\n' '{"kind":"content","turn_id":1,"text":"edited files"}'
printf '%s\n' '{"kind":"done","turn_id":1}'
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestRunDiffReviewFlow drives the full Code Studio review loop: a run
// mutates the workspace, the diff endpoint reports per-hunk changes, a
// partial apply lands selected hunks, and a checkpoint restore rolls
// the workspace back.
func TestRunDiffReviewFlow(t *testing.T) {
	binDir := t.TempDir()
	bin := writeMutatingFakeBinary(t, binDir)
	wsDir := t.TempDir()
	gitInit(t, wsDir)
	home := t.TempDir()

	st := store.New()
	srv := New(st,
		WithExec(&executor.Exec{Bin: bin}),
		WithHome(home),
		WithWorkDir(wsDir),
	)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{
		"name": "Coder", "role": "coder", "route": "glm",
	})
	botID, _ := bot["id"].(string)
	run := postJSON(t, ts, "/api/v1/runs", map[string]any{
		"bot_id": botID, "prompt": "edit files", "intent": "code",
	})
	runID, _ := run["id"].(string)

	// Wait for the run to finish and the diff to be ready.
	deadline := time.Now().Add(10 * time.Second)
	var diffBody map[string]any
	for time.Now().Before(deadline) {
		resp, err := http.Get(ts.URL + "/api/v1/runs/" + runID + "/diff")
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == 200 {
			diffBody = body
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if diffBody == nil {
		t.Fatal("diff never became ready")
	}
	files, _ := diffBody["files"].([]any)
	paths := map[string]bool{}
	for _, f := range files {
		if m, ok := f.(map[string]any); ok {
			paths[m["path"].(string)] = true
		}
	}
	// a.txt (modified) and new.txt (created) must be present; the
	// executor's own docs/ccpm-runs.md log may also appear.
	if !paths["a.txt"] || !paths["new.txt"] {
		t.Fatalf("diff paths = %v", paths)
	}
	tot, _ := diffBody["totals"].(map[string]any)
	if tot == nil || tot["files"].(float64) < 2 {
		t.Fatalf("totals = %v", tot)
	}
	cpID, _ := diffBody["checkpoint"].(string)
	if cpID == "" {
		t.Fatal("missing checkpoint id in diff")
	}

	// AC-2f: run history shows diffSummary and reviewState. The roster
	// merges the persisted summary into each run.
	roster := getRunJSON(t, ts, "/api/v1/roster")
	runs, _ := roster["runs"].([]any)
	var mine map[string]any
	for _, r := range runs {
		if m, ok := r.(map[string]any); ok && m["id"] == runID {
			mine = m
			break
		}
	}
	if mine == nil {
		t.Fatalf("run %s not in roster", runID)
	}
	if mine["diffSummary"] == nil {
		t.Fatalf("roster run missing diffSummary: %v", mine)
	}
	if rs, _ := mine["reviewState"].(string); rs != "pending" {
		t.Fatalf("roster reviewState = %q, want pending", rs)
	}

	// Apply only a.txt hunks (hunk 0), skip new.txt.
	applyResp := postJSON(t, ts, "/api/v1/runs/"+runID+"/diff/apply", map[string]any{
		"selections": []map[string]any{
			{"path": "a.txt", "hunks": []int{0}},
		},
	})
	if state, _ := applyResp["reviewState"].(string); state != "partial" {
		t.Fatalf("reviewState = %v, want partial (resp %v)", state, applyResp)
	}
	// G1: review metrics are stamped on apply (PRD §11).
	if ra, _ := applyResp["hunksAccepted"].(float64); ra != 1 {
		t.Fatalf("hunksAccepted = %v, want 1 (resp %v)", applyResp["hunksAccepted"], applyResp)
	}
	if rt, _ := applyResp["hunksTotal"].(float64); rt < 2 {
		t.Fatalf("hunksTotal = %v, want >= 2 (a.txt + new.txt)", applyResp["hunksTotal"])
	}
	roster = getRunJSON(t, ts, "/api/v1/roster")
	runs, _ = roster["runs"].([]any)
	for _, r := range runs {
		if m, ok := r.(map[string]any); ok && m["id"] == runID {
			mine = m
			break
		}
	}
	if mine == nil || mine["reviewedAt"] == nil {
		t.Fatalf("roster run missing reviewedAt after apply: %v", mine)
	}
	if ht, _ := mine["hunksTotal"].(float64); ht < 2 {
		t.Fatalf("roster hunksTotal = %v, want >= 2", mine["hunksTotal"])
	}
	// a.txt got the appended line; new.txt must not exist.
	got, err := os.ReadFile(filepath.Join(wsDir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello v1\nline two\n" {
		t.Fatalf("a.txt = %q", got)
	}
	if _, err := os.Stat(filepath.Join(wsDir, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("new.txt should not exist after partial apply: %v", err)
	}

	// Restore the checkpoint: workspace returns to the pre-apply state.
	rest := postJSON(t, ts, "/api/v1/checkpoints/"+cpID+"/restore", nil)
	if rest["restored"] != cpID {
		t.Fatalf("restore resp = %v", rest)
	}
	got, err = os.ReadFile(filepath.Join(wsDir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello v1\n" {
		t.Fatalf("a.txt after restore = %q", got)
	}
}

// TestRunDiffNotGitRepo verifies fail-closed review: a non-git workspace
// yields 404 on the diff endpoint, and the run itself still succeeds.
func TestRunDiffNotGitRepo(t *testing.T) {
	binDir := t.TempDir()
	bin, err := executor.WriteFakeBinary(binDir)
	if err != nil {
		t.Fatal(err)
	}
	wsDir := t.TempDir() // no git init
	home := t.TempDir()

	st := store.New()
	srv := New(st,
		WithExec(&executor.Exec{Bin: bin}),
		WithHome(home),
		WithWorkDir(wsDir),
	)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{
		"name": "Coder", "role": "coder", "route": "glm",
	})
	botID, _ := bot["id"].(string)
	run := postJSON(t, ts, "/api/v1/runs", map[string]any{
		"bot_id": botID, "prompt": "hello", "intent": "code",
	})
	runID, _ := run["id"].(string)

	// Run completes.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		body := getRunJSON(t, ts, "/api/v1/runs/"+runID)
		if s, _ := body["status"].(string); s == "done" || s == "error" {
			if s != "done" {
				t.Fatalf("run status = %s", s)
			}
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Diff is fail-closed.
	resp, err := http.Get(ts.URL + "/api/v1/runs/" + runID + "/diff")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("diff status = %d, want 404", resp.StatusCode)
	}
}

func getRunJSON(t *testing.T, ts *httptest.Server, path string) map[string]any {
	t.Helper()
	resp, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

var _ = strings.TrimSpace

// TestRejectDeletesRunCreatedFiles verifies the revert branch deletes
// files the run created that have no pre-run content (binary or empty
// files), instead of leaving them on disk.
func TestRejectDeletesRunCreatedFiles(t *testing.T) {
	binDir := t.TempDir()
	bin := writeMutatingFakeBinary(t, binDir)
	wsDir := t.TempDir()
	gitInit(t, wsDir)
	home := t.TempDir()

	st := store.New()
	srv := New(st,
		WithExec(&executor.Exec{Bin: bin}),
		WithHome(home),
		WithWorkDir(wsDir),
	)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{
		"name": "Coder", "role": "coder", "route": "glm",
	})
	botID, _ := bot["id"].(string)
	run := postJSON(t, ts, "/api/v1/runs", map[string]any{
		"bot_id": botID, "prompt": "edit files", "intent": "code",
	})
	runID, _ := run["id"].(string)

	// Wait for diff readiness.
	deadline := time.Now().Add(10 * time.Second)
	var diffBody map[string]any
	for time.Now().Before(deadline) {
		resp, err := http.Get(ts.URL + "/api/v1/runs/" + runID + "/diff")
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == 200 {
			diffBody = body
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if diffBody == nil {
		t.Fatal("diff never became ready")
	}

	// Reject: everything the run did must be undone.
	rej := postJSON(t, ts, "/api/v1/runs/"+runID+"/diff/reject", nil)
	if state, _ := rej["reviewState"].(string); state != "rejected" {
		t.Fatalf("reviewState = %v, want rejected (resp %v)", state, rej)
	}
	// new.txt was created by the run: it must be gone.
	if _, err := os.Stat(filepath.Join(wsDir, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("new.txt must be deleted after reject: %v", err)
	}
	// a.txt must be back to its pre-run content.
	got, err := os.ReadFile(filepath.Join(wsDir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello v1\n" {
		t.Fatalf("a.txt after reject = %q", got)
	}
}
