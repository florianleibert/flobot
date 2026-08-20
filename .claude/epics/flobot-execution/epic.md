---
name: flobot-execution
status: in-progress
created: 2026-08-19T23:52:29Z
updated: 2026-08-19T23:52:29Z
progress: 0%
prd: .claude/prds/flobot-execution.md
github: (will be set on sync)
---

# Epic: flobot-execution

## Overview

Stand up execution for the amesh Agents-rail flobot plugin by spawning flocode2, ship a standalone loopback UI in `~/dev/flobot`, and extend flocode2 with Muse verification + instrumentation. Three git PRs, exclusive file ownership.

## Architecture Decisions

1. **Exec the binary, do not import modules.** flocode2's `go.mod` path is the donor. amesh must not `require` it. flobot executor is `os/exec`.
2. **Same argv contract** in flobot and amesh (`research/02-round2-amesh-plugin-flocode2.md`).
3. **No new plugin host.** Catalog install + pairing stay as Gmail. `/runs` is a new HTTP surface on the existing plugin.
4. **Test-run is ask-intent.** CODE runs default yolo=false (mutating tools denied in headless).
5. **Muse key** loaded into child env from `~/.accounts/meta.ai`; never in JSON.
6. **Standalone UI on :4120** is the dogfood path; amesh webv2 is the product path.

## Technical Approach

### Frontend
- flobot: single `web/index.html` (roster, prompt, event log, route health).
- amesh `webv2`: extend Agents flobot card (prompt, yolo, verify, events, skills, routines).

### Backend
- flobot: Go 1.25 module, in-memory store + executor + HTTP.
- amesh: `FlobotRun` on `FlobotStore`, `os/exec` in amesh-web.
- flocode2: `--verify`, `RunWithPin`, jsonout `session_id`, slog, muse glob, config sample.

### Infrastructure
- Ports 4120–4124 (flobot). Do not bind 18765/4090–4094.
- `~/.flocode/config.toml` pins (no keys).

## Implementation Strategy

Parallel after research+PRD (this epic):

| Lane | Repo | Branch |
| ---- | ---- | ------ |
| A | `~/dev/flobot` | `feat/execution-plugin` |
| B | `~/dev/amesh` | `feat/flobot-execution` (from main, never commit main) |
| C | `~/dev/flocode2` | `impl/harness-v1` (existing) |

Lead dogfoods, records log hotspots, iterates ≤10, then opens three PRs.

## Task Breakdown Preview

1. flobot executor + store + tests
2. flobot web UI + HTTP
3. amesh `/runs` + TestRun spawn + tests
4. amesh webv2 UI
5. flocode2 verify + jsonout + slog + config
6. dogfood + bugfix loop
7. three PRs

## Dependencies

Research rounds 1–3 in `research/`. flocode2 v0.1.0 on PATH.

## Success Criteria (Technical)

`go test` green in all three repos. One live `kind=done` through :4120. PRs opened.

## Estimated Effort

One implementation pass + ≤10 dogfood cycles.
