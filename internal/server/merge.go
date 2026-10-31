package server

import (
	"net/http"

	"github.com/florianleibert/flobot/internal/diff"
)

// mergeThreadDiff serves POST /api/v1/runs/{id}/diff/merge.
//
// It is the merge-back for a run that worked in a thread's worktree. The
// agent's changes live in the worktree, not in the user's tree, so the
// ordinary apply path cannot be used: that path rebuilds files that are
// already changed on disk, and here there is nothing on disk to rebuild.
//
// The merge takes each changed file's content from the worktree's PRE-RUN
// tree, applies the selected hunks to it, and writes the result into the
// user's tree. That is the same "rebuild from pre-run content plus the
// approved hunks" semantics the ordinary apply uses, with the worktree's
// pre-run content as the base.
//
// The guard: the user's tree must not have changed since the thread
// started. A merge that silently overwrote a human's concurrent edit
// would be the worst possible outcome, so it fails with 409 instead.
func (s *Server) mergeThreadDiff(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Selections []selection `json:"selections"`
	}
	if !decodeJSON(w, r, 64<<10, &body) {
		return
	}
	run, err := s.store.GetRun(id, Actor)
	if err != nil {
		s.httpError(w, err)
		return
	}
	if run.Worktree == "" {
		http.Error(w, `{"error":"run has no thread worktree; use the ordinary apply"}`, http.StatusBadRequest)
		return
	}
	thread, ok := s.threads.Store().Thread(run.Worktree)
	if !ok {
		http.Error(w, `{"error":"thread not found"}`, http.StatusNotFound)
		return
	}
	root := thread.WorktreePath
	if root == "" {
		http.Error(w, `{"error":"thread has no worktree"}`, http.StatusBadRequest)
		return
	}
	cp, err := s.checkpointStoreFor(root)
	if err != nil {
		s.httpError(w, err)
		return
	}
	pre, post, ok := s.snaps.get(id)
	if !ok {
		pre, post, ok = s.resolveRunTrees(cp, id)
		if !ok {
			http.Error(w, `{"error":"no diff for run (not a git repo, run predates Code Studio, or post-run snapshot lost)"}`, http.StatusNotFound)
			return
		}
	}
	// Serialize against reviews: two concurrent merges (or a merge racing
	// a review) must not interleave workspace writes.
	s.revMu.Lock()
	defer s.revMu.Unlock()

	// Guard: the user's tree must be exactly as it was when the thread
	// started. Anything else means a human edited it while the agent
	// worked, and merging would silently discard that edit.
	if thread.UserTree != "" {
		userStore, err := s.checkpointStore()
		if err != nil {
			s.httpError(w, err)
			return
		}
		cur, err := userStore.Capture(r.Context())
		if err != nil {
			wsError(w, err)
			return
		}
		if cur.Tree != thread.UserTree {
			http.Error(w, `{"error":"your tree changed since the thread started; review the diff before merging"}`, http.StatusConflict)
			return
		}
	}

	unified, err := cp.Diff(r.Context(), pre, post)
	if err != nil {
		s.httpError(w, err)
		return
	}
	fds := diff.Parse(unified)
	ws, err := s.workspaceWS()
	if err != nil {
		wsError(w, err)
		return
	}
	var applied, skipped []map[string]any
	for _, fd := range fds {
		sel := selection{Path: fd.Path, Hunks: nil}
		for _, s2 := range body.Selections {
			if s2.Path == fd.Path {
				sel = s2
				break
			}
		}
		idx := sel.indices(fd)
		// A rename is a whole-file decision: the worktree moved the file,
		// so the merge moves it too.
		if fd.IsRename && fd.OldPath != "" {
			if sel.Hunks != "all" && len(idx) == 0 {
				// Not selected: the user's tree keeps the old path.
				skipped = append(skipped, map[string]any{"path": fd.Path, "reason": "rename not selected"})
				continue
			}
			content, ok2, rerr := cp.ReadFile(r.Context(), post, fd.Path)
			if rerr != nil {
				skipped = append(skipped, map[string]any{"path": fd.Path, "reason": rerr.Error()})
				continue
			}
			if _, werr := ws.WriteForce(fd.Path, content); werr != nil {
				skipped = append(skipped, map[string]any{"path": fd.Path, "reason": werr.Error()})
				continue
			}
			if derr := ws.Delete(fd.OldPath); derr != nil {
				skipped = append(skipped, map[string]any{"path": fd.OldPath, "reason": derr.Error()})
				continue
			}
			applied = append(applied, map[string]any{"path": fd.Path, "reason": "rename"})
			_ = ok2
			continue
		}
		base, exists, rerr := cp.ReadFile(r.Context(), pre, fd.Path)
		if rerr != nil {
			skipped = append(skipped, map[string]any{"path": fd.Path, "reason": rerr.Error()})
			continue
		}
		res := diff.Apply(fd.Path, base, exists, fd, idx)
		if len(res.Skipped) > 0 {
			skipped = append(skipped, map[string]any{"path": fd.Path, "reason": "hunks could not be applied cleanly"})
		}
		if res.Exists {
			if _, werr := ws.WriteForce(fd.Path, res.Content); werr != nil {
				skipped = append(skipped, map[string]any{"path": fd.Path, "reason": werr.Error()})
				continue
			}
		} else if derr := ws.Delete(fd.Path); derr != nil {
			skipped = append(skipped, map[string]any{"path": fd.Path, "reason": derr.Error()})
			continue
		}
		applied = append(applied, map[string]any{"path": fd.Path, "reason": "merged from worktree"})
	}
	// The thread's work is now in the user's tree; the queue entry for it
	// can be resolved.
	if s.queue != nil {
		_ = s.queue.Resolve(id)
	}
	writeJSON(w, map[string]any{
		"runId":   id,
		"applied": applied,
		"skipped": skipped,
	})
}
