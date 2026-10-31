package threads

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/florianleibert/flobot/internal/store"
)

// initRepo makes a temporary git repository with one commit.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		dir = r
	}
	run := func(args ...string) {
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
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-q", "-m", "init")
	return dir
}

func newManager(t *testing.T) *Manager {
	t.Helper()
	st := store.NewThreadStore()
	st.SetPath(filepath.Join(t.TempDir(), "threads.json"))
	return New(st)
}

func TestCreateMakesIsolatedWorktree(t *testing.T) {
	repo := initRepo(t)
	m := newManager(t)

	th, err := m.Create(context.Background(), repo, "bot-glm", "add a feature")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	want := filepath.Join(repo, filepath.FromSlash(WorktreeDir), th.ID)
	if th.WorktreePath != want {
		t.Fatalf("WorktreePath = %q, want %q", th.WorktreePath, want)
	}
	st, err := os.Stat(th.WorktreePath)
	if err != nil || !st.IsDir() {
		t.Fatalf("worktree %s not created: %v", th.WorktreePath, err)
	}
	// The worktree must be on flocode's own branch convention, so the
	// flocode child attaches to the worktree flobot made instead of
	// creating a second one.
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = th.WorktreePath
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse in worktree: %v", err)
	}
	gotBranch := strings.TrimSpace(string(out))
	wantBranch := Branch(th.ID)
	if gotBranch != wantBranch {
		t.Fatalf("branch = %q, want %q", gotBranch, wantBranch)
	}
}

func TestCreateLeavesTheUserTreeUntouched(t *testing.T) {
	repo := initRepo(t)
	m := newManager(t)

	th, err := m.Create(context.Background(), repo, "bot-glm", "add a feature")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// The agent's work happens in the worktree. The user's tree must not
	// grow a worktree directory inside it beyond the managed layout, and
	// must not gain the agent's files.
	if _, err := os.Stat(filepath.Join(repo, "agent-file.txt")); err == nil {
		t.Fatal("agent file landed in the user's tree")
	}
	// The worktree is registered with git, so `git worktree list` in the
	// user's repo shows it.
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("worktree list: %v", err)
	}
	if !strings.Contains(string(out), th.WorktreePath) {
		t.Fatalf("git worktree list does not mention %s:\n%s", th.WorktreePath, out)
	}
}

func TestCreateRecordsTheThread(t *testing.T) {
	repo := initRepo(t)
	m := newManager(t)

	th, err := m.Create(context.Background(), repo, "bot-glm", "add a feature")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, ok := m.Store().Thread(th.ID)
	if !ok {
		t.Fatalf("thread %s not recorded", th.ID)
	}
	if got.Goal != "add a feature" || got.BotID != "bot-glm" {
		t.Fatalf("thread = %+v, want the goal and bot", got)
	}
	if got.Status != store.ThreadRunning {
		t.Fatalf("status = %q, want %q", got.Status, store.ThreadRunning)
	}
	if got.WorktreePath != th.WorktreePath {
		t.Fatalf("recorded worktree path = %q, want %q", got.WorktreePath, th.WorktreePath)
	}
}

func TestCreateRejectsNonRepo(t *testing.T) {
	m := newManager(t)
	_, err := m.Create(context.Background(), t.TempDir(), "bot-glm", "x")
	if err == nil {
		t.Fatal("Create in a non-repo: err = nil, want a refusal")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("err = %v, want it to say the folder is not a git repository", err)
	}
}

func TestRemoveDeletesTheWorktree(t *testing.T) {
	repo := initRepo(t)
	m := newManager(t)

	th, err := m.Create(context.Background(), repo, "bot-glm", "x")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := m.Remove(context.Background(), repo, th.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(th.WorktreePath); !os.IsNotExist(err) {
		t.Fatalf("worktree still on disk after Remove (err=%v)", err)
	}
	if _, ok := m.Store().Thread(th.ID); ok {
		t.Fatal("thread still recorded after Remove")
	}
	if err := m.Remove(context.Background(), repo, th.ID); err == nil {
		t.Fatal("Remove of a missing thread: err = nil, want ErrNotFound")
	}
}

func TestResolveRoot(t *testing.T) {
	repo := initRepo(t)
	m := newManager(t)
	th, err := m.Create(context.Background(), repo, "bot-glm", "x")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	root, ok := ResolveRoot(repo, th.ID)
	if !ok || root != th.WorktreePath {
		t.Fatalf("ResolveRoot = %q, %v; want %q, true", root, ok, th.WorktreePath)
	}
	if _, ok := ResolveRoot(repo, "thr-does-not-exist"); ok {
		t.Fatal("ResolveRoot of a missing thread: ok = true, want false")
	}
}
