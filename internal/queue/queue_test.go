package queue

import (
	"path/filepath"
	"testing"

	"github.com/florianleibert/flobot/internal/store"
)

func newQueue(t *testing.T) *Queue {
	t.Helper()
	st := store.NewThreadStore()
	st.SetPath(filepath.Join(t.TempDir(), "threads.json"))
	return New(st)
}

func TestEnqueueAddsPendingEntry(t *testing.T) {
	q := newQueue(t)
	entry, err := q.Enqueue("thr-1", "run-1", []string{"a.go", "b.go"}, 3)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if entry.State != store.QueuePending {
		t.Fatalf("state = %q, want %q", entry.State, store.QueuePending)
	}
	if entry.HunksPending != 3 || len(entry.FilesChanged) != 2 {
		t.Fatalf("entry = %+v, want 3 hunks and 2 files", entry)
	}
	if q.PendingCount() != 1 {
		t.Fatalf("pending = %d, want 1", q.PendingCount())
	}
}

func TestEnqueueSkipsRunsWithNoChanges(t *testing.T) {
	q := newQueue(t)
	if _, err := q.Enqueue("thr-1", "run-1", nil, 0); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	// A run with no changed files has nothing to decide. Queueing it would
	// only add noise to the badge count.
	if q.PendingCount() != 0 {
		t.Fatalf("pending = %d, want 0", q.PendingCount())
	}
	if len(q.List()) != 0 {
		t.Fatalf("entries = %d, want 0", len(q.List()))
	}
}

func TestEnqueueRefreshesTheSameRun(t *testing.T) {
	q := newQueue(t)
	if _, err := q.Enqueue("thr-1", "run-1", []string{"a.go"}, 1); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := q.Enqueue("thr-1", "run-1", []string{"a.go", "b.go"}, 2); err != nil {
		t.Fatalf("Enqueue again: %v", err)
	}
	entries := q.List()
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1 (a second entry for the same run)", len(entries))
	}
	if entries[0].HunksPending != 2 {
		t.Fatalf("hunks = %d, want the refreshed 2", entries[0].HunksPending)
	}
}

func TestSetStateMovesAnEntry(t *testing.T) {
	q := newQueue(t)
	if _, err := q.Enqueue("thr-1", "run-1", []string{"a.go"}, 1); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := q.SetState("run-1", store.QueueInReview); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	entry, ok := q.List()[0], true
	if !ok || entry.State != store.QueueInReview {
		t.Fatalf("entry = %+v, want state %q", entry, store.QueueInReview)
	}
	// In review is still pending work: the badge must not drop.
	if q.PendingCount() != 1 {
		t.Fatalf("pending = %d, want 1 while the entry is in review", q.PendingCount())
	}
	if _, err := q.SetState("run-1", store.QueueResolved); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if q.PendingCount() != 0 {
		t.Fatalf("pending = %d, want 0 once resolved", q.PendingCount())
	}
}

func TestSetStateRejectsUnknownState(t *testing.T) {
	q := newQueue(t)
	if _, err := q.Enqueue("thr-1", "run-1", []string{"a.go"}, 1); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := q.SetState("run-1", "nonsense"); err == nil {
		t.Fatal("SetState with an unknown state: err = nil, want a refusal")
	}
	if _, err := q.SetState("run-missing", store.QueuePending); err == nil {
		t.Fatal("SetState on a missing entry: err = nil, want ErrNotFound")
	}
}

func TestResolveDropsTheEntry(t *testing.T) {
	q := newQueue(t)
	if _, err := q.Enqueue("thr-1", "run-1", []string{"a.go"}, 1); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := q.Resolve("run-1"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(q.List()) != 0 {
		t.Fatalf("entries = %d, want 0 after resolve", len(q.List()))
	}
	if q.PendingCount() != 0 {
		t.Fatalf("pending = %d, want 0", q.PendingCount())
	}
}

func TestQueueSurvivesANewStore(t *testing.T) {
	dir := t.TempDir()
	st := store.NewThreadStore()
	st.SetPath(filepath.Join(dir, "threads.json"))
	q := New(st)
	if _, err := q.Enqueue("thr-1", "run-1", []string{"a.go"}, 2); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	// A restart must not lose a pending decision.
	reloaded := New(mustStore(t, dir))
	if reloaded.PendingCount() != 1 {
		t.Fatalf("pending after reload = %d, want 1", reloaded.PendingCount())
	}
}

func mustStore(t *testing.T, dir string) *store.ThreadStore {
	t.Helper()
	st := store.NewThreadStore()
	st.SetPath(filepath.Join(dir, "threads.json"))
	if err := st.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return st
}
