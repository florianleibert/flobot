package store

import (
	"os"
	"path/filepath"
	"testing"
)

// M4 acceptance: the fire ledger is durable. An occurrence that ran before a
// restart must not run again after it — flobot restarts with the app.
func TestFireStoreClaimIsDurable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fires.json")
	fs := NewFireStore(path)

	if !fs.Claim("r1#2026-11-04T09:00:00Z", "r1", "run-1") {
		t.Fatal("first claim refused")
	}
	if fs.Claim("r1#2026-11-04T09:00:00Z", "r1", "run-2") {
		t.Fatal("second claim of the same occurrence accepted")
	}

	// A fresh store over the same file is what a restart looks like.
	again := NewFireStore(path)
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	if !again.Seen("r1#2026-11-04T09:00:00Z") {
		t.Fatal("the ledger forgot the occurrence across a restart")
	}
	if again.Claim("r1#2026-11-04T09:00:00Z", "r1", "run-3") {
		t.Fatal("a restarted store re-accepted a fired occurrence")
	}

	// The record carries what the UI needs to explain the fire.
	list := again.List()
	if len(list) != 1 {
		t.Fatalf("ledger = %v", list)
	}
	if list[0].RoutineID != "r1" || list[0].RunID != "run-1" || list[0].At == "" {
		t.Fatalf("record = %+v", list[0])
	}
}

// An occurrence with no identity is never deduped: a missing id must not swallow
// a real event.
func TestFireStoreEmptyIDIsNeverDeduped(t *testing.T) {
	fs := NewFireStore(filepath.Join(t.TempDir(), "fires.json"))
	for i := 0; i < 3; i++ {
		if !fs.Claim("", "r1", "run") {
			t.Fatalf("claim %d of an empty id was refused", i)
		}
	}
	if fs.Seen("") {
		t.Fatal("an empty id was recorded as seen")
	}
	if fs.Len() != 0 {
		t.Fatalf("ledger grew to %d on empty ids", fs.Len())
	}
}

// Release frees an occurrence so a fire that failed before spawning can retry.
func TestFireStoreReleaseAllowsRetry(t *testing.T) {
	fs := NewFireStore(filepath.Join(t.TempDir(), "fires.json"))
	if !fs.Claim("r1#a", "r1", "run-1") {
		t.Fatal("claim refused")
	}
	fs.Release("r1#a")
	if fs.Seen("r1#a") {
		t.Fatal("release did not forget the occurrence")
	}
	if !fs.Claim("r1#a", "r1", "run-2") {
		t.Fatal("retry after release refused")
	}
}

// The ledger is bounded so it cannot grow without limit.
func TestFireStoreIsBounded(t *testing.T) {
	fs := NewFireStore(filepath.Join(t.TempDir(), "fires.json"))
	for i := 0; i < maxFireLedger+20; i++ {
		fs.Claim(fireTestID(i), "r1", "run")
	}
	if fs.Len() != maxFireLedger {
		t.Fatalf("len = %d, want %d", fs.Len(), maxFireLedger)
	}
	if fs.Seen(fireTestID(0)) {
		t.Fatal("the oldest occurrence survived past the window")
	}
	if !fs.Seen(fireTestID(maxFireLedger + 19)) {
		t.Fatal("the newest occurrence was evicted")
	}
}

// A ledger with no path still dedupes within the process; it just cannot persist.
func TestFireStoreWithoutPathStillDedupes(t *testing.T) {
	fs := NewFireStore("")
	if !fs.Claim("r1#a", "r1", "run") {
		t.Fatal("claim refused")
	}
	if fs.Claim("r1#a", "r1", "run") {
		t.Fatal("in-memory ledger did not dedupe")
	}
}

// The ledger file is 0600 and holds no secret-looking values.
func TestFireStoreFileIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fires.json")
	fs := NewFireStore(path)
	if !fs.Claim("r1#a", "r1", "run-1") {
		t.Fatal("claim refused")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("perm = %o, want 600", perm)
	}
}

func fireTestID(i int) string { return "r1#" + string(rune('a'+i%26)) + "-" + itoa(i) }

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
