// stream.go is the NDJSON event feed and the hub that fans it out.
//
// The feed used to lie in three ways that all looked like normal operation:
//
//   - A slow subscriber was dropped silently, so the client saw EOF and treated
//     truncation as completion. The store still had every event; nothing told
//     the client to re-read.
//   - An idle connection sent nothing at all, so any proxy in the path could
//     time it out and the client would reconnect into a gap.
//   - Events were message-granular, so a long answer looked like a hang.
//
// So this file adds three things and keeps NDJSON (option A in round 26 §4):
// a terminal `stream_dropped` frame before a slow subscriber is closed, a
// heartbeat frame every ~15 s, and a `delta` kind coalesced server-side into
// ~50 ms windows with a larger hub buffer.
package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/florianleibert/flobot/internal/executor"
	"github.com/florianleibert/flobot/internal/store"
)

const (
	// deltaKind is a token-level fragment of an answer. It is coalesced before
	// it reaches a subscriber: one frame per fragment would be thousands of
	// frames per answer.
	deltaKind = executor.KindDelta
	// streamDroppedKind is the terminal frame a subscriber receives when the
	// hub gave up on it. It is what turns a silent truncation into a visible
	// one: the client reconnects with ?since= instead of assuming completion.
	streamDroppedKind = "stream_dropped"
	// heartbeatKind is an idle keepalive. It carries no state; it exists so an
	// idle connection is not mistaken for a dead one.
	heartbeatKind = "heartbeat"
)

var (
	// hubBuffer is how many events a subscriber may fall behind before it is
	// dropped. Coarse events fit in 64; deltas do not, which is why this is
	// larger than it was.
	hubBuffer = 512
	// deltaWindow is how long deltas are collected before they are flushed.
	deltaWindow = 50 * time.Millisecond
	// streamHeartbeatInterval is how often an idle feed sends a keepalive.
	streamHeartbeatInterval = 15 * time.Second
)

// stream is the NDJSON event feed. It replays the events of the run given by
// ?run= (if any), then forwards live executor events until the client
// disconnects. A disconnect or reconnect never affects the server or other
// clients.
//
// ?since=N skips the first N events of that run, so a client that was dropped
// resumes exactly where it stopped: no duplicate, no gap.
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Flush the headers now so the client's request returns immediately,
	// even before the first event arrives.
	fl.Flush()

	// Subscribe before replaying so no event can slip between the replay
	// snapshot and the subscription: anything appended after the snapshot is
	// re-read from the store below and emitted as a tail.
	ch, cancel := s.hub.subscribe()
	defer cancel()

	// drain discards anything already queued on ch so the tail re-read below is
	// the sole source for pre-live events (no duplicates).
	drain := func() {
		for {
			select {
			case <-ch:
			default:
				return
			}
		}
	}

	if id := strings.TrimSpace(r.URL.Query().Get("run")); id != "" {
		since := 0
		if v := strings.TrimSpace(r.URL.Query().Get("since")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				since = n
			}
		}
		if run, err := s.store.GetRun(id, Actor); err == nil {
			if since > len(run.Events) {
				since = len(run.Events)
			}
			for _, ev := range run.Events[since:] {
				if !writeNDJSON(w, ev) {
					return
				}
			}
			fl.Flush()
			drain()
			if run2, err := s.store.GetRun(id, Actor); err == nil && len(run2.Events) > len(run.Events) {
				for _, ev := range run2.Events[len(run.Events):] {
					if !writeNDJSON(w, ev) {
						return
					}
				}
				fl.Flush()
			}
		}
	}

	hb := time.NewTicker(streamHeartbeatInterval)
	defer hb.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-hb.C:
			if !writeNDJSON(w, store.Event{Kind: heartbeatKind}) {
				return
			}
			fl.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if !writeNDJSON(w, ev) {
				return
			}
			fl.Flush()
			if ev.Kind == streamDroppedKind {
				// The hub closed us because we fell behind. The frame has been
				// delivered; returning ends the response so the client's
				// reconnect logic runs instead of waiting on a dead feed.
				return
			}
		}
	}
}

func writeNDJSON(w http.ResponseWriter, ev store.Event) bool {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(ev) == nil
}

// hub fans executor events out to every connected /api/stream client.
//
// Each subscriber owns a buffered channel; a slow or gone client is dropped
// without blocking the publisher or the other clients — but it is told it was
// dropped first.
type hub struct {
	mu   sync.Mutex
	next uint64
	subs map[uint64]chan store.Event

	// pending holds deltas collected inside the current window; timer fires the
	// flush that merges them into one frame.
	pending []store.Event
	timer   *time.Timer
}

func newHub() *hub {
	return &hub{subs: make(map[uint64]chan store.Event)}
}

func (h *hub) subscribe() (<-chan store.Event, func()) {
	h.mu.Lock()
	h.next++
	id := h.next
	buf := hubBuffer
	if buf <= 0 {
		buf = 64
	}
	ch := make(chan store.Event, buf)
	h.subs[id] = ch
	h.mu.Unlock()
	cancel := func() {
		h.mu.Lock()
		if c, ok := h.subs[id]; ok {
			delete(h.subs, id)
			close(c)
		}
		h.mu.Unlock()
	}
	return ch, cancel
}

// publish sends one event to every subscriber.
//
// A delta is not sent immediately: it joins the current window and one merged
// frame goes out when the window closes. Everything else flushes the window
// first, so a delta never arrives after the message it belongs to.
func (h *hub) publish(ev store.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ev.Kind == deltaKind {
		h.pending = append(h.pending, ev)
		if h.timer == nil {
			h.timer = time.AfterFunc(deltaWindow, h.flushDeltas)
		}
		return
	}
	h.flushPendingLocked()
	h.broadcastLocked(ev)
}

func (h *hub) flushDeltas() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.timer = nil
	h.flushPendingLocked()
}

func (h *hub) flushPendingLocked() {
	if len(h.pending) == 0 {
		return
	}
	batch := coalesceDeltas(h.pending)
	h.pending = h.pending[:0]
	for _, ev := range batch {
		h.broadcastLocked(ev)
	}
}

func (h *hub) broadcastLocked(ev store.Event) {
	for id, ch := range h.subs {
		if push(ch, ev) {
			continue
		}
		// Slow client. Make room for one terminal frame so the truncation is
		// visible rather than an unexplained EOF, then close it.
		makeRoom(ch)
		dropped := store.Event{Kind: streamDroppedKind}
		if ev.Kind == streamDroppedKind {
			dropped = ev
		}
		push(ch, dropped)
		delete(h.subs, id)
		close(ch)
	}
}

// push is a non-blocking send. It reports whether ev was delivered; a full
// buffer means the subscriber has fallen behind and is about to be dropped.
func push(ch chan store.Event, ev store.Event) bool {
	select {
	case ch <- ev:
		return true
	default:
		return false
	}
}

// makeRoom discards the oldest queued event so one terminal frame fits in an
// otherwise full buffer. The client can re-read what was discarded from the
// store with ?since=; a stale frame is worth less than the drop notice.
func makeRoom(ch chan store.Event) {
	select {
	case <-ch:
	default:
	}
}

// coalesceDeltas merges consecutive deltas of the same turn into one event.
//
// Consecutive matters: two turns interleaved must not be concatenated into one
// string, or the transcript would show one turn's text inside another's.
func coalesceDeltas(in []store.Event) []store.Event {
	out := make([]store.Event, 0, len(in))
	for _, ev := range in {
		if n := len(out); n > 0 && out[n-1].TurnID == ev.TurnID && out[n-1].Kind == deltaKind {
			out[n-1].Text += ev.Text
			continue
		}
		out = append(out, ev)
	}
	return out
}
