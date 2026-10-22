// Run summaries persisted to ~/.flobot/runs.json — separate from
// state.json so the "runs and events are never persisted" contract there
// stays intact. Same 0600 + atomic-rename + leak-check pattern.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// RunSummary is one persisted run record (no events, no diffs).
type RunSummary struct {
	ID           string    `json:"id"`
	BotID        string    `json:"botId"`
	Status       string    `json:"status"`
	CreatedAt    string    `json:"createdAt"`
	EndedAt      string    `json:"endedAt,omitempty"`
	DiffSummary  *DiffStat `json:"diffSummary,omitempty"`
	CheckpointID string    `json:"checkpointId,omitempty"`
	// PostTree is the git tree captured at run end. It survives
	// restarts so the apply-time post-tree guard compares against the
	// historical post state, not a fresh capture of the workspace.
	PostTree    string    `json:"postTree,omitempty"`
	ReviewState  string    `json:"reviewState"` // pending|accepted|rejected|partial
	// Result contract (taskclient backend; zero values on executor path).
	Confidence         float64 `json:"confidence,omitempty"`
	RequiresHumanReview bool   `json:"requiresHumanReview,omitempty"`
	TestsPassed        int     `json:"testsPassed,omitempty"`
	TestsTotal        int     `json:"testsTotal,omitempty"`
}

// DiffStat is the +N/-M summary of a run's diff.
type DiffStat struct {
	Files     int `json:"files"`
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
}

// RunStore persists run summaries.
type RunStore struct {
	mu   sync.Mutex
	path string
	runs []RunSummary
}

// NewRunStore returns a run store at path (typically ~/.flobot/runs.json).
func NewRunStore(path string) *RunStore {
	return &RunStore{path: path}
}

// DefaultRunsPath returns $HOME/.flobot/runs.json (or a test override).
func DefaultRunsPath() string {
	if p := os.Getenv("FLOBOT_TEST_STATE"); p != "" {
		return filepath.Join(p, "runs.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".flobot", "runs.json")
	}
	return filepath.Join(home, ".flobot", "runs.json")
}

// Load reads persisted summaries; a missing file is not an error.
func (rs *RunStore) Load() error {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	raw, err := os.ReadFile(rs.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var runs []RunSummary
	if err := json.Unmarshal(raw, &runs); err != nil {
		return fmt.Errorf("runstore: unmarshal: %w", err)
	}
	rs.runs = runs
	return nil
}

// Save persists summaries atomically with the leak check.
func (rs *RunStore) Save() error {
	rs.mu.Lock()
	// Keep the in-memory list consistent with the on-disk cap so List()
	// and the file never disagree.
	if len(rs.runs) > 200 {
		rs.runs = rs.runs[len(rs.runs)-200:]
	}
	runs := append([]RunSummary(nil), rs.runs...)
	rs.mu.Unlock()
	raw, err := json.MarshalIndent(runs, "", "  ")
	if err != nil {
		return err
	}
	if err := assertNoLeaks(raw); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(rs.path), 0o700); err != nil {
		return err
	}
	tmp := rs.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, rs.path)
}

// Record upserts a run summary (keyed by ID).
func (rs *RunStore) Record(r RunSummary) error {
	rs.mu.Lock()
	found := false
	for i := range rs.runs {
		if rs.runs[i].ID == r.ID {
			rs.runs[i] = r
			found = true
			break
		}
	}
	if !found {
		rs.runs = append(rs.runs, r)
	}
	rs.mu.Unlock()
	return rs.Save()
}

// Mutate updates one run's summary in place under the store lock. fn
// returns false to leave the summary unchanged. It reports whether a
// summary was found and updated. Callers must use this (not
// Get/mutate/Record) for partial updates: an unlocked sequence races a
// concurrent Record and can erase fields (e.g. PostTree) with a stale copy.
func (rs *RunStore) Mutate(id string, fn func(*RunSummary) bool) bool {
	rs.mu.Lock()
	found := false
	for i := range rs.runs {
		if rs.runs[i].ID == id {
			found = true
			if fn(&rs.runs[i]) {
				break
			}
			// fn declined the change; nothing to save.
			rs.mu.Unlock()
			return true
		}
	}
	rs.mu.Unlock()
	if !found {
		return false
	}
	_ = rs.Save()
	return true
}

// SetReviewState updates one run's review state.
func (rs *RunStore) SetReviewState(id, state string) error {
	rs.mu.Lock()
	for i := range rs.runs {
		if rs.runs[i].ID == id {
			rs.runs[i].ReviewState = state
			break
		}
	}
	rs.mu.Unlock()
	return rs.Save()
}

// List returns summaries newest-first.
func (rs *RunStore) List() []RunSummary {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	out := append([]RunSummary(nil), rs.runs...)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// Get returns one summary.
func (rs *RunStore) Get(id string) (RunSummary, bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	for _, r := range rs.runs {
		if r.ID == id {
			return r, true
		}
	}
	return RunSummary{}, false
}

// ValidReviewState reports whether s is a known review state.
func ValidReviewState(s string) bool {
	switch s {
	case "pending", "accepted", "rejected", "partial":
		return true
	}
	return false
}

var _ = strings.TrimSpace
