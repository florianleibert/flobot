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
