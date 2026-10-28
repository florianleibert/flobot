package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Thread statuses.
const (
	ThreadRunning   = "running"
	ThreadDone      = "done"
	ThreadFailed    = "failed"
	ThreadCancelled = "cancelled"
)

// Review-queue entry states.
const (
	QueuePending  = "pending"
	QueueInReview = "in_review"
	QueueResolved = "resolved"
)

const (
	// maxThreads and maxQueue cap the persisted file. Each collection keeps
	// the newest entries and evicts the oldest, so a long-lived app cannot
	// grow the file without limit.
	maxThreads = 200
	maxQueue   = 200
)

// Thread is one unit of agent work. Several threads can be active at once,
// each in its own worktree, so two agents never contend for the same files.
type Thread struct {
	ID string `json:"id"`
	// BotID is the teammate the thread runs on.
	BotID string `json:"bot_id,omitempty"`
	// Goal is the task handed to the agent.
	Goal string `json:"goal,omitempty"`
	// WorktreePath is the isolated tree the thread writes into. Empty when
	// the thread runs in the user's own working tree.
	WorktreePath string `json:"worktree_path,omitempty"`
	// Status is one of ThreadRunning, ThreadDone, ThreadFailed,
	// ThreadCancelled.
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// QueueEntry is one finished-but-unreviewed change set. It is what the
// review queue shows: work the agent completed that still needs a human
// decision.
type QueueEntry struct {
	// ThreadID links the entry to the thread that produced it.
	ThreadID string `json:"thread_id,omitempty"`
	// RunID identifies the run; it is the queue entry's key.
	RunID        string   `json:"run_id"`
	FilesChanged []string `json:"files_changed,omitempty"`
	// HunksPending counts the hunks still awaiting an accept/reject.
	HunksPending int `json:"hunks_pending"`
	// State is one of QueuePending, QueueInReview, QueueResolved.
	State     string `json:"state"`
	CreatedAt string `json:"created_at"`
}

// threadsFile is the on-disk shape.
type threadsFile struct {
	Threads []Thread     `json:"threads"`
	Queue   []QueueEntry `json:"queue"`
}

// ThreadStore persists threads and review-queue entries to
// ~/.flobot/threads.json.
//
// It is a separate file from ~/.flobot/state.json on purpose. state.json's
// contract is "the roster only — runs and events are never persisted", and
// this epic needs the opposite for the supervision surface: a thread whose
// worktree and status must survive a restart, and a review queue that must
// not lose a pending decision. Splitting the file keeps the old contract
// true instead of quietly widening it.
//
// Run summaries are deliberately NOT stored here. They already persist to
// ~/.flobot/runs.json through RunStore, with the same 0600 +
// atomic-rename + leak-check pattern. Copying them into a second file
// would create two sources of truth for the same data, and the copy would
// be the one that goes stale.
//
// The file is written 0600 (directory 0700) and never contains an Owner
// field or anything that looks like a secret: the same assertNoLeaks check
// the roster uses is applied at write time, so a leak fails the write
// rather than landing on disk.
type ThreadStore struct {
	mu      sync.Mutex
	threads map[string]Thread
	queue   map[string]QueueEntry

	// path is the threads file location; empty means the default
	// ~/.flobot/threads.json.
	path string
}

// NewThreadStore returns an empty store at the default path.
func NewThreadStore() *ThreadStore {
	return &ThreadStore{
		threads: make(map[string]Thread),
		queue:   make(map[string]QueueEntry),
	}
}

// Path returns the threads file path: $HOME/.flobot/threads.json, or the
// override set via SetPath. Under go test (FLOBOT_TEST_THREADS set), an
// unset path resolves to a temp dir so tests never touch the real home.
func (t *ThreadStore) Path() string {
	if t.path != "" {
		return t.path
	}
	if p := os.Getenv("FLOBOT_TEST_THREADS"); p != "" {
		return filepath.Join(p, "threads.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".flobot", "threads.json")
	}
	return filepath.Join(home, ".flobot", "threads.json")
}

// SetPath overrides the threads file location (tests).
func (t *ThreadStore) SetPath(p string) {
	t.path = p
}

// Load reads the threads file. A missing file is not an error: the store
// starts empty. A corrupted file is handled gracefully — the error is
// returned for the caller to log and the store starts empty instead of
// crashing the app.
func (t *ThreadStore) Load() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	raw, err := os.ReadFile(t.Path())
	switch {
	case err == nil:
		var tf threadsFile
		if jerr := json.Unmarshal(raw, &tf); jerr != nil {
			t.applyDefaults()
			return fmt.Errorf("store: threads file %s is corrupted: %w", t.Path(), jerr)
		}
		t.apply(tf)
		return nil
	case errors.Is(err, os.ErrNotExist):
		t.applyDefaults()
		return nil
	default:
		t.applyDefaults()
		return fmt.Errorf("store: read threads: %w", err)
	}
}

// apply installs a parsed threads file.
func (t *ThreadStore) apply(tf threadsFile) {
	t.threads = make(map[string]Thread, len(tf.Threads))
	for _, th := range tf.Threads {
		t.threads[th.ID] = th
	}
	t.queue = make(map[string]QueueEntry, len(tf.Queue))
	for _, q := range tf.Queue {
		t.queue[q.RunID] = q
	}
}

// applyDefaults empties the store.
func (t *ThreadStore) applyDefaults() {
	t.threads = make(map[string]Thread)
	t.queue = make(map[string]QueueEntry)
}

// Save writes the threads file. The directory is created 0700 and the file
// 0600, written via a temp file and a rename so a crash mid-write cannot
// leave a half-written file behind.
func (t *ThreadStore) Save() error {
	t.mu.Lock()
	tf := threadsFile{
		Threads: make([]Thread, 0, len(t.threads)),
		Queue:   make([]QueueEntry, 0, len(t.queue)),
	}
	for _, th := range t.threads {
		tf.Threads = append(tf.Threads, th)
	}
	for _, q := range t.queue {
		tf.Queue = append(tf.Queue, q)
	}
	t.mu.Unlock()

	sort.Slice(tf.Threads, func(i, j int) bool {
		if tf.Threads[i].CreatedAt != tf.Threads[j].CreatedAt {
			return tf.Threads[i].CreatedAt < tf.Threads[j].CreatedAt
		}
		return tf.Threads[i].ID < tf.Threads[j].ID
	})
	sort.Slice(tf.Queue, func(i, j int) bool {
		if tf.Queue[i].CreatedAt != tf.Queue[j].CreatedAt {
			return tf.Queue[i].CreatedAt < tf.Queue[j].CreatedAt
		}
		return tf.Queue[i].RunID < tf.Queue[j].RunID
	})
	tf.Threads = evictOldest(tf.Threads, maxThreads, func(x Thread) string { return x.CreatedAt })
	tf.Queue = evictOldest(tf.Queue, maxQueue, func(x QueueEntry) string { return x.CreatedAt })

	raw, err := json.MarshalIndent(tf, "", "  ")
	if err != nil {
		return fmt.Errorf("store: marshal threads: %w", err)
	}
	if err := assertNoLeaks(raw); err != nil {
		return err
	}

	path := t.Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("store: mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("store: write threads: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("store: rename threads: %w", err)
	}
	return nil
}

// evictOldest drops the oldest entries past limit. The slice must already
// be sorted oldest-first.
func evictOldest[T any](items []T, limit int, createdAt func(T) string) []T {
	if len(items) <= limit {
		return items
	}
	return items[len(items)-limit:]
}

// UpsertThread stores a thread, replacing any existing record with the same
// id. A thread with no id gets one.
func (t *ThreadStore) UpsertThread(th Thread) (Thread, error) {
	t.mu.Lock()
	if th.ID == "" {
		th.ID = newID("thr")
	}
	if th.CreatedAt == "" {
		th.CreatedAt = nowRFC3339()
	}
	th.UpdatedAt = nowRFC3339()
	if th.Status == "" {
		th.Status = ThreadRunning
	}
	t.threads[th.ID] = th
	t.mu.Unlock()
	return th, t.Save()
}

// Thread returns one thread by id.
func (t *ThreadStore) Thread(id string) (Thread, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	th, ok := t.threads[id]
	return th, ok
}

// ListThreads returns every thread, oldest first.
func (t *ThreadStore) ListThreads() []Thread {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Thread, 0, len(t.threads))
	for _, th := range t.threads {
		out = append(out, th)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt < out[j].CreatedAt
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// DeleteThread removes a thread. It reports whether a thread was removed.
func (t *ThreadStore) DeleteThread(id string) bool {
	t.mu.Lock()
	_, ok := t.threads[id]
	delete(t.threads, id)
	t.mu.Unlock()
	if !ok {
		return false
	}
	_ = t.Save()
	return true
}

// UpsertQueueEntry stores a review-queue entry, replacing any entry for
// the same run. An entry with no state starts pending.
func (t *ThreadStore) UpsertQueueEntry(q QueueEntry) (QueueEntry, error) {
	t.mu.Lock()
	if q.RunID == "" {
		t.mu.Unlock()
		return QueueEntry{}, fmt.Errorf("store: queue entry needs a run id")
	}
	if q.State == "" {
		q.State = QueuePending
	}
	if q.CreatedAt == "" {
		q.CreatedAt = nowRFC3339()
	}
	t.queue[q.RunID] = q
	t.mu.Unlock()
	return q, t.Save()
}

// QueueEntryFor returns one queue entry by run id.
func (t *ThreadStore) QueueEntryFor(runID string) (QueueEntry, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	q, ok := t.queue[runID]
	return q, ok
}

// ListQueue returns every queue entry, oldest first.
func (t *ThreadStore) ListQueue() []QueueEntry {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]QueueEntry, 0, len(t.queue))
	for _, q := range t.queue {
		out = append(out, q)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt < out[j].CreatedAt
		}
		return out[i].RunID < out[j].RunID
	})
	return out
}

// PendingQueue returns the queue entries that still need a decision.
func (t *ThreadStore) PendingQueue() []QueueEntry {
	out := []QueueEntry{}
	for _, q := range t.ListQueue() {
		if q.State != QueueResolved {
			out = append(out, q)
		}
	}
	return out
}

// SetQueueState moves a queue entry to a new state.
func (t *ThreadStore) SetQueueState(runID, state string) (QueueEntry, error) {
	t.mu.Lock()
	q, ok := t.queue[runID]
	if !ok {
		t.mu.Unlock()
		return QueueEntry{}, ErrNotFound
	}
	switch state {
	case QueuePending, QueueInReview, QueueResolved:
	default:
		t.mu.Unlock()
		return QueueEntry{}, fmt.Errorf("store: unknown queue state %q", state)
	}
	q.State = state
	t.queue[runID] = q
	t.mu.Unlock()
	return q, t.Save()
}

// RemoveQueueEntry drops a queue entry. It reports whether one was removed.
func (t *ThreadStore) RemoveQueueEntry(runID string) bool {
	t.mu.Lock()
	_, ok := t.queue[runID]
	delete(t.queue, runID)
	t.mu.Unlock()
	if !ok {
		return false
	}
	_ = t.Save()
	return true
}
