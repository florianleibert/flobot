// eval.go is the HTTP surface for measurement: replaying stored runs through
// deterministic scorers and keeping an operator's verdict on a run.
//
// The point is that a regression must not look like a good day. Replay answers
// "do the stored events still reproduce the recorded outcome", and a verdict
// answers "was this answer actually any good", which no scorer can decide.
package server

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/florianleibert/flobot/internal/eval"
	"github.com/florianleibert/flobot/internal/store"
)

// evalVerdicts returns the lazily-initialized verdict store, bound to
// ~/.flobot/verdicts.json. A load failure is logged, not fatal: losing labels is
// bad, but refusing to start is worse.
func (s *Server) evalVerdicts() *eval.Verdicts {
	s.evalOnce.Do(func() {
		home := s.home
		if home == "" {
			home, _ = os.UserHomeDir()
		}
		v := eval.NewVerdicts("")
		if home != "" {
			v.SetPath(filepath.Join(home, ".flobot", "verdicts.json"))
		}
		if err := v.Load(); err != nil {
			log.Printf("flobot: load verdicts: %v", err)
		}
		s.verdicts = v
	})
	return s.verdicts
}

// evalRuns projects the stored runs into the evaluator's shape.
//
// ListRuns deliberately drops events to keep list payloads small, so each run is
// re-read in full: replaying a run without its events would score nothing and
// report every run as unreproduced.
func (s *Server) evalRuns() []eval.Run {
	runs := s.store.ListRuns(Actor)
	out := make([]eval.Run, 0, len(runs))
	for _, summary := range runs {
		r, err := s.store.GetRun(summary.ID, Actor)
		if err != nil {
			continue
		}
		evs := make([]eval.Event, 0, len(r.Events))
		for _, ev := range r.Events {
			isErr := ev.IsError != nil && *ev.IsError
			evs = append(evs, eval.Event{
				Kind:    ev.Kind,
				TurnID:  ev.TurnID,
				Text:    ev.Text,
				Tool:    ev.Tool,
				IsError: isErr,
			})
		}
		out = append(out, eval.Run{
			ID:     r.ID,
			BotID:  r.BotID,
			Route:  r.Route,
			Status: r.Status,
			Prompt: r.Prompt,
			Events: evs,
		})
	}
	return out
}

// replayRuns handles GET /api/v1/eval/replay. It replays every stored run
// through the default scorers and reports how many reproduced their recorded
// outcome.
func (s *Server) replayRuns(w http.ResponseWriter, r *http.Request) {
	runs := s.evalRuns()
	if id := strings.TrimSpace(r.URL.Query().Get("run")); id != "" {
		var only []eval.Run
		for _, run := range runs {
			if run.ID == id {
				only = append(only, run)
			}
		}
		runs = only
	}
	results := eval.Replay(runs)
	reproduced, total := eval.Summary(results)
	writeJSON(w, map[string]any{
		"schema":     "flobot.eval.replay.v1",
		"results":    results,
		"reproduced": reproduced,
		"total":      total,
	})
}

// recordVerdict handles POST /api/v1/runs/{id}/verdict with
// {"verdict":"good"|"bad","note":"..."}. The label is persisted, so it survives
// a restart instead of being thrown away.
func (s *Server) recordVerdict(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		http.Error(w, `{"error":"run id required"}`, http.StatusBadRequest)
		return
	}
	var body struct {
		Verdict string `json:"verdict"`
		Note    string `json:"note"`
	}
	if !decodeJSON(w, r, 8<<10, &body) {
		return
	}
	v := s.evalVerdicts()
	if err := v.Record(id, strings.TrimSpace(body.Verdict), body.Note); err != nil {
		s.httpError(w, err)
		return
	}
	s.hub.publish(store.Event{Kind: "verdict", Text: id + " " + strings.TrimSpace(body.Verdict)})
	saved, _ := v.Get(id)
	writeJSON(w, map[string]any{
		"schema":  "flobot.eval.verdict.v1",
		"verdict": saved,
	})
}

// listVerdicts handles GET /api/v1/eval/verdicts.
func (s *Server) listVerdicts(w http.ResponseWriter, r *http.Request) {
	v := s.evalVerdicts()
	rows := v.All()
	if id := strings.TrimSpace(r.URL.Query().Get("run")); id != "" {
		if one, ok := v.Get(id); ok {
			rows = []eval.Verdict{one}
		} else {
			rows = nil
		}
	}
	writeJSON(w, map[string]any{
		"schema":   "flobot.eval.verdicts.v1",
		"verdicts": rows,
	})
}
