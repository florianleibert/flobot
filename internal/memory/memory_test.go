package memory

import (
	"errors"
	"strings"
	"testing"
)

func TestFactsAddAndRender(t *testing.T) {
	f := ParseFacts("", 0)
	f, err := f.Add("prefers table-driven tests")
	if err != nil {
		t.Fatal(err)
	}
	f, err = f.Add("never force-push to main")
	if err != nil {
		t.Fatal(err)
	}
	if f.Len() != 2 {
		t.Fatalf("len = %d, want 2", f.Len())
	}
	want := "- prefers table-driven tests\n- never force-push to main"
	if got := f.String(); got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestFactsAddIsIdempotent(t *testing.T) {
	f := ParseFacts("", 0)
	f, _ = f.Add("one")
	f, err := f.Add("one")
	if err != nil {
		t.Fatal(err)
	}
	if f.Len() != 1 {
		t.Fatalf("duplicate add grew the set to %d", f.Len())
	}
}

// The core M1 promise: a fact that does not fit is refused with a message that
// names the usage, instead of being silently clipped.
func TestFactsFullReturnsUsageNotClip(t *testing.T) {
	f := ParseFacts("", 64)
	f, err := f.Add(strings.Repeat("a", 40))
	if err != nil {
		t.Fatalf("first fact should fit: %v", err)
	}
	_, err = f.Add(strings.Repeat("b", 40))
	if err == nil {
		t.Fatal("second fact must be refused, not clipped")
	}
	if !errors.Is(err, ErrFull) {
		t.Fatalf("err = %v, want ErrFull", err)
	}
	var full *FullError
	if !errors.As(err, &full) {
		t.Fatalf("err = %v, want *FullError", err)
	}
	if full.Cap != 64 {
		t.Fatalf("Cap = %d, want 64", full.Cap)
	}
	if full.Used <= full.Cap {
		t.Fatalf("Used = %d should exceed Cap = %d", full.Used, full.Cap)
	}
	if !strings.Contains(err.Error(), "of 64 bytes") {
		t.Fatalf("message must name the cap: %q", err.Error())
	}
}

func TestFactsRejectsPersistenceLeakTokens(t *testing.T) {
	for _, bad := range []string{"the api_key lives in ~/.accounts", "my secret plan", "llm_endpoint is local"} {
		f := ParseFacts("", 0)
		if _, err := f.Add(bad); err == nil {
			t.Fatalf("Add(%q) must be refused: it would break every later save", bad)
		}
	}
}

func TestFactsPatchAndRemove(t *testing.T) {
	f := ParseFacts("- one\n- two\n- three", 0)
	f, err := f.Patch(1, "TWO")
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Lines()[1]; got != "TWO" {
		t.Fatalf("patched line = %q, want TWO", got)
	}
	f, err = f.Remove(0)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Lines(); len(got) != 2 || got[0] != "TWO" {
		t.Fatalf("after remove = %v", got)
	}
	if _, err := f.Patch(9, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("out-of-range patch err = %v, want ErrNotFound", err)
	}
	if _, err := f.Remove(9); !errors.Is(err, ErrNotFound) {
		t.Fatalf("out-of-range remove err = %v, want ErrNotFound", err)
	}
}

func TestFactsParseDropsBlanksAndBullets(t *testing.T) {
	f := ParseFacts("\n- alpha\n\n  beta  \n- alpha\n", 0)
	if got := f.Lines(); len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Fatalf("parsed = %v", got)
	}
}

func TestFactsPercent(t *testing.T) {
	f := ParseFacts("", 100)
	f, _ = f.Add(strings.Repeat("x", 50))
	if p := f.Percent(); p < 50 || p > 52 {
		t.Fatalf("Percent() = %d, want ~51", p)
	}
}

func TestLogAppendEvictsOldest(t *testing.T) {
	l := NewLog(nil)
	for i := 0; i < MaxLogEntries+5; i++ {
		l = l.Append("2026-11-04T00:00:00Z", "", "entry")
	}
	if l.Len() != MaxLogEntries {
		t.Fatalf("log len = %d, want %d", l.Len(), MaxLogEntries)
	}
}

func TestLogRecentIsOldestFirst(t *testing.T) {
	l := NewLog(nil)
	l = l.Append("2026-11-01T00:00:00Z", "run-1", "first")
	l = l.Append("2026-11-02T00:00:00Z", "run-2", "second")
	l = l.Append("2026-11-03T00:00:00Z", "run-3", "third")
	got := l.Recent(2)
	if len(got) != 2 || got[0].Summary != "second" || got[1].Summary != "third" {
		t.Fatalf("Recent(2) = %+v", got)
	}
}

func TestLogRenderIncludesDate(t *testing.T) {
	l := NewLog(nil).Append("2026-11-04T10:00:00Z", "run-9", "shipped M1")
	out := l.Render(5)
	if !strings.Contains(out, "2026-11-04T10:00:00Z") || !strings.Contains(out, "shipped M1") {
		t.Fatalf("Render() = %q", out)
	}
}

func TestLogRedactsLeakTokens(t *testing.T) {
	l := NewLog(nil).Append("2026-11-04T10:00:00Z", "", "rotated the api_key")
	if strings.Contains(strings.ToLower(l.Entries()[0].Summary), "api_key") {
		t.Fatalf("log entry kept a leak token: %q", l.Entries()[0].Summary)
	}
}

func TestNewLogDropsEmptySummaries(t *testing.T) {
	l := NewLog([]LogEntry{{At: "x"}, {At: "y", Summary: "kept"}})
	if l.Len() != 1 || l.Entries()[0].Summary != "kept" {
		t.Fatalf("NewLog = %+v", l.Entries())
	}
}
