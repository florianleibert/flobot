package server

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/florianleibert/flobot/internal/diff"
	"github.com/florianleibert/flobot/internal/store"
)

const (
	// liveDiffInterval is how often a running run's diff is recomputed.
	// Two seconds is frequent enough that the reviewer sees hunks appear
	// as they are written, and rare enough that a long run does not spend
	// its time in git.
	liveDiffInterval = 2 * time.Second
)

// liveStore holds the latest captured tree and unified diff per running
// run. It exists because the settled snapshot (pre/post) is only written
// when a run ends: until then there is nothing for the diff pane to read,
// which is why the pane used to stay empty for the whole run.
type liveStore struct {
	mu    sync.Mutex
	trees map[string]string
	diffs map[string]string
}

func newLiveStore() *liveStore {
	return &liveStore{
		trees: make(map[string]string),
		diffs: make(map[string]string),
	}
}

// setIfChanged stores the tree and diff for a run and reports whether they
// changed since the last poll. Publishing only on a change is what keeps
// the event stream from carrying one diff_ready per tick.
func (l *liveStore) setIfChanged(runID, tree, unified string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.diffs[runID] == unified {
		return false
	}
	l.trees[runID] = tree
	l.diffs[runID] = unified
	return true
}

// tree returns the last captured tree for a run.
func (l *liveStore) tree(runID string) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	t, ok := l.trees[runID]
	return t, ok
}

// forget drops a run's live state.
func (l *liveStore) forget(runID string) {
	l.mu.Lock()
	delete(l.trees, runID)
	delete(l.diffs, runID)
	l.mu.Unlock()
}

// startLiveDiff begins polling a run's tree for changes.
//
// Exactly one poller runs per run: the goroutine owns its own ticker and
// stops itself when the run reaches a terminal state, so a long-lived
// server never accumulates pollers for runs that already ended. The
// poller never mutates the workspace — it only reads git state — so it
// cannot interfere with the agent that is writing.
func (s *Server) startLiveDiff(ctx context.Context, runID string) {
	if s.live == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(liveDiffInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				s.live.forget(runID)
				return
			case <-ticker.C:
				if st := s.runStatus(runID); st != "running" && st != "queued" {
					s.live.forget(runID)
					return
				}
				s.pollLiveDiff(ctx, runID)
			}
		}
	}()
}

// pollLiveDiff recomputes a run's diff once and publishes it if it changed.
func (s *Server) pollLiveDiff(ctx context.Context, runID string) {
	pre, ok := s.preTree(runID)
	if !ok {
		return
	}
	cp, err := s.checkpointStoreFor(s.runRoot(runID))
	if err != nil {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, gitBudget)
	snap, err := cp.Capture(cctx)
	cancel()
	if err != nil {
		return
	}
	cctx, cancel = context.WithTimeout(ctx, gitBudget)
	unified, err := cp.Diff(cctx, pre, snap.Tree)
	cancel()
	if err != nil {
		return
	}
	if s.live.setIfChanged(runID, snap.Tree, unified) {
		s.hub.publish(store.Event{Kind: "diff_ready", Text: runID})
	}
}

// runDiffLive serves the diff of a run that is still in flight.
//
// It returns the same payload shape as the settled diff endpoint, so the
// review pane renders both with one code path. When the run has no live
// snapshot yet — the poller has not ticked, or the run already ended — it
// falls back to the settled diff, so the endpoint is always safe to call.
func (s *Server) runDiffLive(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cp, err := s.checkpointStoreFor(s.runRoot(id))
	if err != nil {
		s.httpError(w, err)
		return
	}
	pre, ok := s.preTree(id)
	if !ok {
		// No pre-tree: the run never started capturing, or the server
		// restarted. Fall back to the settled view, which knows how to
		// recover from the persisted checkpoint.
		s.runDiff(w, r)
		return
	}
	live, ok := s.live.tree(id)
	if !ok {
		s.runDiff(w, r)
		return
	}
	unified, err := cp.Diff(r.Context(), pre, live)
	if err != nil {
		s.httpError(w, err)
		return
	}
	fds := diff.Parse(unified)
	for i := range fds {
		fds[i].State = "pending"
	}
	tot := diff.TotalsOf(fds)
	writeJSON(w, map[string]any{
		"runId":  id,
		"status": s.runStatus(id),
		"files":  fds,
		"totals": tot,
		"live":   true,
	})
}
