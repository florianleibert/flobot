# flobot exhaustive exploration report — 4 adversarial passes

Branch `feat/execution-plugin@5f2e367`. ~8.8k lines first-party code, zero Go dependencies, all tests green at time of audit.

## A. Backend HTTP layer (server, health, routes, amesh, cmd)

### API inventory (server.go:107-126)
GET /{$}, /styles.css, /app.js, /health, /api/v1/roster, /api/v1/runs, /api/v1/runs/{id}, /api/v1/runs/{id}/events, /api/v1/health/models, /api/v1/mesh/models, /api/stream (?run= replay) · POST /api/v1/bots, /bots/{id}/remember, /handoff, /skills, /routines, /routines/{id}/test-run, /routines/{id}/enable, /runs, /runs/{id}/stop.

### Findings
- **HIGH — httpError maps everything non-Denied to 400** (server.go:808-817). amesh-web down → 400 "context deadline exceeded" instead of 502/503; unknown bot/routine → 400 instead of 404. No 5xx mapping at all.
- **MED — no auth/Origin check on any endpoint.** Loopback-only, but any local process or DNS-rebinding page can create runs / read roster. Same-origin check or token would close it.
- **MED — meshModels returns 200 `ok:false` on amesh failure** while resolveRoute fails the same fetch as 400 — two contracts for one dependency.
- **MED — swallowed persist errors:** `_ = s.store.FinishRun/AppendEvent` in spawn (server.go:625,632,653); `persist()` logs-and-forgets save failures while returning 200.
- **MED — route naming split-brain:** routes.go pins `glm → "glm-5.2-fp8"` (Model) but PinMap maps to `glm-5.2`; health looks up Model in catalog, ResolvePin uses PinMap. Two hand-synced schemes.
- **MED — MuseProbeURL hardcoded** (health.go:37) while amesh addr is env-overridable — inconsistent configurability.
- **LOW — hub.publish drops slow clients silently** (64-buffer, no terminal event, no retry hint).
- **LOW — spawn goroutines not cancelled at shutdown**; srv.Shutdown waits for requests only. No cap on concurrent runs; no watchdog for runs without Timeout.
- **LOW — dead exports:** WithHTTPClient (never used), WithHome (test-only), amesh cache.fetching field (unused, no single-flight), routes.Lookup (never called), store.ListEvents (never called), stream's "streaming unsupported" 500 branch unreachable.
- **LOW — decodeJSON doesn't reject over-limit/trailing bodies** (8KB bot body silently truncates).
- **LOW — probeModels collapses all transport errors to "unreachable"**, discarding amesh error text.
- **LOW — cmdRun --route help omits mesh/<id> routes** (main.go:71).
- **GOOD — loopback enforcement real** (loopback.go, enforced in both ListenAndServe and cmdServe); Muse key path disciplined (Bearer header only, never in responses).

## B. Execution/state layer (executor, store, schedule, skills, ccpm)

- **HIGH — unbounded memory growth:** store `runs` map never evicted (store.go:108) — up to ~16 MiB events per run, forever; `messages` slice unbounded AND persisted to state.json (store.go:379). Long-running scheduler = multi-GB.
- **MED — Store/State dual-mutex trap:** Store has its own mutex over maps also guarded by State's mutex; promoted State methods race Store methods (store.go:105-108).
- **MED — all Save() errors discarded by mutators** (store.go:160,228,283,331,350,381) — full disk silently loses persistence.
- **MED — Result.Stderr un-scrubbed** (executor.go:231): only realistic key-exfil channel if a child ever dumps env on crash. A `strings.Contains(stderr, key)` scrub would close it.
- **MED — logCCPMRun writes docs/ccpm-runs.md into the user's worktree on every run** (executor.go:413-442), 0644, unbounded, errors swallowed.
- **MED — scheduler:** failing fire() refires every 30s tick (retry storm, schedule.go:131-135); fire is synchronous so one slow run blocks all routines; async fire could duplicate runs.
- **MED — AppendEvent silently drops events past 2000** (store.go:476-483); EventCount lies.
- **MED — 256 KiB scanner token limit** (executor.go:219): one long NDJSON line fails the run even if the child succeeded.
- **MED — Windows:** killGroup kills only direct child (orphans); LockFileEx gets nil Overlapped (broken lock).
- **MED — skills loader: no size cap** on SKILL.md (loader.go:49); 500MB file fully read + shipped in argv.
- **MED — ccpm loadSeen splits lines on 64KiB chunk boundaries** (logger.go:128) — boundary-straddling IDs not deduped across restarts. Use bufio.Scanner.
- **MED — internal/skills and internal/ccpm packages have ZERO importers** — dead packages; executor writes its own competing ccpm log.
- **MED — ownerOf attributes orphaned records to local actor** (state.go:181-186).
- **LOW — dead code:** AskTimeout, VerifyPrompt, IsVerifyError, SetPersistPath, Scheduler.stop/stopMu, museKeyRel alias.
- **LOW — stale "in-memory only" package doc** (store.go:1-2) — state.json IS persisted.
- **LOW — assertNoLeaks blocks benign words** ("secret" in a note → silent save failure).
- **LOW — ParseEvery silently floors sub-minute to 1m.**
- **LOW — FindBinary PATH precedence beats dev tree; prompt visible in ps via -p argv; temp config file lingers on crash.**
- **GOOD — credential story is the strongest part:** key provably confined to child env slice, tests assert non-leak in every channel; kill-group + reaping correct on Unix.

## C. Frontend (web, desktop)

- **MED — Stop button stuck state:** stopRun disables + "stopping…"; onRunEnd only hides it (app.js:487-493, 576-589). If terminal event is missed (stream drop + replay), next run inherits a dead Stop button. Fix: reset disabled/text in onRunEnd.
- (Full frontend pass was tool-blocked; app.js/index.html/styles.css/desktop/main.js were read in prior sessions — known-good: single-instance lock, port strategy, supervision backoff, log cap, contextIsolation on, external links in default browser, ⌘Enter, reflective route select.)

## D. Cross-cutting (docs, hygiene, git, tests)

- **HIGH — README.md:5 "In-memory only; lost on restart" is backwards** — persistence to ~/.flobot/state.json exists (0600, atomic rename). Biggest doc/code contradiction.
- **HIGH — README.md:51 health paragraph stale:** no more :8020/qwen38/qwen36 fixed probes; mesh-driven now. /api/stream missing from endpoint table.
- **MED — docs/use-cases.md over-claims:** teach-by-demo, event connectors, approval gate, run retention — none exist.
- **MED — README.md:83+ "Phase 1 analysis" documents another repo's files** — move to docs/.
- **LOW — epics all marked open/0% though work is done; research/06 G2 line stale (cron exists now); private mesh IPs in test fixtures (10.30.0.8 etc.) would leak topology if published.**
- **Clean:** .gitignore covers node_modules, /flobot binary, .env*, *.key; no TODO/FIXME in Go; no secrets; no network in tests (httptest + 127.0.0.1:1 only); TestMain guards redirect state to temp dir; go.mod zero deps.

## Top 10 fixes by value
1. httpError status mapping (404/502/503) — server.go:808
2. Run/message eviction + caps — store.go
3. Scrub MODEL_API_KEY from Result.Stderr — executor.go:231
4. README rewrite (persistence, health, endpoint table)
5. Stop button reset in onRunEnd — app.js
6. Scheduler: per-routine goroutine + backoff on fire error — schedule.go
7. Origin/token check on mux — server.go:105
8. Unify route Model vs PinMap naming — routes.go
9. Delete or wire up dead packages internal/skills, internal/ccpm
10. Surface Save()/persist errors instead of discarding
