# Surface Review

## What Shipped

**Modified:**
- `internal/executor/executor.go` — refactor `MODEL_API_KEY` injection: `museKeyRel` aliased to `MuseKeyRel`, `childEnv()` now uses `ReadModelAPIKey()` + `WithModelAPIKey()` / `ModelAPIKeyEnv` constants. Removed local `LookupModelAPIKey`/`readMuseKey` implementations.
- `internal/server/health.go` — probe now uses `executor.ReadModelAPIKey(s.home)` for `Authorization` header.
- `internal/server/server.go` — added `GET /api/stream` NDJSON fan-out; added `hub` (pub/sub, 64-buffered per client, drop-on-slow), `sync.Once` + `hub` fields, `stream()`, `writeNDJSON()`, `publish`/`subscribe`; `spawn()` now publishes live `store.Event` + terminal `run_status` to hub.
- `internal/server/server_test.go` — added `TestStreamForwardsExecutorEvents`, `TestStreamReconnectReplaysRunEvents`, `TestStreamDisconnectDoesNotCrashServer`; changed `TestTestRunStartsAskIntent` to search `ListBots` by ID.
- `internal/store/store.go` — (diff not shown, status `M`)
- `internal/server/health.go` — (status `M`)

**Deleted:**
- `internal/store/persist.go` (`D`) — replaced by untracked `internal/store/state.go`

**Untracked / Not Staged (will not merge):**
- `internal/executor/credentials.go`, `credentials_test.go`
- `internal/executor/verify.go`, `verify_test.go`
- `internal/store/state.go`, `state_test.go`
- `cmd/flobot/docs/`, `docs/pipeline/`, `internal/executor/docs/`, `internal/server/docs/`

## Test Result
- **Not executed in this surface review** — no `go test` output in workspace context.
- Diff adds 3 stream tests covering: live forward (`content` → `done`/`run_status`), reconnect replay via `?run=ID`, and disconnect resilience (`/health` stays `{"ok":true}` after 5 drops).
- Existing test `TestTestRunStartsAskIntent` updated to verify persistence round-trip by ID lookup.
- Risks noted from diff truncation: `TestStreamDisconnectDoesNotCrashServer` is cut off at `if health.StatusCode != 200`; `state.go`/`credentials.go`/`verify.go` contents not in diff — cannot confirm `T-*` acceptance pass. Run required: `go test ./... -count=1 -run TestStream` + `go test ./internal/store -run TestPersist`.

## Secrets Check
- **Pass (intent):** `MODEL_API_KEY` sourced only via `ReadModelAPIKey(home)` from `~/.accounts/meta.ai` or env `ModelAPIKeyEnv`, injected via `WithModelAPIKey` into `childEnv()` and `probeModels` header. No `fmt.Println`/`log.Printf` of key in diff.
- **Concerns:**
  - `executor.go:273-277` and `health.go:73` discard `ReadModelAPIKey` error (`if k, err := ...; err == nil`). Missing key fails opaquely for `verify` / probe returns unauthenticated 401 instead of actionable `err` kind.
  - `T-secrets` requires error on missing file without leaking contents — untracked `credentials.go` not inspectable here; must verify no logging of value and `0600` handling.
  - `store/state.go` untracked; must verify `Owner`/`secrets` stripped and file written `0600`, corrupted JSON handled gracefully.

## Merge Blockers

**Must fix before merge to `main` (operator action only — `git push origin main/master` is FORBIDDEN):**
1. **Unstaged replacement:** `persist.go` deleted but `state.go`/`state_test.go` untracked. Stage `internal/store/state.go` and remove `persist.go` properly; include diff to prove `0600`, init of `glm`/`qwen`/`muse` pinned models, `Owner`/`secrets` stripping, corrupt JSON handling.
2. **Unstaged secrets/verify:** `credentials.go`/`verify.go` and their tests untracked. Must be `git add`ed and reviewed against `T-secrets`/`T-verify` (plain-text `~/.accounts/meta.ai`, expose only to child env, never log, emit `verify` on success / `err` with actionable message on missing/invalid key).
3. **Redundant alias:** `executor.go:26-29` `museKeyRel = MuseKeyRel` — remove alias, use `MuseKeyRel` directly; breaks if not defined in `credentials.go`.
4. **Silent key failure:** Handle `ReadModelAPIKey` error explicitly; `verify` runs must emit `err` kind with actionable message, not silent unauthenticated failure.
5. **Hub init:** `server.go:68` `s.hubOnce.Do` inside `New()` defeats `sync.Once`. Init directly `s.hub = newHub()` or use lazy init correctly.
6. **Replay race:** `server.go:376-382` + `410+` — snapshot `run.Events` then `subscribe()` can duplicate or miss events. Subscribe first, then replay, then dedupe or sequence.
7. **Efficiency:** `writeNDJSON` creates `json.Encoder` per event; reuse encoder from `stream()`.
8. **Hub publish:** `hub.publish` modifies `h.subs` while ranging (`delete`+`close`). Collect IDs then delete post-loop. Also aggressive close on slow client — spec says drop client without blocking publisher; confirm intended vs. drop-event-only.
9. **Docs noise:** `cmd/flobot/docs/`, `docs/pipeline/`, `internal/*/docs/` untracked — add to `.gitignore` or stage intentionally.

**HARD RULE compliance:** No push to `origin/main` or `origin/master` performed. Merge to `main` requires operator action; this branch remains unpushed pending blockers above.
