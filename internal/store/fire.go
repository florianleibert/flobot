// Fire ledger: which scheduled occurrences already ran, persisted to
// ~/.flobot/fires.json.
//
// The scheduler keeps an in-memory dedupe, which is enough for one process. It
// is not enough across a restart: flobot restarts with the app, and an
// occurrence that fired at 09:00 must not fire again because the process that
// recorded it is gone. The ledger is the durable half of fire identity.
//
// Same 0600 + atomic-rename + leak-check pattern as the other stores, and its
// own file so state.json's "the roster only" contract stays intact.
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

// maxFireLedger bounds the ledger. An occurrence older than the newest few
// hundred can no longer be observed by the ticker, so keeping more would only
// grow the file.
const maxFireLedger = 512

// FireRecord is one occurrence that ran.
type FireRecord struct {
	// ID is the occurrence identity (schedule.FireID / schedule.EventFireID).
	ID        string `json:"id"`
	RoutineID string `json:"routine_id,omitempty"`
	RunID     string `json:"run_id,omitempty"`
	At        string `json:"at"`
}

// FireStore persists the fire ledger.
type FireStore struct {
	mu      sync.Mutex
	path    string
	records []FireRecord
}

// NewFireStore returns a ledger at path (typically ~/.flobot/fires.json).
func NewFireStore(path string) *FireStore {
	return &FireStore{path: path}
}

// DefaultFiresPath returns $HOME/.flobot/fires.json (or a test override).
func DefaultFiresPath() string {
	if p := os.Getenv("FLOBOT_TEST_STATE"); p != "" {
		return filepath.Join(p, "fires.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".flobot", "fires.json")
	}
	return filepath.Join(home, ".flobot", "fires.json")
}

// SetPath overrides the ledger location (tests).
func (fs *FireStore) SetPath(p string) {
	fs.mu.Lock()
	fs.path = p
	fs.mu.Unlock()
}

// Load reads the ledger; a missing file is not an error.
func (fs *FireStore) Load() error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	raw, err := os.ReadFile(fs.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var records []FireRecord
	if err := json.Unmarshal(raw, &records); err != nil {
		return fmt.Errorf("firestore: unmarshal: %w", err)
	}
	fs.records = records
	return nil
}

// Seen reports whether id already fired. An empty id is never seen: an
// occurrence with no identity must not be silently swallowed.
func (fs *FireStore) Seen(id string) bool {
	if id == "" {
		return false
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	for _, r := range fs.records {
		if r.ID == id {
			return true
		}
	}
	return false
}

// Claim records id as fired and reports whether it was new. A false return
// means this occurrence already ran and must not run again.
//
// Claim is the single decision point: it both answers and records, so two
// concurrent callers cannot both be told "new".
func (fs *FireStore) Claim(id, routineID, runID string) bool {
	if id == "" {
		return true
	}
	fs.mu.Lock()
	for _, r := range fs.records {
		if r.ID == id {
			fs.mu.Unlock()
			return false
		}
	}
	fs.records = append(fs.records, FireRecord{
		ID:        id,
		RoutineID: routineID,
		RunID:     runID,
		At:        nowRFC3339(),
	})
	if len(fs.records) > maxFireLedger {
		fs.records = append(fs.records[:0], fs.records[len(fs.records)-maxFireLedger:]...)
	}
	records := append([]FireRecord(nil), fs.records...)
	path := fs.path
	fs.mu.Unlock()
	return saveFireRecords(path, records) == nil
}

// Release forgets id so a fire that failed before spawning can be retried.
// Without this a transient failure would permanently consume the occurrence.
func (fs *FireStore) Release(id string) {
	if id == "" {
		return
	}
	fs.mu.Lock()
	out := fs.records[:0]
	for _, r := range fs.records {
		if r.ID != id {
			out = append(out, r)
		}
	}
	fs.records = out
	records := append([]FireRecord(nil), fs.records...)
	path := fs.path
	fs.mu.Unlock()
	_ = saveFireRecords(path, records)
}

// List returns the ledger, newest last.
func (fs *FireStore) List() []FireRecord {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return append([]FireRecord(nil), fs.records...)
}

// Len is the number of remembered occurrences.
func (fs *FireStore) Len() int {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return len(fs.records)
}

// saveFireRecords writes the ledger atomically with the leak check. An empty
// path means "no persistence" (a test server without a home), which is not an
// error: the in-memory ledger still dedupes within the process.
func saveFireRecords(path string, records []FireRecord) error {
	if path == "" {
		return nil
	}
	sort.Slice(records, func(i, j int) bool { return records[i].At < records[j].At })
	if records == nil {
		records = []FireRecord{}
	}
	raw, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	if err := assertNoLeaks(raw); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("firestore: mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("firestore: write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("firestore: rename: %w", err)
	}
	return nil
}
