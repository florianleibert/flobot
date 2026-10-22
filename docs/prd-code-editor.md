# PRD — flobot Code Editor: a flocode-backed editing surface inside flobot

**Status:** Draft v1 · research complete, no code changed
**Author:** Matt (rigorous senior engineer persona) · 2026-10-22
**Owner:** Florian Leibert
**Module:** `github.com/florianleibert/flobot` (`~/dev/flobot`)
**Backend:** `github.com/florianleibert/flocode` (`~/dev/flocode`) — consumed over HTTP only, never imported
**Related:** [`docs/flocode-harness.md`](./flocode-harness.md) (Option A design) · [`docs/decide-routes.html`](./decide-routes.html) · [`docs/audit-exhaustive.md`](./audit-exhaustive.md)

---

## 0. TL;DR

flobot is a local-first, loopback-only web app that today runs **one-shot
agent runs** by spawning the `flocode` binary as a child process. This PRD adds
a **code editor** to flobot: a real file tree + editor pane + diff review +
inline agent chat, backed by **flocode's production HTTP API server**
(`flocode serve`) instead of the hand-rolled child-process executor. The editor
is the *human surface*; flocode is the *execution surface*. flobot stays a thin,
loopback-only UI + routing layer. It never imports flocode or amesh; it talks to
them over HTTP on `127.0.0.1`.

The PRD is deliberately **phased and fail-closed**: Phase 1 ships the editor
against flobot's *existing* run API (no new backend), Phase 2 swaps the
execution backend to `flocode serve`'s task API behind a feature flag, Phase 3
surfaces flocode's richer result contract (patch, test report, provenance,
confidence) in the editor, and Phase 4 adds the semantic surfaces (LSP
diagnostics, code-query, diff review) that make it a *code* editor rather than a
chat window with a file pane.

---

## 1. Problem statement

### 1.1 What flobot is today

flobot is a "local-first named teammates" app: you create bots (each pinned to a
model route), give them memory, and run prompts against them. Runs stream
output to a log pane. It persists roster + run history to `~/.flobot/state.json`
(0600, atomic rename, capped). The web UI is a single-page app embedded in the
Go binary, served on `127.0.0.1:4120`, wrapped by an Electron shell that
supervises the Go server.

### 1.2 The gap

flobot's **only** interaction with code today is "type a prompt, watch a log."
There is no file tree, no editor, no diff review, no diagnostics, no
go-to-definition, no way to see *what changed* in the repo as a result of a run,
and no way to accept/reject a patch. The run output is a text log; the actual
patch the agent produced is invisible. For a tool whose whole job is "run an
agent on your repo," that is a serious blind spot.

### 1.3 Why flocode as the backend

flocode is not just a CLI — it ships a **production HTTP API server**
(`flocode serve`, default port 8080) with:

- `POST /api/v1/tasks` — submit an agent task with a rich contract
  (`project{repo,branch,worktree_mode,dirty_policy}`, `goal`, `permissions`,
  `return_contract`)
- `GET /api/v1/tasks/{id}` — status (queued/running/completed/cancelled/failed)
- `GET /api/v1/tasks/{id}/stream` — SSE event stream
- `POST /api/v1/tasks/{id}/cancel` — cancel
- `GET /api/v1/tasks/{id}/result` — final result envelope with **patch, test
  report, provenance, confidence**
- `GET /api/v1/backends` — model profiles
- `GET /healthz`, `GET /api/v1/capabilities`

And a **separate gateway** (`flocode gateway`) that serves resumable engine
sessions over an authenticated local transport with SSE, idempotent turns,
durable steer, permission/question responses, and verified turns.

The result contract is exactly what an editor needs: the agent returns a
**unified diff** (`PatchInfo{format:"unified-diff", sha256, changed_files[],
diff, applied}`), a **test report**, and **provenance**. Today flobot throws
that away and shows only the log.

### 1.4 Non-negotiables (carried from `docs/flocode-harness.md`)

1. **Loopback-only.** flobot's server rejects non-loopback binds
   (`CheckLoopbackAddr`). flocode serve binds loopback by default and enforces
   same-UID ownership on tasks. This must survive.
2. **Muse key never leaves the machine.** Key lives in `~/.accounts/meta.ai`,
   injected as `MODEL_API_KEY` into the child env; never in JSON/NDJSON/HTML.
   Over HTTP, the task body has no key field — flobot must never send it.
3. **Route abstraction.** The web UI keeps talking to flobot's own
   `/api/v1/*` routes; flobot maps route id → model pin → config override.
4. **Run history + roster persistence.** flobot's store stays; it records task
   IDs + statuses instead of parsing child output.
5. **Electron shell supervision.** `desktop/main.js` supervises the backend
   process (flobot serve today; possibly flocode serve later) with backoff
   restart, log cap, single-instance lock.

---

## 2. Research base (what was verified)

### 2.1 flocode HTTP task API (verified in source)

| Method | Path | Purpose | Source |
|---|---|---|---|
| POST | `/api/v1/tasks` | Submit task; returns `{task_id,status}` (202) | `internal/server/api/tasks.go:40-46` |
| GET | `/api/v1/tasks/{id}` | Status + timestamps + `dropped_events` | `tasks.go:57-58` |
| POST | `/api/v1/tasks/{id}/cancel` | Cancel; waits for terminal state | `tasks.go:59-60` |
| GET | `/api/v1/tasks/{id}/result` | Final `task.Result` envelope | `tasks.go:61-62` |
| GET | `/api/v1/tasks/{id}/stream` | SSE event stream | `tasks.go:63-64` |
| GET | `/api/v1/backends` | Model profiles | `internal/serveops/http.go:79-122` |
| GET | `/healthz` | Liveness | `http.go:54-56` |
| GET | `/api/v1/capabilities` | API version + endpoint list | `http.go:57-78` |

**Task contract** (`internal/task/contract.go`): `task_version` ("2026-04.v1"),
`project{repo,branch,worktree_mode,dirty_policy}`, `goal{kind,title,
success_criteria[],risk_level}`, `permissions{file_writes(allowed_paths[],
denied_paths[]),commands,network}`, `return_contract{patch,test_report,
analysis,provenance,confidence}`.

**Result** (`contract.go:121-136`): task_id, status, backend, model, summary,
patch (`PatchInfo{format:"unified-diff", sha256, changed_files[], diff,
applied}`), tests (`TestReport{command,status,exit_code,duration_ms,log_ref}`),
artifacts[], provenance, confidence, requires_human_review, error.

**Auth:** task API is loopback same-UID, **no bearer token** — trust boundary is
"loopback + same Unix user" (UDS listener enforces same-UID at transport).
Chat uses session token; mesh uses mTLS peer certs.

**Streaming:** SSE (`event: <type>\ndata: <json>\n\n`), event type is the full
JSON body. Slow consumers get `dropped_events` counter on status; clients must
resume rather than silently lose events.

### 2.2 flocode gateway (verified in source)

`flocode gateway --socket <path> --workspace <root>` serves resumable engine
sessions on the authenticated local transport:

| Method | Path | Purpose |
|---|---|---|
| GET | `/v2/handshake` | Capabilities + schema sha256 |
| POST | `/v2/sessions` | Create session |
| GET/DELETE | `/v2/sessions/{id}` | Status / delete |
| GET | `/v2/sessions/{id}/events` | SSE with event IDs + resume |
| POST | `/v2/sessions/{id}/turns` | Submit turn (idempotent via operation_id) |
| POST | `/v2/sessions/{id}/steers` | Durable steer |
| POST | `/v2/sessions/{id}/permissions/{request_id}/responses` | Permission response |
| POST | `/v2/sessions/{id}/questions/{request_id}/responses` | Question response |
| POST | `/v2/sessions/{id}/cancel` | Cancel turn |

Auth: localtransport with endpoint nonce; raw handler reports
`handler_auth: none` so mis-mounted servers are detectable. Production mounts
wrap it (the agent-gui host does this with cookie/bearer session auth).

### 2.3 Prior art: `~/dev/agent-gui`

There is already a **separate product** — `github.com/byollm/agent-gui` — an
Electron + React SPA + Go host that consumes FloCode as a Go module (workflow
canvas, run cockpit, sessions). Its PRDs (`docs/prds/`) contain the most
relevant UX thinking:

- **Cockpit UX 100×** (`cockpit-ux-100x-prd-2026-09-18.md`): "one primary
  question ('what needs me?'), one primary surface (the board), progressive
  disclosure everywhere else." Evidence one click from every agent action; the
  diff *is* the approve dialog; append-only event ledger replayed on connect.
- **Control center** (`control-center-extractable-gui-prd-2026-09-17.md`): one
  window, many git working trees, each with its own execution host; GUI is a
  separate product over a versioned HTTP/Go-module contract.

**Key decision for this PRD:** flobot's editor must **not** re-implement or fork
agent-gui. agent-gui is a workflow-canvas product; flobot's editor is a
*code-editing* surface for one repo. They share the same backend contract but
different UX jobs. This PRD reuses the *patterns* (diff-as-approval, event
ledger, progressive disclosure) without copying code.

### 2.4 Semantic building blocks in flocode (Go APIs today)

flocode has rich semantic code machinery that is **not yet exposed over HTTP**:

- **LSP manager** (`internal/lsp/manager.go`): per-language language-server
  clients with `Diagnostics`, `Hover`, `DocumentSymbols`, `WorkspaceSymbols`,
  `Definition`, `References`, `DidOpen`. Go API only.
- **codequery** (`internal/codequery/contract.go`): one request shape, one
  result shape, deterministic fallback ladder (typed graph → LSP → AST → text).
  Operations: definition, references, callers, callees, implementations, tests,
  structure, impact, call_path, package_deps. Go API only.
- **codegraph** (`internal/codegraph/`): compiler-typed Go relation graph.
- **workspace policy** (`internal/workspace/policy.go`, `guard.go`): what paths
  an agent may touch; worktree mode; dirty policy.

These are the *future* semantic surfaces for the editor (Phase 4). They are Go
APIs today; exposing them over HTTP is a flocode-side change that this PRD
flags as a dependency.

### 2.5 flobot's current API surface (what the editor builds on)

From `internal/server/server.go:127-154`:

```
GET  /                       web UI (embedded SPA)
GET  /health                 liveness
GET  /api/v1/roster          bots + routes + mesh/direct models + local pin
POST /api/v1/bots            create bot {name,role,route}
PATCH/DELETE /api/v1/bots/{id}
POST /api/v1/bots/{id}/remember   memory note (8 KiB cap)
POST /api/v1/handoff         bus message + optional spawned run
POST /api/v1/skills          create skill {name,body}
DELETE /api/v1/skills/{id}
POST /api/v1/routines        create routine {bot_id,skill_id,trigger}
POST /api/v1/routines/{id}/test-run|enable|pause   DELETE /api/v1/routines/{id}
POST /api/v1/runs            create run {bot_id,prompt,intent,yolo,verify,worktree}
GET  /api/v1/runs            list runs (capped 200)
GET  /api/v1/runs/{id}       run detail
GET  /api/v1/runs/{id}/events  event replay (?since=N)
POST /api/v1/runs/{id}/stop  stop run (records "stopped")
GET  /api/v1/health/models   route health probes
GET  /api/v1/mesh/models     amesh catalog (ok:false + reason when down)
GET  /api/v1/direct/models   Meta direct models (server-side fetch)
GET  /api/v1/local           local pin status
POST /api/v1/local/pin       pin local route
GET  /api/stream             SSE run stream (?run= replay)
```

The editor needs three things flobot does not have today:
1. **File access** — read the workspace tree and file contents.
2. **Diff review** — see what a run changed and accept/reject it.
3. **Semantic code** — diagnostics, symbols, go-to-definition.

---

## 3. Product requirements

### 3.1 Vision

The flobot code editor is the place where "run an agent on my repo" becomes a
closed loop:

```
write prompt → agent runs → see the diff → accept/reject → next task
```

One window: file tree on the left, editor in the middle, agent chat + run log +
diff review on the right. The user never leaves the code to understand what an
agent did.

### 3.2 Personas

- **P1 — Solo dev / repo owner.** Runs agents on their own repo; wants to see
  exactly what changed before accepting. Primary user.
- **P2 — Operator of many bots.** Uses flobot's routines/scheduler; wants run
  results (diffs) reviewable from one place.
- **P3 — Reviewer.** Wants to review an agent's patch without running anything.

### 3.3 User stories

| ID | Story |
|---|---|
| US-1 | As a dev, I open flobot and see my repo's file tree so I can navigate to the file I'm working on. |
| US-2 | As a dev, I select a file and edit it in an editor pane (syntax highlighting) so I can make changes directly. |
| US-3 | As a dev, I select a bot + prompt and run it against my repo from the editor view so I don't switch contexts. |
| US-4 | As a dev, I watch the agent's run stream live so I know what it's doing. |
| US-5 | As a dev, when a run finishes I see the **diff** of what changed so I can review it. |
| US-6 | As a dev, I can **accept or reject** each changed file (or the whole patch) so only approved changes land. |
| US-7 | As a dev, I see test results from the run so I know if it passed before I accept. |
| US-8 | As a dev, I can stop a runaway run from the editor so nothing spins forever. |
| US-9 | As an operator, I see run history with diffs so I can audit what agents did over time. |
| US-10 | As a dev, I get diagnostics for the open file so I catch errors before running anything. |

### 3.4 Non-goals (v1)

- No multi-repo workspace management (that's agent-gui's control center).
- No workflow canvas authoring (agent-gui).
- No remote/mesh browser access (loopback-only stays).
- No plugin system.
- No full IDE feature set (refactoring, debugger) — this is an agent-facing code
  surface, not a replacement for Xcode/VS Code.

---

## 4. Architecture

### 4.1 Layered model

```
┌──────────────────────────────────────────────────────────────┐
│ Electron shell (desktop/main.js)                             │
│   supervises flobot serve (backoff restart, log cap)         │
└──────────────────────────────┬───────────────────────────────┘
                               │ HTTP loopback :4120
┌──────────────────────────────▼───────────────────────────────┐
│ flobot server (Go) — thin UI + routing layer                 │
│   - existing: roster/store/runs/routes/mesh/direct/local     │
│   - NEW:      workspace/file API                             │
│   - NEW:      diff review API                                │
│   - NEW:      taskclient → flocode serve (Phase 2)           │
└──────────────────────────────┬───────────────────────────────┘
                               │ HTTP loopback :8080 (flocode serve)
┌──────────────────────────────▼───────────────────────────────┐
│ flocode serve — execution surface                            │
│   tasks/stream/cancel/result/backends                        │
│   (or flocode gateway for resumable sessions)                │
└──────────────────────────────────────────────────────────────┘
```

### 4.2 Phase plan

#### Phase 0 — Workspace/file access in flobot (no backend change)

The editor needs to read files. Add to flobot's server:

```
GET /api/v1/workspace/tree?dir=<rel>       → {entries:[{name,path,type,size}]}
GET /api/v1/workspace/file?path=<rel>      → {path, content}
PUT /api/v1/workspace/file?path=<rel>      → write file {content}
```

**Security:** path validation is mandatory — reject absolute paths,
`..`, symlink escapes outside the workspace root. This mirrors the existing
loopback-only trust boundary but adds a *path* boundary. The workspace root is
the directory flobot was started in (or an explicit flag), same as agent-gui's
`--workspace`.

#### Phase 1 — Editor UI against existing run API

No backend change beyond Phase 0. The editor pane:
- File tree (from workspace API)
- Editor pane with syntax highlighting (CodeMirror — already used by agent-gui)
- Run panel: pick bot + prompt + intent/yolo/verify/worktree → POST `/api/v1/runs`
- Live stream via existing `/api/stream`
- Stop button via existing `/api/v1/runs/{id}/stop`
- Run history with status chips

This ships value immediately and de-risks the UI before any backend swap.

#### Phase 2 — Diff review + accept/reject

The missing piece: **what did the run change?** Today flobot discards it.

Add to flobot's server:
```
GET /api/v1/runs/{id}/diff    → {patch:{format:"unified-diff",diff,files:[...]}, applied}
POST /api/v1/runs/{id}/diff/apply    → apply approved hunks/files to workspace
POST /api/v1/runs/{id}/diff/reject   → mark rejected (no-op on disk)
```

Where does the diff come from in Phase 2?
- **Option A (recommended):** switch runs to `flocode serve` task API (Phase 2a)
  and read `Result.patch`. This is the clean source of truth.
- **Option B:** compute the diff in flobot by snapshotting git state before/after
  each run (`git diff` against pre-run HEAD). Works with today's executor; no
  backend swap needed for diff review.

The PRD recommends **Option B first** (diff review ships against today's
executor via git snapshot), then Phase 2a swaps execution to flocode serve and
the diff source becomes `Result.patch`. This decouples "see diffs" from "swap
backend."

Diff application must be **file-scoped and reversible**: apply per-file or all,
and keep the pre-run snapshot so reject = restore from snapshot.

#### Phase 2a — Execution backend swap to flocode serve (feature flag)

Add `internal/taskclient` (HTTP client to flocode serve):
```
POST /api/v1/tasks        {project{repo,branch,...}, goal{prompt}, permissions{...}, return_contract{patch:true,...}}
GET  /api/v1/tasks/{id}          status polling + dropped_events
GET  /api/v1/tasks/{id}/stream   SSE → translate to flobot run_status events
POST /api/v1/tasks/{id}/cancel   stop button
GET  /api/v1/tasks/{id}/result   patch/test_report/provenance/confidence
```

Feature flag: `FLOBOT_BACKEND=taskclient|executor` (default executor until
proven). Keep the existing executor as fallback when flocode serve is absent —
flobot must keep working standalone.

Model selection: route id → pin → config override stays in flobot; the task's
model selection is carried server-side by flocode's config routing (the task
body has no key field). Muse key stays in flocode config/env; flobot never sends
it.

#### Phase 3 — Richer result contract in the editor

Surface from `Result`: test report (command/status/exit_code/log_ref),
provenance (which model/backend produced it), confidence,
requires_human_review. The editor shows "tests passed/failed" inline with the
diff.

#### Phase 4 — Semantic code surfaces

Expose flocode's semantic machinery over HTTP (flocode-side change):
```
GET /api/v1/lsp/diagnostics?file=<rel>       → diagnostics for open file
GET /api/v1/codequery?op=definition&symbol=X → definition/references/callers/...
```
Editor features: diagnostics gutter, go-to-definition, symbol outline,
"what calls this?" panel. This is the phase that makes it a *code* editor.

### 4.3 Security model

| Layer | Boundary |
|---|---|
| Transport | Loopback-only (`CheckLoopbackAddr`); Electron single-instance lock |
| Path | Workspace root; reject absolute/`..`/symlink escapes |
| Key | Muse key never leaves machine; never in JSON/NDJSON/HTML |
| Execution | flocode serve enforces same-UID ownership; permissions contract gates writes |
| Diff apply | File-scoped; reversible via pre-run snapshot |

### 4.4 Concurrency & lifecycle

- Runs must remain cancellable (`killGroup` on ctx.Done already exists in the
  executor; taskclient uses `POST .../cancel`).
- The scheduler/routines keep working unchanged — they produce runs that now have
  diffs.
- Store caps stay (200 runs / 500 messages); diffs are derived from git snapshot,
  not stored inline (avoid unbounded state growth).

---

## 5. Acceptance criteria

### Phase 0 (workspace/file API)
- [ ] AC-0a: `GET /api/v1/workspace/tree?dir=.` returns entries for the workspace root.
- [ ] AC-0b: path traversal attempts (`../`, absolute paths) return 400 and never read outside root.
- [ ] AC-0c: symlink escape attempts are rejected.
- [ ] AC-0d: file read/write round-trips within root.

### Phase 1 (editor UI)
- [ ] AC-1a: user opens flobot and sees file tree + editor pane.
- [ ] AC-1b: user runs a bot+prompt from the editor; stream renders live.
- [ ] AC-1c: stop button works from the editor; run records "stopped".
- [ ] AC-1d: run history shows status chips and is clickable.

### Phase 2 (diff review)
- [ ] AC-2a: after a run that changes files, `GET /api/v1/runs/{id}/diff` returns the unified diff.
- [ ] AC-2b: user can apply per-file or all; reject restores pre-run state.
- [ ] AC-2c: applying is reversible (reject after apply restores snapshot).
- [ ] AC-2d: diff review works against today's executor (git-snapshot source).

### Phase 2a (taskclient)
- [ ] AC-2a-a: with flag on and flocode serve running, runs execute via task API.
- [ ] AC-2a-b: stream translates to flobot events without loss (resume via dropped_events).
- [ ] AC-2a-c: cancel via task API records "stopped".
- [ ] AC-2a-d: with flag off or serve absent, executor fallback works unchanged.
- [ ] AC-2a-e: Muse key never appears in any request body or log.

### Phase 3 (result contract)
- [ ] AC-3a: test report renders inline with diff.
- [ ] AC-3b: provenance + confidence shown for each run.

### Phase 4 (semantic)
- [ ] AC-4a: diagnostics render in editor gutter for open file.
- [ ] AC-4b: go-to-definition navigates within workspace.
- [ ] AC-4c: codequery operations available from editor context.

---

## 6. Open questions for the operator

1. **Workspace root:** should flobot's workspace be fixed at launch dir, or a new
   explicit flag (`--workspace <dir>`)? agent-gui uses an explicit flag; flobot
   currently has none.
2. **Diff source priority:** ship git-snapshot diff first (Option B) or go
   straight to taskclient swap? Recommended: B first.
3. **Editor library:** CodeMirror (used by agent-gui) vs Monaco? Recommended:
   CodeMirror — smaller, already proven in this stack.
4. **Semantic phase:** is Phase 4 in scope for this effort or a separate PRD?
5. **Relationship to agent-gui:** confirm flobot's editor is intentionally *not*
   reusing agent-gui's canvas product and stays loopback-only single-repo.
6. **Taskclient vs gateway:** for interactive editing sessions with steer +
   permission responses, the *gateway* (`flocode gateway`) may be a better fit
   than one-shot tasks. Which does Phase 2a target first? Recommended: tasks for
   v1 runs; gateway for interactive chat-in-editor later.
7. **Diff storage:** diffs derived from git snapshot vs stored in state.json —
   recommended derived (no unbounded growth).

---

## 7. Risks

| Risk | Mitigation |
|---|---|
| Backend swap breaks standalone mode | Feature flag + executor fallback; keep both until taskclient proven |
| Path traversal in workspace API | Strict validation + tests (AC-0b/c) |
| Diff application corrupts user work | File-scoped apply + reversible snapshot + tests |
| Event loss on stream reconnect | Resume via dropped_events counter + ?since= replay |
| Scope creep into full IDE | Non-goals section; phase gates |
| Duplication with agent-gui | Explicit non-goal; reuse patterns not code |

---

## 8. Effort estimate (rough)

| Phase | Effort |
|---|---|
| Phase 0 workspace API | S |
| Phase 1 editor UI | M |
| Phase 2 diff review (git-snapshot) | M |
| Phase 2a taskclient swap | L |
| Phase 3 result contract UI | S–M |
| Phase 4 semantic surfaces (+ flocode-side HTTP) | L |

---

## Appendix A — Source references

All verified against current trees:
- flobot routes: `internal/server/server.go:127-154`
- flobot executor: `internal/executor/executor.go`
- flobot README identity + non-negotiables: `README.md`
- flocode task API: `internal/server/api/tasks.go`, `internal/serveops/http.go`
- flocode task contract: `internal/task/contract.go`
- flocode gateway: `internal/cli/gateway.go`, `internal/enginegateway/gateway.go`
- flocode LSP: `internal/lsp/manager.go`, `internal/lsp/client.go`
- flocode codequery: `internal/codequery/contract.go`, `ladder.go`
- prior art PRDs: `~/dev/agent-gui/docs/prds/*`
