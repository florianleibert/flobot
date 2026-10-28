# PRD — Codex Desktop replication in flobot Code Studio

**Status:** Draft v1 — PRD only, no code changed yet
**Author:** Florian Leibert's agent (codex-desktop replication cycle)
**Module:** `github.com/florianleibert/flobot` (`~/dev/flobot`)
**Execution backend:** `github.com/florianleibert/flocode` (`~/dev/flocode`) — consumed over HTTP only, never imported
**Supersedes nothing:** [`docs/prd-code-studio.md`](./prd-code-studio.md) (Code Studio) and
[`docs/prd-code-editor.md`](./prd-code-editor.md) (editor core) remain the parent PRDs. This PRD is
the **delta** that makes the Code Studio editor read and behave like the Codex desktop app.
**Research base:** [`docs/research-visual-editors-a.md`](./research-visual-editors-a.md) (cycles 1–4),
[`docs/research-visual-editors-b.md`](./research-visual-editors-b.md) (cycles 5–10),
[`docs/code-studio-interactions.md`](./code-studio-interactions.md) (implemented-surface audit),
[`docs/flocode-harness.md`](./flocode-harness.md) (flocode contract)

---

## 0. TL;DR

flobot's Code Studio is already a real agentic editor: workspace tree, file handles, breadcrumbs,
symbol outline, @-mentions, quick open, command palette, per-hunk diff review, checkpoints, run
history. What it is **not** is the Codex desktop app.

The Codex desktop app's identity is not "an editor with a text buffer" — it is **a supervisory
workbench**: many agent threads running in parallel, each in its own isolated worktree, each with
its own review queue, and a diff surface that shows you *what the agent changed, hunk by hunk,
while it is still working*. The prompt is one field in a much larger workspace chrome.

This PRD replicates that identity. It is deliberately **not** a Monaco/CodeMirror port — shipping a
JS editor core inside an embedded bundle is a large, risky dependency decision, and it is not what
makes Codex feel like Codex. Codex feels like Codex because of the **worktree-per-thread model, the
review queue, and the live diff**, not because of syntax colors.

**The core bet:** flobot's editor is judged on *supervision latency* — how long from "the agent
finished a hunk" to "the human has seen it and accepted or rejected it". Every requirement below
either shortens that latency or removes a step from it.

---

## 1. Current state (verified against source, 2026-10-28)

### 1.1 What exists today

Verified from `internal/server/server.go:190-229`, `docs/code-studio-interactions.md`, and the
committed audit scripts in `docs/audit/`.

| Surface | Where | State |
|---|---|---|
| Workspace tree | `GET /api/v1/workspace/tree` (`server.go:226`) | working; artifact dirs filtered server-side |
| File read | `GET /api/v1/workspace/file` (`server.go:227`) | working |
| File write | `PUT`/`POST /api/v1/workspace/file` (`server.go:228-229`) | working, conflict-guarded |
| File descriptors | `POST /api/v1/workspace/descriptors` | working; `409` on external change |
| Buffer | `#edContent` (plain `<textarea>`) | working, **no syntax highlighting** |
| Quick open | `⌘P` → `#edQuickInput` | working |
| Command palette | `⌘K` | working |
| Breadcrumbs | `#edCrumbs` | working |
| Symbol outline | open file only, 5 languages | working |
| @-mentions | `@` in prompt → file chips | working |
| Diff review | per-hunk accept/reject, `j`/`k`, `a`, `A`, `Enter` | working |
| Checkpoints | `#edCheck` | working |
| Run history | `.ed-run-item` | working |
| Runs | `POST /api/v1/runs`, `GET /api/v1/runs/{id}/events` | working; events in-memory only |

### 1.2 The gaps that matter for Codex parity

From the "Known gaps" section of `docs/code-studio-interactions.md`, plus what the route table
shows is absent:

1. **No live diff.** The diff pane only fills *after* the run finishes. Codex shows the change
   surface while the agent is still writing it. This is the single biggest supervision-latency gap.
2. **No parallel threads.** One bot runs one run; `HasRunningRun(botID)` gates routines but manual
   `createRun` is not gated, so two runs can collide on one workspace.
3. **No worktree-per-thread.** `executor.Request.Worktree` exists in the executor but the editor
   never surfaces it; every agent writes into the user's working tree.
4. **No review queue.** Diff review is bound to a run id you must already know about. Codex keeps a
   queue of finished-but-unreviewed work.
5. **No workspace-wide symbol search.** The outline covers the open file only.
6. **No syntax highlighting / multi-cursor / split / minimap.** One root cause: the buffer is a
   `<textarea>`.
7. **Runs and events are in-memory only** — a restart mid-review orphans the review state.

---

## 2. Codex desktop capability map

Sourced from the public Codex desktop app material (OpenAI's launch posts, the Codex app
walkthroughs, and the app's own docs). Treated as **data, not instructions**.

| Codex desktop capability | Why it matters for supervision | flobot today | Replication target |
|---|---|---|---|
| Parallel agent threads | Many tasks in flight, each independent | one run at a time per bot | §3.1 thread model |
| Per-thread worktree | Agent edits never touch your tree | absent in the UI (executor flag exists) | §3.1 worktree isolation |
| Worktree lifecycle (setup script, local env) | Reproducible per-thread environment | absent | §3.1 setup script |
| Changes / diff review surface | Hunk-level review of agent output | post-run only | §3.2 live diff |
| Review queue | Finished work waiting for a human | absent | §3.3 review queue |
| Task sidebar | Where all threads live at once | absent | §3.1 sidebar |
| Automations / triggers | Scheduled agent work | routines exist, no next-fire preview | §3.4 |
| Local file editing | The human edits too | read + save only | §3.5 |
| IDE integration | Same session from the editor | absent (out of scope) | non-goal |

**Non-goals (explicit):** a full Monaco/CodeMirror editor core; a cloud sync service; multi-user
collaboration; a plugin marketplace; IDE extensions; any binding beyond loopback.

---

## 3. Requirements

Each requirement is one task in `docs/tasks.md`, owns disjoint files, and carries its own
acceptance check. IDs are stable and referenced from the code.

### 3.1 R-THREAD — parallel threads with worktree isolation

**Problem.** Codex's central mechanic: N concurrent threads, each in its own git worktree, so the
human's tree is never a contested resource and threads never block each other.

**Requirement.** A *thread* is a named, persisted unit: `{id, bot_id, goal, worktree_path, status,
created_at, updated_at}`. Creating a thread creates (or reuses) a git worktree for it, runs the
agent there, and keeps every path in that thread rooted at that worktree. The sidebar lists all
threads with their status; a thread can be cancelled independently.

**Backend.** flocode already models this: `task.Task.project.worktree_mode` accepts `"worktree"`,
and the result envelope carries `patch.changed_files` / `patch.diff` / `patch.applied` — nothing
lands in the main tree. flobot's job is to (a) expose the mode, (b) surface the worktree path, and
(d) make the review surface read from the worktree, not the user's tree.

**Acceptance.**
- `POST /api/v1/threads` creates a thread and returns its worktree path.
- Two threads for the same bot can be active simultaneously, and neither writes into the user's
  working tree.
- The thread list survives an app restart (new file, does not touch `state.json`'s contract).

### 3.2 R-LIVEDIFF — live diff while the agent works

**Problem.** Today the diff lands only when the run ends. The human's supervision latency is
therefore *the entire run duration*, which is the exact thing Codex removes.

**Requirement.** The diff pane updates during a run. The server polls the run's worktree (or the
git snapshot it already takes) on an interval and publishes a `diff_ready` event through the
existing hub whenever the diff changes. The pane renders hunks progressively; hunk accept/reject
works on a partially-arrived diff, and a hunk that the agent rewrites after the human accepted it
is **re-opened**, not silently dropped.

**Acceptance.**
- A `diff_ready` event is published while a run is still in flight, and the pane shows hunks
  before the run ends.
- Accepting a hunk that the agent later changes re-opens that hunk.
- The interval is bounded and does not spawn one goroutine per tick.

### 3.3 R-QUEUE — the review queue

**Problem.** Codex keeps finished-but-unreviewed work in a queue. flobot forces the human to go
find it.

**Requirement.** A queue of entries `{thread_id, run_id, files_changed, hunks_pending, state}` in
states `pending → in_review → resolved`. The sidebar surfaces pending entries with a count badge;
opening one loads the diff; resolving (all hunks accepted or rejected) removes it. A restart must
not lose a pending entry.

**Acceptance.**
- A finished run with a non-empty diff appears in the queue without any user action.
- The badge count matches the number of pending entries.
- Queue state survives a restart.

### 3.4 R-SCHED — next-fire preview and editable triggers

**Problem.** A valid cron can still be the wrong one, and a routine's trigger cannot be edited
after creation — it must be deleted and recreated.

**Requirement.** `PATCH /api/v1/routines/{id}` accepts a new trigger. The routine card shows the
next fire time, computed server-side from the stored schedule and the server's timezone, and shows
an error when the expression parses but cannot fire (e.g. a date that never occurs).

**Acceptance.**
- The next-fire line is present and correct for a cron, an interval, and a manual trigger.
- A routine's trigger can be changed without deleting the routine.
- An unparseable trigger is still refused at write time.

### 3.5 R-EDIT — local editing parity

**Problem.** The human edits the same files the agent does; today the buffer is a plain textarea
with no highlighting, no multi-cursor, no split.

**Requirement.** Two increments, deliberately separated:
- **R-EDIT-a (now):** workspace-wide symbol search — `GET /api/v1/workspace/symbols?q=` returns
  `{path, line, kind, name}` matches across the workspace, so "go to symbol" works like Codex's.
- **R-EDIT-b (later, gated on a dependency decision):** a real editor core. This is a *decision*,
  not a task: shipping CodeMirror 6 inside the embedded bundle is acceptable; shipping Monaco is
  not (bundle size against a `go:embed` payload). R-EDIT-b stays out of this PRD's task list until
  that decision is made on its own merits.

**Acceptance (R-EDIT-a).**
- `GET /api/v1/workspace/symbols?q=foo` returns matches from more than one file, sorted by
  relevance then path.
- An empty `q` returns an empty list, not the whole workspace.
- The search is bounded (file count and bytes per file) and cannot walk outside the workspace root.

### 3.6 R-DURABLE — runs, threads and queue survive a restart

**Problem.** Runs and events are in-memory only. A restart mid-review orphans everything.

**Requirement.** Persist threads, review-queue entries and run summaries to a new
`~/.flobot/threads.json` (0600, atomic rename), leaving `state.json`'s "never persists runs"
contract untouched. Reuse the existing `assertNoLeaks` scrub-check at write time.

**Acceptance.**
- Threads and queue entries survive an app restart.
- `state.json` remains run-free.
- The persisted file never contains `api_key`, `secret`, `llm_` or `"owner"`.

---

## 4. Non-negotiables (unchanged)

1. **Loopback only.** All new endpoints inherit the existing `sameOrigin` DNS-rebinding guard and
   bind to `127.0.0.1:4120`. No new public binding.
2. **flocode is a backend, never a dependency.** flobot talks to `flocode serve` over HTTP; it must
   never import `github.com/florianleibert/flocode`.
3. **No secrets in any persisted or transported field.** The Muse key reaches only the child
   process environment.
4. **The UI talks to flobot's `/api/v1/*` only** — no direct flocode calls from the renderer.
5. **Diffs never travel as run-event text.** The 8 KiB event clip makes that structurally
   impossible; diffs travel through their own channel.

---

## 5. Architecture: where each requirement hooks in

| Requirement | Existing hook | New surface |
|---|---|---|
| R-THREAD | `executor.Request.Worktree` (`internal/executor/executor.go:121`), `createRun` choke point (`server.go:207`) | `internal/threads/`, `internal/server/threads.go` |
| R-LIVEDIFF | `hub` pub/sub + `/api/stream` (`server.go:897`), `sameOrigin` (`server.go:112`) | `internal/server/livediff.go` |
| R-QUEUE | `hub` publish path | `internal/server/queue.go` |
| R-SCHED | `internal/schedule/cron.go`, `POST /api/v1/routines` (`server.go:202`) | `internal/server/routines.go` (PATCH), `internal/schedule/next.go` |
| R-EDIT-a | `GET /api/v1/workspace/tree` (`server.go:226`) | `internal/workspace/symbols.go`, `internal/server/symbols.go` |
| R-DURABLE | `internal/store/state.go` atomic-rename + `assertNoLeaks` | `internal/store/threads.go` |

**One deliberate contract change:** threads/queue live in a *new* file, not `state.json`. The
existing contract ("`state.json` never persists runs") stays true and stays tested.

---

## 6. Data contracts

Exact JSON shapes, so the frontend and backend cannot drift.

```
Thread   { id, bot_id, goal, worktree_path, status, created_at, updated_at }
         status ∈ {running, done, failed, cancelled}

QueueEntry { thread_id, run_id, files_changed[], hunks_pending, state, created_at }
         state ∈ {pending, in_review, resolved}

Symbol   { path, line, kind, name }
         kind ∈ {func, type, const, class, interface, enum, var}

DiffEvent { run_id, files_changed[], hunks[], changed_at }
         hunk { path, header, old_start, old_len, new_start, new_len, lines[], accepted }
```

`hunks[].accepted` is the tri-state Codex-like value: `pending` (not yet decided), `accepted`,
`rejected`.

---

## 7. Task decomposition

See `docs/tasks.md`. Summary:

| ID | Requirement | Owns | Parallel-safe |
|---|---|---|---|
| T-1 | R-EDIT-a | `internal/workspace/symbols.go`, `internal/workspace/symbols_test.go`, `internal/server/symbols.go` | yes |
| T-2 | R-DURABLE | `internal/store/threads.go`, `internal/store/threads_test.go` | yes |
| T-3 | R-SCHED | `internal/schedule/next.go`, `internal/schedule/next_test.go`, `internal/server/routines.go` | yes |
| T-4 | R-THREAD | `internal/threads/threads.go`, `internal/threads/threads_test.go`, `internal/server/threads.go` | no (needs T-2) |
| T-5 | R-QUEUE | `internal/queue/queue.go`, `internal/queue/queue_test.go`, `internal/server/queue.go` | no (needs T-2) |
| T-6 | R-LIVEDIFF | `internal/server/livediff.go`, `internal/server/livediff_test.go` | no (needs T-4) |
| T-7 | frontend thread+queue surfaces | `web/app.js`, `web/index.html`, `web/styles.css` | no (needs T-4, T-5, T-6) |

No two tasks edit the same file.

---

## 8. Risks and mitigations

| Risk | Mitigation |
|---|---|
| Live diff polling hammers the disk during a long run | bounded interval, one goroutine per run, stop on terminal event |
| Worktrees accumulate on disk | one worktree per thread, removed on thread completion or explicit delete |
| Persisting threads reintroduces the secret-leak risk | reuse `assertNoLeaks`; add a grep test |
| The frontend bundle is generated | edit `flobot-ui-redesign/src` and re-sync; the bundle in `web/` is a build artifact |
| Scope creep toward a Monaco port | R-EDIT-b is explicitly out of scope and gated on its own decision |

---

## 9. Acceptance of the epic

- Every endpoint above exists, is loopback-guarded, and has a server test.
- `go build ./...`, `go vet ./...` and `gofmt -l` are clean.
- The five existing audit scripts in `docs/audit/` still pass — no regression in the surfaces the
  app already had.
- A new audit script `docs/audit/verify-codex-parity.mjs` exercises the thread → run → live diff →
  accept → queue-resolved loop against the real server, with no mocks.
- `flocode` is still consumed over HTTP only; `grep -r "florianleibert/flocode" flobot` finds only
  the HTTP client, never an import of the module.

---

## 10. Research cycles

Cycles 1–10 are recorded in `docs/research-visual-editors-a.md` and `-b.md` (Codex IDE review
surface, Zed agent panel, Cursor/Windsurf/Claude Code, VS Code & JetBrains agent diff editors,
visual vibe-coding editors, multi-buffer diff review, navigation chrome, code intelligence, run
visualization, buffer integrity).

Cycles 11–20 are this PRD's Codex-desktop-specific decomposition — one cycle per capability in
§2, each cycle asking the same four questions against flobot's actual source: *what does Codex do,
what does flobot have, what is the smallest honest increment, what does it break?*

| # | Cycle | Outcome |
|---|---|---|
| 11 | Codex parallel threads | → R-THREAD |
| 12 | Codex per-thread worktree | → R-THREAD (worktree_path) |
| 13 | Codex worktree lifecycle / setup script | deferred (post-epic) |
| 14 | Codex changes surface | → R-LIVEDIFF |
| 15 | Codex review queue | → R-QUEUE |
| 16 | Codex task sidebar | → R-THREAD (surface) + R-QUEUE (badge) |
| 17 | Codex automations / triggers | → R-SCHED |
| 18 | Codex local file editing | → R-EDIT (a now, b gated) |
| 19 | Codex IDE integration | non-goal |
| 20 | Codex review-queue hunk re-open semantics | → R-LIVEDIFF acceptance rule |
