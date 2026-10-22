# flobot → flocode backend harness

**Status:** design (research complete, no code changed)
**Date:** 2026-10-22
**Scope:** replace flobot's hand-rolled child-process executor with flocode's
production HTTP API server (`flocode serve`), so flobot becomes a thin UI +
routing layer over the full flocode backend.

---

## 1. The key discovery

flocode is **not just a CLI** — it ships a complete, production-grade HTTP API
server (`flocode serve`, default port 8080) with a task-execution surface that
is exactly what flobot needs:

| flobot needs | flocode provides |
|---|---|
| run a prompt against a model | `POST /api/v1/tasks` |
| stream progress | `GET /api/v1/tasks/{id}/stream` (SSE) |
| cancel a run | `POST /api/v1/tasks/{id}/cancel` |
| get status | `GET /api/v1/tasks/{id}` |
| get final result | `GET /api/v1/tasks/{id}/result` |
| list available models | `GET /api/v1/backends` |
| health | `GET /healthz` |

Plus mesh (`/api/v1/mesh/*`), chat (`/api/v1/chat/*`), wiki
(`/api/v1/wiki/*`), sessions (`/api/v1/sessions/*`), work threads
(`/api/v1/work/*`), stats (`/api/v1/stats`), and capabilities.

It also ships a **public Go SDK** (`github.com/florianleibert/flocode/pkg/flocode`)
for in-process embedding.

## 2. What flobot does today

- `internal/executor/executor.go` spawns the `flocode` binary as a child:
  `flocode -p <prompt> --model <pin> --intent <intent> --output streaming-json`
- Parses the legacy `streaming-json` NDJSON protocol by hand
- Kills the process group on cancel (`killGroup` in `sys_unix.go`)
- Routes (`internal/routes/routes.go`): `glm`→`glm-5.2`, `qwen`→`qwen-3.8`,
  `muse`→`muse-spark-1.3`, plus `direct/<model-id>` and `mesh/<model-id>`
- Config overrides written to temp files for mesh/direct routes
- Muse key injected via `MODEL_API_KEY` env

**Problems with this approach:**
- Reimplements process supervision, streaming, cancellation, and error handling
  that flocode already has, battle-tested
- The legacy `streaming-json` protocol is deprecated in favor of `stream-json`
  (I8) and the SSE task API
- No task persistence, no result contracts, no ownership scoping
- Each run is a fresh cold child process (slow startup, no warm runtime)

## 3. The harness design

### Option A (recommended): HTTP backend — `flocode serve` + flobot as client

```
┌─────────────┐   HTTP (loopback)   ┌──────────────────────────────┐
│  Electron   │ ──────────────────▶ │  flobot server (Go)          │
│  (web UI)   │                     │  - routes, roster, store     │
└─────────────┘                     │  - thin task client →        │
                                    └──────────┬───────────────────┘
                                               │ POST /api/v1/tasks
                                               ▼
                                    ┌──────────────────────────────┐
                                    │  flocode serve (127.0.0.1:   │
                                    │   8080 or chosen port)       │
                                    │  - runner, executors, mesh,  │
                                    │    sessions, wiki, chat      │
                                    └──────────────────────────────┘
```

**How it works:**
1. Electron (or flobot's own `serve`) supervises a `flocode serve --port <p>` child
2. flobot's `internal/executor` becomes a thin HTTP client:
   - `POST /api/v1/tasks` with a `task.Task` envelope
   - subscribe to `GET /api/v1/tasks/{id}/stream` (SSE) and translate events
     to flobot's NDJSON/run_status events for the web UI
   - `POST /api/v1/tasks/{id}/cancel` for the Stop button
3. flobot keeps its routing layer: map route id → model pin → config override,
   but now the *task* carries the model selection instead of CLI flags

**Model selection:** the task contract carries `ModelInfo{ModelID, ModelLock}`.
The runner resolves routes via the same catalog/config machinery the TUI uses
(`RequestRouteResolver`, `RequestRouteAuthorizer`). flobot's route pins map to
flocode config profile names; mesh/direct routes become config overrides passed
to the serve process (already how flobot generates config overrides today).

### Option B: In-process SDK — import `pkg/flocode`

flobot imports `github.com/florianleibert/flocode/pkg/flocode`, constructs an
`Agent` per run with `Options{Workdir, AllowedTools, ...}`, subscribes to typed
events, and drives with `Run(ctx, prompt)`.

**Pros:** no HTTP hop, no separate process, direct typed event stream.
**Cons:** couples the two binaries' build/lifecycle; flobot can't run against a
remote/headless flocode; heavier memory footprint per app.

### Option C: Keep child-process, upgrade protocol

Keep spawning `flocode -p ... --output stream-json` (the current I8 protocol)
instead of legacy `streaming-json`. Minimal change; keeps flobot self-contained.
**Cons:** still no persistence/ownership/result contract; still cold-start per run.

## 4. What flobot must preserve (non-negotiables)

- **Loopback-only** server (`127.0.0.1`, `CheckLoopbackAddr`) — flocode serve
  already binds loopback by default and enforces same-UID ownership on tasks
- **Muse key never leaves the machine** — key stays in flocode config / env;
  flobot never sends it over HTTP (task body has no key field)
- **Route abstraction** — web UI keeps talking to flobot's `/api/v1/runs`,
  `/api/v1/roster`, `/api/v1/mesh/models`, `/api/v1/direct/models`
- **Run history + roster persistence** — flobot's store stays; it now records
  task IDs + statuses instead of parsing child output
- **Electron shell supervision** — desktop/main.js supervises `flocode serve`
  instead of (or in addition to) `flobot serve`

## 5. Migration path (phased)

1. **Phase 1 — adapter:** add `internal/taskclient` that talks to flocode serve's
   `/api/v1/tasks*`. Keep the existing executor as fallback. Feature-flag.
2. **Phase 2 — switch:** default to taskclient; executor becomes dead code.
3. **Phase 3 — enrich:** surface flocode's richer result contract (test report,
   analysis, provenance, confidence) in the web UI; add mesh/session/wiki tabs.
4. **Phase 4 — de-duplicate:** consider running *only* `flocode serve` and making
   flobot a pure front-end proxy (or embedding via SDK).

## 6. Open questions for the operator

- [ ] Which option: A (HTTP backend), B (SDK embed), or C (protocol upgrade)?
- [ ] Should flobot's own Go server remain, or become a thin proxy over flocode?
- [ ] Port strategy: reuse 8080 or pick a dedicated port (e.g. 4121)?
- [ ] Does flobot need to keep working when flocode serve is absent (fallback)?
