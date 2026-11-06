package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/florianleibert/flobot/internal/executor"
	"github.com/florianleibert/flobot/internal/store"
)

// TestStreamSendsHeartbeatWhenIdle is the idle-connection fix: a feed with
// nothing to say must still say something, or a proxy in the path times it out
// and the client reconnects into a gap.
func TestStreamSendsHeartbeatWhenIdle(t *testing.T) {
	old := streamHeartbeatInterval
	streamHeartbeatInterval = 20 * time.Millisecond
	t.Cleanup(func() { streamHeartbeatInterval = old })

	srv := New(store.New())
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	sc := bufio.NewScanner(resp.Body)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !sc.Scan() {
			t.Fatalf("stream closed before a heartbeat: %v", sc.Err())
		}
		var ev store.Event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("bad frame %q: %v", sc.Text(), err)
		}
		if ev.Kind == heartbeatKind {
			return
		}
	}
	t.Fatal("no heartbeat on an idle stream")
}

// TestStreamDropIsVisible is the silent-truncation fix: a subscriber that falls
// behind is told it was dropped before its channel closes, so the client can
// resume with ?since= instead of treating EOF as completion.
func TestStreamDropIsVisible(t *testing.T) {
	h := newHub()
	ch, cancel := h.subscribe()
	defer cancel()

	// Fill the buffer and then some, without ever reading: exactly the slow
	// reader the old hub dropped silently.
	total := hubBuffer + 8
	for i := 0; i < total; i++ {
		h.publish(store.Event{Kind: "content", Text: "x"})
	}

	var sawDrop bool
	for ev := range ch {
		if ev.Kind == streamDroppedKind {
			sawDrop = true
		}
	}
	if !sawDrop {
		t.Fatal("slow subscriber was closed without a stream_dropped frame")
	}
	if len(h.subs) != 0 {
		t.Fatalf("dropped subscriber still registered: %d", len(h.subs))
	}
}

// TestStreamDeltasCoalesce is the long-answer fix: fragments are merged into
// ~50 ms windows, and two interleaved turns are never concatenated.
func TestStreamDeltasCoalesce(t *testing.T) {
	h := newHub()
	ch, cancel := h.subscribe()
	defer cancel()

	h.publish(store.Event{Kind: deltaKind, TurnID: 1, Text: "he"})
	h.publish(store.Event{Kind: deltaKind, TurnID: 1, Text: "llo"})
	h.publish(store.Event{Kind: deltaKind, TurnID: 2, Text: "other"})
	// A non-delta event flushes the window first, so a delta never arrives
	// after the message it belongs to.
	h.publish(store.Event{Kind: "content", TurnID: 1, Text: "hello"})

	var got []store.Event
	for len(got) < 3 {
		select {
		case ev := <-ch:
			got = append(got, ev)
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out after %d frames: %v", len(got), got)
		}
	}
	if got[0].Kind != deltaKind || got[0].Text != "hello" || got[0].TurnID != 1 {
		t.Fatalf("first frame = %+v, want coalesced turn-1 delta", got[0])
	}
	if got[1].Kind != deltaKind || got[1].Text != "other" || got[1].TurnID != 2 {
		t.Fatalf("second frame = %+v, want turn-2 delta kept separate", got[1])
	}
	if got[2].Kind != "content" {
		t.Fatalf("third frame = %+v, want the content event", got[2])
	}
}

// TestStreamResumeHasNoDuplicateAndNoGap is the resume acceptance criterion:
// killing the client mid-run and reconnecting with ?since= yields exactly the
// events it had not seen yet.
func TestStreamResumeHasNoDuplicateAndNoGap(t *testing.T) {
	st := store.New()
	dir := t.TempDir()
	bin, err := executor.WriteFakeBinary(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(st, WithExec(&executor.Exec{Bin: bin}))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{
		"name": "Resumer", "role": "coder", "route": "glm",
	})
	run := postJSON(t, ts, "/api/v1/runs", map[string]any{
		"bot_id": bot["id"], "prompt": "hello", "intent": "ask",
	})
	runID, _ := run["id"].(string)
	if runID == "" {
		t.Fatalf("run = %v", run)
	}
	full := waitEvents(t, ts, runID)
	total := len(full["events"].([]any))
	if total < 2 {
		t.Fatalf("events = %v", full["events"])
	}

	kinds := func(since int) []string {
		need := total - since
		if need <= 0 {
			// Nothing to replay: the client already has every event. Reading
			// here would pick up live events published after the run, which
			// are not part of the resume contract.
			return nil
		}
		resp, err := http.Get(ts.URL + "/api/stream?run=" + runID + "&since=" + strconv.Itoa(since))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out []string
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			var ev store.Event
			if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
				t.Fatalf("bad frame %q: %v", sc.Text(), err)
			}
			if ev.Kind == heartbeatKind {
				continue
			}
			out = append(out, ev.Kind)
			if len(out) >= need {
				break
			}
		}
		return out
	}

	all := kinds(0)
	if len(all) != total {
		t.Fatalf("since=0 replayed %d of %d events: %v", len(all), total, all)
	}
	for _, since := range []int{1, total / 2, total} {
		got := kinds(since)
		want := all[since:]
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("since=%d replayed %v, want %v (duplicate or gap)", since, got, want)
		}
	}
}

// TestLiveFramesCarryStoredSeq pins the other half of the resume contract: a
// frame delivered live must carry the same seq the store gave it, or a client
// cannot say where it stopped without re-reading the run.
func TestLiveFramesCarryStoredSeq(t *testing.T) {
	st := store.New()
	srv := New(st)

	if _, err := st.CreateRun(Actor, "", "p", "ask", false, false, "", "", ""); err != nil {
		t.Fatal(err)
	}
	runID := ""
	if runs := st.ListRuns(Actor); len(runs) > 0 {
		runID = runs[0].ID
	}
	if runID == "" {
		t.Fatal("no run created")
	}
	for _, ev := range []store.Event{
		{Kind: "content", TurnID: 1, Text: "one"},
		{Kind: "content", TurnID: 1, Text: "two"},
	} {
		// Mirror the run loop: store, then publish what was stored.
		if err := st.AppendEvent(runID, Actor, ev); err != nil {
			t.Fatal(err)
		}
		stored, err := st.ListEvents(runID, Actor)
		if err != nil {
			t.Fatal(err)
		}
		want := stored[len(stored)-1].Seq
		if got := srv.storedEvent(runID, ev); got.Seq != want {
			t.Fatalf("live frame for %q carried seq %d, want %d", ev.Text, got.Seq, want)
		}
	}
}

// TestResumeOffsetIsSeqNotFrameCount is the defect this round found, stated the
// way a browser hits it.
//
// The hub merges consecutive deltas of one turn into a single frame, but the
// store keeps every fragment. A client that counted the frames it had rendered
// and asked to resume from that count was indexing the frame sequence with a
// number that belongs to the event sequence: after an answer streamed, the
// replay started too early and re-rendered events the client had already shown.
// Every frame now carries the seq the store gave it, and the client resumes
// from that number, so the offset is in the sequence the server replays from.
//
// The subscriber is opened before the deltas are published: coalescing only
// happens for frames that are actually broadcast, and a delta published to no
// subscriber is simply stored.
func TestResumeOffsetIsSeqNotFrameCount(t *testing.T) {
	st := store.New()
	srv := New(st)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{
		"name": "Streamer", "role": "coder", "route": "glm",
	})
	run := postJSON(t, ts, "/api/v1/runs", map[string]any{
		"bot_id": bot["id"], "prompt": "hello", "intent": "ask",
	})
	runID, _ := run["id"].(string)
	if runID == "" {
		t.Fatalf("run = %v", run)
	}
	waitEvents(t, ts, runID)

	resp, err := http.Get(ts.URL + "/api/stream?run=" + runID)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	frames := make(chan store.Event, 64)
	go func() {
		for sc.Scan() {
			var ev store.Event
			if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
				return
			}
			if ev.Kind == heartbeatKind {
				continue
			}
			frames <- ev
		}
	}()

	// A turn whose answer arrives as five fragments. Pairing the store write
	// with the publish is what the run loop does; the divergence exists
	// because publish coalesces what AppendEvent kept.
	deltas := []store.Event{
		{Kind: executor.KindDelta, TurnID: 9, Text: "a"},
		{Kind: executor.KindDelta, TurnID: 9, Text: "b"},
		{Kind: executor.KindDelta, TurnID: 9, Text: "c"},
		{Kind: executor.KindDelta, TurnID: 9, Text: "d"},
		{Kind: executor.KindDelta, TurnID: 9, Text: "e"},
	}
	for _, ev := range deltas {
		if err := st.AppendEvent(runID, Actor, ev); err != nil {
			t.Fatal(err)
		}
		srv.hub.publish(srv.storedEvent(runID, ev))
	}

	stored, err := st.ListEvents(runID, Actor)
	if err != nil {
		t.Fatal(err)
	}
	for i, ev := range stored {
		if ev.Seq != i+1 {
			t.Fatalf("event %d has seq %d, want %d (seq must be its position)", i, ev.Seq, i+1)
		}
	}

	// Collect what the live subscriber received for this turn. The five
	// fragments must arrive as one coalesced frame, which is the whole point:
	// the client rendered one frame but the store holds five events.
	var live []store.Event
	for {
		select {
		case ev := <-frames:
			if ev.TurnID != 9 {
				continue
			}
			live = append(live, ev)
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out after %d live frames: %v", len(live), live)
		}
		if len(live) > 0 {
			break
		}
	}
	if len(live) >= len(deltas) {
		t.Fatalf("no coalescing happened: %d frames for %d deltas", len(live), len(deltas))
	}
	if got := live[0].Text; got != "abcde" {
		t.Fatalf("coalesced frame text = %q, want %q", got, "abcde")
	}

	// The frame count is smaller than the seq the client reached: that gap is
	// the defect. Resuming from the seq must replay exactly the events after
	// it, and resuming from the frame count must not.
	lastSeq := live[len(live)-1].Seq
	if lastSeq <= len(live) {
		t.Fatalf("last seq %d is not beyond the frame count %d; the defect cannot be exercised",
			lastSeq, len(live))
	}

	resp2, err := http.Get(ts.URL + "/api/stream?run=" + runID + "&since=" + strconv.Itoa(lastSeq))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var replay []int
	sc2 := bufio.NewScanner(resp2.Body)
	for len(replay) < len(stored)-lastSeq && sc2.Scan() {
		var ev store.Event
		if err := json.Unmarshal(sc2.Bytes(), &ev); err != nil {
			t.Fatalf("bad frame %q: %v", sc2.Text(), err)
		}
		if ev.Kind == heartbeatKind {
			continue
		}
		if ev.Seq <= lastSeq {
			t.Fatalf("resuming from seq %d replayed seq %d (duplicate)", lastSeq, ev.Seq)
		}
		replay = append(replay, ev.Seq)
	}
	if len(replay) != len(stored)-lastSeq {
		t.Fatalf("resuming from seq %d replayed %d of %d events: %v",
			lastSeq, len(replay), len(stored)-lastSeq, replay)
	}
}

// TestResumeSeesTerminalEventAtMaxEvents is the case the seq actually fixes.
//
// A run that reaches cap overwrites its final slot with the terminal
// event, so the event count stops growing. The old resume compared lengths to
// decide whether anything had arrived since the snapshot, which meant a client
// that subscribed at exactly that moment never saw the run finish: the length
// was unchanged, so the second read was skipped. Comparing seqs instead makes
// the overwrite visible, because the last seq is the same but the event behind
// it is not.
func TestResumeSeesTerminalEventAtMaxEvents(t *testing.T) {
	st := store.New()
	srv := New(st)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	bot := postJSON(t, ts, "/api/v1/bots", map[string]any{
		"name": "Full", "role": "coder", "route": "glm",
	})
	run := postJSON(t, ts, "/api/v1/runs", map[string]any{
		"bot_id": bot["id"], "prompt": "hello", "intent": "ask",
	})
	runID, _ := run["id"].(string)
	if runID == "" {
		t.Fatalf("run = %v", run)
	}
	waitEvents(t, ts, runID)

	// Fill the run to the store's cap with content events. The cap is
	// unexported, so it is mirrored here; a change there fails this test
	// loudly rather than silently skipping the overwrite case.
	const cap = 2000
	for i := 0; i < cap; i++ {
		if err := st.AppendEvent(runID, Actor, store.Event{Kind: "content", TurnID: 5, Text: "filler"}); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := st.ListEvents(runID, Actor)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != cap {
		t.Fatalf("run holds %d events, want %d", len(stored), cap)
	}

	// The terminal event replaces the last slot rather than extending the run.
	if err := st.AppendEvent(runID, Actor, store.Event{Kind: "done", TurnID: 5}); err != nil {
		t.Fatal(err)
	}
	stored2, err := st.ListEvents(runID, Actor)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored2) != len(stored) {
		t.Fatalf("terminal event grew the run from %d to %d; the overwrite case is not exercised",
			len(stored), len(stored2))
	}
	if last := stored2[len(stored2)-1]; last.Kind != "done" {
		t.Fatalf("last event is %q, want done", last.Kind)
	}
	if last := stored2[len(stored2)-1]; last.Seq != cap {
		t.Fatalf("terminal event seq = %d, want %d", last.Seq, cap)
	}

	// Resuming from just before the end must deliver the terminal event.
	resp, err := http.Get(ts.URL + "/api/stream?run=" + runID + "&since=" + strconv.Itoa(cap-1))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got []store.Event
	sc := bufio.NewScanner(resp.Body)
	for len(got) < 1 && sc.Scan() {
		var ev store.Event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("bad frame %q: %v", sc.Text(), err)
		}
		if ev.Kind == heartbeatKind {
			continue
		}
		got = append(got, ev)
	}
	if len(got) != 1 || got[0].Kind != "done" {
		t.Fatalf("resume from %d delivered %v, want the terminal event", cap-1, got)
	}
}
