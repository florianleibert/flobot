package schedule

import (
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return ts
}

func TestNextFireCron(t *testing.T) {
	// 2026-10-28 is a Wednesday. 09:00 has already passed, so the next
	// occurrence is the following day.
	now := mustTime(t, "2026-10-28T10:00:00Z")
	got, ok := NextFire("cron 0 9 * * *", now, time.Time{})
	if !ok {
		t.Fatal("NextFire ok = false, want true")
	}
	want := mustTime(t, "2026-10-29T09:00:00Z")
	if !got.Equal(want) {
		t.Fatalf("NextFire = %s, want %s", got, want)
	}
}

func TestNextFireCronLaterSameDay(t *testing.T) {
	now := mustTime(t, "2026-10-28T08:00:00Z")
	got, ok := NextFire("cron 0 9 * * *", now, time.Time{})
	if !ok {
		t.Fatal("NextFire ok = false, want true")
	}
	want := mustTime(t, "2026-10-28T09:00:00Z")
	if !got.Equal(want) {
		t.Fatalf("NextFire = %s, want %s", got, want)
	}
}

func TestNextFireCronIgnoresLastRun(t *testing.T) {
	// A cron fires on the clock: a recent last run does not push the next
	// occurrence out, because the clock decides.
	now := mustTime(t, "2026-10-28T08:00:00Z")
	last := mustTime(t, "2026-10-28T07:59:00Z")
	got, ok := NextFire("cron 0 9 * * *", now, last)
	if !ok {
		t.Fatal("NextFire ok = false, want true")
	}
	want := mustTime(t, "2026-10-28T09:00:00Z")
	if !got.Equal(want) {
		t.Fatalf("NextFire = %s, want %s", got, want)
	}
}

func TestNextFireCronAlias(t *testing.T) {
	now := mustTime(t, "2026-10-28T08:00:00Z")
	got, ok := NextFire("cron @daily", now, time.Time{})
	if !ok {
		t.Fatal("NextFire ok = false, want true")
	}
	want := mustTime(t, "2026-10-29T00:00:00Z")
	if !got.Equal(want) {
		t.Fatalf("NextFire = %s, want %s", got, want)
	}
}

func TestNextFireIntervalFromLastRun(t *testing.T) {
	now := mustTime(t, "2026-10-28T10:00:00Z")
	last := mustTime(t, "2026-10-28T09:57:00Z")
	got, ok := NextFire("every 5m", now, last)
	if !ok {
		t.Fatal("NextFire ok = false, want true")
	}
	want := mustTime(t, "2026-10-28T10:02:00Z")
	if !got.Equal(want) {
		t.Fatalf("NextFire = %s, want %s", got, want)
	}
}

func TestNextFireIntervalNeverFired(t *testing.T) {
	now := mustTime(t, "2026-10-28T10:00:00Z")
	got, ok := NextFire("every 5m", now, time.Time{})
	if !ok {
		t.Fatal("NextFire ok = false, want true")
	}
	want := mustTime(t, "2026-10-28T10:05:00Z")
	if !got.Equal(want) {
		t.Fatalf("NextFire = %s, want %s", got, want)
	}
}

func TestNextFireIntervalOverdueReportsFromNow(t *testing.T) {
	// The routine is overdue: lastRun+5m is already in the past. The next
	// fire is the one the scheduler will make on its next tick, so it must
	// not be reported as a time that has already passed.
	now := mustTime(t, "2026-10-28T10:00:00Z")
	last := mustTime(t, "2026-10-28T09:00:00Z")
	got, ok := NextFire("every 5m", now, last)
	if !ok {
		t.Fatal("NextFire ok = false, want true")
	}
	want := mustTime(t, "2026-10-28T10:05:00Z")
	if !got.Equal(want) {
		t.Fatalf("NextFire = %s, want %s", got, want)
	}
	if !got.After(now) {
		t.Fatalf("NextFire = %s, want a time after now (%s)", got, now)
	}
}

func TestNextFireManualHasNoNextFire(t *testing.T) {
	now := mustTime(t, "2026-10-28T10:00:00Z")
	for _, trigger := range []string{"manual", "", "on-demand", "every notaduration"} {
		got, ok := NextFire(trigger, now, time.Time{})
		if ok {
			t.Errorf("NextFire(%q) ok = true (%s), want false", trigger, got)
		}
		if !got.IsZero() {
			t.Errorf("NextFire(%q) = %s, want the zero time", trigger, got)
		}
	}
}

func TestNextFireUnparseableCronHasNoNextFire(t *testing.T) {
	now := mustTime(t, "2026-10-28T10:00:00Z")
	got, ok := NextFire("cron 09 * *", now, time.Time{})
	if ok {
		t.Errorf("NextFire ok = true (%s), want false for an unparseable cron", got)
	}
}

func TestNextFireString(t *testing.T) {
	now := mustTime(t, "2026-10-28T08:00:00Z")
	got := NextFireString("cron 0 9 * * *", now, time.Time{})
	want := "2026-10-28T09:00:00Z"
	if got != want {
		t.Fatalf("NextFireString = %q, want %q", got, want)
	}
	if s := NextFireString("manual", now, time.Time{}); s != "" {
		t.Fatalf("NextFireString(manual) = %q, want empty", s)
	}
}
