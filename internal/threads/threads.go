// Package threads implements the Codex-style thread model: a named unit of
// agent work that runs in its own git worktree.
//
// Codex's desktop app's central mechanic is that many tasks can be in
// flight at once, each in an isolated checkout. That is what makes the
// supervisor's job tractable: two agents never contend for the same files,
// and the user's working tree is never a contested resource.
//
// flobot does not invent a second convention. flocode already owns the
// ".flocode/worktrees/<name>" layout on the "flocode/worktree/<name>"
// branch, and the executor already passes --worktree <name> to the child.
// This package creates the worktree at exactly that path, so the child
// attaches to the worktree flobot made instead of making another one.
package threads

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/florianleibert/flobot/internal/store"
)

const (
	// WorktreeDir is the directory, relative to the repository root, that
	// holds thread worktrees. It matches flocode's own convention.
	WorktreeDir = ".flocode/worktrees"
	// BranchPrefix is the git branch prefix for a thread's worktree. It
	// matches flocode's own convention (flocode/worktree/<name>).
	BranchPrefix = "flocode/worktree/"
	// gitBudget bounds every git command the manager runs.
	gitBudget = 20 * time.Second
)

// WorktreeName is the flocode worktree name for a thread id.
func WorktreeName(threadID string) string { return threadID }

// Branch is the git branch name for a thread's worktree.
func Branch(threadID string) string { return BranchPrefix + WorktreeName(threadID) }

// WorktreePath is the absolute path of a thread's worktree inside repoRoot.
// It is the same path flocode resolves for --worktree <name>.
func WorktreePath(repoRoot, threadID string) string {
	return filepath.Join(repoRoot, filepath.FromSlash(WorktreeDir), WorktreeName(threadID))
}

// Manager creates, tracks and removes thread worktrees.
type Manager struct {
	st *store.ThreadStore
}

// New returns a manager over st.
func New(st *store.ThreadStore) *Manager { return &Manager{st: st} }

// Store returns the manager's durable store.
func (m *Manager) Store() *store.ThreadStore { return m.st }

// Create makes a thread with its own worktree and records it.
//
// The order matters: the worktree is created first, and the thread record
// is written after, so a thread is never listed without a worktree to run
// in. If the worktree cannot be created the thread is not recorded at all
// — a thread that cannot run is worse than no thread.
func (m *Manager) Create(ctx context.Context, repoRoot, botID, goal string) (store.Thread, error) {
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return store.Thread{}, fmt.Errorf("threads: abs root: %w", err)
	}
	if !isRepo(ctx, root) {
		return store.Thread{}, fmt.Errorf("threads: %s is not a git repository", root)
	}
	// A provisional thread id is needed before the worktree exists, because
	// the worktree path is derived from it.
	th, err := m.st.UpsertThread(store.Thread{BotID: botID, Goal: goal, Status: store.ThreadRunning})
	if err != nil {
		return store.Thread{}, err
	}
	path := WorktreePath(root, th.ID)
	if err := m.addWorktree(ctx, root, th.ID, path); err != nil {
		m.st.DeleteThread(th.ID)
		return store.Thread{}, err
	}
	th.WorktreePath = path
	th, err = m.st.UpsertThread(th)
	if err != nil {
		return store.Thread{}, err
	}
	return th, nil
}

// addWorktree creates the git worktree for a thread.
//
// A leftover directory at the path is removed first: git refuses to add a
// worktree over a non-empty directory, and a stale directory from a killed
// run is exactly what that looks like. The branch is created on first use
// and reused afterwards, so a thread that is recreated keeps its history.
func (m *Manager) addWorktree(ctx context.Context, root, threadID, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("threads: mkdir %s: %w", filepath.Dir(path), err)
	}
	if _, err := os.Stat(path); err == nil {
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("threads: clear %s: %w", path, err)
		}
	}
	branch := Branch(threadID)
	if _, err := git(ctx, root, "worktree", "add", "-q", "-b", branch, path); err == nil {
		return nil
	}
	// The branch exists already: attach to it rather than failing.
	if _, err := git(ctx, root, "worktree", "add", "-q", path, branch); err != nil {
		return fmt.Errorf("threads: create worktree %s: %w", threadID, err)
	}
	return nil
}

// Remove deletes the thread's worktree and forgets the thread.
//
// The worktree is removed with --force: an agent may have left uncommitted
// changes in it, and those changes are the thread's output. They are not
// silently deleted here — the review surface copies what the human accepts
// into the user's tree first. Removing a thread is an explicit user act.
func (m *Manager) Remove(ctx context.Context, repoRoot, threadID string) error {
	th, ok := m.st.Thread(threadID)
	if !ok {
		return store.ErrNotFound
	}
	if th.WorktreePath != "" {
		_, _ = git(ctx, repoRoot, "worktree", "remove", "--force", th.WorktreePath)
		_, _ = git(ctx, repoRoot, "worktree", "prune")
	}
	m.st.DeleteThread(threadID)
	return nil
}

// ResolveRoot returns the worktree root for a thread id in repoRoot, and
// whether the worktree is actually there. A run that was launched with a
// worktree whose directory has since been removed must fall back to the
// user's tree rather than diff against a path that is gone.
func ResolveRoot(repoRoot, threadID string) (string, bool) {
	if threadID == "" || repoRoot == "" {
		return "", false
	}
	path := WorktreePath(repoRoot, threadID)
	st, err := os.Stat(path)
	if err != nil || !st.IsDir() {
		return "", false
	}
	return path, true
}

// git runs a git command in root and returns its output.
func git(ctx context.Context, root string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, gitBudget)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return string(out), nil
}

// isRepo reports whether root is inside a git working tree.
func isRepo(ctx context.Context, root string) bool {
	_, err := git(ctx, root, "rev-parse", "--is-inside-work-tree")
	return err == nil
}
