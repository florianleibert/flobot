// fire.go gives every scheduled fire an identity and validates it before
// anything is spawned.
//
// Two failures motivate this file:
//
//   - A fire that happens twice runs twice. The ticker can observe the same due
//     minute more than once (a slow tick, a restart, a clock step), and without
//     an identity there is nothing to compare against. Identity is derived from
//     the routine and the scheduled occurrence, not from wall-clock "now", so
//     two observations of the same occurrence produce the same key.
//   - A fire that cannot work still spawns a child. A paused routine, a deleted
//     bot, a draft skill, an unresolvable route: each of those used to be
//     discovered by the child failing. Validation answers first, and the reason
//     is recorded instead of guessed at from an empty run.
package schedule

import (
	"fmt"
	"strings"
	"time"
)

// FireID is the identity of one scheduled occurrence: routine plus the instant
// the occurrence was scheduled for, truncated to the trigger's resolution.
//
// Truncation is what makes it stable. A cron routine scheduled for 09:00 has the
// same identity whether the ticker notices at 09:00:01 or 09:00:41; an interval
// routine buckets to its own period. Two observations of one occurrence
// therefore collide on purpose, and a genuinely new occurrence does not.
func FireID(routineID string, trigger string, at time.Time) string {
	if routineID == "" {
		return ""
	}
	at = at.UTC()
	if _, ok := ParseCronTrigger(trigger); ok {
		// A cron occurrence is identified by its minute.
		return fmt.Sprintf("%s@%s", routineID, at.Truncate(time.Minute).Format(time.RFC3339))
	}
	if d, ok := ParseEvery(trigger); ok {
		// An interval occurrence is identified by its bucket: floor the instant
		// to a multiple of the period since the epoch, so the key is stable for
		// the whole period regardless of when the tick lands.
		bucket := at.Unix() / int64(d.Seconds())
		return fmt.Sprintf("%s@%d", routineID, bucket)
	}
	// A manual/event trigger has no schedule; its identity is the instant.
	return fmt.Sprintf("%s@%s", routineID, at.Format(time.RFC3339Nano))
}

// SkipReason explains why a fire was not spawned. It is a value rather than an
// error so a skip is not confused with a failure: a paused routine is not an
// error, and backing it off would be wrong.
type SkipReason string

const (
	SkipNone        SkipReason = ""
	SkipPaused      SkipReason = "routine is paused"
	SkipNoBot       SkipReason = "bot no longer exists"
	SkipNoSkill     SkipReason = "skill no longer exists"
	SkipDraftSkill  SkipReason = "skill is a draft; approve it before it can fire"
	SkipNoRoute     SkipReason = "bot route cannot be resolved"
	SkipRunning     SkipReason = "the bot's previous run is still going"
	SkipDuplicate   SkipReason = "this occurrence already fired"
	SkipBadTrigger  SkipReason = "trigger does not schedule anything"
	SkipRateLimited SkipReason = "routine is firing faster than its rate limit allows"
	SkipTooDeep     SkipReason = "this fire would continue a chain that is already too deep"
	SkipBudgetSpent SkipReason = "the fire's total time budget is spent"
)

// Validatable is what pre-dispatch validation needs to know. The server fills it
// from the store; keeping it a plain struct means this package stays free of
// store imports and stays testable without one.
type Validatable struct {
	RoutineID string
	Paused    bool
	Trigger   string

	BotExists   bool
	SkillExists bool
	SkillLive   bool
	RouteOK     bool
	BotRunning  bool
}

// Validate answers whether a fire may be spawned, before anything is spawned.
// It returns SkipNone when the fire is good to go.
//
// The order matters for the message a person reads: the most actionable reason
// wins. A paused routine reports "paused", not "duplicate".
func Validate(v Validatable) SkipReason {
	if v.Paused {
		return SkipPaused
	}
	if _, _, ok := ParseTrigger(v.Trigger); !ok {
		return SkipBadTrigger
	}
	if !v.BotExists {
		return SkipNoBot
	}
	if !v.SkillExists {
		return SkipNoSkill
	}
	if !v.SkillLive {
		return SkipDraftSkill
	}
	if !v.RouteOK {
		return SkipNoRoute
	}
	if v.BotRunning {
		return SkipRunning
	}
	return SkipNone
}

// Error renders a skip reason as an error, or nil for SkipNone. The scheduler's
// fire callback returns an error to trigger backoff; a skip must not back off,
// so callers use this only for reasons that are genuinely transient.
func (r SkipReason) Error() error {
	if r == SkipNone {
		return nil
	}
	return fmt.Errorf("%s", string(r))
}

// Transient reports whether a skip may clear on its own. A running bot will
// finish; a missing bot will not.
func (r SkipReason) Transient() bool {
	switch r {
	case SkipRunning, SkipNoRoute, SkipRateLimited:
		return true
	default:
		return false
	}
}

// String renders the reason for a log line.
func (r SkipReason) String() string { return string(r) }

// Dedupe remembers which occurrences already fired. It is bounded: only the most
// recent keys are kept, because an occurrence older than the window can no longer
// be observed by the ticker.
type Dedupe struct {
	max  int
	keys map[string]struct{}
	ring []string
}

// NewDedupe returns a Dedupe holding up to max keys (0 means 512).
func NewDedupe(max int) *Dedupe {
	if max <= 0 {
		max = 512
	}
	return &Dedupe{max: max, keys: make(map[string]struct{}, max)}
}

// Seen reports whether key already fired.
func (d *Dedupe) Seen(key string) bool {
	if d == nil || key == "" {
		return false
	}
	_, ok := d.keys[key]
	return ok
}

// Mark records key as fired and reports whether it was new. A false return means
// this occurrence already fired and must not run again.
func (d *Dedupe) Mark(key string) bool {
	if d == nil || key == "" {
		return true
	}
	if _, ok := d.keys[key]; ok {
		return false
	}
	d.keys[key] = struct{}{}
	d.ring = append(d.ring, key)
	if len(d.ring) > d.max {
		oldest := d.ring[0]
		d.ring = d.ring[1:]
		delete(d.keys, oldest)
	}
	return true
}

// Len is the number of remembered keys.
func (d *Dedupe) Len() int {
	if d == nil {
		return 0
	}
	return len(d.keys)
}

// clipNote bounds a recorded note so one long error cannot bloat the state file.
func clipNote(s string) string {
	s = strings.TrimSpace(s)
	const max = 240
	if len(s) <= max {
		return s
	}
	return s[:max]
}
