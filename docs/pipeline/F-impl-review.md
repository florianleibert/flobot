# Implementation Review — Code Studio Phases 0–3 (commit 733e44f + d8f16d7)

**Reviewer:** adversarial senior-engineer pass · 2026-10-22
**Scope:** implementation vs `docs/prd-code-studio.md` (v4) and the 100-round feature research (`docs/research-gui-editor-features.md`)
**Verdict:** Phases 0–3 are implemented, tested, and committed. The core loop (run → diff → per-hunk accept/reject → checkpoint restore) is solid, with unusually good guard discipline. Six gaps remain, ranked below; the top three are implemented in this pass.

---

## 1. What's built (verified against source)

| PRD item | Status | Evidence |
|---|---|---|
| Phase 0 workspace API (tree/file CRUD, traversal guards, caps) | ✅ | `internal/workspace/`, `internal/server/workspace.go`, routes at `server.go:207-216` |
| Phase 1 editor UI (tree, editor, quick-open, tabs) | ✅ | `web/editor.js` (571 lines), `web/app.js` |
| Phase 2 diff review + checkpoints | ✅ | `internal/server/diffreview.go` (826 lines), `internal/diff/`, `internal/checkpoint/`, `internal/store/runs.go` |
| Phase 2a taskclient | ✅ | `internal/taskclient/` (client, adapter, contract tests) |
| Phase 3 result contract UI | ✅ | `resultContractEl` in `app.js:1260+`, `noteResultContract` in `diffreview.go:792` |
| Completion notifications (Electron) | ✅ | `desktop/main.js:211` `notifyRunEnd`, gated on window blur |
| Run summaries persisted (`~/.flobot/runs.json`) | ✅ | `RunStore`, 0600, atomic |
| Per-hunk accept/reject with fuzzy apply | ✅ | `applyDiff` (diffreview.go:397), `internal/diff/apply.go` |
| Review-state guard, live-run guard, post-tree guard, torn-apply guard | ✅ | diffreview.go:424-458 — four distinct guards, each commented with the failure mode it prevents |

## 2. Strengths worth keeping

1. **Guard discipline in `applyDiff`** — review-state (409 on re-review), live-run (409), any-running-run (409), post-tree TOCTOU (409 with "restore a checkpoint instead"), and the detached-context apply (survives client disconnect). This is the best code in the repo.
2. **Apply semantics** (rebuild-from-pre + selected hunks, revert unselected) prevents double-apply structurally rather than by flag.
3. **Restart fallback** (`resolveRunTrees`) — pre from checkpoint, post from persisted `PostTree`, never a fresh capture.
4. **Leak discipline** — `assertNoLeaks`, no keys in events, 8 KiB event cap keeps diffs out of the event stream.

## 3. Gaps (ranked by research demand × implementation cost)

### G1 — Review metrics not recorded (PRD §11 unimplemented) — HIGH
PRD §11 defines success metrics: review time (run end → decision) and accept rate (accepted/total hunks). Neither is recorded: `applyDiff` sets `ReviewState` but stamps no decision timestamp, and hunk counts are not persisted. Without them, the PRD's own success criteria are unmeasurable.

**Fix:** add `ReviewedAt` + `HunksAccepted`/`HunksTotal` to `RunSummary`; stamp in `applyDiff`/`rejectDiff`.

### G2 — No session/run resume list with review state filter — HIGH
The runs list exists but there's no "needs review" surface: a run finishing while the user is elsewhere leaves no visible pending-review queue. Research R95 (session history) + the PRD's supervisory-first principle both call for it.

**Fix:** "Needs review" filter chip on the runs list (reviewState=pending && diffSummary), plus a badge in the header.

### G3 — No context/token usage meter — MEDIUM
Research R85/R87: token/cost and context-window meters are top open requests in AI editors. flocode events don't carry usage today, so this is data-blocked; but the run summary can at least show the result-contract fields it already has (confidence, tests) — it does. Defer the meter until flocode emits usage events.

### G4 — Collapsible command blocks: partially built — LOW
Thinking blocks collapse (`<details>`), tool_start/tool_end pair, but long tool output is not collapsible (Zed #58333 pattern). Cheap QoL win.

**Fix:** wrap tool output in `<details>` when > N lines.

### G5 — Plan-mode review UI — MEDIUM (data-blocked)
`plan` events render as plain text. A plan→approve→execute flow needs flocode to emit structured plan events; currently unstructured. Defer.

### G6 — Live preview pane — LOW (out of scope for v1)
Web-dev specific; not in the PRD. Defer.

## 4. Correctness notes (no action needed)

- `noteResultContract`'s locked `Mutate` correctly avoids the race with `capturePostRun`'s `Record` (comment documents it).
- `FinishRun` stamps `EndedAt` (store.go:644) — the earlier "no durations recorded" observation is fixed for new runs; historical runs.json rows have `endedAt == createdAt` and are not backfilled (correct: don't fabricate data).
- `evictRunsLocked` keeps running runs; eviction only trims finished ones.

## 5. What this pass implements

1. **G1:** review metrics (`ReviewedAt`, `HunksAccepted`, `HunksTotal`) stamped on apply/reject; surfaced in the runs list.
2. **G2:** "Needs review" filter + pending-review badge in the header.
3. **G4:** collapsible long tool output in the transcript.

G3/G5/G6 are documented as deferred with their data/scope blockers.
