package schedule

import (
	"strings"
	"time"
)

// NextFire returns the next activation time for a trigger.
//
// now is the current time and lastRun is the routine's last fire (the zero
// time means it has never fired). It reports ok=false for a trigger that
// never fires on its own — "manual" or an event trigger — so a caller can
// say "no next fire" instead of inventing one.
//
// The two shapes differ in what they anchor to:
//
//   - A cron trigger fires on the clock, so the next occurrence is computed
//     from now and lastRun is irrelevant.
//   - An interval trigger fires every d, so the next one is lastRun+d. When
//     lastRun is zero the interval starts at now. When the routine is
//     overdue (lastRun+d is already in the past) the next fire is now+d:
//     the scheduler will fire it on its next tick, and reporting the
//     already-past time would claim a fire that has not happened.
//
// An unparseable trigger (a typo'd cron) reports ok=false. The trigger is
// validated at write time, so reaching here means the stored trigger is
// bad — the honest answer is "no next fire", not a guess.
func NextFire(trigger string, now, lastRun time.Time) (time.Time, bool) {
	t := strings.TrimSpace(trigger)
	if c, ok := ParseCronTrigger(t); ok {
		return c.Next(now)
	}
	if d, ok := ParseEvery(t); ok {
		base := lastRun
		if base.IsZero() {
			base = now
		}
		next := base.Add(d)
		if next.Before(now) {
			next = now.Add(d)
		}
		return next, true
	}
	return time.Time{}, false
}

// NextFireString renders NextFire as an RFC3339 timestamp, or "" when the
// trigger has no next fire. The empty string is what the UI shows as "no
// next fire" — a manual routine has none, and neither does a trigger that
// does not parse.
func NextFireString(trigger string, now, lastRun time.Time) string {
	next, ok := NextFire(trigger, now, lastRun)
	if !ok {
		return ""
	}
	return next.Format(time.RFC3339)
}
