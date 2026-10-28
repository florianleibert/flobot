package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestThreadStore(t *testing.T) *ThreadStore {
	t.Helper()
	ts := NewThreadStore()
	// A nested path so the directory does not exist yet: the store creates
	// it, and the created directory's mode is what the permission test
	// checks. MkdirAll does not change the mode of a directory that
	// already exists.
	ts.SetPath(filepath.Join(t.TempDir(), ".flobot", "threads.json"))
	return ts
}

func TestThreadStoreRoundTrip(t *testing.T) {
	ts := newTestThreadStore(t)
	th, err := ts.UpsertThread(Thread{BotID: "bot-glm", Goal: "add a feature"})
	if err != nil {
		t.Fatalf("UpsertThread: %v", err)
	}
	if th.ID == "" {
		t.Fatal("thread got no id")
	}
	if th.Status != ThreadRunning {
		t.Fatalf("status = %q, want %q", th.Status, ThreadRunning)
	}
	if th.CreatedAt == "" || th.UpdatedAt == "" {
		t.Fatalf("timestamps not set: %+v", th)
	}

	if _, err := ts.UpsertQueueEntry(QueueEntry{
		ThreadID:     th.ID,
		RunID:        "run-1",
		FilesChanged: []string{"a.go", "b.go"},
		HunksPending: 3,
	}); err != nil {
		t.Fatalf("UpsertQueueEntry: %v", err)
	}

	// A fresh store over the same file must see both records: that is the
	// whole point of the file.
	reloaded := NewThreadStore()
	reloaded.SetPath(ts.Path())
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, ok := reloaded.Thread(th.ID)
	if !ok {
		t.Fatalf("thread %s not found after reload", th.ID)
	}
	if got.Goal != "add a feature" || got.BotID != "bot-glm" {
		t.Fatalf("thread = %+v, want the same goal and bot", got)
	}
	entries := reloaded.ListQueue()
	if len(entries) != 1 {
		t.Fatalf("got %d queue entries, want 1", len(entries))
	}
	if entries[0].RunID != "run-1" || entries[0].HunksPending != 3 {
		t.Fatalf("queue entry = %+v, want run-1 with 3 pending hunks", entries[0])
	}
	if entries[0].State != QueuePending {
		t.Fatalf("state = %q, want %q", entries[0].State, QueuePending)
	}
}

func TestThreadStoreFilePermissions(t *testing.T) {
	ts := newTestThreadStore(t)
	if _, err := ts.UpsertThread(Thread{Goal: "x"}); err != nil {
		t.Fatalf("UpsertThread: %v", err)
	}
	fi, err := os.Stat(ts.Path())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file mode = %o, want 600", perm)
	}
	di, err := os.Stat(filepath.Dir(ts.Path()))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Fatalf("dir mode = %o, want 700", perm)
	}
}

func TestThreadStoreNeverPersistsSecrets(t *testing.T) {
	ts := newTestThreadStore(t)
	if _, err := ts.UpsertThread(Thread{Goal: "rename a helper"}); err != nil {
		t.Fatalf("UpsertThread: %v", err)
	}
	if _, err := ts.UpsertQueueEntry(QueueEntry{RunID: "run-1"}); err != nil {
		t.Fatalf("UpsertQueueEntry: %v", err)
	}
	raw, err := os.ReadFile(ts.Path())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	low := strings.ToLower(string(raw))
	for _, leak := range []string{`"owner"`, "llm_", "api_key", "secret"} {
		if strings.Contains(low, leak) {
			t.Errorf("persisted file contains %q:\n%s", leak, raw)
		}
	}
}

func TestThreadStoreRefusesToPersistSecret(t *testing.T) {
	ts := newTestThreadStore(t)
	// The leak check is fail-closed: a goal that mentions a secret name
	// must not land on disk.
	if _, err := ts.UpsertThread(Thread{Goal: "rotate the api_key"}); err == nil {
		t.Fatal("UpsertThread with a secret-shaped goal: err = nil, want a refusal")
	}
	if _, err := os.Stat(ts.Path()); !os.IsNotExist(err) {
		t.Fatalf("file exists after a refused write (err=%v)", err)
	}
}

func TestThreadStoreCorruptFileIsGraceful(t *testing.T) {
	ts := newTestThreadStore(t)
	if err := os.MkdirAll(filepath.Dir(ts.Path()), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(ts.Path(), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	err := ts.Load()
	if err == nil {
		t.Fatal("Load: err = nil, want a corruption error")
	}
	// The store must still be usable: no crash, empty state.
	if got := ts.ListThreads(); len(got) != 0 {
		t.Fatalf("got %d threads after a corrupt load, want 0", len(got))
	}
	if _, err := ts.UpsertThread(Thread{Goal: "recovered"}); err != nil {
		t.Fatalf("UpsertThread after corrupt load: %v", err)
	}
}

func TestThreadStoreMissingFileIsNotAnError(t *testing.T) {
	ts := newTestThreadStore(t)
	if err := ts.Load(); err != nil {
		t.Fatalf("Load: %v, want nil for a missing file", err)
	}
	if got := ts.ListThreads(); len(got) != 0 {
		t.Fatalf("got %d threads, want 0", len(got))
	}
}

func TestThreadStoreQueueState(t *testing.T) {
	ts := newTestThreadStore(t)
	if _, err := ts.UpsertQueueEntry(QueueEntry{RunID: "run-1", HunksPending: 2}); err != nil {
		t.Fatalf("UpsertQueueEntry: %v", err)
	}
	if got := len(ts.PendingQueue()); got != 1 {
		t.Fatalf("pending = %d, want 1", got)
	}
	if _, err := ts.SetQueueState("run-1", QueueInReview); err != nil {
		t.Fatalf("SetQueueState: %v", err)
	}
	if got := len(ts.PendingQueue()); got != 1 {
		t.Fatalf("pending = %d after in_review, want 1", got)
	}
	if _, err := ts.SetQueueState("run-1", QueueResolved); err != nil {
		t.Fatalf("SetQueueState: %v", err)
	}
	if got := len(ts.PendingQueue()); got != 0 {
		t.Fatalf("pending = %d after resolved, want 0", got)
	}
	if _, err := ts.SetQueueState("run-1", "nonsense"); err == nil {
		t.Fatal("SetQueueState with an unknown state: err = nil, want a refusal")
	}
	if _, err := ts.SetQueueState("run-missing", QueuePending); err == nil {
		t.Fatal("SetQueueState on a missing entry: err = nil, want ErrNotFound")
	}
}

func TestThreadStoreDelete(t *testing.T) {
	ts := newTestThreadStore(t)
	th, err := ts.UpsertThread(Thread{Goal: "x"})
	if err != nil {
		t.Fatalf("UpsertThread: %v", err)
	}
	if !ts.DeleteThread(th.ID) {
		t.Fatal("DeleteThread = false, want true")
	}
	if ts.DeleteThread(th.ID) {
		t.Fatal("second DeleteThread = true, want false")
	}
	if _, ok := ts.Thread(th.ID); ok {
		t.Fatal("thread still present after delete")
	}
}

func TestThreadStoreDefaultPathIsUnderFlobotHome(t *testing.T) {
	ts := NewThreadStore()
	t.Setenv("FLOBOT_TEST_THREADS", "")
	t.Setenv("HOME", "/tmp/flobot-home-test")
	p := ts.Path()
	if !strings.HasSuffix(p, filepath.Join(".flobot", "threads.json")) {
		t.Fatalf("path = %q, want it to end in .flobot/threads.json", p)
	}
}
