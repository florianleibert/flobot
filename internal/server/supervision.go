package server

import (
	"os"
	"path/filepath"

	"github.com/florianleibert/flobot/internal/checkpoint"
	"github.com/florianleibert/flobot/internal/threads"
)

// runRoot returns the git root a run's snapshot must be taken in.
//
// A thread run works inside its own worktree, so its changes are in the
// worktree, not in the user's tree. Snapshotting the user's tree for such
// a run would show an empty diff — the agent's work would be invisible.
// The worktree root is therefore the run's root.
//
// An empty result means "the user's tree": a run without a worktree, or a
// worktree whose directory has since been removed.
func (s *Server) runRoot(runID string) string {
	run, err := s.store.GetRun(runID, Actor)
	if err != nil || run.Worktree == "" {
		return ""
	}
	root := s.workDirNow()
	if root == "" {
		root, _ = os.Getwd()
	}
	if wt, ok := threads.ResolveRoot(root, run.Worktree); ok {
		return wt
	}
	return ""
}

// preTree returns a run's pre-run tree.
func (s *Server) preTree(runID string) (string, bool) {
	s.snaps.mu.Lock()
	defer s.snaps.mu.Unlock()
	t, ok := s.snaps.pre[runID]
	return t, ok
}

// checkpointStoreFor returns a checkpoint store rooted at root, sharing the
// metadata directory with the workspace store.
//
// A run in a worktree needs its own store: Capture writes a temporary
// index in the root it is given, so a shared store would snapshot the
// user's tree and miss the agent's work entirely. The metadata directory
// stays shared, so checkpoints and run summaries still live in one place.
//
// An empty root means the workspace store.
func (s *Server) checkpointStoreFor(root string) (*checkpoint.Store, error) {
	if root == "" {
		return s.checkpointStore()
	}
	s.cpMu.Lock()
	defer s.cpMu.Unlock()
	if s.cpRoots == nil {
		s.cpRoots = map[string]*checkpoint.Store{}
	}
	if cp, ok := s.cpRoots[root]; ok {
		return cp, nil
	}
	dir := s.home
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	cp, err := checkpoint.New(root, filepath.Join(dir, ".flobot"))
	if err != nil {
		return nil, err
	}
	s.cpRoots[root] = cp
	return cp, nil
}
