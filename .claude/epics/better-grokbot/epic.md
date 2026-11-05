---
name: better-grokbot
status: in-progress
created: 2026-11-04T19:25:00Z
updated: 2026-11-04T19:25:00Z
progress: 0%
prd: research/SYNTHESIS-better-grokbot.md
github: (will be set on sync)
---

# Epic: better-grokbot

## Overview

Turn flobot from a command runner into a named teammate with memory that compounds,
skills you teach it, routines that fire unattended, and an app that tells you what
happened. Derived from research rounds 7–26 (`research/SYNTHESIS-better-grokbot.md`),
which consolidated 20 gap tables into 30 capabilities across four tiers.

## Architecture Decisions

1. **Zero third-party dependencies is a product property, not an accident.** `go.mod`
   has no `require` block and no `go.sum`. FTS5/SQLite and Temporal are therefore
   *design* inputs only: their shapes transfer (three memory tiers, fire identity,
   timeout split, heartbeat checkpoints), their engines do not.
2. **Every change is additive to existing structs.** Persistence is JSON at
   `~/.flobot/*.json` with a leak scanner; there is no schema, so there is no migration.
   New fields must be `omitempty` so old state files load unchanged.
3. **Struct fields are added once, up front, by the epic owner.** Lanes then add
   *methods in new files* inside the same package, so parallel lanes never edit the
   same file. This is what makes the lanes conflict-free.
4. **The leak scanner stays authoritative.** Any new persisted field must survive
   `assertNoLeaks`; a test that writes memory and reloads from disk proves it.
5. **Silent degradation is a bug.** Memory clips → return an error naming usage.
   Stream drops → publish a drop frame. Notification fails → log it. Every place the
   old code swallowed a failure gets a visible signal.

## Technical Approach

### Backend
- `internal/memory` — bounded facts + dated log + usage accounting (M1).
- `internal/skills` — skill contract, prompt index assembly, draft-from-run (M2).
- `internal/sandbox` — env allowlist, Seatbelt profile generation, secret scrubbing (M3).
- `internal/eval` — replay + deterministic scorers + operator verdicts (M7).
- Existing packages gain methods in new files; `store.go` struct fields land first.

### Frontend
- `web/app.js` — incremental delta rendering, resume with `?since=`, unread count.
- `desktop/main.js` — dock badge, coalesced notifications with actions, Tray,
  a `failed` listener that logs instead of swallowing.

### Infrastructure
- No new processes, no new ports, no new dependencies.
- Tests use the existing fake-binary pattern (`internal/executor/fake.go`) and
  temp-dir state paths (`State.SetPath`).

## Implementation Strategy

Lanes are ordered by dependency and grouped so that no two concurrent lanes write
the same file:

| Lane | Milestone | Owns |
| ---- | --------- | ---- |
| A | M1 memory | `internal/memory/`, `store/memory.go`, `state.go` |
| B | M2 skills | `internal/skills/`, `store/skill_contract.go`, `server/skills_prompt.go` |
| C | M3 sandbox | `internal/sandbox/`, `executor/env.go`, `executor/scrub.go` |
| D | M4 routines | `schedule/fire.go`, `server/fire.go`, `store/fire.go` |
| E | M5 streaming | `server/stream.go`, `executor/delta.go`, `web/app.js` |
| F | M6 desktop | `desktop/main.js`, `desktop/preload.js` |
| G | M7 measure | `internal/eval/`, `routes/decision.go` |

Lanes A–D and G are independent Go work and run concurrently. Lane E depends on D's
fire identity for resume correctness and runs after it. Lane F is independent of all Go.

## Task Breakdown Preview

1. M1 — memory that compounds
2. M2 — skills as contracts, skills from runs
3. M3 — sandbox and secrets
4. M4 — routines safe to leave unattended
5. M5 — streaming that does not lie
6. M6 — the app tells you what happened
7. M7 — measure it

## Dependencies

`research/SYNTHESIS-better-grokbot.md` § Tiers 1–4 and §4 milestones; rounds 07–26 for
the primary-source detail behind each capability.

## Success Criteria (Technical)

All five statements in synthesis §5 hold and are covered by a test:

1. A bot remembers what it did last week without being told. *(M1)*
2. You can teach it something once and it uses it next time. *(M2)*
3. You can leave it on a schedule and it cannot read your SSH keys or phone home. *(M3)*
4. A routine that fires twice runs once; a cancelled routine leaves nothing half-done. *(M4)*
5. The app tells you what happened without you having watched. *(M5, M6)*

Plus: `go build ./... && go vet ./... && gofmt -l` clean, `go test ./...` green,
and `go.mod` still three lines with no `go.sum`.

## Estimated Effort

Seven milestones; each ships independently. Lanes A–D/G concurrent.
