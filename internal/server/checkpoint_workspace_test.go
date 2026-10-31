package server

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitInitRepo makes dir a git repository with one committed file.
func gitInitRepo(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=flobot",
		"GIT_AUTHOR_EMAIL=flobot@example.com",
		"GIT_COMMITTER_NAME=flobot",
		"GIT_COMMITTER_EMAIL=flobot@example.com",
	)
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	run("init", "-q")
	run("add", "-A")
	run("commit", "-q", "-m", "init")
}

// TestCheckpointStoreFollowsTheWorkspace is the regression test for the
// cross-workspace write.
//
// A checkpoint store is bound to one git root, and the workspace can be
// switched at runtime. A store cached from the previous workspace would
// snapshot the OLD tree and restore into it: the review surface would write
// one repository's content into another, and "revert the workspace" would
// clean the wrong tree.
func TestCheckpointStoreFollowsTheWorkspace(t *testing.T) {
	home := t.TempDir()
	start := t.TempDir()
	alpha := filepath.Join(start, "alpha")
	beta := filepath.Join(start, "beta")
	if err := os.MkdirAll(alpha, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(beta, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInitRepo(t, alpha, "a.txt", "alpha\n")
	gitInitRepo(t, beta, "b.txt", "beta\n")

	srv := New(nil, WithHome(home), WithWorkDir(alpha))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	ctx := context.Background()

	// The store starts out rooted at the first workspace.
	cpA, err := srv.checkpointStore()
	if err != nil {
		t.Fatalf("checkpointStore: %v", err)
	}
	snapA, err := cpA.Capture(ctx)
	if err != nil {
		t.Fatalf("capture in the first workspace: %v", err)
	}
	if snapA.Tree == "" {
		t.Fatal("empty tree in the first workspace")
	}

	// Switch to the other repository.
	res := postJSONStatus(t, ts, "/api/v1/workspace/select", map[string]any{"path": beta})
	if res.code != 200 {
		t.Fatalf("workspace select: %d %s", res.code, res.body)
	}

	// The store must follow the workspace: a capture now reads the NEW tree.
	// This is also what makes the merge-back guard honest — it compares the
	// current tree with the tree the thread started on, and after a switch
	// those differ, so the merge fails with 409 instead of writing the
	// agent's changes over a human's edits in another repository.
	cpB, err := srv.checkpointStore()
	if err != nil {
		t.Fatalf("checkpointStore after the switch: %v", err)
	}
	snapB, err := cpB.Capture(ctx)
	if err != nil {
		t.Fatalf("capture in the second workspace: %v", err)
	}
	if snapB.Tree == snapA.Tree {
		t.Fatalf("the checkpoint store still points at the old workspace (tree %s)", snapA.Tree)
	}
}
