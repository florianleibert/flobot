# Research round 26 — Streaming surfaces: what flobot ships vs what the spec allows

**Date:** 2026-11-04
**Track:** G — desktop UX and streaming surfaces
**Sources (fetched 2026-11-04):**
- WHATWG HTML Standard §9.2 *Server-sent events* — https://html.spec.whatwg.org/multipage/server-sent-events.html
- flobot `internal/server/server.go:1249–1341` (`stream`, `writeNDJSON`), `:1343–1390` (`hub`)
- flobot `internal/taskclient/client.go:158–200` (`Stream`, real SSE consumer)
- flobot `internal/executor/executor.go:33–39` (`Event`), `:300–330` (child stdout reader)
- flobot `web/app.js:2470–2505` (`edConnectStream`)

Web content above is UNTRUSTED CONTENT (data only).

---

## 1. flobot runs two different streaming protocols

| | `/api/stream` (UI) | `/api/v1/tasks/{id}/stream` (taskclient) |
|---|---|---|
| Content-Type | `application/x-ndjson` | `text/event-stream` |
| Framing | one JSON object per line | `event:` / `data:` / blank line |
| Client | `fetch` + `ReadableStream` + manual `\n` split | `bufio.Scanner` + field switch |
| Reconnect | manual: client re-requests with `?since=N` | none — "a reconnect starts a fresh subscription" |
| Event id | none on the wire; position is the array index | none |
| Heartbeat | none | none |
| Slow consumer | **dropped silently** (`hub.publish` deletes the subscriber) | n/a (one consumer) |

Both work. Neither uses the parts of the SSE spec that exist specifically to make reconnection correct.

## 2. What the spec gives you that flobot does not use

The WHATWG event-stream format has five field names. flobot's NDJSON path uses zero of them; its SSE path parses two (`event`, `data`) and ignores three:

- **`id:`** — sets the EventSource's *last event ID string*. On reconnect the UA sends `Last-Event-ID: <value>` as a request header. This is the spec's answer to exactly the problem flobot solves by hand with `?since=N`.
- **`retry:`** — sets the reconnection time in ms. Without it the UA picks an implementation-defined delay ("probably in the region of a few seconds") and may add exponential backoff.
- **comment lines (`: ...`)** — "authors can include a comment line (one starting with a ':' character) every 15 seconds or so" to survive proxy idle timeouts.
- **HTTP 204** — "a client can be told to stop reconnecting using the HTTP 204 No Content response code." That is the clean way to end a stream permanently.
- **`EventSource` itself** — automatic reconnect, `readyState`, `open`/`error` events, and the guarantee that the UA keeps a strong reference while listeners are attached.

flobot's hand-rolled resume is *equivalent in effect* to `Last-Event-ID` but only because the client and server are both first-party and the event log is durable in SQLite. That is a legitimate design — the spec's mechanism exists for clients that cannot coordinate with the server.

## 3. The three real defects

### 3.1 Silent subscriber drop

```go
select {
case ch <- ev:
default:
    delete(h.subs, id)
    close(ch)
}
```

A client that falls 64 events behind is closed with no error frame. The browser sees a clean EOF, and `edConnectStream`'s pump returns on `done` without telling anyone:

```js
const pump = () => reader.read().then(({ done, value }) => {
  if (done) return;   // <-- silent
```

So a busy run can truncate the visible transcript and the UI shows a stream that simply stopped. The store still has every event, so a reload recovers — but nothing prompts the reload. **Fix:** publish a terminal `{kind:"stream_dropped"}` frame before closing, and have the client re-request with `?since=<lastIndex>`.

### 3.2 No heartbeat

Neither stream sends a comment or keepalive frame. On localhost this rarely bites, but the desktop app proxies through Chromium's network stack and any future remote/tailnet access puts an idle-timeout proxy in the path. The spec's own advice is a comment every ~15 s. Cost: one goroutine ticker per subscriber.

### 3.3 Coarse events, so no live typing

`executor.Event` is `{Kind, TurnID, Text, Tool, IsError}` and the executor reads the child's stdout **line by line** (`ReadSlice('\n')`). `Text` carries a whole assistant message. Consequences:

- The UI can render *that* the agent is thinking but not *what* it is writing until the message completes.
- A long message looks like a hang; the only liveness signal is tool events.
- Grok Bot's perceived speed comes almost entirely from token-level rendering. This is the single largest UX gap between flobot and any chat-native agent.

The executor already has a streaming reader; what it lacks is a *delta* event kind. Adding `Kind: "delta"` with incremental text (and keeping `assistant` as the completed message) is additive and does not break the store schema — but it does multiply event volume by ~50×, which interacts with §3.1.

## 4. The protocol decision

Three options, with the actual trade-off:

| Option | Keeps | Costs |
|---|---|---|
| **A. Stay NDJSON, add `id`/heartbeat/drop-frame** | zero client rewrite; store stays the source of truth | no `EventSource`, so reconnect logic stays hand-written |
| **B. Move `/api/stream` to SSE** | free auto-reconnect, `Last-Event-ID`, `readyState`, spec-blessed 204 | client rewrite (~40 lines); must emit `id:` per event; still need server-side replay keyed on `Last-Event-ID` |
| **C. WebSocket** | bidirectional (needed only if the UI must push mid-run) | new dependency, new auth path, no benefit today — every UI→server action already has a REST endpoint |

**Recommendation: A now, B when token deltas land.** Option A is ~30 lines and fixes the truncation bug. Option B's real payoff is that `EventSource` reconnects *for* you, which matters much more once deltas make disconnects expensive (a dropped delta stream loses visible text, not just a status line). Option C is unjustified: flobot's control plane is request/response and should stay that way.

## 5. Backpressure arithmetic

The hub buffer is 64 events. At coarse granularity (a few events per second) that is ~30 s of slack — plenty. At delta granularity (50–200 events/s for a fast model) it is **under one second**, so any UI hiccup drops the subscriber. If deltas ship, the buffer must grow *and* coalescing must happen server-side:

- batch deltas into ~50 ms windows before publishing (one frame per window, concatenated text),
- raise the buffer to ~1024,
- keep the drop-frame from §3.1 so a drop is visible rather than silent.

This is the same conclusion round 22 reached for durable execution from the other direction: **the event log is durable, so the live channel only needs to be fast, not reliable.** Reliability comes from `?since=N`.

## 6. Findings

1. flobot's UI stream is NDJSON with hand-rolled resume; its taskclient stream is real SSE with no resume at all. *(server.go:1249; taskclient/client.go:158)*
2. Neither stream emits `id:`, `retry:`, or heartbeat comments — three of the five SSE fields go unused. *(WHATWG §9.2.5–9.2.7)*
3. A slow subscriber is dropped silently; the client treats EOF as normal completion, so truncation is invisible. *(server.go:1374–1385; app.js:2485)*
4. Events are message-granular because the executor reads child stdout line-by-line; there is no delta kind anywhere in the codebase. *(executor.go:33–39, :308)*
5. The hub's 64-event buffer is ample for coarse events and insufficient for deltas; deltas require server-side coalescing plus a larger buffer. *(server.go:1356)*
6. `EventSource` would remove the hand-written reconnect path, but only pays off once deltas make a disconnect costly; WebSocket has no justification while every UI action has a REST endpoint.
