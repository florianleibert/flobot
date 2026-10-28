// Package queue is the review queue: agent work that finished but still
// needs a human decision.
//
// Codex's desktop app keeps finished-but-unreviewed work in a queue rather
// than making the human go and find it. This package is that queue. It is
// a thin, honest layer over the durable ThreadStore: the queue entries
// live in ~/.flobot/threads.json next to the threads that produced them,
// so a restart does not lose a pending decision.
//
// The queue never decides anything. It records what happened (a run
// finished, these files changed, this many hunks await a verdict) and it
// tracks the verdict. The diff itself stays where it already lives — in
// the run's snapshot trees — so there is exactly one copy of the change.
package queue

import (
	"sort"
	"sync"

	"github.com/florianleibert/flobot/internal/store"
)

// Queue serves review-queue entries backed by a durable store.
type Queue struct {
	st *store.ThreadStore

	// mu serializes enqueue/resolve so two runs finishing at the same
	// moment cannot interleave their writes to the same file.
	mu sync.Mutex
}

// New returns a queue over st.
func New(st *store.ThreadStore) *Queue {
	return &Queue{st: st}
}

// Enqueue records a finished run's changes as awaiting review.
//
// A run with no changed files is not queued: there is nothing to decide,
// and an empty entry would only add noise to the badge count. Re-enqueuing
// the same run refreshes its entry rather than adding a second one.
func (q *Queue) Enqueue(threadID, runID string, files []string, hunks int) (store.QueueEntry, error) {
	if q == nil || q.st == nil {
		return store.QueueEntry{}, nil
	}
	if len(files) == 0 {
		return store.QueueEntry{}, nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.st.UpsertQueueEntry(store.QueueEntry{
		ThreadID:     threadID,
		RunID:        runID,
		FilesChanged: files,
		HunksPending: hunks,
		State:        store.QueuePending,
	})
}

// List returns every queue entry, oldest first.
func (q *Queue) List() []store.QueueEntry {
	if q == nil || q.st == nil {
		return []store.QueueEntry{}
	}
	return q.st.ListQueue()
}

// Pending returns the entries that still need a decision. This is what the
// badge counts.
func (q *Queue) Pending() []store.QueueEntry {
	if q == nil || q.st == nil {
		return []store.QueueEntry{}
	}
	return q.st.PendingQueue()
}

// PendingCount returns the number of entries awaiting a decision.
func (q *Queue) PendingCount() int {
	return len(q.Pending())
}

// SetState moves an entry to a new state: pending, in_review or resolved.
func (q *Queue) SetState(runID, state string) (store.QueueEntry, error) {
	if q == nil || q.st == nil {
		return store.QueueEntry{}, store.ErrNotFound
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.st.SetQueueState(runID, state)
}

// Resolve marks an entry resolved and drops it from the queue. A resolved
// entry is history, not work; keeping it would keep the badge count wrong.
func (q *Queue) Resolve(runID string) error {
	if q == nil || q.st == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.st.RemoveQueueEntry(runID)
	return nil
}

// FilesChanged returns the sorted, de-duplicated file list of a run's
// changes. The diff pane needs it for the "N files" label, and a stable
// order keeps that label from shuffling between refreshes.
func FilesChanged(entries []store.QueueEntry, runID string) []string {
	for _, e := range entries {
		if e.RunID != runID {
			continue
		}
		out := append([]string(nil), e.FilesChanged...)
		sort.Strings(out)
		return out
	}
	return nil
}
