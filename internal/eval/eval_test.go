package eval

import (
	"path/filepath"
	"testing"
)

// TestReplayReproducesRecordedOutcomes is the M7 acceptance check: replaying ten
// stored runs reproduces their recorded outcomes.
func TestReplayReproducesRecordedOutcomes(t *testing.T) {
	runs := make([]Run, 0, 10)
	for i := 0; i < 10; i++ {
		status := "done"
		events := []Event{
			{Kind: "delta", TurnID: 1, Text: "partial "},
			{Kind: "content", TurnID: 1, Text: "the answer"},
			{Kind: "done", TurnID: 1},
		}
		if i%3 == 0 {
			status = "error"
			events = []Event{
				{Kind: "content", TurnID: 1, Text: "started"},
				{Kind: "err", TurnID: 1, Text: "flocode exited: exit status 1"},
			}
		}
		runs = append(runs, Run{
			ID:     "run-" + string(rune('a'+i)),
			Status: status,
			Events: events,
		})
	}

	results := Replay(runs)
	if len(results) != 10 {
		t.Fatalf("replayed %d runs, want 10", len(results))
	}
	for _, r := range results {
		if !r.Reproduced {
			t.Fatalf("run %s did not reproduce its recorded outcome: %+v", r.RunID, r.Scores)
		}
		// An error run is expected to fail no_error_events; every other scorer
		// must still pass, or the scorer set is measuring the wrong thing.
		for _, sc := range r.Scores {
			if sc.Scorer == "no_error_events" && r.Status == "error" {
				continue
			}
			if !sc.Pass {
				t.Fatalf("run %s failed %s: %s", r.RunID, sc.Scorer, sc.Detail)
			}
		}
	}
	reproduced, total := Summary(results)
	if reproduced != 10 || total != 10 {
		t.Fatalf("summary = %d/%d", reproduced, total)
	}
}

// TestReplayCatchesStatusDrift: a run whose stored status disagrees with its
// events is exactly the drift replay exists to surface.
func TestReplayCatchesStatusDrift(t *testing.T) {
	runs := []Run{{
		ID:     "run-drift",
		Status: "done",
		Events: []Event{{Kind: "err", Text: "boom"}},
	}}
	res := Replay(runs)[0]
	if res.Reproduced {
		t.Fatal("status drift was not detected")
	}
	var found bool
	for _, sc := range res.Scores {
		if sc.Scorer == "status_matches_events" && !sc.Pass {
			found = true
		}
	}
	if !found {
		t.Fatalf("status_matches_events did not fail: %+v", res.Scores)
	}
}

// TestReplayFlagsEmptyOutput keeps a silent run from passing as a good one.
func TestReplayFlagsEmptyOutput(t *testing.T) {
	res := Replay([]Run{{
		ID:     "run-silent",
		Status: "done",
		Events: []Event{{Kind: "done"}},
	}})[0]
	for _, sc := range res.Scores {
		if sc.Scorer == "has_output" && sc.Pass {
			t.Fatal("a run with no text passed has_output")
		}
	}
}

// TestVerdictSurvivesRestart is the M7 acceptance check for labels: an operator
// verdict is still there after a reload from disk.
func TestVerdictSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "verdicts.json")

	v := NewVerdicts(path)
	if err := v.Load(); err != nil {
		t.Fatalf("load empty: %v", err)
	}
	if err := v.Record("run-1", VerdictGood, "clean diff"); err != nil {
		t.Fatal(err)
	}
	if err := v.Record("run-2", VerdictBad, "hallucinated a file"); err != nil {
		t.Fatal(err)
	}

	// Restart: a fresh store reading the same file.
	v2 := NewVerdicts(path)
	if err := v2.Load(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, ok := v2.Get("run-1")
	if !ok || got.Verdict != VerdictGood || got.Note != "clean diff" {
		t.Fatalf("run-1 = %+v ok=%v", got, ok)
	}
	if got, ok := v2.Get("run-2"); !ok || got.Verdict != VerdictBad {
		t.Fatalf("run-2 = %+v ok=%v", got, ok)
	}
	if all := v2.All(); len(all) != 2 || all[0].RunID != "run-1" || all[1].RunID != "run-2" {
		t.Fatalf("all = %+v", all)
	}
}

// TestVerdictRejectsInvalidValue: a label that did not stick must be visible,
// not a silent no-op.
func TestVerdictRejectsInvalidValue(t *testing.T) {
	v := NewVerdicts(filepath.Join(t.TempDir(), "verdicts.json"))
	if err := v.Record("run-1", "meh", ""); err == nil {
		t.Fatal("accepted an invalid verdict")
	}
	if err := v.Record("", VerdictGood, ""); err == nil {
		t.Fatal("accepted an empty run id")
	}
}

// TestPromptPrefixIsStable is the M7 acceptance check for cache discipline:
// assembling the same bot twice differs only after the stable prefix.
func TestPromptPrefixIsStable(t *testing.T) {
	bot := BotContext{
		Name:      "Coder",
		Role:      "coder",
		SysPrompt: "You are flobot.",
		Soul:      "Be terse.",
		Memory:    "- shipped the stream fix",
		Sessions:  []string{"Session: earlier today we fixed the hub."},
	}

	a := Assemble(bot, Request{Prompt: "first prompt", Intent: "code"})
	b := Assemble(bot, Request{Prompt: "a completely different prompt", Intent: "ask"})

	if a.PrefixString() != b.PrefixString() {
		t.Fatalf("prefix differs between runs:\n%q\n%q", a.PrefixString(), b.PrefixString())
	}
	if a.String() == b.String() {
		t.Fatal("full prompts are identical; the volatile tail was dropped")
	}
}

// TestPromptPrefixIsCacheableAcrossMemoryGrowth proves the prefix is stable for
// the same bot state and that memory lives inside it, not ahead of it.
func TestPromptPrefixIsCacheableAcrossMemoryGrowth(t *testing.T) {
	bot := BotContext{Name: "Coder", Role: "coder", SysPrompt: "You are flobot."}
	p := Assemble(bot, Request{Prompt: "hello"})
	if len(p.Prefix) == 0 {
		t.Fatal("no stable prefix")
	}
	if p.Prefix[0] != "You are flobot." {
		t.Fatalf("sys prompt is not first: %q", p.Prefix[0])
	}
	if p.Tail[len(p.Tail)-1] != "hello" {
		t.Fatalf("prompt is not last: %q", p.Tail)
	}

	grown := bot
	grown.Memory = "- learned something"
	p2 := Assemble(grown, Request{Prompt: "hello"})
	if p2.PrefixString() == p.PrefixString() {
		t.Fatal("memory did not reach the prefix")
	}
}

// TestDecisionStringExplainsRoute keeps an unexplained route from being
// indistinguishable from a bug.
func TestDecisionStringExplainsRoute(t *testing.T) {
	d := Decision{
		Route:     "qwen",
		Model:     "qwen-3.8",
		Reason:    "bot pin",
		Fallbacks: []string{"glm"},
		Filtered:  []string{"local"},
	}
	want := "qwen (qwen-3.8): bot pin · fallback glm · filtered local"
	if got := d.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
