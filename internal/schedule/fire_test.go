package schedule

import (
	"testing"
	"time"
)

// M4 acceptance: the same occurrence observed twice has the same identity, and
// a genuinely new occurrence does not. Without this, a slow tick or a restart
// double-runs a routine.
func TestFireIDIsStablePerOccurrence(t *testing.T) {
	base := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)

	// Cron: the same minute is one occurrence regardless of the second.
	a := FireID("r1", "cron 0 9 * * *", base)
	b := FireID("r1", "cron 0 9 * * *", base.Add(41*time.Second))
	if a != b {
		t.Fatalf("same cron minute produced different ids: %q vs %q", a, b)
	}
	// The next minute is a different occurrence.
	if c := FireID("r1", "cron 0 9 * * *", base.Add(time.Minute)); c == a {
		t.Fatalf("next minute reused id %q", c)
	}

	// Interval: any instant inside one period is the same occurrence.
	i1 := FireID("r1", "every 5m", base)
	i2 := FireID("r1", "every 5m", base.Add(4*time.Minute))
	if i1 != i2 {
		t.Fatalf("same interval bucket produced different ids: %q vs %q", i1, i2)
	}
	if i3 := FireID("r1", "every 5m", base.Add(5*time.Minute)); i3 == i1 {
		t.Fatalf("next interval bucket reused id %q", i3)
	}

	// Different routines never collide.
	if FireID("r1", "every 5m", base) == FireID("r2", "every 5m", base) {
		t.Fatal("two routines shared an occurrence id")
	}
	// No routine id means no identity, and an empty identity is never deduped.
	if got := FireID("", "every 5m", base); got != "" {
		t.Fatalf("FireID with no routine = %q, want empty", got)
	}
}

// M4 acceptance: a duplicate webhook delivery runs once. The provider's delivery
// id is the identity, so a retry collides and a new delivery does not.
func TestEventFireIDDedupesRetriedDelivery(t *testing.T) {
	a := EventFireID("r1", "delivery-1")
	b := EventFireID("r1", "delivery-1")
	if a != b || a == "" {
		t.Fatalf("retried delivery got a different id: %q vs %q", a, b)
	}
	if c := EventFireID("r1", "delivery-2"); c == a {
		t.Fatalf("a new delivery reused id %q", c)
	}
	// No delivery id means no identity: the fire must run rather than be
	// silently swallowed.
	if got := EventFireID("r1", ""); got != "" {
		t.Fatalf("EventFireID with no delivery = %q, want empty", got)
	}
}

func TestDedupeMarkAndEvict(t *testing.T) {
	d := NewDedupe(2)
	if !d.Mark("a") {
		t.Fatal("first mark of a was refused")
	}
	if d.Mark("a") {
		t.Fatal("second mark of a was accepted")
	}
	if !d.Mark("b") || !d.Mark("c") {
		t.Fatal("b or c refused")
	}
	// max=2: a was evicted when c landed.
	if d.Seen("a") {
		t.Fatal("a survived past the window")
	}
	if !d.Seen("c") || d.Len() != 2 {
		t.Fatalf("len = %d, seen(c) = %v", d.Len(), d.Seen("c"))
	}
	// An empty key is never deduped.
	if !d.Mark("") || d.Seen("") {
		t.Fatal("empty key was deduped")
	}
}

func TestBudgetAttemptTimeout(t *testing.T) {
	b := Budget{QueueWait: 30 * time.Second, Attempt: 10 * time.Minute, Total: 30 * time.Minute}

	if got, err := b.AttemptTimeout(0, 0); err != nil || got != 10*time.Minute {
		t.Fatalf("fresh attempt = %v, %v", got, err)
	}
	// Waited too long to start: not retryable.
	if _, err := b.AttemptTimeout(31*time.Second, 0); err != ErrQueueWaitExpired {
		t.Fatalf("queue-wait expiry = %v", err)
	}
	// Total budget gone: no further attempt.
	if _, err := b.AttemptTimeout(0, 30*time.Minute); err != ErrBudgetSpent {
		t.Fatalf("total expiry = %v", err)
	}
	// A short remaining total clamps the attempt rather than overrunning it.
	if got, err := b.AttemptTimeout(0, 27*time.Minute); err != nil || got != 3*time.Minute {
		t.Fatalf("clamped attempt = %v, %v", got, err)
	}
	// A zero budget means the defaults, never "unbounded".
	var zero Budget
	if got, err := zero.AttemptTimeout(0, 0); err != nil || got != DefaultAttempt {
		t.Fatalf("zero budget attempt = %v, %v", got, err)
	}
}

func TestLimiterRefusesBurst(t *testing.T) {
	l := NewLimiter(2, time.Hour)
	now := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	if !l.Allow("r1", now) || !l.Allow("r1", now) {
		t.Fatal("first two hits refused")
	}
	if l.Allow("r1", now) {
		t.Fatal("third hit accepted past a limit of two")
	}
	// A different routine has its own budget.
	if !l.Allow("r2", now) {
		t.Fatal("r2 refused because r1 was busy")
	}
	// The window slides.
	if !l.Allow("r1", now.Add(61*time.Minute)) {
		t.Fatal("hit refused after the window passed")
	}
}

func TestRecursionGuardRefusesDeepChain(t *testing.T) {
	r := NewRecursion()
	for i := 0; i < MaxDepth; i++ {
		if err := r.Enter("r1"); err != nil {
			t.Fatalf("depth %d refused: %v", i+1, err)
		}
	}
	if err := r.Enter("r1"); err != ErrTooDeep {
		t.Fatalf("depth %d = %v, want ErrTooDeep", MaxDepth+1, err)
	}
	r.Leave("r1")
	if err := r.Enter("r1"); err != nil {
		t.Fatalf("re-enter after leave refused: %v", err)
	}
	if r.Depth("r-unknown") != 0 {
		t.Fatalf("unknown key depth = %d", r.Depth("r-unknown"))
	}
}

// The scheduler must refuse to fire the same occurrence twice even when Due()
// hands it the same identity again (a restart or a clock step).
func TestSchedulerRefusesRepeatedOccurrence(t *testing.T) {
	s := New()
	if !s.MarkSeen("r1@2026-11-04T09:00:00Z") {
		t.Fatal("first occurrence refused")
	}
	if s.MarkSeen("r1@2026-11-04T09:00:00Z") {
		t.Fatal("repeated occurrence accepted")
	}
}

// Due() must stamp an identity on every fire it emits: the server dedupes on it.
func TestDueStampsFireIdentity(t *testing.T) {
	s := New()
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	items := []Fireable{{RoutineID: "r1", Trigger: "every 5m"}}
	s.Due(items) // first observation arms the schedule
	now = now.Add(5 * time.Minute)
	fired := s.Due(items)
	if len(fired) != 1 {
		t.Fatalf("fired = %v", fired)
	}
	if fired[0].ID == "" {
		t.Fatal("Due() emitted a fire with no identity")
	}
	want := FireID("r1", "every 5m", now)
	if fired[0].ID != want {
		t.Fatalf("id = %q, want %q", fired[0].ID, want)
	}
}

func TestValidateOrderPrefersActionableReason(t *testing.T) {
	v := Validatable{
		RoutineID:   "r1",
		Paused:      true,
		Trigger:     "every nonsense",
		BotExists:   false,
		SkillExists: false,
	}
	if got := Validate(v); got != SkipPaused {
		t.Fatalf("paused+bad trigger = %q, want paused first", got)
	}
	v.Paused = false
	if got := Validate(v); got != SkipBadTrigger {
		t.Fatalf("bad trigger = %q", got)
	}
	v.Trigger = "every 5m"
	if got := Validate(v); got != SkipNoBot {
		t.Fatalf("missing bot = %q", got)
	}
	v.BotExists = true
	v.SkillExists = true
	v.SkillLive = false
	if got := Validate(v); got != SkipDraftSkill {
		t.Fatalf("draft skill = %q", got)
	}
	v.SkillLive = true
	v.RouteOK = true
	v.BotRunning = true
	if got := Validate(v); got != SkipRunning {
		t.Fatalf("running bot = %q", got)
	}
	v.BotRunning = false
	if got := Validate(v); got != SkipNone {
		t.Fatalf("valid fire = %q", got)
	}
}

func TestSkipReasonTransience(t *testing.T) {
	for _, r := range []SkipReason{SkipRunning, SkipNoRoute, SkipRateLimited} {
		if !r.Transient() {
			t.Errorf("%q should be transient", r)
		}
	}
	for _, r := range []SkipReason{SkipPaused, SkipNoBot, SkipNoSkill, SkipDraftSkill, SkipDuplicate} {
		if r.Transient() {
			t.Errorf("%q should not be transient", r)
		}
	}
	if SkipNone.Error() != nil {
		t.Error("SkipNone must not be an error")
	}
}
