# PRD — flobot Code Studio: a best-in-class agentic code editor

**Status:** Draft v4 (adversarially reviewed) · research complete (25 cycles + repo audit + API verification), no code changed
**Author:** Matt (rigorous senior engineer persona) · 2026-10-22
**Owner:** Florian Leibert
**Module:** `github.com/florianleibert/flobot` (`~/dev/flobot`)
**Execution backend:** `github.com/florianleibert/flocode` (`~/dev/flocode`) — consumed over HTTP only, never imported
**Research base:** [`docs/research-code-editor.md`](./research-code-editor.md)
**Related:** [`docs/flocode-harness.md`](./flocode-harness.md)

---

## 0. TL;DR

flobot is a local-first, loopback-only web app that runs one-shot agent runs by
spawning the `flocode` binary. This PRD turns it into **Code Studio** — an
agentic code editor for the *supervisory engineer*.

The research (25 cycles: papers, GitHub engineering blogs, product teardowns of
Cursor/Zed/Aider/Claude Code) converges on one picture:

- **The job is supervision.** 82% of engineers write less code; work shifts to
  directing, evaluating, correcting AI output (arXiv 2605.23135). Anthropic's
  400k-session study: users make ~70% of *planning* decisions, the agent ~80%
  of *execution* decisions. **The prompt is the input surface; the diff is the
  output surface.** Manual editing is secondary.
- **The diff is the universal edit representation.** Aider: unified diffs with
  flexible application beat every structured format (JSON/function-calling
  performed *worse*; disabling fuzzy apply → 9X editing errors). flocode
  already returns `PatchInfo{format:"unified-diff"}`.
- **Review is the product.** Zed: checkpoints per edit, multi-buffer review
  with per-hunk accept/reject, follow-the-agent mode. Cursor: live diff during
  generation, plan-first mode, agent review pass.

Code Studio makes the loop `direct → run → review diff → accept/reject` as
fast and low-cognitive-load as possible. Phased, fail-closed, with exact
schemas in §6 and file-level task plans in §8.

---

## 1. Problem statement

### 1.1 What flobot is today (verified against source)

Local-first "named teammates" app: bots pinned to model routes, memory,
one-shot runs via `flocode` child process, stream to a log pane. Roster
(bots/skills/routines/messages) persists to `~/.flobot/state.json` (0600,
atomic rename, `assertNoLeaks` refuses to write anything containing
`api_key`/`secret`/`llm_`). **Runs and events are in-memory only** — lost on
restart (`internal/store/store.go:110`, `state.go:39`). Runs are capped at
200 finished / 2000 events, event text at 8 KiB. SPA embedded in the Go
binary on `127.0.0.1:4120` behind a `sameOrigin` DNS-rebinding guard,
supervised by Electron. The repo has **zero open or closed GitHub issues** —
clean slate, no issue-driven constraints.

### 1.2 The gap

No file tree, no editor, no diff review, no accept/reject, no diagnostics.
The agent's actual patch is invisible — the user reads a text log and then
runs `git status` in a terminal. Every other surface in the loop (prompt,
run, review) lives outside the app.

### 1.3 Research-grounded "why now"

| Finding | Source | Consequence |
|---|---|---|
| Supervisory engineering is the job; flow erodes while productivity rises | arXiv 2605.23135 | Optimize direction/evaluation/correction; protect attention |
| Users plan, agents execute (~70/20 split); ~10 agent actions per prompt | Anthropic 400k-session study | Prompt composition + diff review are the two surfaces to build |
| Unified diff + flexible apply beats structured formats; 9X errors without fuzzy apply | Aider | Diff-patch everywhere; fuzzy hunk matching in apply path |
| Checkpoints, per-hunk accept/reject, follow mode, steer | Zed Agent Panel | Concrete review UX to copy |
| Plan-first, live diff during run, review pass, worktree isolation | Cursor best practices | Plan toggle, live diff, review pass |
| Knowing when *not* to suggest: 21% fewer suggestions → 28% higher accept rate | Cursor Tab RL | Suppression policy for proactive UI |
| Bimodal acceleration/exploration | Grounded Copilot (arXiv 2206.15000) | One-keystroke accept AND rich context views |
| ACI is a design surface | SWE-agent (arXiv 2405.15793) | Agent-facing interface quality is a feature |

### 1.4 Why flocode as the backend

`flocode serve` exposes `POST /api/v1/tasks` (submit body is a
`task.Task`: `project{repo,branch,worktree_mode,dirty_policy}`, `goal`,
`permissions`, `return_contract`; version string `2026-04.v1`),
`GET /tasks/{id}`, `/stream` (SSE), `/cancel`, `/result` — the result
envelope carries **patch (`PatchInfo{format:"unified-diff", sha256,
changed_files, diff, applied}` — the diff string is `.patch.diff` and may
be omitted for huge patches), test report, provenance, confidence,
requires_human_review**. That is exactly the supervisory contract.
Today flobot discards it.

Verified limits of the task API (from `internal/server/api/tasks.go` and
`internal/task/contract.go`):
- **No SSE replay**: no event ids, no Last-Event-ID handling; a reconnect
  starts a fresh subscription. The gap signal is the `dropped_events`
  counter in the status response. flobot's taskclient must treat a
  reconnect as "poll status, re-render from result" — not "resume stream".
- **No mid-run input**: cancel is the only POST to a running task.
- **No API key can travel in a submit body**: `task.Task` has no
  model/provider/key field; serve holds its own model config. (AC-2a-e
  is satisfied structurally, keep the grep test anyway.)
- **Worktree mode**: with `worktree_mode:"worktree"`, edits land in a
  separate git worktree and return as a `PatchInfo` diff — nothing lands
  in the main tree and there is no merge-back. flobot's apply path is the
  merge-back.

### 1.5 Non-negotiables (unchanged)

1. Loopback-only; flocode serve task API enforces loopback-host on both
   RemoteAddr and Host header (verified: `tasks.go:119-151` — there is no
   UID check on the task path).
2. Muse key never leaves the machine; never in JSON/NDJSON/HTML.
3. Route abstraction: UI talks to flobot's `/api/v1/*` only.
4. Roster + run history persistence stays (capped).
5. Electron shell supervision stays.

---

## 2. Architecture inventory — what each phase hooks into (verified)

Every claim below was checked against source on 2026-10-22:

| Existing hook | Location | What it gives Code Studio |
|---|---|---|
| `spawn(run, req)` | `server.go:236, 427, 518, 771` | **Single choke point** for all runs (createRun, handoff, testRun, fireRoutine). Pre/post-run git snapshots hook here once and cover every run source, including scheduled routines. |
| `WithWorkDir(dir)` | `server.go:75`, flows to `executor.Request.Dir` | Workspace root plumbing already exists; Phase 0's `--workspace` flag reuses it instead of adding a parallel mechanism. |
| `HasRunningRun(botID)` gate | `server.go:216` (routines), `store.go:705` | Routines skip a tick if the bot is already running; **manual createRun is NOT gated** — two runs on the same bot can run concurrently today. Diff review must key on run ID, never "the bot's latest run". |
| `hub` pub/sub + `/api/stream` SSE | `server.go:897`, `newHub()` | New events (`diff_ready`, `review_done`) publish through the existing hub; the UI stream path needs no new transport. |
| `sameOrigin` middleware | `server.go:112` | All new workspace/diff endpoints inherit DNS-rebinding protection for free. |
| `assertNoLeaks` | `internal/store/state.go:244` | Persisted state is scrub-checked at write time; any new persisted fields must not contain the strings `api_key`/`secret`/`llm_`/`"owner"`. |
| Event text cap 8 KiB (`clip`) | `store.go:747`, `executor.go:325` | Diffs can **never** travel through run events — confirms the git-snapshot diff source and rules out event-borne patches. |
| `stopped` map + `cancels` | `server.go:53-57` | Stop semantics already distinguish user-stop from error; diff review must treat a stopped run's partial changes as reviewable. |
| `executor.Request.Worktree` | `executor.go:121` | Worktree isolation flag already passes through to flocode — a per-run worktree option is a small extension, not new surface. |
| `docs/ccpm-runs.md` log | `executor.go:459` | A run audit trail already exists in the workspace; run summaries belong beside it, not in state.json. |
| Electron shell | `desktop/main.js` | `Notification` API available for completion notifications; preload currently exposes **no IPC channels** (UI talks HTTP directly) — notifications need a new preload channel or can be done from the renderer via the Notification API. |
| `go:embed` asset list | `web/embed.go` | Embeds are **per-file directives** (`index.html`, `app.js`, `styles.css`) — every new JS file needs its own `//go:embed` line and a `<script>` tag in index.html. |

**One deliberate contract change required:** runs are ephemeral today. Diff
review needs `reviewState` + `checkpointId` to survive restart, or a restart
mid-review silently orphans a checkpoint. Decision in §6.4: a separate
`~/.flobot/runs.json`, leaving `state.json`'s "never persists runs" contract
intact.

## 3. Competitive landscape — top 20 OSS agentic coding tools (issue-mined)

Stars via GitHub API, 2026-10-22; pain points from each repo's
highest-engagement open issues (full mining in research notes, cycle 4):

| # | Repo | Stars | Category | Top pain point (their issues) |
|---|---|---|---|---|
| 1 | anomalyco/opencode | 219.9k | terminal agent | sandboxing (100👍), session goals, agent teams |
| 2 | openai/codex | 130.4k | terminal agent | **/undo back (96👍)**, LSP integration (72👍), **IDE-integrated diff/approval (67👍)** |
| 3 | google-gemini/gemini-cli | 107.5k | terminal agent | capacity/429s, instructions file ignored |
| 4 | zed-industries/zed | 92.0k | editor | test runner, multi-monitor, jj SCM |
| 5 | OpenHands/OpenHands | 91.5k | autonomous agent | — |
| 6 | FoundationAgents/MetaGPT | 71.0k | multi-agent framework | — |
| 7 | cline/cline | 70.7k | VS Code agent | **file-edit tool reliability (47👍)**, terminal integration (66👍), context window mgmt |
| 8 | openinterpreter/openinterpreter | 68.6k | local code interpreter | — |
| 9 | aaif-goose/goose | 55.5k | terminal agent | — |
| 10 | AntonOsika/gpt-engineer | 55.0k | one-shot generator | — |
| 11 | Aider-AI/aider | 49.7k | terminal pair | MCP support, provider breadth |
| 12 | continuedev/continue | 36.3k | IDE assistant | **API keys in plaintext (32👍)**, edit-diff token limits |
| 13 | TabbyML/tabby | 33.9k | self-hosted completion | — |
| 14 | Pythagora-io/gpt-pilot | 33.6k | app builder | — |
| 15 | charmbracelet/crush | 28.7k | terminal agent | ACP/IDE integration, plan/build modes, subagents |
| 16 | voideditor/void | 28.7k | editor (VS Code fork) | **agent can't find search anchor / edit failures**, apply-block errors |
| 17 | QwenLM/qwen-code | 28.6k | terminal agent | IDE integration, instructions ignored |
| 18 | onlook-dev/onlook | 27.0k | visual vibe-coding | — |
| 19 | RooCodeInc/Roo-Code | 24.3k | VS Code agent | multi-root workspace bugs, MCP OAuth |
| 20 | plandex-ai/plandex | 15.8k | terminal plan/apply | MCP, install ergonomics |

### What their issues tell us to build (and not build)

1. **Edit-apply reliability is the #1 cross-repo pain** (cline #4384 47👍,
   void #611/#857, continue #7544). Their agents fail to *find anchors* in
   search/replace. Code Studio's design sidesteps the entire class: the
   agent's edits land on disk via flocode, and the diff is computed by git —
   never parsed out of model output. Fuzzy apply exists only for the
   *user-directed* apply path. This is a structural differentiator; state it
   in the README when Phase 2 ships.
2. **Undo is demanded, not optional** (codex #9203, 96👍). Checkpoints with
   one-click restore are a headline feature, not a safety net — promote to
   principle 3 wording and AC-2d.
3. **IDE-integrated diff/approval is a top ask** (codex #2998, 67👍; ACP
   asks in crush and qwen-code). Validates diff review as the core product.
4. **LSP integration** (codex #8745, 72👍) — validates Phase 4; prefer
   auto-detection over manual config.
5. **Sandboxing** (opencode #2242, 100👍) and **secrets in plaintext**
   (continue #1729, 32👍) are widespread fears. flobot's loopback-only
   binding, sameOrigin guard, no-key-persistence, and assertNoLeaks are
   already answers — document them as product guarantees, not footnotes.
6. **MCP** is the most common integration ask (aider, plandex, void, roo).
   Explicit non-goal for v1 (§5.3); revisit after Phase 4.
7. **Plan/build mode separation** (crush #1734) and **subagents/teams**
   (crush #431, opencode #12711) are active frontier areas — plan-first
   toggle is in scope (US-13), teams are future work.
8. **Instructions/memory ignored** (gemini-cli #13852, qwen-code #494) —
   bot memory is verifiably prepended in `createRun` (server.go:762); keep
   it that way and test it.

## 4. Design principles (each mapped to evidence)

1. **Supervisory-first** — optimize direct/evaluate/correct, not token
   throughput. [longitudinal study; Anthropic division-of-labor]
2. **Diff-patch universal** — every agent edit is a unified diff; every review
   is a per-hunk diff review; apply is fuzzy. [Aider]
3. **Reversible** — checkpoint before every run and every apply; one-click
   restore is a headline feature, not a safety net (codex users beg for
   /undo back, 96👍). [Zed checkpoints; codex #9203]
4. **Bimodal** — acceleration: one keystroke accept-all; exploration: per-hunk
   review, provenance, "what calls this?". [Grounded Copilot]
5. **Flow-protective** — no modal interruptions; completion notification +
   sound instead of polling; follow-mode optional. [Zed; DevEx]
6. **Silence is a feature** — proactive UI (diagnostics, suggestions) ships
   with a suppression policy from day one. [Cursor Tab RL]
7. **Secret-safe** — keys never leave the machine; run records scrubbed.
   [flobot non-negotiables; XAgent running_records]

---

## 5. Product spec

### 5.1 Personas

- **P1 — Solo dev / repo owner.** Runs agents on their own repo; wants to see
  exactly what changed before it lands.
- **P2 — Operator of many bots.** Routines/scheduler produce runs; wants diffs
  reviewable from one place.
- **P3 — Reviewer.** Reviews an agent's patch without running anything.

### 5.2 User stories (with acceptance hooks)

| ID | Story | Phase |
|---|---|---|
| US-1 | Open flobot, see my repo's file tree, navigate files | 1 |
| US-2 | Select a file, view/edit with syntax highlighting | 1 |
| US-3 | Run bot+prompt against my repo from the editor, watch the live stream | 1 |
| US-4 | Watch the diff grow live while the agent works; stop and redirect | 2 |
| US-5 | When a run finishes, review the diff per file and per hunk | 2 |
| US-6 | Accept/reject per hunk, per file, or whole patch; only approved hunks land | 2 |
| US-7 | See test results inline with the diff before accepting | 3 |
| US-8 | Stop a runaway run; restore the pre-run checkpoint in one click | 2 |
| US-9 | See run history with diffs; audit what agents did over time | 2 |
| US-10 | Get notified (notification + sound) when a background run finishes | 2 |
| US-11 | Acceleration mode: accept-all with one keystroke | 2 |
| US-12 | Exploration mode: "what calls this?" grounded in my repo | 4 |
| US-13 | Agent asks for help mid-run; I answer inline (executor path only — the task API has no input endpoint, cancel only; see §5.3) | 2a+ |
| US-14 | See provenance + confidence per run | 3 |
| US-15 | Edit a file manually; my edits are checkpointed too | 2 |

### 5.3 Non-goals (v1)

No multi-repo workspaces, no workflow canvas, no remote/mesh access, no
plugin system, no debugger, no inline ghost-text completions (that is a
different product; revisit after Phase 4), **no mid-run Q&A over the task
API** (flocode exposes only cancel for a running task — verified
`internal/server/api/tasks.go`; any input/respond endpoint is a flocode-side
ask, not a flobot feature), no MCP (top integration ask across the
landscape, but out of scope for v1).

---

## 6. Concrete API and data contracts

All new endpoints live on flobot's existing loopback server. Errors are
`{"error":"..."}` with 4xx. All paths are workspace-relative.

### 6.1 Phase 0 — workspace/file API

```
GET  /api/v1/workspace/root                 → {"root":"/abs/path","name":"flobot"}
GET  /api/v1/workspace/tree?dir=<rel>&depth=1
     → {"dir":".","entries":[
          {"name":"cmd","path":"cmd","type":"dir","fileCount":3},
          {"name":"README.md","path":"README.md","type":"file","size":4120}
        ]}
GET  /api/v1/workspace/file?path=<rel>
     → {"path":"web/app.js","size":38211,"content":"...","sha256":"ab..","readOnly":false}
PUT  /api/v1/workspace/file?path=<rel>   body {"content":"...","baseSha256":"ab.."}
     → 200 {"path":...,"sha256":"cd.."} | 409 on sha mismatch (concurrent edit)
POST /api/v1/workspace/file?path=<rel>   body {"content":"..."}
     → 201 {"path":...,"sha256":"..."}   // create; 409 if exists
DELETE /api/v1/workspace/file?path=<rel>
     → 200 | 404 if missing
POST /api/v1/workspace/rename   body {"from":"...","to":"..."}
     → 200 | 409 if target exists
```

Rules (enforced in one helper, unit-tested):
- Reject absolute paths, any `..` segment, empty, NUL bytes → 400.
- `filepath.EvalSymlinks` result must stay under workspace root → 400.
- DELETE and rename refuse paths under `.git/` → 400 (checkpoints own
  git state; the UI never mutates it).
- Tree: skip `.git`, `node_modules`, dotfiles by default (`?all=1` to include);
  cap 500 entries/dir, sorted dirs-first.
- File read cap 2 MiB → 413; binary detection (NUL in first 8 KiB) →
  `{"binary":true}` without content.
- PUT writes via temp file + atomic rename (same pattern as state.json).
- `baseSha256` optimistic concurrency: mismatch → 409, client re-reads.

### 6.2 Phase 2 — diff review API

```
GET  /api/v1/runs/{id}/diff
     → {"runId":"r-123","status":"succeeded",
        "files":[
          {"path":"internal/server/server.go","oldMode":"100644",
           "hunks":[{"header":"@@ -10,7 +10,9 @@","lines":[
              {"type":"ctx","text":"func main() {"},
              {"type":"del","text":"  old()"},
              {"type":"add","text":"  new()"}]}],
           "additions":2,"deletions":1,
           "state":"pending"}   // pending|accepted|rejected
        ],
        "checkpoint":"cp-7f3a",  // pre-run snapshot id
        "totals":{"files":3,"additions":41,"deletions":12}}

POST /api/v1/runs/{id}/diff/apply
     body {"selections":[
        {"path":"internal/server/server.go","hunks":[0,2]},   // hunk indices
        {"path":"web/app.js","hunks":"all"}
     ]}  → 200 {"applied":[...],"skipped":[...]}

POST /api/v1/runs/{id}/diff/reject   → 200 (marks run rejected; no disk change)

POST /api/v1/checkpoints/{id}/restore → 200 (restores snapshot; creates new checkpoint first)
GET  /api/v1/checkpoints?limit=50     → list {id, runId, createdAt, files, additions, deletions}
```

Diff source: **git snapshot before/after each run** (Option B). The
snapshot must be attribution-correct, not naive `git diff HEAD`:

- **Dirty tree at run start**: the user may have uncommitted edits. A
  plain `git diff HEAD` after the run would attribute the user's own
  pre-existing edits to the agent. So: pre-run, record
  `git stash create`-style tree objects — concretely `git add -A` in a
  temporary index (`GIT_INDEX_FILE=tmp`) then `git write-tree` — giving a
  pre-run tree; post-run, the same gives a post-run tree; the run's diff
  is `git diff-tree pre post`. This captures tracked files without
  touching the user's index or worktree.
- **Untracked files**: `git add -A` in the temp index includes untracked
  files, so new files created by the agent appear in the diff (as new
  files). Files the user had untracked *before* the run are in the pre
  tree too, so they don't leak into the run's diff.
- **Deleted files** appear naturally in diff-tree.
- **Not a git repo**: no diff, review shows "workspace not a git repo";
  runs still work, review is unavailable (fail-closed, no silent
  corruption).

Phase 2a makes `Result.patch` the source of truth and the git snapshot
becomes the safety net (and the only source for executor-path runs).

Apply semantics (Aider-informed):
- Hunks apply with exact match first; on failure, fuzzy strategies in order:
  trim leading/trailing context, whitespace-normalized match, sub-hunk split.
- A hunk that cannot apply even fuzzily → reported in `skipped`, never
  silently dropped.
- Every apply creates a checkpoint before touching disk.

### 6.3 Phase 2a — taskclient (feature flag)

`FLOBOT_BACKEND=taskclient|executor` (default `executor`). New package
`internal/taskclient`:

```go
type Client struct{ base string; hc *http.Client }
func (c *Client) Submit(ctx, task.Task) (SubmitResult, error)  // POST /api/v1/tasks → 202 {task_id,status}
func (c *Client) Status(ctx, id string) (Status, error)            // GET  /api/v1/tasks/{id} (has dropped_events)
func (c *Client) Stream(ctx, id string) (<-chan Event, error)       // GET  .../stream (SSE; no replay)
func (c *Client) Cancel(ctx, id string) error                       // POST .../cancel
func (c *Client) Result(ctx, id string) (*task.Result, error)        // GET  .../result
```

Event translation: flocode SSE events (`event: <type>\ndata: <json>`)
→ flobot's existing run-event shape so `web/app.js` stream rendering is
unchanged. **No replay exists**: on disconnect, stop rendering, poll
Status until terminal, then fetch Result and render from it. The
`dropped_events` counter drives that switch. Executor fallback: if
`taskclient` and serve is unreachable at submit time, log once, fall back
to child-process executor for that run (never mid-run).

### 6.4 State model additions

**Decision (new in v3): runs stay out of `state.json`.** The existing
contract — "runs and events are never persisted" (`state.go:39`) — stays
intact. Instead, a separate `~/.flobot/runs.json` (same 0600 + atomic-rename
+ `assertNoLeaks` pattern) holds only run *summaries*:

```json
{"runs":[{
  "id":"run-9f2c","botId":"bot-1","status":"succeeded",
  "createdAt":"...","endedAt":"...",
  "diffSummary":{"files":3,"additions":41,"deletions":12},
  "checkpointId":"cp-7f3a",
  "reviewState":"pending"      // pending|accepted|rejected|partial
}]}
```

Capped at the same 200 finished runs as the in-memory store. Diffs are
never stored — recomputed from the git snapshot pair (Phase 2) or fetched
from the taskclient result (Phase 2a). Checkpoints live under
`~/.flobot/checkpoints/<id>/` (mechanism decided in §9 Q1).

---

## 7. UI spec

### 7.1 Layout (single window, three panes)

```
┌────────────┬──────────────────────────────┬───────────────────────┐
│ FILE TREE  │ EDITOR (CodeMirror 6)        │ AGENT PANEL           │
│ (240px)    │  - syntax highlighting       │ (380px, collapsible)  │
│            │  - dirty dot on modified     │ ┌───────────────────┐ │
│ cmd/       │  - diagnostics gutter (P4)  │ │ run header:       │ │
│ internal/  │                              │ │  bot ▾  [Run][Stop]│ │
│ web/       │  tabs: app.js × app.js·edit  │ ├───────────────────┤ │
│ docs/      │                              │ │ stream (live)     │ │
│            │                              │ │  ▸ tool: read ...  │ │
│            │                              │ │  ▸ edit server.go │ │
│            │                              │ │ [live diff badge] │ │
│            │                              │ ├───────────────────┤ │ │
│            │                              │ │ prompt input       │ │
│            │                              │ │ [@file context]   │ │
└────────────┴──────────────────────────────┴───────────────────────┘
 status bar: run r-123 · 3 files +41/-12 · checkpoint cp-7f3a [restore]
```

When a run finishes, the Agent Panel's stream section collapses to a summary
and a **Review Changes** button opens the diff review view (below).

### 7.2 Diff review view

Replaces the editor pane content (tab titled `review r-123`):

- File list rail on the left of the pane (path, +adds/-dels, per-file
  accept/reject checkbox).
- Per-file unified diff with per-hunk accept/reject toggles; hunk states
  persist in memory until Apply/Reject.
- Header: totals, test report chip (Phase 3), provenance/confidence (Phase 3),
  `Accept all` / `Reject all` / `Apply selection`.
- Keyboard: `j/k` hunk nav, `a` accept hunk, `r` reject hunk,
  `A` accept all, `Enter` apply selection, `Esc` cancel.

### 7.3 Keyboard map (global)

| Keys | Action |
|---|---|
| `Cmd-P` | file quick-open (fuzzy over tree) |
| `Cmd-Enter` | run prompt |
| `Cmd-.` | stop run |
| `Cmd-Shift-R` | open review for latest run |
| `Cmd-Shift-Z` | restore latest checkpoint |
| `@` in prompt | attach file/symbol context (Phase 4: symbols) |

### 7.4 Flow-protection behaviors

- Run completion while window blurred → OS notification + optional sound
  (Electron `Notification` API; no content beyond "run finished: 3 files").
- Follow-mode toggle (off by default): editor jumps to each file the agent
  edits. Off = editor stays where the user is.
- No modal dialogs anywhere in the run/review loop; errors are inline banners.
- Diagnostics/suggestions (Phase 4) render only when confidence threshold met
  (suppression policy; log suppression rate locally).

---

## 8. Phase plan with file-level tasks

### Phase 0 — workspace/file API (est. S)

| # | Task | Files |
|---|---|---|
| 0.1 | `workspace` package: root resolution, path validation, tree, read, write | `internal/workspace/workspace.go` (new) |
| 0.2 | HTTP handlers wiring | `internal/server/server.go`, `internal/server/workspace.go` (new) |
| 0.3 | Tests: traversal, symlink escape, caps, binary, sha concurrency | `internal/workspace/workspace_test.go` (new) |
| 0.4 | Workspace root flag `--workspace <dir>` (default: cwd) | `cmd/flobot/main.go` |

AC-0a..0d as in §10.

### Phase 1 — editor UI (est. M)

| # | Task | Files |
|---|---|---|
| 1.1 | Vendor CodeMirror 6 bundle (no build step: prebuilt ESM into `web/vendor/`) | `web/vendor/codemirror/*` (new) |
| 1.2 | File tree component (lazy dirs, dirty markers) | `web/tree.js` (new) |
| 1.3 | Editor pane: open/save (PUT with baseSha256), dirty state, tabs | `web/editor.js` (new) |
| 1.4 | Agent panel: bot select, prompt, run/stop, live stream (reuse existing stream code) | `web/app.js` (extract) |
| 1.5 | Quick-open (Cmd-P) | `web/quickopen.js` (new) |
| 1.6 | Embed assets (existing `web/embed.go` picks up new files) | — |

### Phase 2 — diff review + checkpoints (est. M–L)

| # | Task | Files |
|---|---|---|
| 2.1 | Git snapshot capture pre/post run in `spawn()` (single choke point, covers createRun/handoff/testRoutine/scheduler) | `internal/server/server.go:813` |
| 2.2 | Diff computation + hunk parsing (own parser, no external dep) | `internal/diff/diff.go` (new) |
| 2.3 | Checkpoint store (spike: git stash vs file bundle) | `internal/checkpoint/` (new) |
| 2.4 | Diff review API endpoints | `internal/server/diffreview.go` (new) |
| 2.5 | Fuzzy hunk apply | `internal/diff/apply.go` (new) |
| 2.6 | Review UI (per §7.2) + keyboard map | `web/review.js` (new) |
| 2.7 | Completion notification (Electron) | `desktop/main.js`, `desktop/preload.js` |
| 2.8 | Run summaries persisted to `~/.flobot/runs.json` (0600, atomic, leak-checked) | `internal/store/runs.go` (new) |

### Phase 2a — taskclient swap (est. L)

| # | Task | Files |
|---|---|---|
| 2a.1 | HTTP client + SSE parsing | `internal/taskclient/client.go` (new) |
| 2a.2 | Event translation to flobot run events | `internal/taskclient/translate.go` (new) |
| 2a.3 | Backend selection + executor fallback | `internal/executor/executor.go` |
| 2a.4 | Result envelope → diff review pipeline (patch becomes source of truth) | `internal/server/diffreview.go` |
| 2a.5 | Contract tests against a stub serve (httptest) | `internal/taskclient/client_test.go` |

### Phase 3 — result contract UI (est. S–M)

Test report (pass/fail chips per test), provenance, confidence, and
`requires_human_review` banner rendered in the review header. Data already
arrives via taskclient result; executor path shows "unavailable" chips.

### Phase 4 — semantic surfaces (est. L, flocode-side dependency)

Requires flocode to expose LSP diagnostics + codequery over HTTP (currently
Go-API-only — separate ask on the flocode repo). Editor: diagnostics gutter,
go-to-definition, symbol outline, "what calls this?" panel feeding prompt
context via `@symbol`.

---

## 9. Open questions (decision needed before Phase 2)

1. **Checkpoint mechanism:** `git stash` (needs git, cheap, atomic) vs file
   bundle under `~/.flobot/checkpoints/` (works without git). Recommend: git
   stash when repo, error clearly when not.
2. **Editor library:** CodeMirror 6 (recommended; agent-gui precedent, small)
   vs Monaco.
3. **Diff source:** confirm git-snapshot-first (Option B) before taskclient.
4. **Phase 4 scope:** in this effort or separate PRD?
5. **Notifications:** default on or opt-in?
6. **Stopped runs:** a user-stopped run may have partial edits on disk.
   Review the partial diff, or auto-restore the pre-run checkpoint on stop?
   Recommend: review (never destroy work silently), with a one-click
   "restore pre-run state" in the review header.

---

## 10. Acceptance criteria

### Phase 0
- [ ] AC-0a: `GET /api/v1/workspace/tree?dir=.` returns root entries.
- [ ] AC-0b: `../`, absolute, NUL, symlink-escape paths → 400, never read outside root (table-driven tests).
- [ ] AC-0c: read/write round-trip; sha mismatch → 409.
- [ ] AC-0d: >2 MiB file → 413; binary → `binary:true` without content.

### Phase 1
- [ ] AC-1a: file tree renders, lazy-loads dirs, shows dirty markers.
- [ ] AC-1b: open file, edit, save; reload shows saved content.
- [ ] AC-1c: run bot+prompt from editor; stream renders live; Stop works.
- [ ] AC-1d: Cmd-P quick-open navigates files.

### Phase 2
- [ ] AC-2a: run that changes files → `GET /runs/{id}/diff` returns parsed hunks.
- [ ] AC-2b: per-hunk and per-file accept/reject; only accepted hunks land on disk.
- [ ] AC-2c: fuzzy apply: hunk with trailing-context drift still applies; unappliable hunk reported in `skipped`.
- [ ] AC-2d: reject-all restores pre-run state; restore-checkpoint after apply works; restore is one keystroke (`Cmd-Shift-Z`) from anywhere.
- [ ] AC-2e: completion while blurred → OS notification; no notification while focused.
- [ ] AC-2f: run history shows diffSummary and reviewState.

### Phase 2a
- [ ] AC-2a-a: flag on + serve running → runs execute via task API; stream renders unchanged UI.
- [ ] AC-2a-b: stream disconnect mid-run → UI switches to status polling and
  renders the final state from Result (no replay exists; no fabricated
  "resumed" stream).
- [ ] AC-2a-c: cancel → run records "stopped".
- [ ] AC-2a-d: serve absent at submit → executor fallback, single log line.
- [ ] AC-2a-e: Muse key absent from every request body and log (grep test).

### Phase 3
- [ ] AC-3a: test report chips render with pass/fail per test.
- [ ] AC-3b: provenance + confidence + requires_human_review banner render.

### Phase 4
- [ ] AC-4a: diagnostics in gutter for open file, suppressed below threshold.
- [ ] AC-4b: go-to-definition navigates within workspace.
- [ ] AC-4c: "what calls this?" panel; result attachable to prompt via @.

---

## 11. Success metrics (local, privacy-preserving)

Logged to state.json run records, never sent anywhere:

- **Review time:** run end → apply/reject decision (target: <60s median for
  small patches).
- **Accept rate:** accepted hunks / total hunks (Cursor Tab RL analog; a low
  accept rate means the agent or the diff view is failing).
- **Fallback rate:** % runs on executor fallback (should be ~0 when serve up).
- **Restore rate:** checkpoints restored per 10 runs (high rate = agent
  quality problem, not UI problem — but the restore must always work).

---

## 12. Risks

| Risk | Mitigation |
|---|---|
| Backend swap breaks standalone mode | Feature flag + executor fallback (AC-2a-d) |
| Path traversal in workspace API | Single validation helper + table-driven tests (AC-0b) |
| Diff apply corrupts user work | Checkpoint before every apply; fuzzy apply never silently drops hunks |
| state.json cap pressure from diff data | Store summaries only; diffs derived |
| Scope creep into full IDE | Non-goals §5.3; phase gates |
| CodeMirror vendoring bloat | Prebuilt ESM bundle, size budget <400 KiB gzipped |
| flocode LSP/codequery not over HTTP | Phase 4 explicitly gated on flocode-side change |

---

## 13. Effort

| Phase | Effort | Ships |
|---|---|---|
| 0 workspace API | S | file access |
| 1 editor UI | M | tree + editor + run panel |
| 2 diff review + checkpoints | M–L | the core supervisory loop |
| 2a taskclient | L | flocode serve as backend |
| 3 result contract UI | S–M | tests/provenance inline |
| 4 semantic surfaces | L | diagnostics, navigation |

---

## Appendix A — Research sources

Papers: SWE-agent (arXiv 2405.15793) · XAgent (OpenBMB) · AI Agentic
Programming survey (arXiv 2508.11126) · longitudinal supervisory-engineering
study (arXiv 2605.23135) · Grounded Copilot (arXiv 2206.15000) · Expectation
vs Experience (CHI'22) · Reading Between the Lines (CHI'24) · DevEx (Noda et
al. 2023) · IntelliCode design exploration (ICSE'23).

Engineering: Aider unified diffs + benchmarks (aider.chat) · Cursor agent best
practices (Jan 2026) · Cursor Tab online RL (Sep 2025) · VS Code Copilot
inline suggestions parts 1–2 (Sep 2026) · Zed Agent Panel docs · Anthropic
"How Claude Code is used in practice" (Jun 2026, ~400k sessions).

Local ground truth: flobot `internal/server`, `internal/executor`,
`web/app.js`, `desktop/main.js`; flocode `internal/server/api/tasks.go`,
`internal/task/contract.go`, `internal/cli/serve.go`, `internal/cli/gateway.go`.
