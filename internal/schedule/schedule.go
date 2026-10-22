// Package schedule fires enabled routines on "every <duration>" triggers.
// Each fire is a fresh flocode child (cron = fresh agent, Hermes-style):
// no state carries over between fires.
package schedule

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ParseEvery parses a trigger of the form "every 5m", "every 30s", "every 2h".
// It returns the interval. Any other trigger returns ok=false (not scheduled).
func ParseEvery(trigger string) (time.Duration, bool) {
	trigger = strings.TrimSpace(strings.ToLower(trigger))
	if !strings.HasPrefix(trigger, "every ") {
		return 0, false
	}
	spec := strings.TrimSpace(strings.TrimPrefix(trigger, "every "))
	d, err := time.ParseDuration(spec)
	if err != nil || d <= 0 {
		return 0, false
	}
	if d < time.Minute {
		d = time.Minute // floor: a routine never fires more than once a minute
	}
	return d, true
}

// Fireable is what the scheduler needs to fire a routine.
type Fireable struct {
	RoutineID string
	BotID     string
	SkillID   string
	Trigger   string
	// LastFire is the last time this routine fired (zero = never).
	LastFire time.Time
}

// Fired is one scheduled fire handed to the callback.
type Fired struct {
	RoutineID string
	BotID     string
	SkillID   string
}

// Scheduler ticks and fires due routines. It never fires a routine twice
// concurrently for the same tick, and a fire callback error is recorded and
// skipped, never fatal.
type Scheduler struct {
	// Tick is the polling interval. Zero defaults to 30s.
	Tick time.Duration

	// Now is overridable in tests.
	Now func() time.Time

	mu     sync.Mutex
	last   map[string]time.Time
	fails  map[string]int
	stop   chan struct{}
	stopMu sync.Mutex
}

// New returns a scheduler. Call Start to begin ticking.
func New() *Scheduler {
	return &Scheduler{
		Tick:  30 * time.Second,
		Now:   time.Now,
		last:  make(map[string]time.Time),
		fails: make(map[string]int),
	}
}

// Due returns the fireable routines that are due now.
func (s *Scheduler) Due(items []Fireable) []Fired {
	now := s.now()
	var out []Fired
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range items {
		d, ok := ParseEvery(it.Trigger)
		if !ok {
			continue
		}
		last, seen := s.last[it.RoutineID]
		if !seen {
			// First observation: fire on the next interval, not immediately,
			// so enabling a routine never triggers an instant burst.
			s.last[it.RoutineID] = now
			continue
		}
		if it.LastFire.After(last) {
			last = it.LastFire
		}
		if now.Sub(last) >= d {
			out = append(out, Fired{RoutineID: it.RoutineID, BotID: it.BotID, SkillID: it.SkillID})
		}
	}
	return out
}

// MarkFired records a successful fire and clears any backoff state.
func (s *Scheduler) MarkFired(routineID string) {
	s.mu.Lock()
	s.last[routineID] = s.now()
	delete(s.fails, routineID)
	s.mu.Unlock()
}

func (s *Scheduler) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Start runs the tick loop until ctx is cancelled. Each due routine fires in
// its own goroutine so one slow run never blocks the others. A fire error
// backs the routine off exponentially (tick → 8× tick) instead of retrying
// every tick.
func (s *Scheduler) Start(ctx context.Context, list func() []Fireable, fire func(Fired) error) {
	tick := s.Tick
	if tick <= 0 {
		tick = 30 * time.Second
	}
	go func() {
		t := time.NewTicker(tick)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				for _, f := range s.Due(list()) {
					go func(f Fired) {
						if err := fire(f); err == nil {
							s.MarkFired(f.RoutineID)
							return
						}
						// Backoff: pretend the routine just fired so the next
						// attempt is one interval out, doubling per failure.
						s.mu.Lock()
						s.fails[f.RoutineID]++
						backoff := tick << min(s.fails[f.RoutineID]-1, 3) // cap at 8×
						s.last[f.RoutineID] = s.now().Add(backoff - tick)
						s.mu.Unlock()
					}(f)
				}
			}
		}
	}()
}

// String renders a trigger for logs. Never includes secrets.
func (f Fired) String() string {
	return fmt.Sprintf("routine %s bot %s", f.RoutineID, f.BotID)
}
