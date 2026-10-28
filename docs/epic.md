outcome: Make flobot's Code Studio read and behave like the OpenAI Codex desktop app — parallel agent threads in isolated worktrees, a live diff while the agent works, a review queue of finished-but-unreviewed work, and workspace-wide symbol search — all driven by flocode as the backend.
in-scope:
- Threads: a named, persisted unit of agent work with its own git worktree; several may run at once; the user's tree is never a contested resource.
- Live diff: the diff pane fills while the run is still in flight; a hunk the agent rewrites after the human accepted it is re-opened, not silently dropped.
- Review queue: finished runs with a non-empty diff appear automatically, carry a pending-hunk count, and survive a restart.
- Next-fire preview and editable routine triggers: the schedule card says when it will next fire, and a trigger can be changed without deleting the routine.
- Workspace-wide symbol search: "go to symbol" across the whole workspace, not just the open file.
- Durability: threads, queue entries and run summaries persist to a new `~/.flobot/threads.json`, leaving `state.json`'s "never persists runs" contract intact.
non-goals:
- Shipping a Monaco or CodeMirror editor core. Highlighting, multi-cursor, split views and a minimap are a separate dependency decision, not a task in this epic.
- Cloud sync, remote workspace sharing, or multi-user collaboration.
- A plugin marketplace or IDE extensions.
- Any binding beyond loopback, or any import of the `flocode` module (HTTP only).
- A worktree setup-script lifecycle (Codex has one; it is post-epic).
acceptance:
- `GET /api/v1/workspace/symbols?q=` returns matches from more than one file, ranked by relevance then path, and an empty query returns an empty list rather than the whole workspace.
- Two threads for the same bot can be active at once, and neither writes into the user's working tree.
- A `diff_ready` event is published while a run is still in flight, and the diff pane shows hunks before the run ends.
- Accepting a hunk that the agent later changes re-opens that hunk.
- A finished run with a non-empty diff appears in the review queue with no user action, and the queue badge count matches the pending entries.
- Threads and queue entries survive an app restart; `state.json` remains run-free; the persisted file never contains `api_key`, `secret`, `llm_` or `"owner"`.
- A routine's trigger can be changed without deleting the routine, and the card shows the correct next fire time for a cron, an interval and a manual trigger.
- `go build ./...`, `go vet ./...` are clean; the five existing audit scripts in `docs/audit/` still pass; a new `docs/audit/verify-codex-parity.mjs` exercises the thread → run → live diff → accept → queue-resolved loop against the real server with no mocks.
