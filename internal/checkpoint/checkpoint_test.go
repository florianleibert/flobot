package checkpoint

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	// init a git repo
	run := func(args ...string) {
		t.Helper()
		cmd := execCommand(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("git", "init", "-q")
	run("git", "config", "user.email", "t@t")
	run("git", "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", "-A")
	run("git", "commit", "-qm", "init")
	st, err := New(dir, filepath.Join(dir, ".flobot-test"))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestCaptureDiff(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	pre, err := st.Capture(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate an agent run: modify + create + delete.
	if err := os.WriteFile(filepath.Join(st.root, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(st.root, "new.txt"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	post, err := st.Capture(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d, err := st.Diff(ctx, pre.Tree, post.Tree)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d, "a.txt") || !strings.Contains(d, "new.txt") {
		t.Errorf("diff missing files:\n%s", d)
	}
	if !strings.Contains(d, "-one") || !strings.Contains(d, "+two") {
		t.Errorf("diff missing content:\n%s", d)
	}
}

func TestCaptureDirtyTreeAttribution(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	// User has uncommitted edits BEFORE the run.
	if err := os.WriteFile(filepath.Join(st.root, "a.txt"), []byte("user edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pre, err := st.Capture(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Agent run changes only new.txt.
	if err := os.WriteFile(filepath.Join(st.root, "agent.txt"), []byte("agent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	post, err := st.Capture(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d, err := st.Diff(ctx, pre.Tree, post.Tree)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(d, "a.txt") {
		t.Errorf("user's pre-existing edit attributed to run:\n%s", d)
	}
	if !strings.Contains(d, "agent.txt") {
		t.Errorf("agent change missing:\n%s", d)
	}
}

func TestRestore(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	pre, err := st.Capture(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cp := Checkpoint{ID: "cp-test", Tree: pre.Tree, CreatedAt: "now"}
	if err := st.Save(cp); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(st.root, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(st.root, "junk.txt"), []byte("j\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	safety, err := st.Restore(ctx, "cp-test")
	if err != nil {
		t.Fatal(err)
	}
	if safety.ID == "" {
		t.Error("restore did not create safety checkpoint")
	}
	got, err := os.ReadFile(filepath.Join(st.root, "a.txt"))
	if err != nil || string(got) != "one\n" {
		t.Errorf("a.txt after restore: %q err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(st.root, "junk.txt")); !os.IsNotExist(err) {
		t.Error("junk.txt survived restore")
	}
	// The safety checkpoint restores the changed state back.
	if _, err := st.Restore(ctx, safety.ID); err != nil {
		t.Fatal(err)
	}
	got2, _ := os.ReadFile(filepath.Join(st.root, "a.txt"))
	if string(got2) != "changed\n" {
		t.Errorf("safety restore: %q", got2)
	}
}

func TestNotARepo(t *testing.T) {
	dir := t.TempDir()
	st, err := New(dir, filepath.Join(dir, "meta"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Capture(context.Background()); err != ErrNotGitRepo {
		t.Errorf("want ErrNotGitRepo, got %v", err)
	}
}

func execCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
