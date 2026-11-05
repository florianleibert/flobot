// fire.go is the one place a routine becomes a run.
//
// Before this file there were two fire paths (the scheduler ticker and the
// integration webhook) that each re-derived "may this run?" from scratch, and
// neither could answer "did this occurrence already run?". The consequences
// were the ones the research calls out: a provider retry double-ran a routine,
// a paused or unpinned routine spawned a child that then failed and spent
// tokens discovering what was knowable up front, and nothing bounded a chain of
// fires.
//
// So every fire — scheduled or event-driven — goes through dispatchFire, which
// validates first (zero tokens spent on a fire that cannot work), claims the
// occurrence identity once (a duplicate runs once), and applies the budget,
// rate limit and recursion guard.
package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/florianleibert/flobot/internal/amesh"
	"github.com/florianleibert/flobot/internal/executor"
	"github.com/florianleibert/flobot/internal/schedule"
	"github.com/florianleibert/flobot/internal/store"
	"github.com/florianleibert/flobot/internal/threads"
)

// heartbeatInterval is how often a running fire publishes progress. A run that
// goes quiet for minutes is indistinguishable from a hung one; a heartbeat is
// what lets the UI say "still working" instead of showing nothing.
const heartbeatInterval = 15 * time.Second

// fireGate holds the state that makes fires safe to leave unattended: the
// durable occurrence ledger, a per-routine rate limit, a recursion guard and
// the global stop.
type fireGate struct {
	fires     *store.FireStore
	limiter   *schedule.Limiter
	recursion *schedule.Recursion

	mu      sync.Mutex
	stopped bool
}

// gate returns the server's fire gate, building it on first use.
func (s *Server) gate() *fireGate {
	s.gateOnce.Do(func() {
		fs := store.NewFireStore(store.DefaultFiresPath())
		if err := fs.Load(); err != nil {
			log.Printf("flobot: load fire ledger: %v", err)
		}
		s.fireGate = &fireGate{
			fires:     fs,
			limiter:   schedule.NewLimiter(0, 0),
			recursion: schedule.NewRecursion(),
		}
	})
	return s.fireGate
}

// StopAllFires is the global stop: no routine fires until ResumeFires. It is
// what "pause everything" means when the operator walks away — a per-routine
// pause cannot express "stop all of it now".
func (s *Server) StopAllFires() {
	g := s.gate()
	g.mu.Lock()
	g.stopped = true
	g.mu.Unlock()
	s.hub.publish(store.Event{Kind: "fires_stopped", Text: "all routines paused"})
}

// ResumeFires lifts the global stop.
func (s *Server) ResumeFires() {
	g := s.gate()
	g.mu.Lock()
	g.stopped = false
	g.mu.Unlock()
	s.hub.publish(store.Event{Kind: "fires_resumed", Text: "routines resumed"})
}

// FiresStopped reports whether the global stop is engaged.
func (s *Server) FiresStopped() bool {
	g := s.gate()
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.stopped
}

// FireLedger returns the occurrence ledger (read-only view for the API).
func (s *Server) FireLedger() []store.FireRecord { return s.gate().fires.List() }

// fireRequest is one pre-dispatch fire: which routine, why, and the occurrence
// identity that makes it idempotent.
type fireRequest struct {
	Routine store.Routine
	// FireID is the occurrence identity (schedule.FireID or
	// schedule.EventFireID). Empty means the occurrence has no identity and is
	// not deduped — an event with no delivery id must not be swallowed.
	FireID string
	// EventText is the external event that caused the fire (integration path).
	EventText string
	// Scheduled marks a ticker fire, which prepends the skill how-to.
	Scheduled bool
}

// dispatchFire validates a fire, claims its occurrence and spawns at most one
// run. It returns the run id ("" when the fire was skipped) and an error only
// for failures that may clear on their own.
//
// A skip is not an error: a paused routine must not back off.
func (s *Server) dispatchFire(req fireRequest) (string, error) {
	rt := req.Routine
	g := s.gate()

	if s.FiresStopped() || rt.Paused {
		return "", nil
	}
	// Already fired? Answer before anything else so a provider retry is told
	// "duplicate", not "busy".
	if g.fires.Seen(req.FireID) {
		s.noteSkip(rt.ID, schedule.SkipDuplicate)
		return "", nil
	}
	if !g.limiter.Allow(rt.ID, time.Now()) {
		s.noteSkip(rt.ID, schedule.SkipRateLimited)
		return "", nil
	}
	if err := g.recursion.Enter(rt.ID); err != nil {
		s.noteSkip(rt.ID, schedule.SkipTooDeep)
		return "", nil
	}
	defer g.recursion.Leave(rt.ID)

	bot, sk, pin, cfgOverride, skip := s.validateFire(rt)
	if skip != schedule.SkipNone {
		s.noteSkip(rt.ID, skip)
		if skip.Transient() {
			return "", skip.Error()
		}
		return "", nil
	}

	budget := schedule.DefaultBudget()
	if rt.TimeoutSeconds > 0 {
		budget.Attempt = time.Duration(rt.TimeoutSeconds) * time.Second
	}
	attempt, err := budget.AttemptTimeout(0, 0)
	if err != nil {
		s.noteSkip(rt.ID, schedule.SkipBudgetSpent)
		return "", nil
	}

	prompt := firePrompt(rt, sk, req)
	run, err := s.store.CreateRun(Actor, bot.ID, prompt, "ask", false, false, rt.Worktree, pin, bot.Route)
	if err != nil {
		return "", fmt.Errorf("routine %s run: %w", rt.ID, err)
	}
	// Claim after validation and after the run exists: a fire refused for a
	// reason that can clear must not consume its occurrence.
	if !g.fires.Claim(req.FireID, rt.ID, run.ID) {
		_ = s.store.FinishRun(run.ID, Actor, "stopped", "")
		s.noteSkip(rt.ID, schedule.SkipDuplicate)
		return "", nil
	}
	s.spawn(run, executor.Request{
		Prompt:         prompt,
		SkillBody:      skillBodyFor(req.Scheduled, sk),
		Pin:            pin,
		Intent:         "ask",
		Tools:          rt.Tools,
		Route:          bot.Route,
		Worktree:       threads.WorktreeName(rt.Worktree),
		Dir:            s.workDirNow(),
		ConfigOverride: cfgOverride,
		Timeout:        attempt,
	})
	s.hub.publish(store.Event{Kind: "routine_fire", Text: rt.ID})
	if _, err := s.store.TestRun(rt.ID, rt.Owner, run.ID, fireNote(req)); err != nil {
		return run.ID, fmt.Errorf("routine %s record fire: %w", rt.ID, err)
	}
	return run.ID, nil
}

// validateFire answers "can this fire work at all?" without spawning anything.
//
// Every check here used to be discovered by the child failing: a deleted bot, a
// draft skill, an unpinned route. Each of those spent tokens to learn something
// the store already knew.
func (s *Server) validateFire(rt store.Routine) (bot store.Bot, sk store.Skill, pin, cfgOverride string, skip schedule.SkipReason) {
	if _, _, ok := schedule.ParseTrigger(rt.Trigger); !ok && rt.IntegrationID == "" {
		return bot, sk, "", "", schedule.SkipBadTrigger
	}
	b, err := s.store.GetBot(rt.BotID, Actor)
	if err != nil {
		return bot, sk, "", "", schedule.SkipNoBot
	}
	if s.store.HasRunningRun(b.ID) {
		return bot, sk, "", "", schedule.SkipRunning
	}
	k, err := s.store.GetSkill(rt.SkillID, Actor)
	if err != nil {
		return bot, sk, "", "", schedule.SkipNoSkill
	}
	if !k.Live() {
		return bot, sk, "", "", schedule.SkipDraftSkill
	}
	// Bound route resolution: a hung amesh-web must not stall consequence-free
	// validation, let alone the scheduler tick.
	ctx, cancel := context.WithTimeout(context.Background(), amesh.FetchBudget)
	defer cancel()
	pin, cfgOverride, err = s.resolveRoute(ctx, b.Route)
	if err != nil {
		return bot, sk, "", "", schedule.SkipNoRoute
	}
	return b, k, pin, cfgOverride, schedule.SkipNone
}

// firePrompt builds the prompt for one fire. A scheduled fire tells the agent it
// is on a schedule; an integration fire carries the external event.
func firePrompt(rt store.Routine, sk store.Skill, req fireRequest) string {
	if req.Scheduled {
		return "Scheduled routine fire. Follow the skill how-to.\n\n" + sk.Contract().Render()
	}
	prompt := "Automation fire from an external integration.\n\nEvent:\n" + req.EventText
	if sk.ID != "" {
		prompt += "\n\nFollow this skill how-to:\n" + sk.Contract().Render()
	}
	return prompt
}

// skillBodyFor returns the how-to to prepend to the child's prompt. Only a
// scheduled fire prepends it: an integration prompt already carries it inline,
// and prepending twice would send the same text to the model twice.
func skillBodyFor(scheduled bool, sk store.Skill) string {
	if !scheduled || sk.ID == "" {
		return ""
	}
	return sk.Contract().Render()
}

// fireNote is what the routine card shows as its last activity note.
func fireNote(req fireRequest) string {
	if req.Scheduled {
		return "scheduled fire"
	}
	return "integration fire"
}

// noteSkip records why a fire did not run on the routine's card. A skip used to
// be invisible: the routine simply never fired and nothing said why.
func (s *Server) noteSkip(routineID string, reason schedule.SkipReason) {
	if routineID == "" || reason == schedule.SkipNone {
		return
	}
	s.hub.publish(store.Event{Kind: "routine_skip", Text: routineID + ": " + reason.String()})
}

// listFires handles GET /api/v1/fires. It returns the occurrence ledger and
// whether the global stop is engaged, so the UI can show what has fired and
// offer "resume everything".
func (s *Server) listFires(w http.ResponseWriter, r *http.Request) {
	records := s.FireLedger()
	if records == nil {
		records = []store.FireRecord{}
	}
	writeJSON(w, map[string]any{
		"schema":  "flobot.fires.v1",
		"fires":   records,
		"stopped": s.FiresStopped(),
	})
}

// stopFires handles POST /api/v1/fires/stop — the global stop. It refuses every
// subsequent fire until resume, without touching any individual routine's
// paused flag, so resuming restores exactly the previous set.
func (s *Server) stopFires(w http.ResponseWriter, r *http.Request) {
	s.StopAllFires()
	writeJSON(w, map[string]any{"schema": "flobot.fires.v1", "stopped": true})
}

// resumeFires handles POST /api/v1/fires/resume.
func (s *Server) resumeFires(w http.ResponseWriter, r *http.Request) {
	s.ResumeFires()
	writeJSON(w, map[string]any{"schema": "flobot.fires.v1", "stopped": false})
}

// startHeartbeat publishes a progress event for a running run every
// heartbeatInterval until ctx is cancelled.
//
// The event carries the run id and how long it has been going, which is the
// minimum a UI needs to distinguish "working" from "gone". It is a comment-like
// signal: it never changes the run's status and never fails the run.
func (s *Server) startHeartbeat(ctx context.Context, runID string) {
	if runID == "" {
		return
	}
	start := time.Now()
	go func() {
		t := time.NewTicker(heartbeatInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.hub.publish(store.Event{
					Kind: "heartbeat",
					Text: fmt.Sprintf("%s %ds", runID, int(time.Since(start).Seconds())),
				})
			}
		}
	}()
}
