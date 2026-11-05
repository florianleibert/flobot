package executor

import "testing"

// TestCoalesceDeltasMergesConsecutiveFragments is the long-answer fix: one
// frame per fragment would be thousands of frames per answer.
func TestCoalesceDeltasMergesConsecutiveFragments(t *testing.T) {
	in := []Event{
		{Kind: KindDelta, TurnID: 1, Text: "he"},
		{Kind: KindDelta, TurnID: 1, Text: "llo"},
		{Kind: KindDelta, TurnID: 1, Text: " world"},
	}
	got := CoalesceDeltas(in)
	if len(got) != 1 {
		t.Fatalf("got %d frames, want 1: %+v", len(got), got)
	}
	if got[0].Text != "hello world" || got[0].TurnID != 1 {
		t.Fatalf("frame = %+v", got[0])
	}
}

// TestCoalesceDeltasKeepsTurnsSeparate: two interleaved turns must never be
// concatenated, or one turn's text would appear inside another's.
func TestCoalesceDeltasKeepsTurnsSeparate(t *testing.T) {
	in := []Event{
		{Kind: KindDelta, TurnID: 1, Text: "a"},
		{Kind: KindDelta, TurnID: 2, Text: "b"},
		{Kind: KindDelta, TurnID: 1, Text: "c"},
	}
	got := CoalesceDeltas(in)
	if len(got) != 3 {
		t.Fatalf("got %d frames, want 3: %+v", len(got), got)
	}
	for i, want := range []string{"a", "b", "c"} {
		if got[i].Text != want {
			t.Fatalf("frame %d = %q, want %q", i, got[i].Text, want)
		}
	}
}

// TestCoalesceDeltasBreaksOnNonDelta: a delta must never be merged across the
// message it belongs to.
func TestCoalesceDeltasBreaksOnNonDelta(t *testing.T) {
	in := []Event{
		{Kind: KindDelta, TurnID: 1, Text: "a"},
		{Kind: "tool_start", TurnID: 1, Tool: "read"},
		{Kind: KindDelta, TurnID: 1, Text: "b"},
	}
	got := CoalesceDeltas(in)
	if len(got) != 3 {
		t.Fatalf("got %d frames, want 3: %+v", len(got), got)
	}
	if got[1].Kind != "tool_start" {
		t.Fatalf("middle frame = %+v", got[1])
	}
}

// TestTurnTextPrefersDeltas proves the reconstruction contract: a turn that
// streamed deltas is rebuilt from them, and a turn that did not falls back to
// its content event, so old runs still render.
func TestTurnTextPrefersDeltas(t *testing.T) {
	streamed := []Event{
		{Kind: KindDelta, TurnID: 1, Text: "hel"},
		{Kind: KindDelta, TurnID: 1, Text: "lo"},
		{Kind: "content", TurnID: 1, Text: "hello"},
		{Kind: "content", TurnID: 2, Text: "other turn"},
	}
	if got := TurnText(streamed, 1); got != "hello" {
		t.Fatalf("turn 1 = %q", got)
	}

	legacy := []Event{{Kind: "content", TurnID: 1, Text: "from content"}}
	if got := TurnText(legacy, 1); got != "from content" {
		t.Fatalf("legacy turn = %q", got)
	}
	if got := TurnText(legacy, 9); got != "" {
		t.Fatalf("unknown turn = %q", got)
	}
}
