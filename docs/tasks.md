# Tasks — Codex desktop replication in Code Studio

Epic: [`docs/epic.md`](./epic.md) · PRD: [`docs/prd-codex-desktop-editor.md`](./prd-codex-desktop-editor.md)

Ordering is by dependency. Tasks marked `parallel: true` own disjoint files and may run
concurrently. **No two tasks edit the same file** — with two deliberate exceptions: the route
table and the Server struct in `internal/server/server.go` are shared, and each server-side task
adds exactly one additive line to each. Those lines never touch the same context, so they merge
cleanly. T-4, T-5 and T-6 additionally share `internal/server/diffreview.go` (the snapshot and
review machinery) and are therefore **sequential**, not parallel.

| id | requirement | files owned | depends_on | parallel | status |
|---|---|---|---|---|---|
| T-1 | R-EDIT-a | `internal/workspace/symbols.go`, `internal/workspace/symbols_test.go`, `internal/server/symbols.go`, `internal/server/symbols_test.go`, `internal/server/server.go` | none | true | **done** |
| T-2 | R-DURABLE | `internal/store/threads.go`, `internal/store/threads_test.go` | none | true | **done** |
| T-3 | R-SCHED | `internal/schedule/next.go`, `internal/schedule/next_test.go`, `internal/server/routines.go`, `internal/server/routines_test.go`, `internal/store/store.go` | none | true | **done** |
| T-4 | R-THREAD | `internal/threads/threads.go`, `internal/threads/threads_test.go`, `internal/server/threads.go`, `internal/server/supervision.go`, `internal/server/merge.go`, `internal/server/diffreview.go` | T-2 | false | **done** |
| T-5 | R-QUEUE | `internal/queue/queue.go`, `internal/queue/queue_test.go`, `internal/server/queue.go`, `internal/server/diffreview.go` | T-2 | false | **done** |
| T-6 | R-LIVEDIFF | `internal/server/livediff.go`, `internal/server/diffreview.go` | T-4 | false | **done** |
| T-7 | frontend thread, queue and symbol surfaces | `flobot-ui-redesign/src/supervision.js`, `flobot-ui-redesign/src/main.js`, `web/app.js` (generated) | T-1, T-4, T-5, T-6 | false | **done** |
| T-8 | R-EDITOR: open editors, find & replace, workspace text search | `internal/workspace/search.go`, `internal/workspace/search_test.go`, `internal/server/search.go`, `internal/server/server.go`, `flobot-ui-redesign/src/editor.js`, `flobot-ui-redesign/src/find.js`, `flobot-ui-redesign/src/search.js`, `flobot-ui-redesign/index.html`, `web/*` (generated) | T-7 | false | **done** |

## T-1 — workspace-wide symbol search (R-EDIT-a)

files: `internal/workspace/symbols.go`, `internal/workspace/symbols_test.go`, `internal/server/symbols.go`,
`internal/server/symbols_test.go`, `internal/server/server.go`
depends_on: none
parallel: true
acceptance: `GET /api/v1/workspace/symbols?q=<name>` returns `{query, symbols[{path,line,kind,name}], truncated}`.
Matches are ranked exact → case-insensitive exact → prefix → substring, then by path, then by line. An empty
query returns an empty list, not the whole workspace. The search is bounded (2000 files, 512 KiB per file,
200 results) and `truncated` reports that a bound cut the result short. Artifact directories, dot-directories
and symlinks are skipped, so the search cannot walk out of the workspace root.

## T-2 — durable threads and queue store (R-DURABLE)

files: `internal/store/threads.go`, `internal/store/threads_test.go`
depends_on: none
parallel: true
acceptance: Reads and writes `~/.flobot/threads.json` with 0600 permissions via atomic rename. Persists thread
records and review-queue entries. Corrupted JSON is handled without crashing. The persisted bytes never
contain `api_key`, `secret`, `llm_` or `"owner"` — enforced by the existing `assertNoLeaks` check at write
time plus a test that the write is refused when a goal looks like a secret.
**Run summaries are deliberately not stored here.** They already persist to `~/.flobot/runs.json` through
`RunStore` (`internal/store/runs.go`); copying them into a second file would create two sources of truth
for the same data. The PRD was corrected to match.

## T-3 — next-fire preview and editable triggers (R-SCHED)

files: `internal/schedule/next.go`, `internal/schedule/next_test.go`, `internal/server/routines.go`,
`internal/server/routines_test.go`, `internal/store/store.go`
depends_on: none
parallel: true
acceptance: `internal/schedule.NextFire` computes the next fire time for a cron expression, an interval and a
manual trigger. A cron is anchored to the clock; an interval is anchored to the last fire, and an overdue
routine reports its next fire from now rather than a time that has already passed. `PATCH
/api/v1/routines/{id}` accepts a new trigger and/or paused flag, validates the trigger with the same rule the
create path uses (an unparseable cron is refused at write time with 400 and is not stored), and leaves the
routine's other fields untouched. The roster now carries each routine's `next_fire`.

## T-4 — threads with worktree isolation (R-THREAD)

files: `internal/threads/threads.go`, `internal/threads/threads_test.go`, `internal/server/threads.go`,
`internal/server/supervision.go`, `internal/server/merge.go`, `internal/server/diffreview.go`
depends_on: T-2
parallel: false
acceptance: `POST /api/v1/threads` creates a thread with its own git worktree and returns its path. The
worktree is created at `.flocode/worktrees/<name>` on branch `flocode/worktree/<name>` — flocode's own
convention — so the child attaches to the worktree flobot made instead of creating a second one. A run
started in a thread works inside that worktree, and the review surface shows **the agent's changes**:
the snapshot is taken in the run's own root, not the user's tree. Two threads for the same bot can be
active at once and neither writes into the user's tree. `POST /api/v1/runs/{id}/diff/merge` merges the
approved hunks back into the user's tree, and refuses with 409 if the user's tree changed since the
thread started.

## T-5 — review queue (R-QUEUE)

files: `internal/queue/queue.go`, `internal/queue/queue_test.go`, `internal/server/queue.go`,
`internal/server/diffreview.go`
depends_on: T-2
parallel: false
acceptance: A finished run with a non-empty diff enqueues a `pending` entry without user action — the
hook is in `capturePostRun`, the one place that knows the diff. Entries move `pending → in_review →
resolved`; resolving removes the entry from the pending count. The badge count equals the number of
pending entries. Queue state persists through the T-2 store.

## T-6 — live diff (R-LIVEDIFF)

files: `internal/server/livediff.go`, `internal/server/diffreview.go`
depends_on: T-4
parallel: false
acceptance: While a run is in flight, a bounded interval recomputes the run's diff and publishes a
`diff_ready` event through the existing hub whenever it changes. Exactly one poller runs per run and it
stops with the run's context. `GET /api/v1/runs/{id}/diff/live` serves the in-flight diff in the same
payload shape as the settled diff, so one renderer serves both.

## T-7 — frontend surfaces

files: `flobot-ui-redesign/src/supervision.js`, `flobot-ui-redesign/src/main.js`, `web/app.js` (generated)
depends_on: T-1, T-4, T-5, T-6
parallel: false
acceptance: The frontend bundle in `web/` is generated from `flobot-ui-redesign/src` — edit the source and
re-sync with `node sync-to-flobot.mjs`. The new module is self-contained: it creates its own DOM and
its own styles, so it depends on the editor only through the `openFile` bridge the editor already
exposes. It adds a supervision card (threads plus the review-queue badge), a symbol palette on
`⌘⇧O` that queries `GET /api/v1/workspace/symbols` and opens a file at the matching line, and a
4-second poll that pauses while the tab is hidden.

## T-8 — editor core: open editors, find & replace, workspace text search

files: `internal/workspace/search.go`, `internal/workspace/search_test.go`,
`internal/server/search.go`, `internal/server/server.go`,
`flobot-ui-redesign/src/editor.js`, `flobot-ui-redesign/src/find.js`,
`flobot-ui-redesign/src/search.js`, `flobot-ui-redesign/index.html`,
`web/app.js` (generated)
depends_on: T-7
parallel: false
acceptance: The PRD's Codex-parity gaps are closed; what was left was the editor
itself — one buffer, one file, no way to search. Three additions:

- **Open editors.** `editor.js` keeps a tab per open file, each with its own
  buffer, file handle and dirty state. Switching tabs snapshots the outgoing
  buffer, so an edit is never lost; a dirty buffer asks before it is closed.
  The open editors — unsaved buffers included — are persisted in
  `localStorage` and restored on reload, so a restart does not cost an edit.
  `⌘W` closes the active editor, `⌘Tab` cycles.
- **Find & replace.** `⌘F` opens a find bar that selects each match and
  reports its position ("3 of 17 · line 42"); replace and replace-all work on
  the buffer and keep the dirty state honest.
- **Workspace text search.** `GET /api/v1/workspace/search?q=…` returns
  `{query, matches[{path,line,col,text}], truncated, scope}`. The search is
  bounded (2000 files, 512 KiB per file, 200 hits) and skips artifact
  directories, dot-directories and symlinks, so it cannot walk out of the
  workspace. `scope` narrows it to a project's files or to the projects behind
  a tag, which is what makes "search inside #frontend" one click.

Verified: `go build`, `go vet`, `gofmt`, the workspace and server tests, and a
12-check Playwright audit (`docs/audit/verify-editor-overhaul.mjs`).
