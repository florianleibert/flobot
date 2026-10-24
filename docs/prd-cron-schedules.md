# PRD — Scheduled runs ("cron jobs") for teammates

- **Status:** Draft for decision — implementation-ready
- **Owner:** flobot product / Code Studio
- **Date:** 2026-02
- **Scope:** `flobot` Go server (`internal/server`, `internal/store`, `internal/schedule`, `internal/executor`, new `internal/cronrun`), embedded web UI, `flobot.app` (Electron)
- **Related:** `docs/prd-code-studio.md`, `docs/prd.md`, `SPEC.md`
- **Supersedes:** the earlier `internal/schedule` interval-routine design and the
  `cron <expr>` *trigger-string* variant of this document (see §2, and §5.7 for the
  migration path). This document describes the **target** feature and is not a description
  of shipped code.

---

## 1. Summary

A **schedule** is a persisted, cron-triggered run attached to a teammate (bot). At fire
time the server creates a normal `store.Run` with the schedule's prompt, intent, yolo
flag and — critically — the schedule's own recorded workspace folder, then spawns it
through the existing `Server.spawn` → `executor.Exec.Run` choke point. Nothing new is
invented for the execution half: a scheduled run is a run, and it appears in the run
history, the event stream, the diff-review queue and the checkpoints exactly like a run
the user pressed *Run* on.

Three decisions define the feature:

1. **The Go server owns the clock.** One goroutine inside `flobot serve` ticks once per
   second. The Electron renderer never schedules anything, because the window is usually
   closed and `flobot serve` outlives it as a child of the Electron main process.
2. **Cron is parsed in-house, stdlib only.** `go.mod` currently declares **zero**
   requires (module `github.com/florianleibert/flobot`, `go 1.25.0`). A 5-field cron
   matcher is ~200 lines and is fully testable; adding `robfig/cron` would be the first
   third-party dependency in the repo and is rejected (§5.8).
3. **Missed fires are skipped by default.** Schedules record `catch_up: "skip" | "once"`
   and default to `"skip"`, because a yolo-capable agent run against a repo at 04:00
   after a two-week lid-closed gap is a destructive surprise, not a feature (§5.5).

**Non-goals:** no distributed scheduler, no per-user tenancy, no editing of the OS
crontab, no wall-clock-accurate sub-minute triggers.

---

## 2. Problem

Today flobot can start work in exactly three ways, all of which require a human at the
keyboard:

| Path | Code | Trigger |
| --- | --- | --- |
| `POST /api/v1/runs` | `internal/server/server.go:852` (`createRun`) | user presses Run |
| `POST /api/v1/handoff` (`run:true`) | `server.go:502` | user hands off to a teammate |
| `internal/schedule` interval routines | `schedule.go:123` (`Scheduler.Start`) | `"every 5m"` text trigger |

The existing scheduler (`internal/schedule`) is a useful precedent — one goroutine, an
injectable `Now func() time.Time` (schedule.go:57), a re-fire guard, exponential backoff —
but it cannot express the things that actually matter to a user: *"every weekday at 08:45
review yesterday's CI failures in ~/dev/flobot"*. Concretely it is limited to:

- **Relative intervals only.** `ParseEvery` (schedule.go:16) accepts `"every 30s|5m|2h"`
  and floors sub-minute values to 1m. There is no notion of "at 09:00", no weekdays, no
  day-of-month, no timezone. (An earlier revision of this feature bolted cron on as a
  **trigger string** — `"cron 0 9 * * 1-5"` — which inherits every limitation below;
  §5.7 says why that shape is being retired.)
- **The ambient workspace only.** `fireRoutine` (server.go:278) passes
  `Dir: s.workDirNow()`. Since the workspace became switchable at runtime
  (`POST /api/v1/workspace/select`, guarded by `workMu`, `workDirNow()` at server.go:116),
  a routine silently follows whatever folder the user last opened in Code Studio. A
  nightly job aimed at `~/dev/flobot` will happily run against `~/dev/other-repo` after
  the user pokes around elsewhere. This is a correctness bug waiting to be user-visible.
- **No per-schedule prompt.** Routines reference a `skill_id`; there is no way to write a
  one-line prompt with no skill, which is the 90% case.
- **No code intent, no yolo.** `fireRoutine` hardcodes `intent:"ask"` and `yolo:false`
  (server.go:309-321). A scheduled job that cannot edit files is a notifier, not a
  teammate.
- **No user-visible next-fire time.** Due-ness lives in an in-memory `last`/`fails` map
  (schedule.go:57-58) that is rebuilt on restart, so nothing can answer "when does this
  run next?" — and a restart resets backoff state, which makes restart behaviour subtle.
- **Failure is silent and unbounded.** The scheduler backs off exponentially (capped at
  8× the tick, schedule.go:146) and never gives up; the only signal is a `routine_fire`
  SSE event that a closed window never renders.

What users ask for, verbatim from the operator loop: dependency updates that run before
they wake up, a nightly "triage the failing tests, open a draft fix, leave it pending
review" job per repo, and a weekly "summarize what changed in this repo" that lands as a
run they can read over coffee. All three need absolute time, a pinned folder, and a
failure path that stops rather than hammering.

---

## 3. Goals / Non-goals

### Goals

- **G1 — Absolute time.** Standard 5-field cron (`m h dom mon dow`) plus the conventional
  `@daily`/`@hourly`-style aliases, with an IANA timezone per schedule.
- **G2 — Fires unattended.** Correct behaviour while the window is closed, while the app
  is running with no renderer attached, and across display sleep. Explicit, testable
  policy for missed fires.
- **G3 — Folder is part of the schedule.** The run executes in the folder recorded on the
  schedule, never in `s.workDirNow()`. Switching the workspace in Code Studio must not
  move a schedule.
- **G4 — Ordinary runs.** A scheduled run is a `store.Run` with `schedule_id` set; it
  flows through `spawn`, the pre/post git snapshot, checkpoint capture, diff review,
  `/api/v1/runs/{id}/events` and the SSE hub unchanged.
- **G5 — Survive restarts.** Schedules live in `~/.flobot/schedules.json`, written with
  the same 0700-dir / 0600-file / tmp+rename discipline as `state.json`.
- **G6 — Cannot pile up.** A schedule never has two runs in flight; a run is skipped (and
  recorded) rather than queued when its own previous run is still going.
- **G7 — Gives up on repeated failure.** `max_consecutive_failures` auto-disables a
  schedule that keeps failing and notifies.
- **G8 — Dangerous by consent.** A yolo schedule requires an explicit, recorded
  confirmation made at create/edit time, is capped in count, and is visibly badged.

### Non-goals

- **N1 — No distributed or redundant scheduler.** One process, one goroutine, one tick.
  There is no leader election, no lease file, no cross-host coordination. Two `flobot
  serve` processes on one machine is already a bug today (they contend for
  `127.0.0.1:4120`); this PRD does not make that safe.
- **N2 — No per-user tenancy.** Everything is `ActorLocal`, same as `store.State`, which
  keys every record on a single local actor and strips `Owner` from the wire
  (`json:"-"`, carried in the separate `owners` map). Schedules inherit that model.
- **N3 — No crontab editing.** We never write `crontab`, `launchd` plists, or
  `/etc/cron.d`. Flobot is its own scheduler; the OS clock is not our scheduler.
- **N4 — No second-level or jitter scheduling.** Minimum granularity is one minute;
  two schedules at `0 9 * * *` fire in the same tick.
- **N5 — No new model routing.** A schedule fires on the **bot's** route, resolved by the
  existing `resolveRoute` (server.go:699). Schedules cannot set a model, a pin, or a
  route. See §6.1 — this is a standing operator rule, not a style preference.
- **N6 — No retry/backoff of a *failed run*.** Auto-disable is not auto-retry.

---

## 4. User stories

| # | As | I want | So that | Acceptance |
| --- | --- | --- | --- | --- |
| U1 | operator | create a schedule from the nav ("Schedules") with name, teammate, prompt, folder, cron and tz | the 08:45 weekday CI review runs itself | `POST /api/v1/schedules` returns the schedule with `next_run_at` set |
| U2 | operator | see the next 3 fire times while typing the cron expression | `30 8 * * 1-5` means what I think | live preview via `POST /api/v1/schedules/preview`, invalid cron → inline error, never a toast on every keystroke |
| U3 | operator | run a schedule immediately | I can test a schedule at 14:00 without waiting for 08:45 | `POST /api/v1/schedules/{id}/run-now` returns the created run; `409` while a previous run is in flight |
| U4 | operator | pause a schedule for a week | my laptop battery is not spent on nightly builds | `disable` keeps the record, clears nothing, sets `next_run_at:""` |
| U5 | operator | know when a schedule fails | I do not discover a broken nightly by reading a stale `last_run_at` | macOS notification + red badge + `last_status:"error"` + `failure_count` |
| U6 | operator | a schedule that keeps failing stops itself | it does not burn tokens every hour for a month | auto-disable at `max_consecutive_failures` (default 3) with `disabled_reason:"auto_disabled"` |
| U7 | operator | a schedule's run history | I can answer "what did the nightly actually do?" | `GET /api/v1/schedules/{id}/runs`, "View runs" in the UI filtered to that schedule |
| U8 | operator | close the window and keep schedules | the app is a background service, not a page | window closed + app running → fires on time (server-side tick) |
| U9 | operator | open the lid after a weekend asleep | I get today's 09:00 run, not four backlogged runs | `catch_up:"skip"` (default) drops missed fires and logs one `schedule_missed` event |
| U10 | operator | a yolo nightly that edits files | unattended fixes are possible | yolo requires `confirm:true` + non-empty `reason`; UI shows a red badge and a second confirmation |
| U11 | operator | delete a schedule | it never fires again, but its past runs remain | `DELETE` → 204; runs keep their `schedule_id` (dangling id renders as "deleted schedule") |
| U12 | operator | move a schedule to another folder | the repo moved, the schedule follows | editing `path` re-runs root validation and resets `next_run_at` |

---

## 5. Detailed design

### 5.1 Data model

New file `internal/store/schedules.go`, a `ScheduleStore` sibling to `store.RunStore`
(`internal/store/runs.go`) — **not** a new collection inside `stateFile`.

Reason, concretely: `store.State.Save()` calls `assertNoLeaks` (state.go:244), which
rejects any serialized state containing `"owner"`, `llm_`, `api_key` **or `secret`**, and
`store.State` carries `bots/skills/routines/messages/local_pin`. A schedule's prompt is
free text supplied by the user and can legitimately contain the word "secret" (e.g.
*"audit config for secret leakage"*), which would make every subsequent roster save fail
and silently stop persisting the roster. That is a cross-feature coupling we do not want,
so schedules get their own file, their own mutex and their own — narrower — leak check
(no `api_key`, no `llm_`; the literal word "secret" inside a prompt is allowed).

```go
// internal/schedule/cron.go
type Cron struct {
    Expr               string // canonical: original string, aliases expanded
    Min, Hour, Dom     bitset // 60 / 24 / 31-bit masks
    Mon, Dow           bitset // 12 / 7-bit masks
    DomStar, DowStar   bool   // for Vixie-cron OR semantics (§5.4)
    Loc                *time.Location
}
func Parse(expr, tz string) (Cron, error)             // errors are user-facing
func (c Cron) Next(after time.Time) (time.Time, bool) // false = none within 5 years
func (c Cron) NextN(after time.Time, n int) ([]time.Time, error)
```

```go
// internal/store/schedules.go
type Schedule struct {
    ID     string `json:"id"`      // "sched-" + 8 hex
    Name   string `json:"name"`    // required, <= 80 runes
    BotID  string `json:"bot_id"`  // must be an existing local bot
    Prompt string `json:"prompt"`  // required, <= 16 KiB after TrimSpace
    Path   string `json:"path"`    // absolute workspace folder, validated (§5.6)
    Intent string `json:"intent"`  // "ask" | "plan" | "code"; default "code"
    Yolo   bool   `json:"yolo"`
    Cron   string `json:"cron"`    // 5-field or alias, stored canonical
    TZ     string `json:"timezone"`// IANA, "" = server local zone
    Enabled bool  `json:"enabled"`
    CatchUp string `json:"catch_up"` // "skip" (default) | "once"

    CreatedAt  string `json:"created_at"`   // RFC3339
    NextRunAt  string `json:"next_run_at"`  // RFC3339, "" when disabled
    LastRunID  string `json:"last_run_id"`
    LastRunAt  string `json:"last_run_at"`
    LastStatus string `json:"last_status"`  // "" | running|done|error|stopped|interrupted|skipped_overlap|missed
    SkippedAt  string `json:"skipped_at,omitempty"`
    MissedAt   string `json:"missed_at,omitempty"`

    FailureCount           int    `json:"failure_count"`
    MaxConsecutiveFailures int    `json:"max_consecutive_failures"` // default 3, 1..20, 0 => default
    DisabledReason         string `json:"disabled_reason,omitempty"` // "" | "auto_disabled" | "user"

    // Consent (§5.10). Recorded, never inferred.
    YoloConfirmedAt string `json:"yolo_confirmed_at,omitempty"`
    YoloReason      string `json:"yolo_reason,omitempty"`

    Owner string `json:"-"` // ActorLocal, never on the wire (state.go convention)
}
```

Persisted file shape, `~/.flobot/schedules.json` (dir 0700, file 0600, write `.tmp` then
`os.Rename` — identical to `State.Save`, state.go:227-239):

```json
{
  "schema": "flobot.schedules.v1",
  "schedules": [
    {
      "id": "sched-3f9a1c02",
      "name": "weekday CI triage",
      "bot_id": "bot-qwen",
      "prompt": "Triage CI failures from the last 24h in this repo. Propose a fix as a pending diff; do not push.",
      "path": "/Users/me/dev/flobot",
      "intent": "code",
      "yolo": false,
      "cron": "45 8 * * 1-5",
      "timezone": "Europe/Berlin",
      "enabled": true,
      "catch_up": "skip",
      "created_at": "2026-02-11T09:12:33Z",
      "next_run_at": "2026-02-12T08:45:00+01:00",
      "last_run_id": "run-7c1d",
      "last_run_at": "2026-02-11T08:45:02+01:00",
      "last_status": "done",
      "failure_count": 0,
      "max_consecutive_failures": 3
    }
  ]
}
```

`ScheduleStore` API (mirrors `State`): `Load() error`, `Save() error`, `Path() string`
(`$HOME/.flobot/schedules.json`; under `FLOBOT_TEST_STATE` resolves inside the temp home
so tests never touch the real `~` — the exact mechanism `State.Path()` uses, state.go:175),
`SetPath(string)`, `List(actor)`, `Get(id, actor)`, `Create`, `Update`, `Delete`,
`SetEnabled(id, actor, enabled, reason)`, `RecordFire(id, runID, at)`,
`RecordOutcome(id, runID, status)`, `All()`. A missing file is not an error (empty set); a
corrupted file returns the error for logging and falls back to the empty set — **never** to
defaults, because the correct default for schedules is "fire nothing".

The server holds the store beside the others and starts the loop next to the existing
`s.StartScheduler(ctx)` call in `ListenAndServe` (server.go:339):

```go
s.cronSched = cronrun.New(s.schedules, s.fireSchedule)
s.cronSched.Start(ctx)
```

### 5.2 REST API

Registered in `Server.Handler()` (server.go:188) with Go 1.22 method+wildcard patterns,
behind the existing `sameOrigin` middleware. Conventions preserved: JSON bodies,
`decodeJSON(w, r, limit, &body)` (`smallBodyLimit` 8 KiB for control calls,
`runBodyLimit` 64 KiB where a prompt is present), `writeJSON`, errors via `s.httpError`
mapping `store.ErrNotFound`→404 / `store.ErrDenied`→403 and everything else → 500,
`{"error": "..."}` on failure, `204` on delete. Bodies stay `http.MaxBytesReader`-capped,
and 500s keep their text body so the UI can fall back to it (the `runNow` precedent).

| Method | Path | Handler | Notes |
| --- | --- | --- | --- |
| GET | `/api/v1/schedules` | `listSchedules` | all, including disabled |
| POST | `/api/v1/schedules` | `createSchedule` | 200 + schedule (existing convention: `createBot`/`createRun` return 200, not 201) |
| POST | `/api/v1/schedules/preview` | `previewSchedule` | validation + next-N fires; no persistence |
| GET | `/api/v1/schedules/{id}` | `getSchedule` | |
| PATCH | `/api/v1/schedules/{id}` | `updateSchedule` | pointer fields = sparse update |
| DELETE | `/api/v1/schedules/{id}` | `deleteSchedule` | 204 |
| POST | `/api/v1/schedules/{id}/enable` | `enableSchedule` | mirrors `POST /routines/{id}/enable` |
| POST | `/api/v1/schedules/{id}/disable` | `disableSchedule` | `{"reason":"user"}` |
| POST | `/api/v1/schedules/{id}/run-now` | `runNowSchedule` | 200 run / 409 overlap / 423 folder missing |
| GET | `/api/v1/schedules/{id}/runs` | `scheduleRuns` | runs with this `schedule_id`, newest first |

`GET /api/v1/schedules`

```json
{
  "schema": "flobot.schedules.v1",
  "actor": "local",
  "server_time": "2026-02-11T10:02:11Z",
  "timezone": "Europe/Berlin",
  "schedules": [ { "…Schedule…": "" } ]
}
```

`server_time` and server `timezone` are included so the UI can label "next fire in 4h 12m
(server-local)" honestly instead of assuming browser == server.

`POST /api/v1/schedules` — body (all required unless marked; `timezone` defaults to the
server zone, `catch_up` to `"skip"`, `intent` to `"code"`, `enabled` to `true`,
`max_consecutive_failures` to 3):

```json
{
  "name": "weekday CI triage",
  "bot_id": "bot-qwen",
  "prompt": "Triage CI failures from the last 24h in this repo.",
  "path": "/Users/me/dev/flobot",
  "intent": "code",
  "yolo": false,
  "cron": "45 8 * * 1-5",
  "timezone": "Europe/Berlin",
  "catch_up": "skip",
  "max_consecutive_failures": 3,
  "confirm": false
}
```

`200` → the full `Schedule`. Failures, all `{"error": "..."}`:

| Status | Condition |
| --- | --- |
| 400 | `{"error":"invalid cron expression: day-of-week must be 0-7"}` |
| 400 | `{"error":"unknown timezone \"Europe/Berln\""}` |
| 400 | `{"error":"unknown intent \"build\" (want ask, plan or code)"}` |
| 400 | `{"error":"prompt required"}` / `name required` |
| 400 | `{"error":"yolo schedules require confirm:true and a reason"}` |
| 400 | `{"error":"schedule limit reached (12); delete one first"}` |
| 404 | `{"error":"bot not found"}` |
| 403 | `{"error":"path is outside the allowed workspace roots"}` (not under a `GET /api/v1/workspace/roots` recent) |
| 423 | `{"error":"folder does not exist: ~/dev/gone"}` (path must exist and be a directory at create/edit time) |

`PATCH /api/v1/schedules/{id}` uses `*string`/`*bool`/`*int` so absent means unchanged;
explicit `null` is rejected. Changing `cron`, `timezone`, `path` or re-enabling recomputes
`next_run_at` from *now* (never from the old `next_run_at`) and clears `disabled_reason`.
Setting `yolo: true` requires `confirm:true` **again** — the confirmation is per-edit, not
per-record — and overwrites `yolo_confirmed_at`/`yolo_reason`.

`POST /api/v1/schedules/preview` body `{"cron": "...", "timezone": "...", "n": 3}` →

```json
{
  "schema": "flobot.schedule_preview.v1",
  "ok": true,
  "normalized": "45 8 * * 1-5",
  "timezone": "Europe/Berlin",
  "next": [
    "2026-02-12T08:45:00+01:00",
    "2026-02-13T08:45:00+01:00",
    "2026-02-16T08:45:00+01:00"
  ]
}
```

`ok:false` with `"error"` and `"field"` for a client-side inline message — the same
`ok:false`-with-HTTP-200 shape the UI already relies on for `GET /api/v1/mesh/models` when
amesh is unreachable, so a half-typed expression never throws in `api()` and blanks the
panel.

`POST /api/v1/schedules/{id}/run-now` → `200` `{"run": {…Run…}, "schedule": {…}}`.
`run-now` **does** bypass `enabled` (that is the point) but **not** the overlap guard or
the folder check; a successful run resets `failure_count`, so a manual fix-and-verify run
can rescue an auto-disabled schedule once the user re-enables it.

`store.Run` gains one field so history works without a second index:

```go
ScheduleID string `json:"schedule_id,omitempty"` // internal/store/store.go:91 Run
```

and `CreateRun` grows a trailing `scheduleID string` parameter (all call sites —
`createRun`, `handoff`, `testRun`, `fireRoutine`, and the new firer — updated in the same
change).

### 5.3 Scheduler loop

New package `internal/cronrun` — deliberately separate from `internal/schedule`, which
stays as-is for legacy `every` routines until P4 (§5.7).

**Tick: 1 second, `time.NewTicker`, monotonic.** `time.Now()` is used only to *compare*
instants; every elapsed decision rides Go's monotonic clock (`t.After`/`t.Sub` prefer the
monotonic reading when both `t` values come from the same process), so an NTP step or a
manual date change cannot cause a double fire. We never subtract wall-clock strings.

Why 1s and not 60s: cron granularity is a minute, so a minute tick is "correct", but with
a 60s ticker the tick phase is arbitrary relative to the minute boundary and any tick
delay past the boundary pushes a `* * * * *` job into the next minute — a ~1% miss rate
that compounds with sleep/wake. A 1s tick costs one map scan per second and gives a
one-second late-fire bound. We additionally align the first tick to the next whole second
so the loop is phase-stable.

The loop holds no per-schedule mutable state; `next_run_at` **is** the state, and it is
persisted. That is what makes restart semantics trivial (§5.5) and removes the in-memory
`last`/`fails` maps that make `internal/schedule` restart-sensitive.

```go
func (c *Cron) Start(ctx context.Context) {
    go func() {
        alignToSecond()                 // sleep to the next whole second
        t := time.NewTicker(time.Second)
        defer t.Stop()
        for {
            select {
            case <-ctx.Done():
                c.wg.Wait()             // in-flight fires finish
            case now := <-t.C:
                c.tick(now)
            }
        }
    }()
}

// tick fires at most once per (schedule, minute) and never holds a
// lock across a spawn.
func (c *Cron) tick(now time.Time) {
    for _, s := range c.store.List(store.ActorLocal) {
        if !s.Enabled || s.NextRunAt == "" {
            continue
        }
        due, err := time.Parse(time.RFC3339, s.NextRunAt)
        if err != nil {
            c.recompute(s, now)         // corrupt next_run_at: self-heal
            continue
        }
        if now.Before(due) {
            continue                    // not yet
        }
        if now.Sub(due) > catchUpWindow { // 60 * time.Minute
            c.handleMissed(s, due, now) // §5.5: skip (default) or fire once
            continue
        }
        if s.LastStatus == "running" || c.firing[s.ID] || c.store.HasRunningRun(s.BotID) {
            c.recordSkipped(s, now)     // §5.6
            c.advance(s, due)
            continue
        }
        go c.fire(s, due)               // a slow fire must never block the tick
    }
}
```

`fire` does, in order, all under the schedule's own single-flight guard:

1. `bot, err := store.GetBot(s.BotID, Actor)` — a deleted bot is **not** a transient
   failure: auto-disable with `disabled_reason:"auto_disabled"`, `last_status:"error"`,
   notify "schedule X has no teammate qwen". `fireRoutine` currently returns an error here
   and therefore backs off and retries a deleted teammate forever; we downgrade it.
2. `pin, cfgOverride, err := s.resolveRoute(ctx, bot.Route)` with the same
   `context.WithTimeout(context.Background(), amesh.FetchBudget)` bound `fireRoutine` uses
   (server.go:302), so a hung amesh-web cannot pin a goroutine. The route comes from the
   **bot**, and nothing else, per N5 and §6.1.
3. `path` validation (`os.Stat`, is-directory, `filepath.EvalSymlinks` still matching the
   recorded value). Failure → `last_status:"error"`, no run, `failure_count++`.
4. `run := store.CreateRun(Actor, bot.ID, prompt, intent, yolo, verify, worktree, pin,
   bot.Route, s.ID)` — with the bot `Memory` note prepended exactly as `createRun` does
   (server.go:881).
5. `s.spawn(run, executor.Request{… Dir: s.Path …})` — **`Dir: s.Path`, the schedule's
   folder**, and never `s.workDirNow()`. This is G3 and the single most important line in
   the feature.
6. `store.RecordFire(s.ID, run.ID, now)` → sets `last_run_id`/`last_run_at`,
   `last_status:"running"`, and advances `next_run_at` to the next fire **after `due`**
   (not after `now`), so a late 14:00 fire of a daily `0 9 * * *` schedules tomorrow's
   09:00 rather than skipping a day.
7. `hub.publish(store.Event{Kind:"schedule_fire", Text: s.ID})`.
8. On terminal run status the existing completion path calls
   `cronSched.ObserveRunEnd(runID, status)`, which maps `done→done`, `error→error`,
   `stopped→stopped`. Only `error` increments `failure_count`; success zeroes it. At
   `failure_count >= max_consecutive_failures` → `enabled=false`,
   `disabled_reason:"auto_disabled"`, persist, notify (§6.3).

`spawn` itself is left untouched: the schedule path inherits the `revMu` serialization
against in-flight diff review (server.go:936), the pre/post git snapshot goroutine, the
checkpoint capture, and the `FinishRun` status transitions, for free.

### 5.4 Cron grammar and next-fire computation

The parser lives in `internal/schedule/cron.go`, exported through the existing package so
`internal/cronrun` and the handlers share one implementation.

**Grammar (accepted):**
- Exactly 5 whitespace-separated fields: `minute hour day-of-month month day-of-week`.
- Each field: `*`, a number, a comma list, a range `a-b`, a step `*/n` or `a-b/n`. Spaces
  inside a field are rejected (they would break field splitting).
- Ranges: minute 0-59, hour 0-23, dom 1-31, month 1-12 (names `jan..dec` accepted), dow
  0-7 (7 == 0 == Sunday; names `sun..sat` accepted). A range whose start > end is an error
  — we do **not** wrap, unlike some crons.
- Aliases: `@yearly`/`@annually` = `0 0 1 1 *`, `@monthly` = `0 0 1 * *`, `@weekly` =
  `0 0 * * 0`, `@daily`/`@midnight` = `0 0 * * *`, `@hourly` = `0 * * * *`. `@reboot` is
  **rejected** with a message pointing at `catch_up`: "fire when the app starts" *is* the
  `once` policy, stated honestly.
- `@every 5m` is **rejected**. That is the legacy `internal/schedule` trigger syntax;
  accepting it in two files with two engines is how duplicate fires happen. Migration is
  explicit (§5.7).
- Canonical form on store: aliases expanded to 5 fields, single spaces, so
  `GET /schedules` round-trips and `PATCH` diffing is meaningful.

**Next-fire algorithm.** `Next(after)` walks candidate minutes forward from
`after.Truncate(minute).Add(minute)` **in the schedule's location**, rejecting on month,
then day, then hour, then minute; on a month/day/hour rejection it jumps the lower fields
forward instead of stepping one minute at a time, so `0 0 29 2 *` does not cost four years
of minute-steps. Hard cap: 5 years of candidate days → `ok:false`, and `Create` rejects a
schedule whose first fire is more than 5 years out as unreachable ("cron never fires:
feb 30").

**DOM/DOW semantics — Vixie/cronie OR.** When **both** day-of-month and day-of-week are
restricted (neither is `*`), a day matches if **either** matches, matching
`/usr/bin/crontab` on macOS and Linux, so `0 9 13 * 5` means "the 13th, and every Friday".
When one is `*`, plain AND. `DomStar`/`DowStar` are recorded at parse time to make the rule
explicit rather than accidentally-AND. This choice is called out because it surprises
people — and the preview (U2) is the mitigation: the user always sees three concrete dates.

**DST — decided, not deferred.** Fire times are wall-clock times in the schedule's zone
(`time.LoadLocation`), which is what a human means by "08:45 every weekday":

- **Spring-forward gap** (local 02:30 does not exist on the transition day, e.g.
  Europe/Berlin 02:00→03:00): the fire is **skipped once**, `next_run_at` advances to the
  following match, and one `schedule_missed` SSE event with `reason:"dst_gap"` is
  published. We do **not** fire at the post-transition equivalent instant, because a job
  pinned to "02:30 while nobody is awake" is exactly the thing that must not silently move
  an hour earlier into a working hour.
- **Fall-back overlap** (02:30 occurs twice): fire on the **earliest UTC** occurrence only.
  Dedup is automatic — `next_run_at` advances to the next match, and comparisons are on
  instants, never on wall-clock strings.
- A zone with no DST (`UTC`, `Asia/Tokyo`) has neither case. The UI shows every preview
  timestamp with an explicit offset (`2026-03-29T08:45:00+01:00`) so a user who changes
  zones sees the shift immediately rather than inferring it.

### 5.5 Persistence, restart and missed-fire policy

**Restart.** On `New()` the server loads `schedules.json`; on start the loop walks every
enabled schedule:

```
now := time.Now()
due, err := time.Parse(time.RFC3339, s.NextRunAt)
switch {
case err != nil || s.NextRunAt == "":        s.NextRunAt = cron.Next(now)  // self-heal
case now.Sub(due) > catchUpWindow:           policy(s, due, now)           // see below
default:                                     /* leave alone: it fires */ }
```

`catchUpWindow = 60 * time.Minute` (one hour). A schedule whose stored `next_run_at` is
inside the window fires normally on the next tick, which covers "the server restarted in 40
seconds" without special-casing.

**`catch_up` policy — the decision.**

| value | behaviour on a missed fire | default |
| --- | --- | --- |
| `"skip"` | drop every missed fire, set `missed_at`, publish one `schedule_missed` event carrying `count`, advance `next_run_at` to the first match after `now` | **yes** |
| `"once"` | fire exactly one run covering the whole gap (however many fires were missed), then continue normally | no |

**Why `skip` is the default, concretely.** A laptop closes Friday 18:00 and opens Monday
10:00; a `0 9 * * *` schedule missed two fires. Under `once` that is a **yolo-capable agent
editing a repo that has had two days of other people's pushes**, in a tree the user has not
looked at, with a pre-run git snapshot taken *after* those pushes — so diff review shows
the agent's edits while the agent's reasoning was written against a stale tree. Under
`skip` the user gets a badge ("2 fires skipped") and presses *Run now* with their eyes
open. The asymmetry is decisive: a skipped fire costs one missed morning; a wrong
unattended fire costs a corrupted working tree discovered Monday at 10:04. Schedules are
for **recurring** work, and recurring work that missed its slot is stale by definition — a
backfill of N runs is not "catching up", it is N agents rewriting the same folder, which
the overlap guard would reject anyway (skip → skip → skip). `once` exists because some jobs
genuinely are "recompute the report" and idempotent; we make the user opt in per schedule.

We deliberately do **not** attempt a persistent wakeup (`IOPMAssertionCreateWithName` via a
helper, or a `launchd` `StartCalendarInterval` agent): both are effectively N3, and a timer
that wakes a sleeping Mac to run a yolo agent is a battery and thermals story the app should
not own. Flobot fires when it is awake; missed fires are declared, not hidden.

### 5.6 Concurrency guard — what the code actually enforces today

Read carefully, because "one run per workspace" is **not** what exists:

| existing primitive | source | real semantics |
| --- | --- | --- |
| `Store.HasRunningRun(botID)` | `internal/store/store.go:717` | **per-bot**: any run with `BotID == botID && Status == "running"`. Used by `fireRoutine` (server.go:293) to skip a tick. Not workspace-scoped — two different bots in the same folder run concurrently today. |
| `Store.AnyRunningRun()` | `store.go:731` | global "anything running". Used **only** in the `ListenAndServe` graceful-shutdown drain (server.go:368) and by diff review to fail closed — **not** to gate run creation. |
| `Server.revMu` | `server.go:82`, taken in `spawn` (server.go:936) | a **lock**, not a constraint: `spawn` acquires and releases it to wait out an in-flight `applyDiff`/`rejectDiff`, so a run never writes the tree mid-apply. |
| `Server.workMu` | `server.go:53` | guards `workDir`/`ws` against a concurrent workspace switch; does not limit runs. |

So today the server has **no** "one run per workspace" constraint at all, and this PRD does
not add one — a global per-folder mutex would let one 40-minute nightly block the user's
interactive *Run* button in the same repo, which is unacceptable UX. What we add, in the
firer only:

1. **Per-schedule single flight.** `firing map[string]bool` owned by the cronrun goroutine
   (set before `spawn`, cleared on `ObserveRunEnd`) plus the persisted
   `last_status == "running"` check, so the guard also holds across a restart that lost the
   map. Reconciliation at load: a schedule whose `last_status` is `running` but which has
   no live entry in `Server.cancels` becomes `last_status:"interrupted"` and the orphaned
   run is finished as `error` with `err:"interrupted by restart"` — otherwise the guard
   blocks that schedule forever.
2. **Per-bot, reusing the existing primitive.** `if s.store.HasRunningRun(bot.ID) { skip }`
   — identical to `fireRoutine`'s guard. Rationale: a bot with two concurrent runs doubles
   its context and cost, and this is already the behaviour users see.
3. **Diff-review serialization.** free, via `spawn`'s `revMu` wait (server.go:936).
4. **Skips are recorded, never silent**: `last_status:"skipped_overlap"`, `skipped_at`, and
   `hub.publish{Kind:"schedule_skipped", Text: id}`. A skip does **not** increment
   `failure_count` — a long legitimate run is not a failure — and it **does** advance
   `next_run_at`, so a schedule whose run routinely outruns its period degrades to "fires
   whenever free" instead of accumulating a skip backlog.

Advisory UI rule: if a schedule's cron period is shorter than the median duration of its own
last 5 runs (`created_at`→`ended_at`), the row shows "⚠ runs longer than its interval —
expect skips".

### 5.7 Relationship to the existing `every` routines (and to the `cron <expr>` trigger variant)

Two shapes exist or have been proposed for the same job:

| shape | where the expression lives | problem |
| --- | --- | --- |
| `Routine{Trigger:"every 5m"}` | `state.json`, in-memory due-ness | intervals only; ambient folder; skill-only; `intent:"ask"` |
| `Routine{Trigger:"cron 0 9 * * 1-5"}` | one string in `state.json` | the folder is still `workDirNow()`; a timezone cannot be expressed in the string; the next fire is not persisted, so a restart recomputes due-ness from `last`; no prompt/intent/yolo; validation is a parse-error at enable time with no preview |

Decision: **schedules are a first-class collection, not a trigger string.** A cron
expression alone cannot carry `path`, `timezone`, `catch_up`, `yolo` consent or
`next_run_at`, and overloading a free-text `trigger` field to hold five concepts is why
the variant above has no persisted next-fire time to show.

`internal/schedule` + `Routine{BotID,SkillID,Trigger,Paused,LastRun}` keeps working through
P1 — no user-visible change, no data loss. Migration in P3:

- On load, any `Routine` with a parseable trigger and no `migrated_to` is offered for
  one-click conversion: `every 30m` → `*/30 * * * *` (server-local zone),
  `cron 0 9 * * 1-5` → `0 9 * * 1-5`, `skill.Body` → `prompt`, `Paused` →
  `enabled:false`, `LastRun` → `last_run_at`.
- Conversion is **not** automatic: the `path` field is new and must be chosen by the user,
  which is the whole point of G3.
- After one release with the migration banner reading zero, P4 deletes `internal/schedule`,
  `POST /api/v1/routines/{id}/enable|pause`, and the `routines` collection from
  `state.json`.

### 5.8 Cron library decision

`go.mod` is 3 lines and declares **no** `require` block — flobot is currently a
zero-dependency module (`github.com/florianleibert/flobot`, `go 1.25.0`; `robfig/cron`
appears nowhere). The repo's preference is few deps, and the maintenance surface here is
genuinely small:

- 5 fields, `*`/number/list/range/step, month+dow names, 6 aliases ≈ 200 LoC, plus ~150 LoC
  of table-driven tests we want regardless to pin the DST and DOM/DOW-OR decisions in §5.4
  — a library would hand us *its* DST semantics, which is exactly what we are specifying
  away from.
- No upstream in the path that decides when a yolo agent runs; the existing pre-push gate
  (`go vet`, lints, govulncheck, tests) covers it with zero new supply chain.

**Decision: write `internal/schedule/cron.go` in-house; add no dependency.** Revisit only if
we need seconds (6/7-field), `L`/`W`/`#` day modifiers, or RFC 5545 recurrences — at which
point `github.com/robfig/cron/v3` in parser mode is the drop-in, and the
`Parse/Next/NextN` surface in §5.1 is deliberately shaped so that swap changes nothing but
the file body.

### 5.9 UI

**Placement.** `flobot-ui-redesign` ships three panels (dashboard/roster, run, code) plus a
runs rail. Add a fourth nav item **"Schedules"** between *Runs* and *Code*, using the same
`data-panel` mechanism as the existing nav. The panel is a list plus an inline editor
drawer, mirroring the Roster panel's list→detail interaction rather than introducing a
modal system.

```
┌ Schedules ────────────────────────────────────────── [+ New schedule] ┐
│ ⚠ 1 schedule was auto-disabled after 3 failures        [review]      │
│──────────────────────────────────────────────────────────────────────│
│ ▸ ● weekday CI triage            qwen  45 8 * * 1-5   Europe/Berlin  │
│     ~/dev/flobot   next Wed 08:45 (in 22h 03m)                       │
│     last: done, 12m ago · run-7c1d →            [Run now] [⋯]        │
│ ▸ ○ nightly dep audit            muse   0 3 * * *     UTC            │
│     ~/dev/other   disabled (auto_disabled)                           │
│     last: error, 2d ago · run-91ba →           [Run now] [⋯]        │
│ ▸ ● weekly summary  ⚠YOLO        glm    0 9 * * 1    Europe/Berlin  │
└──────────────────────────────────────────────────────────────────────┘
```

Row contents: enabled dot, name, teammate (from the roster), cron expression in a monospace
chip with its human rendering ("Every weekday at 08:45"), timezone, resolved folder path
with a `↗` that focuses that folder in the Code panel, `next fire` in relative + absolute
form, last outcome + relative time + a link to the run, and a per-row `[Run now]` / `[⋯]`
menu (Edit, Enable, Disable, Duplicate, View runs, Delete). `⚠YOLO` is a permanent badge on
any yolo schedule; `⚠ runs longer than its interval` per §5.6.

**Editor drawer fields**, in this order (each maps 1:1 to the POST/PATCH body): `name` ·
`bot_id` (roster dropdown, showing the bot's route read-only) · `prompt` (textarea, the same
component as the Run panel's prompt box) · **`path`** (folder picker reusing
`GET /api/v1/workspace/roots` recents plus the existing select flow; shows the resolved
absolute path and the note *"the Code panel is currently on ~/elsewhere — this schedule
stays on ~/dev/flobot"*, which is the G3 affordance) · `intent` (ask/plan/code segmented
control) · `cron` + `timezone` · `catch_up` (skip/once, one line of explanation under each)
· `max_consecutive_failures` (number, default 3) · `enabled` toggle · `yolo` toggle
(§5.10).

**Next-3-fires preview.** Debounced (300 ms) `POST /api/v1/schedules/preview` on every
`cron`/`timezone` change, rendered as three rows with explicit offsets (`Wed 2026-02-12
08:45 +01:00`), with a red inline error naming the offending field on `ok:false`. Shown
inside the drawer always — including for `@daily` — because DOM/DOW-OR and DST gaps are
exactly where a human's mental model breaks.

**Run-now.** Switches to the Run panel streaming the returned `run_id`, so a manual fire
looks and behaves like any other run. Disabled while that schedule's
`last_status === "running"`; a `409` renders as a toast ("previous run is still going"),
not an error state.

**Run history from a schedule.** `[⋯] → View runs` and the `run-7c1d →` link both use
`GET /api/v1/schedules/{id}/runs`; the runs rail gains a `schedule_id` filter chip so
scheduled and manual runs sit side by side.

**Empty state.** No schedules: a plain three-line card — "Nothing runs on a schedule yet." /
"Try: *every weekday at 08:45, triage CI failures in this repo*." /
`[Create your first schedule]`, which opens the drawer **pre-filled with the current Code
panel folder** (the common case is "schedule work on what I have open"). No schedules but at
least one legacy routine: the §5.7 migration banner.

**Stream.** `web/app.js`'s existing SSE handler (`GET /api/stream`) gains `schedule_fire`,
`schedule_skipped`, `schedule_missed`, `schedule_failed`
(`hub.publish(store.Event{Kind: …, Text: scheduleID})`), which refresh the list without
polling. Live countdowns are computed against the `server_time` from the list response so
they never drift against a wrong browser clock.

### 5.10 Security — yolo on a folder, unattended

A schedule with `yolo: true` is an agent with **no-confirmation tool access** (see
`executor.Argv`, `req.Yolo` → the child's yolo flag, executor.go:115) running unattended, on
a folder, with nobody to reject a bad diff. Required affordances:

1. **Explicit recorded consent, per edit.** The API requires `confirm: true` **and** a
   non-empty `reason` whenever `yolo` transitions `false → true`, on create and on PATCH:
   `{"error":"yolo schedules require confirm:true and a reason"}`. The UI shows a blocking
   confirmation naming the exact folder and the full prompt body, with a 3-second disabled
   window on the confirm button (not a checkbox that can be clicked reflexively), and stores
   `yolo_confirmed_at` + `yolo_reason`. The row keeps a permanent `⚠YOLO` badge and the
   drawer shows the reason the user typed.
2. **A cap.** At most **12** schedules total, at most **4** of them enabled with `yolo`, and
   at most **1** yolo schedule per folder — two unattended writers on one tree is where data
   loss actually lives. All three are constants in `internal/store/schedules.go` (like
   `maxRuns`), not config knobs.
3. **Safety rails that are already true and stay true:** `intent:"ask"` ignores `yolo`
   (unchanged); `spawn` still serializes against in-flight diff review via `revMu`; the
   pre-run git snapshot still runs, so every scheduled mutation is revertible through the
   existing checkpoint/diff-review path, and a failed snapshot keeps its fail-closed
   behaviour. Scheduled runs never receive a `--resume`/continue path (§6.2).
4. **No secrets in the record.** `schedules.json` is 0600 in a 0700 dir and runs the
   narrowed leak check (no `api_key`, no `llm_`); the route/pin is resolved at fire time as
   today, so no credential is ever persisted with a schedule. `Owner` stays `json:"-"`.
5. **Same-origin + loopback still apply.** `Handler()` is wrapped in `sameOrigin`
   (server.go:175) and the listen address is loopback-checked (`CheckLoopbackAddr`), so a
   hostile page cannot POST a yolo schedule to `127.0.0.1:4120`. If an unauthenticated
   allowlist is ever introduced, `/api/v1/schedules*` must **not** be on it.

---

## 6. Desktop-specific concerns (flobot.app)

Electron's main process supervises `flobot serve` as a child; closing the window does not
stop the server. All scheduling logic therefore lives in the Go child and the renderer stays
a pure client — which is why §5.3 puts the tick server-side and why **no** `setInterval` in
`web/app.js` may ever fire a run (a hidden window is throttled to roughly once a minute, can
be discarded entirely under macOS App Nap, and is reloaded on every render).

| Situation | Behaviour |
| --- | --- |
| **Window closed, app running** | Fires normally; nothing renderer-side is required. |
| **Display sleep / lid closed** | The Go process is suspended, so the ticker does not advance while suspended. On resume the first tick sees `now - next_run_at > catch_up_window` → the `catch_up` policy applies. No double fire is possible, because firing compares `now` to the persisted `next_run_at` and never counts ticks. |
| **Clock jumps (NTP, manual, timezone travel)** | Elapsed-time decisions ride the monotonic clock; `next_run_at` is only ever recomputed as `cron.Next(now)`. A backward clock jump cannot re-fire a minute already fired, since `next_run_at` already advanced past it (E23). |
| **App not running at fire time** | Nothing fires. On next start the `catch_up` policy decides, and the desktop shows "n schedules were missed while Flobot was closed" once per launch (§6.3) so the gap is never invisible. |
| **Server child dies / is relaunched by Electron** | Same as any restart: schedules reload from `schedules.json`; `last_status:"running"` with no live child becomes `interrupted` (§5.6) instead of blocking the schedule forever. |
| **Two `flobot serve` processes** | Out of scope per N1; the second cannot bind `127.0.0.1:4120` and exits. Electron must not respawn-loop on that error. |
| **Folder on an unmounted volume (external disk, iCloud Drive, network mount)** | `os.Stat` fails at fire time → `last_status:"error"`, `failure_count++`, notification "folder not available: /Volumes/…". We do not wait or retry inside the tick; the auto-disable counter handles a disk that never comes back. |

### 6.1 Model routes — a standing rule, not a preference

Schedules **cannot** select a model, route or pin. Those fields do not exist on the record
(§5.1); the route is `bot.Route`, resolved by `resolveRoute` at fire time. The operator's
standing rule is that model launches go to the local Mac or an explicitly nominated remote
Mac, and never to a GPU node — a schedule that could pin an arbitrary route would be a
mechanism for unattended launches on the wrong machine at 03:00, which is exactly the failure
the rule exists to prevent. Consequences we accept and document:

- If a bot's route is unreachable at fire time, the fire **fails** (`failure_count++`). We
  never fall back to another route or to "whatever is free": falling back would silently
  change which machine runs the model.
- An unpinned `local` route returns `routes.ErrLocalUnpinned` → the fire fails with that
  error surfaced in the notification (already a mapped case in `httpError`).
- The editor shows the teammate's route read-only ("runs as qwen · mesh/glm-4.6") so the
  user can see what will be launched; changing it means editing the **bot**.
- For the same reason schedules expose no model dropdown, no "cheapest available model"
  option, and no route-pinning shortcut: each would be a route-change path that bypasses the
  roster, and the roster is where route changes stay visible and auditable.

### 6.2 What a scheduled run must never do

No `--resume`/session continuation (each fire is a fresh agent — the `internal/schedule`
package header already commits to "cron = fresh agent"); no implicit `git push`; no deploy
step; no installing into the OS; no touching any path outside the recorded folder. The argv
for a scheduled fire is built by the existing `executor.Argv`; schedules add no new flags.

### 6.3 Notifications

`desktop/main.js` subscribes to the existing SSE stream (falling back to polling
`GET /api/v1/schedules` on `schedule_failed`) and posts a macOS notification for: a
`failure_count` reaching its limit plus auto-disable ("**weekday CI triage** disabled after 3
failures — open Flobot"), a missing-`path` error, and a yolo run that ended `error` (highest
priority: something mutated the tree unattended and failed). Notifications are **never**
posted per-occurrence for `skipped_overlap` or `skip`-policy missed fires; those aggregate
into the once-per-launch banner. Notification text never contains the prompt body (which can
hold private text) — name and error reason only. No notification may be sent from the
renderer (`Notification` in a closed window is unreliable); Electron main owns them.

---

## 7. Edge cases (E1–E24)

| # | Case | Behaviour |
| --- | --- | --- |
| E1 | Cron expression invalid | 400 at create/preview naming the `field`; nothing persisted; `next_run_at` never set |
| E2 | Unknown timezone | 400 `unknown timezone "…"`; no silent fallback to server-local |
| E3 | Expression never fires within 5 years (`0 0 30 2 *`) | 400 `cron never fires` at create; `Next` returns `ok:false` |
| E4 | Bot deleted after creation | next fire auto-disables (`disabled_reason:"auto_disabled"`, `last_status:"error"`) + notification; no backoff loop |
| E5 | Bot's route unreachable / unpinned `local` | fire fails, `failure_count++`, notification; **never** a route fallback (§6.1) |
| E6 | Folder deleted or moved | fire fails at `os.Stat`, `failure_count++`, row shows a red path chip; auto-disables at the limit |
| E7 | Folder path is a symlink retargeted after creation | `filepath.EvalSymlinks` at fire time must still match the recorded value, else fail like E6 — a retargeted symlink must not repoint a yolo job |
| E8 | App closed across N fires, `catch_up:"skip"` | 0 runs, `missed_at` set, one `schedule_missed` event carrying `count:N`, launch banner |
| E9 | Same, `catch_up:"once"` | exactly 1 run, `missed_at` set, banner says "N skipped, 1 ran" |
| E10 | Previous run still `running` at fire time | `last_status:"skipped_overlap"`, `skipped_at`, no `failure_count++`, `next_run_at` advances |
| E11 | Server restarted mid-scheduled-run | run orphaned; at load `last_status:"running"` + no live child → `error` / `interrupted by restart`; the schedule resumes on its next fire |
| E12 | `next_run_at` corrupt or empty on an enabled schedule | recomputed from `now` at load and on the first tick (self-heal, never a crash — mirroring `state.go`'s log-and-fallback discipline for corrupt files) |
| E13 | `schedules.json` unparseable | log the error, load the empty set, **do not** overwrite the file until the next mutation; UI shows "schedules unavailable (file corrupt)" instead of an empty list that invites a save-clobber |
| E14 | `failure_count` hits the limit while a run is in flight | disable + persist + notify; the in-flight run is **not** killed (it may be the one that succeeds) |
| E15 | DST spring-forward gap | that one fire is skipped; `schedule_missed` with `reason:"dst_gap"` |
| E16 | DST fall-back overlap | fires once, on the earliest UTC occurrence |
| E17 | Two schedules, same cron, same folder, both yolo | second create rejected — 1 yolo per folder (§5.10.2) |
| E18 | 12 schedules all at `0 9 * * *` | one tick dispatches them all as separate goroutines; the per-bot guard collapses same-bot ones into one run plus recorded skips |
| E19 | Prompt larger than the body limit | `decodeJSON` with `runBodyLimit` (64 KiB) accepts ≤16 KiB prompts and rejects the rest with a size hint |
| E20 | `run-now` on a disabled schedule | allowed, 200 — explicit user intent beats `enabled` |
| E21 | `run-now` twice while the first is running | first 200, second 409 |
| E22 | `DELETE` while its run is in flight | 204; the run continues and finishes (the user may Stop it); its `schedule_id` dangles and renders "deleted schedule" |
| E23 | Server clock moved backwards across a fire | no re-fire: `next_run_at` already advanced |
| E24 | Valid timezone name but a zone the user mistook for another | preview rows carry explicit offsets; no server-side guessing |

---

## 8. Metrics

Log lines plus a cheap `GET /api/v1/schedules/stats` (P2, for the dashboard — an aggregate
over the store and run history, no new persistence).

- `schedules_total`, `schedules_enabled`, `schedules_yolo` — adoption and risk surface.
- `fires_total{outcome=done|error|skipped_overlap|missed|blocked_path}` — the ratio that says
  whether this feature is useful or just noisy. Targets: `skipped_overlap` < 10% of fires,
  `blocked_path` < 2%.
- `late_fire_seconds` (histogram of `spawn` time − scheduled minute), p95 ≤ 2 s while awake.
  This is the direct measure of the 1s-tick decision.
- `missed_fires_total`, `catchup_once_fires_total` — if `catch_up:"once"` adoption is < 5%,
  the option is dead weight and we cut it.
- `auto_disabled_total{reason=failures|missing_bot}` plus the re-enable rate: a schedule
  re-enabled within an hour was probably killed by a transient route outage, which means the
  default limit of 3 is too aggressive.
- `run_duration_seconds` for runs with `schedule_id` vs. manual — much longer means the
  prompts are underspecified.
- `diff_pending_after_scheduled_run` — how often a scheduled `code` run leaves an unreviewed
  diff, i.e. real user backlog created *by* the feature. This is the number that decides
  whether scheduled `code` runs should default to `verify` or to a worktree (Q1).
- Desktop: `launch_banner_shown` vs. `launch_banner_dismissed` — is the "missed while closed"
  banner informing, or being reflexively dismissed?

---

## 9. Rollout

**P1 — Engine + API + file (no UI).** `internal/schedule/cron.go` + tests (grammar, DOM/DOW
OR, DST gap/overlap, 5-year cap); `internal/store/schedules.go` + tests (0600, tmp+rename,
corrupt file, self-healing `next_run_at`, the three caps); `internal/cronrun` + tests with an
injected `Now func() time.Time` — the pattern `internal/schedule` already uses (schedule.go:57)
— plus `store.Run.ScheduleID` and the handlers. No flag: nothing can create a schedule
without a client. **Done when** a hand-written `schedules.json` fires on a minute boundary,
writes an ordinary run with the correct `Dir` into the run history, and survives a restart.

**P2 — UI.** Schedules panel, editor drawer, next-3 preview, run-now, view-runs filter, empty
state, SSE events, `⚠YOLO` badge and the blocking yolo confirmation. **Done when** U1–U3,
U7, U10 and U12 pass by clicking.

**P3 — Desktop + migration.** Missed-fire banner, failure notifications from Electron main,
one-click migration of legacy routines, `GET /api/v1/schedules/stats`, read-only route
display in the editor.

**P4 — Cleanup.** Remove `internal/schedule`, the `routines` collection and its enable/pause
routes once the migration banner reads zero; update `SPEC.md` and drop `routines` from the
roster response (a wire change — bump `schema` to `flobot.v2`).

Each phase runs `go vet ./...` plus the narrow package tests
(`go test ./internal/schedule/ ./internal/store/ ./internal/cronrun/ -run 'Cron|Schedule'`),
leaving the repo's pre-push hooks as the full gate rather than re-running `./...` by hand.

**Kill switch:** none needed for P1/P2 (no UI = no surface). If one is wanted,
`FLOBOT_CRON=0` disables `cronrun.Start` at boot while the API keeps serving read-only —
cheaper and more honest than a per-schedule `experimental` bit.

---

## 10. Test plan

| Area | Test | Shape |
| --- | --- | --- |
| Grammar | 40 valid + 20 invalid expressions (names, ranges, steps, `a>b` rejected, spaces rejected, `@reboot`/`@every` rejected) | table-driven, `cron_test.go` |
| Next-fire | `Next` from a fixed `Now` for daily / `*/15` / `0 9 * * 1-5` / `0 0 29 2 *` / impossible date; monotonic non-decreasing over 10k calls | golden times |
| DOM/DOW OR | `0 9 13 * 5` yields both a 13th and a Friday; `0 9 13 * *` yields only the 13th | `cron_test.go` |
| DST | Europe/Berlin gap day skips exactly one fire and emits `dst_gap`; overlap day fires once (earliest UTC) | fixed transition dates |
| Store | 0600 file mode, tmp+rename atomicity, missing file → empty, corrupt file → error + empty set without clobbering, `Owner` never serialized | `schedules_test.go` |
| Caps | 13th schedule → 400; 5th enabled yolo → 400; 2nd yolo on the same folder → 400 | store-level, no server |
| Scheduler | fake `Now` advancing 1s: fires exactly once per due minute; no burst on first tick; late-by-30s fire still fires; late-by-90min → skip policy; overlap → `skipped_overlap` and `next_run_at` advanced | `cronrun_test.go` |
| Auto-disable | 3 injected `error` outcomes → `enabled:false`, `disabled_reason:"auto_disabled"`, one notification event, and a 4th fire does not happen | fake clock |
| Folder pinning | set `workDirNow()` to folder B, fire a schedule pinned to A, assert `executor.Request.Dir == A` (fake `RunFunc`) | the G3 regression test |
| API | each route's happy path + the §5.2 error table; `PATCH` sparse semantics; yolo requires `confirm` on both create and edit; `run-now` on disabled = 200; `run-now` twice = 200 then 409 | `httptest`, `FLOBOT_TEST_STATE` temp home |
| Restart | write `schedules.json`, restart the server, assert no double fire for a `next_run_at` inside the window and correct skip for one outside it | two `New()` lifetimes |
| E23 clock | fake `Now` moved backwards across a fire → no second fire | fake clock |

---

## 11. Open questions

1. **Should scheduled `intent:"code"` runs default to a `worktree`?** `Run` already carries
   `Worktree` and `createRun` accepts it. A yolo edit of the user's *working* tree at 03:00
   is the scariest thing in this PRD; the proposal is `worktree:true` by default for
   scheduled `code` intent, surfaced as an off-switchable checkbox. Must be decided before
   P2 — it changes the drawer.
2. **One run per gap, or one per missed minute, for `catch_up:"once"`?** This design says
   one. A `*/5 * * * *` job that wants every missed sample is a metrics feature, not a
   schedule; confirm nobody actually wants that before cutting the option.
3. **Does `Schedule` need `skill_id`?** The migration currently flattens `skill.Body` into
   `prompt`, losing skill reuse and skill editing. The clean answer is an optional
   `skill_id` with `prompt` as fallback, prepended exactly as `fireRoutine` prepends
   `sk.Body` (server.go:284). Deferred to P2 — but if legacy routines have real adoption,
   this becomes P1-blocking for the migration.
4. **Are 12 schedules / 4 yolo the right caps?** Chosen so the panel stays scannable and the
   risk surface stays countable. If someone needs 30 schedules that is a folder-level
   template feature, not a higher cap; revisit with P2 telemetry.
5. **Which store is authoritative for schedule run history?** `/api/v1/runs` is in-memory and
   evicts to `RunStore` (`evictRunsLocked`, store.go:657), so an old scheduled run may exist
   only in the summary store. Proposal: `RunStore` is authoritative for
   `GET /api/v1/schedules/{id}/runs`, with in-memory runs filling only the most recent rows.
6. **Should a scheduled run's leftover pending diff auto-reject after N days?** Without a
   policy, an unattended `code` schedule accumulates unreviewed diffs in the working tree
   indefinitely. `internal/checkpoint` retention probably already answers this; verify before
   P3 and reference it rather than inventing a second TTL.
