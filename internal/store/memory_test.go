package store

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/florianleibert/flobot/internal/memory"
)

// M1 acceptance: filling memory past the cap returns an error naming the usage
// instead of silently clipping the fact.
func TestMemoryAddRefusesWhenFull(t *testing.T) {
	st := New()
	st.SetPersistPath(filepath.Join(t.TempDir(), "state.json"))
	bot, err := st.CreateBot(ActorLocal, "M", "coder", "glm")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetMemoryCap(bot.ID, ActorLocal, 64); err != nil {
		t.Fatal(err)
	}
	if _, err := st.MemoryAdd(bot.ID, ActorLocal, strings.Repeat("a", 40)); err != nil {
		t.Fatalf("first fact should fit: %v", err)
	}
	_, err = st.MemoryAdd(bot.ID, ActorLocal, strings.Repeat("b", 40))
	if err == nil {
		t.Fatal("second fact must be refused, not clipped")
	}
	if !errors.Is(err, memory.ErrFull) {
		t.Fatalf("err = %v, want ErrFull", err)
	}
	if !strings.Contains(err.Error(), "of 64 bytes") {
		t.Fatalf("error must name the usage: %q", err.Error())
	}
	// The refused fact must not have been stored.
	got, err := st.GetBot(bot.ID, ActorLocal)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.Memory, "bbbb") {
		t.Fatalf("refused fact was stored anyway: %q", got.Memory)
	}
}

func TestMemoryAddAppendsFacts(t *testing.T) {
	st := New()
	st.SetPersistPath(filepath.Join(t.TempDir(), "state.json"))
	bot, _ := st.CreateBot(ActorLocal, "M", "coder", "glm")
	if _, err := st.Remember(bot.ID, ActorLocal, "always write tests first"); err != nil {
		t.Fatal(err)
	}
	got, err := st.Remember(bot.ID, ActorLocal, "prefers table-driven tests")
	if err != nil {
		t.Fatal(err)
	}
	facts := got.Facts().Lines()
	if len(facts) != 2 || facts[0] != "always write tests first" || facts[1] != "prefers table-driven tests" {
		t.Fatalf("facts = %v", facts)
	}
}

func TestMemoryPatchAndRemove(t *testing.T) {
	st := New()
	st.SetPersistPath(filepath.Join(t.TempDir(), "state.json"))
	bot, _ := st.CreateBot(ActorLocal, "M", "coder", "glm")
	st.Remember(bot.ID, ActorLocal, "one")
	st.Remember(bot.ID, ActorLocal, "two")

	got, err := st.MemoryPatch(bot.ID, ActorLocal, 0, "ONE")
	if err != nil {
		t.Fatal(err)
	}
	if facts := got.Facts().Lines(); facts[0] != "ONE" {
		t.Fatalf("patched = %v", facts)
	}
	got, err = st.MemoryRemove(bot.ID, ActorLocal, 0)
	if err != nil {
		t.Fatal(err)
	}
	if facts := got.Facts().Lines(); len(facts) != 1 || facts[0] != "two" {
		t.Fatalf("after remove = %v", facts)
	}
	if _, err := st.MemoryRemove(bot.ID, ActorLocal, 5); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("out-of-range remove err = %v", err)
	}
}

// M1 acceptance: a log entry survives a reload from disk.
func TestWorkLogSurvivesReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := New()
	st.SetPersistPath(path)
	bot, err := st.CreateBot(ActorLocal, "M", "coder", "glm")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.LogWorkAt(bot.ID, ActorLocal, "2026-11-04T10:00:00Z", "run-7", "shipped M1"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Remember(bot.ID, ActorLocal, "keeps a work log"); err != nil {
		t.Fatal(err)
	}

	loaded := New()
	loaded.SetPersistPath(path)
	if err := loaded.Load(); err != nil {
		t.Fatal(err)
	}
	got, err := loaded.GetBot(bot.ID, ActorLocal)
	if err != nil {
		t.Fatal(err)
	}
	log := got.WorkLog().Entries()
	if len(log) != 1 {
		t.Fatalf("log = %+v", log)
	}
	if log[0].Summary != "shipped M1" || log[0].RunID != "run-7" || log[0].At != "2026-11-04T10:00:00Z" {
		t.Fatalf("log entry = %+v", log[0])
	}
	if facts := got.Facts().Lines(); len(facts) != 1 || facts[0] != "keeps a work log" {
		t.Fatalf("facts after reload = %v", facts)
	}
}

// M1 acceptance: the leak scanner still passes with memory and a log written.
func TestMemoryAndLogSurviveLeakScanner(t *testing.T) {
	st := New()
	st.SetPersistPath(filepath.Join(t.TempDir(), "state.json"))
	bot, _ := st.CreateBot(ActorLocal, "M", "coder", "glm")
	if _, err := st.Remember(bot.ID, ActorLocal, "runs the test suite before pushing"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LogWorkAt(bot.ID, ActorLocal, "2026-11-04T10:00:00Z", "", "reviewed the diff"); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(); err != nil {
		t.Fatalf("save with memory and log must pass the leak scanner: %v", err)
	}
}

// A fact carrying a persistence-guard token is refused at the door, where the
// message can name it — not at save time, far from its cause.
func TestMemoryRejectsLeakToken(t *testing.T) {
	st := New()
	st.SetPersistPath(filepath.Join(t.TempDir(), "state.json"))
	bot, _ := st.CreateBot(ActorLocal, "M", "coder", "glm")
	if _, err := st.Remember(bot.ID, ActorLocal, "the api_key is in ~/.accounts"); err == nil {
		t.Fatal("a fact containing a leak token must be refused")
	}
}

func TestMemoryPromptIncludesFactsAndLog(t *testing.T) {
	st := New()
	st.SetPersistPath(filepath.Join(t.TempDir(), "state.json"))
	bot, _ := st.CreateBot(ActorLocal, "M", "coder", "glm")
	if got := MemoryPrompt(bot); got != "" {
		t.Fatalf("fresh bot prompt block = %q, want empty", got)
	}
	st.Remember(bot.ID, ActorLocal, "always write tests first")
	st.LogWorkAt(bot.ID, ActorLocal, "2026-11-04T10:00:00Z", "", "shipped M1")
	bot, _ = st.GetBot(bot.ID, ActorLocal)

	prompt := MemoryPrompt(bot)
	if !strings.Contains(prompt, "always write tests first") {
		t.Fatalf("prompt missing facts: %q", prompt)
	}
	if !strings.Contains(prompt, "shipped M1") {
		t.Fatalf("prompt missing log: %q", prompt)
	}
}

func TestBotViewReportsUsage(t *testing.T) {
	st := New()
	st.SetPersistPath(filepath.Join(t.TempDir(), "state.json"))
	bot, _ := st.CreateBot(ActorLocal, "M", "coder", "glm")
	st.Remember(bot.ID, ActorLocal, strings.Repeat("x", 100))
	bot, _ = st.GetBot(bot.ID, ActorLocal)

	v := bot.View()
	if v.MemoryUsed == 0 || v.MemoryCap == 0 {
		t.Fatalf("view = %+v", v)
	}
	if v.MemoryPercent < 1 || v.MemoryPercent > 100 {
		t.Fatalf("percent = %d", v.MemoryPercent)
	}
}

func TestMemoryDeniedForOtherActor(t *testing.T) {
	st := New()
	st.SetPersistPath(filepath.Join(t.TempDir(), "state.json"))
	bot, _ := st.CreateBot(ActorLocal, "M", "coder", "glm")
	if _, err := st.MemoryAdd(bot.ID, "someone-else", "x"); !errors.Is(err, ErrDenied) {
		t.Fatalf("err = %v, want ErrDenied", err)
	}
	if _, err := st.LogWork(bot.ID, "someone-else", "", "x"); !errors.Is(err, ErrDenied) {
		t.Fatalf("err = %v, want ErrDenied", err)
	}
}
