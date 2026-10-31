// Diff review endpoints: run diff, apply/reject selections, checkpoints.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/florianleibert/flobot/internal/checkpoint"
	"github.com/florianleibert/flobot/internal/diff"
	"github.com/florianleibert/flobot/internal/executor"
	"github.com/florianleibert/flobot/internal/store"
)

// gitBudget is the single time budget for one git operation set on
// this workspace (capture, diff, read-tree, apply loop). Deriving all
// per-call timeouts from it keeps capture/apply consistent: a workspace
// where apply can finish in budget must also be capturable.
const gitBudget = 2 * time.Minute

// runSnapshots holds pre/post-run git trees per run ID (in-memory; the
// pre-run tree id is also persisted in runs.json via checkpointId).
type runSnapshots struct {
	mu   sync.Mutex
	pre  map[string]string // runID -> pre-run tree
	post map[string]string // runID -> post-run tree
}

func newRunSnapshots() *runSnapshots {
	return &runSnapshots{pre: map[string]string{}, post: map[string]string{}}
}

func (rs *runSnapshots) setPre(id, tree string) {
	rs.mu.Lock()
	rs.pre[id] = tree
	rs.mu.Unlock()
}

func (rs *runSnapshots) setPost(id, tree string) {
	rs.mu.Lock()
	rs.post[id] = tree
	rs.mu.Unlock()
}

func (rs *runSnapshots) get(id string) (pre, post string, ok bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	pre, post = rs.pre[id], rs.post[id]
	return pre, post, pre != "" && post != ""
}

// evict drops a run's snapshots once its review is complete; the pre
// tree stays recoverable from the persisted checkpoint.
func (rs *runSnapshots) evict(id string) {
	rs.mu.Lock()
	delete(rs.pre, id)
	delete(rs.post, id)
	rs.mu.Unlock()
}

// checkpointStore returns the server's checkpoint store, lazily created
// under ~/.flobot (or home override). Nil-safe: when the workspace is
// not a git repo, capture fails and review is fail-closed.
func (s *Server) checkpointStore() (*checkpoint.Store, error) {
	s.cpMu.Lock()
	defer s.cpMu.Unlock()
	// A checkpoint store is bound to one git root, and the workspace can be
	// switched at runtime. A store left over from the previous workspace
	// would snapshot the old tree and restore into it — the wrong repository
	// entirely — so it is rebuilt when the root changes.
	root := s.workDirNow()
	if root == "" {
		root, _ = os.Getwd()
	}
	if s.cp != nil && s.cpRoot == root {
		return s.cp, nil
	}
	dir := s.home
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		dir = home
	}
	cp, err := checkpoint.New(root, filepath.Join(dir, ".flobot"))
	if err != nil {
		return nil, err
	}
	s.cp = cp
	s.cpRoot = root
	return cp, nil
}

// runStore returns the persisted run summaries store.
func (s *Server) runStore() *store.RunStore {
	s.rsOnce.Do(func() {
		dir := s.home
		if dir == "" {
			dir, _ = os.UserHomeDir()
		}
		if dir == "" {
			dir = "."
		}
		s.rs = store.NewRunStore(filepath.Join(dir, ".flobot", "runs.json"))
		if err := s.rs.Load(); err != nil {
			// Quarantine rather than silently overwrite: the next Save
			// would destroy whatever history was in the corrupt file.
			p := filepath.Join(dir, ".flobot", "runs.json.corrupt-"+time.Now().UTC().Format("20060102T150405"))
			if rerr := os.Rename(filepath.Join(dir, ".flobot", "runs.json"), p); rerr == nil {
				log.Printf("flobot: runs.json corrupt (%v); quarantined to %s", err, p)
			} else {
				log.Printf("flobot: runs.json corrupt (%v) and could not be quarantined (%v)", err, rerr)
			}
		}
	})
	return s.rs
}

// capturePreRun snapshots the workspace before a run. Failures are
// logged, never fatal: runs must always work; review is fail-closed.
func (s *Server) capturePreRun(runID string) {
	cp, err := s.checkpointStoreFor(s.runRoot(runID))
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitBudget)
	defer cancel()
	snap, err := cp.Capture(ctx)
	if err != nil {
		if !errors.Is(err, checkpoint.ErrNotGitRepo) {
			log.Printf("flobot: pre-run snapshot %s: %v", runID, err)
		}
		return
	}
	s.snaps.setPre(runID, snap.Tree)
}

// capturePostRun snapshots after a run, computes the diff, records the
// run summary, and publishes diff_ready.
func (s *Server) capturePostRun(runID, botID, status string) {
	// The summary is knowable even when the diff pipeline fails; a
	// transient git error must not erase the run from history.
	record := func(sum store.RunSummary) {
		s.contractsMu.Lock()
		if c, ok := s.contracts[runID]; ok {
			sum.Confidence = c.Confidence
			sum.RequiresHumanReview = c.RequiresHumanReview
			sum.TestsPassed = c.TestsPassed
			sum.TestsTotal = c.TestsTotal
			delete(s.contracts, runID)
		}
		s.contractsMu.Unlock()
		if err := s.runStore().Record(sum); err != nil {
			log.Printf("flobot: record run %s: %v", runID, err)
		}
	}
	noDiff := func() store.RunSummary {
		return store.RunSummary{ID: runID, BotID: botID, Status: status,
			CreatedAt: time.Now().UTC().Format(time.RFC3339), EndedAt: time.Now().UTC().Format(time.RFC3339)}
	}
	cp, err := s.checkpointStoreFor(s.runRoot(runID))
	if err != nil {
		record(noDiff())
		return
	}
	pre, ok := func() (string, bool) {
		s.snaps.mu.Lock()
		defer s.snaps.mu.Unlock()
		t, ok := s.snaps.pre[runID]
		return t, ok
	}()
	if !ok {
		record(noDiff())
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitBudget)
	defer cancel()
	snap, err := cp.Capture(ctx)
	if err != nil {
		if !errors.Is(err, checkpoint.ErrNotGitRepo) {
			log.Printf("flobot: post-run snapshot %s: %v", runID, err)
		}
		record(noDiff())
		return
	}
	s.snaps.setPost(runID, snap.Tree)
	unified, err := cp.Diff(ctx, pre, snap.Tree)
	if err != nil {
		log.Printf("flobot: run diff %s: %v", runID, err)
		sum := noDiff()
		sum.PostTree = snap.Tree
		sum.ReviewState = "pending"
		record(sum)
		return
	}
	fds := diff.Parse(unified)
	tot := diff.TotalsOf(fds)
	cpID := "cp-" + pre[:min(7, len(pre))] + "-" + runID
	if err := cp.Save(checkpoint.Checkpoint{
		ID:        cpID,
		RunID:     runID,
		Tree:      pre,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Files:     tot.Files,
		Additions: tot.Additions,
		Deletions: tot.Deletions,
	}); err != nil {
		log.Printf("flobot: save checkpoint %s: %v", cpID, err)
	}
	sum := store.RunSummary{
		ID:           runID,
		BotID:        botID,
		Status:       status,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
		EndedAt:      time.Now().UTC().Format(time.RFC3339),
		DiffSummary:  &store.DiffStat{Files: tot.Files, Additions: tot.Additions, Deletions: tot.Deletions},
		CheckpointID: cpID,
		PostTree:     snap.Tree,
		ReviewState:  "pending",
	}
	record(sum)
	// The run's changes are now reviewable work. Queue them so the review
	// queue shows them without the user having to go and find them.
	if s.queue != nil {
		files := make([]string, 0, len(fds))
		hunks := 0
		for _, fd := range fds {
			files = append(files, fd.Path)
			hunks += len(fd.Hunks)
		}
		threadID := ""
		if run, err := s.store.GetRun(runID, Actor); err == nil {
			threadID = run.Worktree
		}
		if _, err := s.queue.Enqueue(threadID, runID, files, hunks); err != nil {
			log.Printf("flobot: enqueue run %s: %v", runID, err)
		}
	}
	s.hub.publish(store.Event{Kind: "diff_ready", Text: runID})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// runDiff serves the parsed per-hunk diff for a run.
func (s *Server) runDiff(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cp, err := s.checkpointStoreFor(s.runRoot(id))
	if err != nil {
		s.httpError(w, err)
		return
	}
	pre, post, ok := s.snaps.get(id)
	if !ok {
		// Restart fallback: pre from the persisted checkpoint, post
		// from the persisted PostTree (never a fresh capture — that
		// would attribute later changes to this run).
		pre, post, ok = s.resolveRunTrees(cp, id)
		if !ok {
			// Fail closed: no post tree means no trustworthy diff (non-git
			// workspace, run predates Code Studio, or the post snapshot
			// was lost to a crash between run end and capturePostRun).
			http.Error(w, `{"error":"no diff for run (not a git repo, run predates Code Studio, or post-run snapshot lost)"}`, http.StatusNotFound)
			return
		}
	}
	unified, err := cp.Diff(r.Context(), pre, post)
	if err != nil {
		s.httpError(w, err)
		return
	}
	fds := diff.Parse(unified)
	for i := range fds {
		fds[i].State = "pending"
	}
	tot := diff.TotalsOf(fds)
	sum, _ := s.runStore().Get(id)
	checkpointID := ""
	if sum.CheckpointID != "" {
		checkpointID = sum.CheckpointID
	} else {
		checkpointID = "cp-" + pre[:min(7, len(pre))] + "-" + id
	}
	writeJSON(w, map[string]any{
		"runId":      id,
		"status":     s.runStatus(id),
		"files":      fds,
		"checkpoint": checkpointID,
		"totals":     tot,
		"reviewState": func() string {
			if sum.ReviewState != "" {
				return sum.ReviewState
			}
			return "pending"
		}(),
	})
}

func (s *Server) runStatus(id string) string {
	if run, err := s.store.GetRun(id, Actor); err == nil {
		return run.Status
	}
	if sum, ok := s.runStore().Get(id); ok {
		return sum.Status
	}
	return "unknown"
}

// resolveRunTrees returns the pre and post trees for a run, preferring
// in-memory snapshots. In the restart case it uses the persisted
// checkpoint for pre and the persisted PostTree for post; when the
// post tree was never captured (server died between run end and
// capturePostRun) it fails closed.
func (s *Server) resolveRunTrees(cp *checkpoint.Store, id string) (pre, post string, ok bool) {
	if pre, post, ok = s.snaps.get(id); ok {
		return pre, post, true
	}
	sum, found := s.runStore().Get(id)
	if !found {
		return "", "", false
	}
	if sum.PostTree == "" {
		// Run finished but the post snapshot was lost (crash between
		// FinishRun and capturePostRun). The workspace state at finish
		// is unknown; fail closed rather than diffing against now.
		return "", "", false
	}
	pre, ok = s.restartPre(cp, id)
	if !ok {
		return "", "", false
	}
	return pre, sum.PostTree, true
}

// restartPre recovers the pre-run tree for a run whose in-memory
// snapshots were lost to a server restart, via the persisted checkpoint.
func (s *Server) restartPre(cp *checkpoint.Store, id string) (string, bool) {
	sum, found := s.runStore().Get(id)
	if !found || sum.CheckpointID == "" {
		return "", false
	}
	c, err := cp.Get(sum.CheckpointID)
	if err != nil {
		return "", false
	}
	return c.Tree, true
}

// selection is one file's hunk selection from the apply body.
type selection struct {
	Path  string `json:"path"`
	Hunks any    `json:"hunks"` // []int | "all"
}

func (sel selection) indices(fd diff.FileDiff) []int {
	switch v := sel.Hunks.(type) {
	case string:
		if v == "all" {
			out := make([]int, len(fd.Hunks))
			for i := range fd.Hunks {
				out[i] = i
			}
			return out
		}
		return nil
	case []any:
		var out []int
		for _, x := range v {
			f, ok := x.(float64)
			if !ok {
				// Non-numeric entry: reject the whole selection
				// rather than silently dropping it (a dropped index
				// reverts a hunk the user believed they approved).
				return nil
			}
			if f != math.Trunc(f) {
				return nil
			}
			out = append(out, int(f))
		}
		return out
	}
	return nil
}

// restoreMode re-applies a path's pre-run permission bits after a
// revert write (WriteForceMode preserves the on-disk mode, which for a
// revert is the run's post-run mode, not the pre-run one).
func (s *Server) restoreMode(cp *checkpoint.Store, ctx context.Context, tree, path string) {
	if path == "" {
		return
	}
	mode, err := cp.FileMode(ctx, tree, path)
	if err != nil || mode == 0 {
		return
	}
	root := s.workDirNow()
	if root == "" {
		root, _ = os.Getwd()
	}
	_ = os.Chmod(filepath.Join(root, filepath.FromSlash(path)), mode)
}

// applyDiff applies the selected hunks of a run's diff to the workspace.
func (s *Server) applyDiff(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Selections []selection `json:"selections"`
	}
	if !decodeJSON(w, r, 64<<10, &body) {
		return
	}
	cp, err := s.checkpointStore()
	if err != nil {
		s.httpError(w, err)
		return
	}
	// Serialize reviews: two concurrent applies (or an apply racing a
	// reject) must not interleave workspace writes.
	s.revMu.Lock()
	defer s.revMu.Unlock()
	pre, post, ok := s.snaps.get(id)
	if !ok {
		// Restart fallback: pre from the persisted checkpoint, post
		// from the persisted PostTree (never a fresh capture).
		pre, post, ok = s.resolveRunTrees(cp, id)
		if !ok {
			http.Error(w, `{"error":"no diff for run (not a git repo, run predates Code Studio, or post-run snapshot lost)"}`, http.StatusNotFound)
			return
		}
	}
	// Review-state guard: a completed review must not re-execute —
	// the second apply would rebuild files from pre-run content and
	// destroy any post-apply edits.
	if sum, found := s.runStore().Get(id); found && sum.ReviewState != "" && sum.ReviewState != "pending" {
		http.Error(w, fmt.Sprintf(`{"error":"review already %s"}`, sum.ReviewState), http.StatusConflict)
		return
	}
	// Live-run guard: applying while this run (or any concurrent run
	// on the same workspace) is still writing would clobber in-flight
	// edits or destroy the other run's changes.
	if st := s.runStatus(id); st == "running" || st == "queued" {
		http.Error(w, `{"error":"run still in progress"}`, http.StatusConflict)
		return
	}
	if s.store.AnyRunningRun() {
		http.Error(w, `{"error":"another run is in progress on this workspace"}`, http.StatusConflict)
		return
	}
	// Post-tree guard: if the workspace changed after the post snapshot
	// (a human edit, or a later run), rebuilding files from pre-run
	// content would silently destroy those changes. Fail with 409.
	cur, err := cp.Capture(r.Context())
	if err != nil {
		wsError(w, err)
		return
	}
	if cur.Tree != post {
		http.Error(w, `{"error":"workspace changed since the run finished; restore a checkpoint instead"}`, http.StatusConflict)
		return
	}
	// The apply loop must survive a client disconnect: a torn apply
	// (half the files rebuilt, half still post-run) would wedge the
	// review in pending and permanently 409 the post-tree guard. The
	// request context gated entry; the mutation runs detached.
	actx, cancel := context.WithTimeout(context.Background(), gitBudget)
	defer cancel()
	unified, err := cp.Diff(actx, pre, post)
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
	// Checkpoint before touching disk. Reuse the guard's capture: the
	// tree is unchanged (the guard just verified it) and a second
	// capture would reopen the TOCTOU window.
	snap := cur
	_ = cp.Save(checkpoint.Checkpoint{
		ID:        "cp-" + snap.Tree[:min(7, len(snap.Tree))] + "-" + id,
		RunID:     id,
		Tree:      snap.Tree,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	var applied, skipped []map[string]any
	anyApplied := false
	// Apply semantics (PRD §6.2): only approved hunks land. The run's
	// changes are already on disk, so apply = rebuild each changed file
	// from its PRE-RUN content plus the selected hunks, and revert files
	// with no selected hunks to their pre-run state. This both prevents
	// double-apply and makes unselected changes disappear.
	for _, fd := range fds {
		sel := selection{Path: fd.Path, Hunks: nil}
		for _, s := range body.Selections {
			if s.Path == fd.Path {
				sel = s
				break
			}
		}
		// Renames, binaries, and hunkless diffs (mode-only changes,
		// empty-file create/delete) are whole-file decisions.
		// Selected = keep the run's outcome, unselected = revert to
		// the pre-run state.
		wholeFile := fd.IsRename || fd.IsBinary || len(fd.Hunks) == 0
		if wholeFile && fd.IsRename && len(fd.Hunks) > 0 && sel.Hunks != "all" {
			if _, ok2 := sel.Hunks.([]any); ok2 {
				// A rename with content edits is whole-file, but the
				// client sent per-hunk selections: disclose that the
				// selection is ignored rather than silently reverting.
				skipped = append(skipped, map[string]any{"path": fd.Path,
					"reason": "rename with edits is a whole-file decision; per-hunk selection ignored, file reverted"})
			}
		}
		if wholeFile {
			if sel.Hunks == "all" {
				// Keep: nothing to do, the run's outcome is on disk.
				// (A mode-only change's chmod is likewise kept.)
				anyApplied = true
				applied = append(applied, map[string]any{"path": fd.Path, "reason": "whole-file"})
			} else {
				// Revert: restore the pre-run content at the old path
				// (or the same path for a plain binary modification),
				// remove the new path. A file absent pre-run (a new
				// binary/empty file the run created) must be deleted:
				// for new files OldPath equals Path (or /dev/null), so
				// the delete is keyed on pre-tree absence, not on
				// OldPath differing from Path.
				revertPath := fd.OldPath
				if revertPath == "" || revertPath == "/dev/null" {
					revertPath = fd.Path
				}
				preContent, ok2, err := cp.ReadFile(actx, pre, revertPath)
				if err != nil {
					skipped = append(skipped, map[string]any{"path": fd.Path, "reason": "read pre-run: " + err.Error()})
					continue
				}
				if ok2 {
					if _, err := ws.WriteForce(revertPath, preContent); err != nil {
						skipped = append(skipped, map[string]any{"path": fd.Path, "reason": err.Error()})
						continue
					}
					// A rename also leaves the new path behind when the
					// old path was restored.
					if fd.Path != "" && fd.Path != revertPath {
						if err := ws.Delete(fd.Path); err != nil {
							skipped = append(skipped, map[string]any{"path": fd.Path, "reason": err.Error()})
							continue
						}
					}
				} else if fd.Path != "" {
					// Absent pre-run: the run created this file; remove it.
					if err := ws.Delete(fd.Path); err != nil {
						skipped = append(skipped, map[string]any{"path": fd.Path, "reason": err.Error()})
						continue
					}
				}
				skipped = append(skipped, map[string]any{"path": fd.Path, "reason": "reverted (whole-file)"})
			}
			continue
		}
		indices := sel.indices(fd)
		if len(indices) == 0 {
			// No hunks selected: revert the file to its pre-run state.
			preContent, ok, err := cp.ReadFile(actx, pre, fd.Path)
			if err != nil {
				skipped = append(skipped, map[string]any{"path": fd.Path, "reason": "read pre-run: " + err.Error()})
				continue
			}
			if !ok {
				// File did not exist pre-run: remove it.
				if err := ws.Delete(fd.Path); err != nil {
					skipped = append(skipped, map[string]any{"path": fd.Path, "reason": err.Error()})
				}
				continue
			}
			if _, err := ws.WriteForce(fd.Path, preContent); err != nil {
				skipped = append(skipped, map[string]any{"path": fd.Path, "reason": err.Error()})
				continue
			}
			continue
		}
		// Selected hunks: apply to the PRE-RUN content.
		preContent, ok, err := cp.ReadFile(actx, pre, fd.Path)
		if err != nil {
			skipped = append(skipped, map[string]any{"path": fd.Path, "reason": "read pre-run: " + err.Error()})
			continue
		}
		res := diff.Apply(fd.Path, preContent, ok, fd, indices)
		if len(res.Applied) == 0 {
			// Fail closed: zero hunks applied must not leave the run's
			// unapproved post content on disk. Revert to pre-run state.
			if !ok {
				if err := ws.Delete(fd.Path); err != nil {
					skipped = append(skipped, map[string]any{"path": fd.Path, "reason": err.Error()})
					continue
				}
			} else if _, err := ws.WriteForce(fd.Path, preContent); err != nil {
				skipped = append(skipped, map[string]any{"path": fd.Path, "reason": err.Error()})
				continue
			} else {
				s.restoreMode(cp, actx, pre, fd.Path)
			}
			skipped = append(skipped, map[string]any{"path": fd.Path, "hunks": res.Skipped, "reason": "no hunk applied; reverted to pre-run state"})
			continue
		}
		if fd.IsDelete {
			if err := ws.Delete(fd.Path); err != nil {
				skipped = append(skipped, map[string]any{"path": fd.Path, "reason": err.Error()})
				continue
			}
		} else {
			if _, err := ws.WriteForce(fd.Path, res.Content); err != nil {
				skipped = append(skipped, map[string]any{"path": fd.Path, "reason": err.Error()})
				continue
			}
		}
		anyApplied = true
		applied = append(applied, map[string]any{"path": fd.Path, "hunks": res.Applied})
		if len(res.Skipped) > 0 {
			skipped = append(skipped, map[string]any{"path": fd.Path, "hunks": res.Skipped, "reason": "unappliable hunks"})
		}
	}
	// Selections naming paths not in the diff are skipped.
	inDiff := map[string]bool{}
	for _, fd := range fds {
		inDiff[fd.Path] = true
	}
	for _, sel := range body.Selections {
		if !inDiff[sel.Path] {
			skipped = append(skipped, map[string]any{"path": sel.Path, "reason": "not in diff"})
		}
	}
	state := "rejected"
	if anyApplied {
		state = "partial"
		// "accepted" requires every diff file to have had selected,
		// successfully applied hunks — a file reverted (no hunks
		// selected) or a selection not in the diff means partial.
		if len(skipped) == 0 && len(applied) == len(fds) {
			state = "accepted"
		}
	}
	// Review metrics (PRD §11): hunk accept rate + decision timestamp.
	hunksTotal := 0
	for _, fd := range fds {
		if len(fd.Hunks) > 0 {
			hunksTotal += len(fd.Hunks)
		} else {
			hunksTotal++ // whole-file decision counts as one
		}
	}
	hunksAccepted := 0
	for _, a := range applied {
		if hs, ok := a["hunks"].([]int); ok {
			hunksAccepted += len(hs)
		} else {
			hunksAccepted++ // whole-file keep
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_ = s.runStore().Mutate(id, func(sum *store.RunSummary) bool {
		sum.ReviewState = state
		sum.ReviewedAt = now
		sum.HunksAccepted = hunksAccepted
		sum.HunksTotal = hunksTotal
		return true
	})
	s.snaps.evict(id)
	s.hub.publish(store.Event{Kind: "review_done", Text: id})
	writeJSON(w, map[string]any{"applied": applied, "skipped": skipped, "reviewState": state,
		"hunksAccepted": hunksAccepted, "hunksTotal": hunksTotal})
}

// rejectDiff marks a run rejected and reverts the workspace to the
// pre-run state: rejecting means the run's changes must not stay on disk.
func (s *Server) rejectDiff(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.runStore().Get(id); !ok {
		if _, err := s.store.GetRun(id, Actor); err != nil {
			http.Error(w, `{"error":"run not found"}`, http.StatusNotFound)
			return
		}
	}
	cp, err := s.checkpointStore()
	if err != nil {
		s.httpError(w, err)
		return
	}
	// Serialize reviews (same mutex as apply).
	s.revMu.Lock()
	defer s.revMu.Unlock()
	// Review-state guard: a completed review must not re-execute.
	if sum, found := s.runStore().Get(id); found && sum.ReviewState != "" && sum.ReviewState != "pending" {
		http.Error(w, fmt.Sprintf(`{"error":"review already %s"}`, sum.ReviewState), http.StatusConflict)
		return
	}
	// Live-run guard: reverting the tree under a live run (this one or
	// a concurrent one) would destroy its in-flight writes.
	if st := s.runStatus(id); st == "running" || st == "queued" {
		http.Error(w, `{"error":"run still in progress"}`, http.StatusConflict)
		return
	}
	if s.store.AnyRunningRun() {
		http.Error(w, `{"error":"another run is in progress on this workspace"}`, http.StatusConflict)
		return
	}
	// Revert to the pre-run tree when recoverable; otherwise the run's
	// changes stay and the response says so. RestoreTree captures a
	// safety checkpoint first, so the revert is itself reversible.
	safetyID := ""
	if pre, ok := s.restartPre(cp, id); ok {
		// Detached context: a client disconnect mid read-tree would
		// tear the worktree and wedge the review in pending (the same
		// hazard applyDiff's actx guards against).
		rctx, rcancel := context.WithTimeout(context.Background(), gitBudget)
		safety, err := cp.RestoreTree(rctx, pre)
		rcancel()
		if err != nil {
			s.httpError(w, err)
			return
		}
		safetyID = safety.ID
	}
	_ = s.runStore().Mutate(id, func(sum *store.RunSummary) bool {
		sum.ReviewState = "rejected"
		sum.ReviewedAt = time.Now().UTC().Format(time.RFC3339)
		sum.HunksAccepted = 0
		// Total hunks unknown here without recomputing the diff; 0
		// means "not recorded" (omitempty) rather than "zero of zero".
		return true
	})
	s.snaps.evict(id)
	s.hub.publish(store.Event{Kind: "review_done", Text: id})
	writeJSON(w, map[string]any{"runId": id, "reviewState": "rejected", "safetyCheckpoint": safetyID})
}

// listCheckpoints serves the checkpoint list.
func (s *Server) listCheckpoints(w http.ResponseWriter, r *http.Request) {
	cp, err := s.checkpointStore()
	if err != nil {
		s.httpError(w, err)
		return
	}
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n := parseIntDefault(l, 50); n > 0 {
			limit = n
		}
	}
	cps, err := cp.List()
	if err != nil {
		s.httpError(w, err)
		return
	}
	if len(cps) > limit {
		cps = cps[:limit]
	}
	writeJSON(w, map[string]any{"checkpoints": cps})
}

// restoreCheckpoint restores a checkpoint. It shares the review mutex
// and live-run guard with apply/reject: a restore interleaving an
// in-flight review, or running under a live run, would destroy the
// very writes those paths are trying to protect.
func (s *Server) restoreCheckpoint(w http.ResponseWriter, r *http.Request) {
	cp, err := s.checkpointStore()
	if err != nil {
		s.httpError(w, err)
		return
	}
	s.revMu.Lock()
	defer s.revMu.Unlock()
	if s.store.AnyRunningRun() {
		http.Error(w, `{"error":"a run is in progress on this workspace"}`, http.StatusConflict)
		return
	}
	rctx, rcancel := context.WithTimeout(context.Background(), gitBudget)
	safety, err := cp.Restore(rctx, r.PathValue("id"))
	rcancel()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, `{"error":"checkpoint not found"}`, http.StatusNotFound)
			return
		}
		s.httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"restored": r.PathValue("id"), "safetyCheckpoint": safety.ID})
}

func parseIntDefault(s string, def int) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
		if n > 10000 {
			return def
		}
	}
	if n == 0 {
		return def
	}
	return n
}

var _ = strings.TrimSpace

// runBackend dispatches a run to the selected backend: the taskclient
// adapter when configured, else the child-process executor.
func (s *Server) runBackend(ctx context.Context, req executor.Request, emit func(ev executor.Event)) executor.Result {
	if s.taskExec != nil {
		return s.taskExec.Run(ctx, req, emit)
	}
	return s.exec.Run(ctx, req, emit)
}

// noteResultContract records the taskclient result contract so the UI
// can render chips after a restart. The event arrives during the run,
// before capturePostRun writes the summary, so the payload is stashed
// and merged there; if a summary already exists (late event), it is
// updated in place. Nothing is ever fabricated.
// maxContracts caps the in-memory result-contract stash; without a cap
// runs that never reach capturePostRun leak entries forever.
const maxContracts = 200

func (s *Server) noteResultContract(runID, payload string) {
	var c contractPayload
	var raw struct {
		Confidence          float64 `json:"confidence"`
		RequiresHumanReview bool    `json:"requires_human_review"`
		TestsPassed         int     `json:"tests_passed"`
		TestsTotal          int     `json:"tests_total"`
	}
	if err := json.Unmarshal([]byte(payload), &raw); err != nil {
		return
	}
	c = contractPayload(raw)
	rs := s.runStore()
	// Locked update-in-place: a get/mutate/record sequence here races
	// capturePostRun's record and can erase PostTree with a stale copy.
	updated := rs.Mutate(runID, func(sum *store.RunSummary) bool {
		sum.Confidence = c.Confidence
		sum.RequiresHumanReview = c.RequiresHumanReview
		sum.TestsPassed = c.TestsPassed
		sum.TestsTotal = c.TestsTotal
		return true
	})
	if updated {
		return
	}
	s.contractsMu.Lock()
	if len(s.contracts) >= maxContracts {
		for k := range s.contracts { // drop one; order is arbitrary
			delete(s.contracts, k)
			break
		}
	}
	s.contracts[runID] = c
	s.contractsMu.Unlock()
}
