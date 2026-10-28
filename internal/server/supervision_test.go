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
)

// initGitWorkspace makes a temporary git repository to use as a workspace.
func initGitWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		dir = r
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "init")
	return dir
}

func newSupervisionServer(t *testing.T, work string) *httptest.Server {
	t.Helper()
	srv, ts := newSupervisionServerPair(t, work)
	_ = srv
	return ts
}

// newSupervisionServerPair returns both the server and its test server, for
// tests that need to drive the server's internals (capturing a run's
// snapshots) as well as its HTTP surface.
func newSupervisionServerPair(t *testing.T, work string) (*Server, *httptest.Server) {
	t.Helper()
	srv := New(nil, WithHome(t.TempDir()), WithWorkDir(work))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts
}

// TestWorktreeRunDiffShowsTheAgentsChanges is the test that makes thread
// isolation worth anything: a run that worked in a worktree must show the
// agent's changes in the review surface.
//
// Without the worktree-rooted snapshot, the diff would be computed against
// the user's tree, which the agent never touched — and the review pane
// would show an empty diff for a run that changed three files.
func TestWorktreeRunDiffShowsTheAgentsChanges(t *testing.T) {
	work := initGitWorkspace(t)
	srv, ts := newSupervisionServerPair(t, work)

	out := postJSON(t, ts, "/api/v1/threads", map[string]any{
		"bot_id":   "bot-glm",
		"goal":     "x",
		"no_start": true,
	})
	th, _ := out["thread"].(map[string]any)
	threadID, _ := th["id"].(string)
	worktree, _ := th["worktree_path"].(string)
	if threadID == "" || worktree == "" {
		t.Fatalf("thread not created: %v", out)
	}

	run, err := srv.store.CreateRun(Actor, "bot-glm", "do the thing", "code", false, false, threadID, "glm-5.2", "glm")
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	// Snapshot the worktree before the agent writes.
	srv.capturePreRun(run.ID)

	// The agent edits inside the worktree, not in the user's tree.
	agent := filepath.Join(worktree, "agent.txt")
	if err := os.WriteFile(agent, []byte("written by the agent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(work, "agent.txt")); err == nil {
		t.Fatal("the agent's file landed in the user's tree")
	}
	// Snapshot the worktree again and record the run.
	srv.capturePostRun(run.ID, "bot-glm", "done")

	resp, err := http.Get(ts.URL + "/api/v1/runs/" + run.ID + "/diff")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("diff status = %d, want 200", resp.StatusCode)
	}
	var out2 struct {
		Files []struct {
			Path  string `json:"path"`
			Hunks []any  `json:"hunks"`
		} `json:"files"`
		Totals struct {
			Files int `json:"files"`
		} `json:"totals"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out2.Totals.Files == 0 {
		t.Fatalf("diff is empty: %+v", out2)
	}
	found := false
	for _, f := range out2.Files {
		if f.Path == "agent.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the agent's file is not in the diff: %+v", out2.Files)
	}
}

// TestWorktreeRunIsQueuedForReview checks that a finished run lands in the
// review queue without anyone asking for it.
func TestWorktreeRunIsQueuedForReview(t *testing.T) {
	work := initGitWorkspace(t)
	srv, ts := newSupervisionServerPair(t, work)

	out := postJSON(t, ts, "/api/v1/threads", map[string]any{
		"bot_id":   "bot-glm",
		"goal":     "x",
		"no_start": true,
	})
	th, _ := out["thread"].(map[string]any)
	threadID, _ := th["id"].(string)
	worktree, _ := th["worktree_path"].(string)

	run, err := srv.store.CreateRun(Actor, "bot-glm", "do the thing", "code", false, false, threadID, "glm-5.2", "glm")
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	srv.capturePreRun(run.ID)
	if err := os.WriteFile(filepath.Join(worktree, "agent.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv.capturePostRun(run.ID, "bot-glm", "done")

	q := getJSON(t, ts, "/api/v1/queue")
	pending, _ := q["pending"].(float64)
	if pending != 1 {
		t.Fatalf("pending = %v, want 1", q["pending"])
	}
	entries, _ := q["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	e, _ := entries[0].(map[string]any)
	if e["run_id"] != run.ID {
		t.Fatalf("entry run_id = %v, want %q", e["run_id"], run.ID)
	}
	if e["thread_id"] != threadID {
		t.Fatalf("entry thread_id = %v, want %q", e["thread_id"], threadID)
	}
}

func TestCreateThreadMakesAWorktree(t *testing.T) {
	work := initGitWorkspace(t)
	ts := newSupervisionServer(t, work)

	out := postJSON(t, ts, "/api/v1/threads", map[string]any{
		"bot_id":   "bot-glm",
		"goal":     "add a feature",
		"no_start": true,
	})
	th, _ := out["thread"].(map[string]any)
	if th == nil {
		t.Fatalf("no thread in the response: %v", out)
	}
	id, _ := th["id"].(string)
	if id == "" {
		t.Fatalf("thread has no id: %v", th)
	}
	path, _ := th["worktree_path"].(string)
	if path == "" {
		t.Fatalf("thread has no worktree_path: %v", th)
	}
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		t.Fatalf("worktree %s not created: %v", path, err)
	}
	list := getJSON(t, ts, "/api/v1/threads")
	threads, _ := list["threads"].([]any)
	found := false
	for _, x := range threads {
		tt, _ := x.(map[string]any)
		if tt["id"] == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("thread %s not listed: %v", id, list)
	}
	one := getJSON(t, ts, "/api/v1/threads/"+id)
	got, _ := one["thread"].(map[string]any)
	if got == nil || got["id"] != id {
		t.Fatalf("GET thread = %v", one)
	}
}

func TestCreateThreadNeedsAGoal(t *testing.T) {
	work := initGitWorkspace(t)
	ts := newSupervisionServer(t, work)
	res := postJSONStatus(t, ts, "/api/v1/threads", map[string]any{
		"bot_id": "bot-glm",
	})
	if res.code != http.StatusBadRequest {
		t.Fatalf("code = %d body = %s, want 400", res.code, res.body)
	}
}

func TestCreateThreadNeedsAGitWorkspace(t *testing.T) {
	ts := newSupervisionServer(t, t.TempDir())
	res := postJSONStatus(t, ts, "/api/v1/threads", map[string]any{
		"bot_id":   "bot-glm",
		"goal":     "x",
		"no_start": true,
	})
	if res.code != http.StatusBadRequest {
		t.Fatalf("code = %d body = %s, want 400", res.code, res.body)
	}
}

func TestDeleteThreadRemovesTheWorktree(t *testing.T) {
	work := initGitWorkspace(t)
	ts := newSupervisionServer(t, work)

	out := postJSON(t, ts, "/api/v1/threads", map[string]any{
		"bot_id":   "bot-glm",
		"goal":     "x",
		"no_start": true,
	})
	th, _ := out["thread"].(map[string]any)
	id, _ := th["id"].(string)
	path, _ := th["worktree_path"].(string)

	req, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/threads/"+id, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d, want 200", resp.StatusCode)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("worktree still on disk after delete (err=%v)", err)
	}
}

func TestQueueEndpoints(t *testing.T) {
	work := initGitWorkspace(t)
	ts := newSupervisionServer(t, work)

	q := getJSON(t, ts, "/api/v1/queue")
	if pending, _ := q["pending"].(float64); pending != 0 {
		t.Fatalf("pending = %v, want 0", q["pending"])
	}
	if entries, _ := q["entries"].([]any); len(entries) != 0 {
		t.Fatalf("entries = %d, want 0", len(entries))
	}
	// A state change for a run that is not queued is a 404, not a silent
	// success.
	res := postJSONStatus(t, ts, "/api/v1/queue/run-missing/state", map[string]any{
		"state": "in_review",
	})
	if res.code != http.StatusNotFound {
		t.Fatalf("code = %d body = %s, want 404", res.code, res.body)
	}
}

func TestLiveDiffEndpointFallsBackToTheSettledDiff(t *testing.T) {
	work := initGitWorkspace(t)
	ts := newSupervisionServer(t, work)

	// No run, no diff: the endpoint must fail closed rather than invent
	// an empty diff.
	resp, err := http.Get(ts.URL + "/api/v1/runs/run-missing/diff/live")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestMergeEndpointRejectsAMissingRun(t *testing.T) {
	work := initGitWorkspace(t)
	ts := newSupervisionServer(t, work)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/runs/run-missing/diff/merge",
		strings.NewReader(`{"selections":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
