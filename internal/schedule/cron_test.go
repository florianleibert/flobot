package schedule

import (
	"testing"
	"time"
)

func TestParseCron(t *testing.T) {
	ok := []string{
		"*/5 * * * *",
		"0 9 * * 1-5",
		"30 8 1 * *",
		"0 0 * * 0",
		"0-30/2 9-17 * * *",
		"@hourly",
		"@daily",
		"@weekly",
		"@monthly",
	}
	for _, expr := range ok {
		if _, err := ParseCron(expr); err != nil {
			t.Errorf("ParseCron(%q) = %v, want ok", expr, err)
		}
	}
	bad := []string{"", "*", "* *", "1 2 3 4 5 6", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "* * * * 7"}
	for _, expr := range bad {
		if _, err := ParseCron(expr); err == nil {
			t.Errorf("ParseCron(%q) = ok, want error", expr)
		}
	}
}

func TestCronNext(t *testing.T) {
	// Wednesday 2026-10-14 08:59:30 UTC.
	start := time.Date(2026, 10, 14, 8, 59, 30, 0, time.UTC)
	c, err := ParseCron("0 9 * * *")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := c.Next(start)
	if !ok || got.String() != "2026-10-14 09:00:00 +0000 UTC" {
		t.Fatalf("Next(daily 09:00) = %v,%v", got, ok)
	}
	// */15 from 12:07 → 12:15
	c, _ = ParseCron("*/15 * * * *")
	got, _ = c.Next(time.Date(2026, 10, 14, 12, 7, 0, 0, time.UTC))
	if want := time.Date(2026, 10, 14, 12, 15, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("Next(*/15) = %v want %v", got, want)
	}
	// Weekdays only: Friday 2026-10-16 10:00 → Monday 2026-10-19 09:00
	c, _ = ParseCron("0 9 * * 1-5")
	got, _ = c.Next(time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC))
	if want := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("Next(weekdays) = %v want %v", got, want)
	}
	// Impossible date returns not-ok instead of spinning.
	c, _ = ParseCron("0 0 30 2 *")
	if _, ok := c.Next(start); ok {
		t.Fatal("Feb 30 should never activate")
	}
}

func TestParseCronTriggerAndValidator(t *testing.T) {
	if _, ok := ParseCronTrigger("cron 0 9 * * *"); !ok {
		t.Fatal("cron trigger should parse")
	}
	if _, ok := ParseCronTrigger("every 5m"); ok {
		t.Fatal("every is not a cron trigger")
	}
	if _, ok := ParseCronTrigger("cron nonsense * * *"); ok {
		t.Fatal("invalid cron should not parse")
	}
	if _, _, ok := ParseTrigger("manual"); ok {
		t.Fatal("manual is not scheduled")
	}
	if _, c, ok := ParseTrigger("cron @daily"); !ok || c == nil {
		t.Fatal("ParseTrigger should accept cron")
	}
	if d, c, ok := ParseTrigger("every 5m"); !ok || d != 5*time.Minute || c != nil {
		t.Fatal("ParseTrigger should accept every")
	}
}

func TestDueFiresCronOnScheduledMinute(t *testing.T) {
	s := New()
	now := time.Date(2026, 10, 14, 8, 59, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	items := []Fireable{{RoutineID: "c1", Trigger: "cron 0 9 * * *"}}

	if fired := s.Due(items); len(fired) != 0 {
		t.Fatalf("cron fired on first observation: %v", fired)
	}
	now = now.Add(time.Minute) // 09:00
	fired := s.Due(items)
	if len(fired) != 1 || fired[0].RoutineID != "c1" {
		t.Fatalf("cron not due at 09:00: %v", fired)
	}
	s.MarkFired("c1")
	if fired := s.Due(items); len(fired) != 0 {
		t.Fatalf("cron double-fired at same minute: %v", fired)
	}
	now = now.Add(24 * time.Hour) // tomorrow 09:00
	if fired := s.Due(items); len(fired) != 1 {
		t.Fatalf("cron not due next day: %v", fired)
	}
}

// TestDueCronSlowFireNoRefire pins the fix where a cron fire that outlives
// one scheduler tick must not re-fire in the same minute before MarkFired
// runs. Due() advances last to the scheduled minute at fire time, so a
// second Due() call at 09:00:30 (no MarkFired in between) is a no-op.
func TestDueCronSlowFireNoRefire(t *testing.T) {
	s := New()
	now := time.Date(2026, 10, 14, 8, 59, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	items := []Fireable{{RoutineID: "slow", Trigger: "cron 0 9 * * *"}}
	if fired := s.Due(items); len(fired) != 0 {
		t.Fatalf("fired on first observation: %v", fired)
	}
	now = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	if fired := s.Due(items); len(fired) != 1 {
		t.Fatalf("not due at 09:00: %v", fired)
	}
	// The fire is still running (MarkFired not called); the next tick lands
	// in the same minute. Without the fix this re-fires "slow".
	now = time.Date(2026, 10, 14, 9, 0, 30, 0, time.UTC)
	if fired := s.Due(items); len(fired) != 0 {
		t.Fatalf("slow fire re-fired in same minute: %v", fired)
	}
	// After the fire completes and MarkFired runs, the next day fires once.
	s.MarkFired("slow")
	now = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	if fired := s.Due(items); len(fired) != 1 {
		t.Fatalf("not due next day after MarkFired: %v", fired)
	}
}
