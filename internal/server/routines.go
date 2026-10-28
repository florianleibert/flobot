package server

import (
	"net/http"
	"time"

	"github.com/florianleibert/flobot/internal/schedule"
	"github.com/florianleibert/flobot/internal/store"
)

// routineView is a routine plus the one thing the roster could not tell the
// user before: when it will next fire.
//
// A schedule can be valid and still be the wrong one — "0 9 * * *" in a
// timezone you did not expect. Showing the next occurrence is what turns a
// parsed expression into a schedule the user can check.
type routineView struct {
	store.Routine
	// NextFire is an RFC3339 timestamp, empty when the routine has no next
	// fire (a manual or event trigger, or a trigger that does not parse).
	NextFire string `json:"next_fire,omitempty"`
}

// lastRunOf parses a routine's LastRun stamp. An unparseable or absent
// stamp means "never fired" for the purpose of computing the next fire.
func lastRunOf(rt store.Routine) time.Time {
	if rt.LastRun == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, rt.LastRun)
	if err != nil {
		return time.Time{}
	}
	return t
}

// routineViews enriches routines with their next fire time.
func routineViews(rts []store.Routine, now time.Time) []routineView {
	out := make([]routineView, 0, len(rts))
	for _, rt := range rts {
		out = append(out, routineView{
			Routine:  rt,
			NextFire: schedule.NextFireString(rt.Trigger, now, lastRunOf(rt)),
		})
	}
	return out
}

// patchRoutine handles PATCH /api/v1/routines/{id}.
//
// It accepts a new trigger and/or a new paused flag. A routine's trigger
// used to be immutable: correcting a typo meant deleting the routine and
// creating it again, which lost its id, its history and its enabled state.
// The trigger is validated here, before it is stored, with the same rule
// the create path uses — a typo'd cron is refused rather than silently
// treated as a manual routine that never fires.
func (s *Server) patchRoutine(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Trigger *string `json:"trigger"`
		Paused  *bool   `json:"paused"`
	}
	if !decodeJSON(w, r, smallBodyLimit, &body) {
		return
	}
	if body.Trigger == nil && body.Paused == nil {
		http.Error(w, `{"error":"nothing to update"}`, http.StatusBadRequest)
		return
	}
	if body.Trigger != nil {
		if err := validateTrigger(*body.Trigger); err != nil {
			s.httpError(w, err)
			return
		}
	}
	rt, err := s.store.UpdateRoutine(id, Actor, body.Trigger, body.Paused)
	if err != nil {
		s.httpError(w, err)
		return
	}
	s.persist()
	writeJSON(w, routineView{
		Routine:  rt,
		NextFire: schedule.NextFireString(rt.Trigger, time.Now(), lastRunOf(rt)),
	})
}
