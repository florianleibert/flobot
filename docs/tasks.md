# Tasks — Codex desktop replication in Code Studio

Epic: [`docs/epic.md`](./epic.md) · PRD: [`docs/prd-codex-desktop-editor.md`](./prd-codex-desktop-editor.md)

Ordering is by dependency. Tasks marked `parallel: true` own disjoint files and may run
concurrently. **No two tasks edit the same file.**

| id | requirement | files owned | depends_on | parallel | status |
|---|---|---|---|---|---|
| T-1 | R-EDIT-a | `internal/workspace/symbols.go`, `internal/workspace/symbols_test.go`, `internal/server/symbols.go`, `internal/server/symbols_test.go`, `internal/server/server.go` | none | true | **done** |
| T-2 | R-DURABLE | `internal/store/threads.go`, `internal/store/threads_test.go` | none | true | pending |
| T-3 | R-SCHED | `internal/schedule/next.go`, `internal/schedule/next_test.go`, `internal/server/routines.go`, `internal/server/routines_test.go` | none | true | pending |
| T-4 | R-THREAD | `internal/threads/threads.go`, `internal/threads/threads_test.go`, `internal/server/threads.go`, `internal/server/threads_test.go` | T-2 | false | pending |
| T-5 | R-QUEUE | `internal/queue/queue.go`, `internal/queue/queue_test.go`, `internal/server/queue.go`, `internal/server/queue_test.go` | T-2 | false | pending |
| T-6 | R-LIVEDIFF | `internal/server/livediff.go`, `internal/server/livediff_test.go` | T-4 | false | pending |
| T-7 | frontend thread, queue and symbol surfaces | `web/app.js`, `web/index.html`, `web/styles.css` | T-1, T-4, T-5, T-6 | false | pending |

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
records, review-queue entries and run summaries. Corrupted JSON is handled without crashing. The persisted
bytes never contain `api_key`, `secret`, `llm_` or `"owner"` — enforced by the existing `assertNoLeaks` check
at write time plus a grep test.

## T-3 — next-fire preview and editable triggers (R-SCHED)

files: `internal/schedule/next.go`, `internal/schedule/next_test.go`, `internal/server/routines.go`,
`internal/server/routines_test.go`
depends_on: none
parallel: true
acceptance: `internal/schedule` computes the next fire time for a cron expression, an interval and a manual
trigger. `PATCH /api/v1/routines/{id}` accepts a new trigger and validates it before storing; an unparseable
trigger is still refused at write time with the existing 400. The routine card renders the next fire time.

## T-4 — threads with worktree isolation (R-THREAD)

files: `internal/threads/threads.go`, `internal/threads/threads_test.go`, `internal/server/threads.go`,
`internal/server/threads_test.go`
depends_on: T-2
parallel: false
acceptance: `POST /api/v1/threads` creates a thread with its own git worktree and returns its path. Two threads
for the same bot can be active simultaneously. Every path a thread touches is rooted at that thread's worktree,
so no agent writes into the user's working tree. The executor's existing `Worktree` flag is reused, not
replaced. Thread state persists through the T-2 store.

## T-5 — review queue (R-QUEUE)

files: `internal/queue/queue.go`, `internal/queue/queue_test.go`, `internal/server/queue.go`,
`internal/server/queue_test.go`
depends_on: T-2
parallel: false
acceptance: A finished run with a non-empty diff enqueues a `pending` entry without user action. Entries move
`pending → in_review → resolved`; resolving removes the entry from the pending count. The badge count equals
the number of pending entries. Queue state persists through the T-2 store.

## T-6 — live diff (R-LIVEDIFF)

files: `internal/server/livediff.go`, `internal/server/livediff_test.go`
depends_on: T-4
parallel: false
acceptance: While a run is in flight, a bounded interval publishes a `diff_ready` event through the existing
hub whenever the run's diff changes, and the pane renders hunks before the run ends. Exactly one goroutine
per run polls; it stops on the terminal event. A hunk whose content changes after the human accepted it is
re-opened as `pending` rather than dropped.

## T-7 — frontend surfaces

files: `web/app.js`, `web/index.html`, `web/styles.css`
depends_on: T-1, T-4, T-5, T-6
parallel: false
acceptance: The thread sidebar lists threads with status and a pending-hunk badge; the review queue opens a diff
from the badge; a symbol palette queries `GET /api/v1/workspace/symbols` and opens a file at the matching line.
The frontend bundle in `web/` is generated from `flobot-ui-redesign/src` — edit the source and re-sync.
