package server

import (
	"net/http"
	"os"
	"strings"

	"github.com/florianleibert/flobot/internal/executor"
	"github.com/florianleibert/flobot/internal/store"
	"github.com/florianleibert/flobot/internal/threads"
)

// createThread serves POST /api/v1/threads.
//
// A thread is a named unit of agent work with its own git worktree. The
// worktree is the point: the agent writes into it, so the user's tree is
// never touched, and two threads can run at the same time without
// contending for the same files.
//
// When the request carries a prompt, the thread starts its run inside its
// own worktree. The child's cwd stays the repository root and the worktree
// is attached with --worktree, which is how flocode resolves it.
func (s *Server) createThread(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BotID   string `json:"bot_id"`
		Goal    string `json:"goal"`
		Prompt  string `json:"prompt"`
		Intent  string `json:"intent"`
		Yolo    bool   `json:"yolo"`
		Verify  bool   `json:"verify"`
		NoStart bool   `json:"no_start"`
	}
	if !decodeJSON(w, r, runBodyLimit, &body) {
		return
	}
	goal := strings.TrimSpace(body.Goal)
	if goal == "" {
		goal = strings.TrimSpace(body.Prompt)
	}
	if goal == "" {
		http.Error(w, `{"error":"goal required"}`, http.StatusBadRequest)
		return
	}
	bot, err := s.store.GetBot(body.BotID, Actor)
	if err != nil {
		s.httpError(w, err)
		return
	}
	root := s.workDirNow()
	if root == "" {
		root, _ = os.Getwd()
	}
	th, err := s.threads.Create(r.Context(), root, bot.ID, goal)
	if err != nil {
		s.httpError(w, err)
		return
	}
	// Remember the user's tree at thread creation. The merge-back compares
	// against it: if the human edited the tree while the agent worked,
	// merging would silently discard that edit, so it fails instead.
	if cp, cerr := s.checkpointStore(); cerr == nil {
		if snap, serr := cp.Capture(r.Context()); serr == nil && snap.Tree != "" {
			th.UserTree = snap.Tree
			th, _ = s.threads.Store().UpsertThread(th)
		}
	}
	out := map[string]any{"thread": th}
	// A thread with a prompt starts its run right away; without one it is
	// an idle slot the user can start later.
	if prompt := strings.TrimSpace(body.Prompt); prompt != "" && !body.NoStart {
		run, err := s.startThreadRun(r, th, bot, prompt, body.Intent, body.Yolo, body.Verify)
		if err != nil {
			s.httpError(w, err)
			return
		}
		out["run"] = run
	}
	writeJSON(w, out)
}

// startThreadRun spawns a run inside a thread's worktree.
func (s *Server) startThreadRun(r *http.Request, th store.Thread, bot store.Bot, prompt, intent string, yolo, verify bool) (store.Run, error) {
	pin, cfgOverride, err := s.resolveRoute(r.Context(), bot.Route)
	if err != nil {
		return store.Run{}, err
	}
	if intent == "" {
		intent = "code"
	}
	if bot.Memory != "" {
		prompt = "Teammate memory (you said to remember this):\n" + bot.Memory + "\n\n" + prompt
	}
	run, err := s.store.CreateRun(Actor, bot.ID, prompt, intent, yolo, verify, th.ID, pin, bot.Route)
	if err != nil {
		return store.Run{}, err
	}
	// The child's cwd is the repository root; --worktree makes flocode
	// attach the thread's worktree and work inside it.
	s.spawn(run, executor.Request{
		Prompt:         prompt,
		Pin:            pin,
		Intent:         intent,
		Yolo:           yolo,
		Verify:         verify,
		Worktree:       threads.WorktreeName(th.ID),
		Route:          bot.Route,
		Dir:            s.workDirNow(),
		ConfigOverride: cfgOverride,
	})
	return run, nil
}

// listThreads serves GET /api/v1/threads.
func (s *Server) listThreads(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"schema":  "flobot.threads.v1",
		"threads": s.threads.Store().ListThreads(),
	})
}

// getThread serves GET /api/v1/threads/{id}.
func (s *Server) getThread(w http.ResponseWriter, r *http.Request) {
	th, ok := s.threads.Store().Thread(r.PathValue("id"))
	if !ok {
		http.Error(w, `{"error":"thread not found"}`, http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"thread": th})
}

// deleteThread serves DELETE /api/v1/threads/{id}.
//
// It removes the thread's worktree and forgets the thread. The worktree is
// removed with --force: an agent may have left uncommitted changes there,
// and those changes are the thread's output. They are not silently
// discarded — the review surface copies what the human accepts into the
// user's tree first, and removing a thread is an explicit act.
func (s *Server) deleteThread(w http.ResponseWriter, r *http.Request) {
	root := s.workDirNow()
	if root == "" {
		root, _ = os.Getwd()
	}
	if err := s.threads.Remove(r.Context(), root, r.PathValue("id")); err != nil {
		s.httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
