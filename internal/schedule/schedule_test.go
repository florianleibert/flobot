package schedule

import (
	"context"
	"testing"
	"time"
)

func TestParseEvery(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"every 5m", 5 * time.Minute, true},
		{"every 30s", time.Minute, true}, // floored to a minute
		{"every 2h", 2 * time.Hour, true},
		{"every 1h30m", 90 * time.Minute, true},
		{"manual", 0, false},
		{"", 0, false},
		{"every", 0, false},
		{"every nonsense", 0, false},
		{"every 0s", 0, false},
		{"every -5m", 0, false},
	}
	for _, c := range cases {
		got, ok := ParseEvery(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("ParseEvery(%q) = %v,%v want %v,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestDueFiresAfterInterval(t *testing.T) {
	s := New()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	items := []Fireable{{RoutineID: "r1", Trigger: "every 5m"}}

	// First observation: never fires immediately.
	if fired := s.Due(items); len(fired) != 0 {
		t.Fatalf("fired on first observation: %v", fired)
	}
	// Before the interval: no fire.
	now = now.Add(4 * time.Minute)
	if fired := s.Due(items); len(fired) != 0 {
		t.Fatalf("fired early: %v", fired)
	}
	// At/after the interval: fire.
	now = now.Add(1 * time.Minute)
	fired := s.Due(items)
	if len(fired) != 1 || fired[0].RoutineID != "r1" {
		t.Fatalf("fired = %v", fired)
	}
	s.MarkFired("r1")
	// Right after a fire: not due again.
	if fired := s.Due(items); len(fired) != 0 {
		t.Fatalf("double fire: %v", fired)
	}
}

func TestDueIgnoresPausedAndUnknownTriggers(t *testing.T) {
	s := New()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	items := []Fireable{
		{RoutineID: "manual", Trigger: "manual"},
		{RoutineID: "event", Trigger: "on-push"},
	}
	s.Due(items) // observe
	now = now.Add(2 * time.Hour)
	if fired := s.Due(items); len(fired) != 0 {
		t.Fatalf("non-every triggers fired: %v", fired)
	}
}

func TestStartFiresDueRoutines(t *testing.T) {
	s := New()
	s.Tick = 10 * time.Millisecond
	base := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	now := base
	s.Now = func() time.Time { return now }
	items := []Fireable{{RoutineID: "r1", BotID: "b1", SkillID: "s1", Trigger: "every 1m"}}
	s.Due(items) // observe

	firedCh := make(chan Fired, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx, func() []Fireable {
		now = now.Add(2 * time.Minute) // advance time each tick
		return items
	}, func(f Fired) error {
		firedCh <- f
		return nil
	})
	select {
	case f := <-firedCh:
		if f.RoutineID != "r1" || f.BotID != "b1" {
			t.Fatalf("fired = %+v", f)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler never fired")
	}
	cancel()
}
