package server

import (
	"net/http"
	"strings"
)

// listQueue serves GET /api/v1/queue.
//
// The queue is finished-but-unreviewed work. Codex's desktop app keeps
// this list because "the agent finished something" and "the human looked
// at it" are different moments, and the gap between them is where
// supervision is lost. The pending count is what the sidebar badge shows.
func (s *Server) listQueue(w http.ResponseWriter, _ *http.Request) {
	entries := s.queue.List()
	writeJSON(w, map[string]any{
		"schema":  "flobot.queue.v1",
		"entries": entries,
		"pending": s.queue.PendingCount(),
	})
}

// setQueueState serves POST /api/v1/queue/{id}/state.
//
// The state moves an entry between pending, in_review and resolved. It
// does not apply anything: applying a hunk is the diff surface's job, and
// keeping the two apart means the queue can never leave the app in a
// state where it claims work was reviewed that was not.
func (s *Server) setQueueState(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		State string `json:"state"`
	}
	if !decodeJSON(w, r, smallBodyLimit, &body) {
		return
	}
	entry, err := s.queue.SetState(id, strings.TrimSpace(body.State))
	if err != nil {
		s.httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"entry":   entry,
		"pending": s.queue.PendingCount(),
	})
}

// resolveQueueEntry serves DELETE /api/v1/queue/{id}.
//
// Resolving drops the entry: it is history, not work. Keeping resolved
// entries would keep the badge count wrong, and a badge that lies is worse
// than no badge.
func (s *Server) resolveQueueEntry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.queue.Resolve(id); err != nil {
		s.httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "pending": s.queue.PendingCount()})
}
