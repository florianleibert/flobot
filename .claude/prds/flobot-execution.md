---
name: flobot-execution
description: Execute named amesh flobot teammates via the flocode2 binary, with a standalone web UI and Muse verification.
status: active
created: 2026-08-19T23:52:29Z
---

# PRD: flobot-execution

## Executive Summary

flobot is a local-first analogue of [Grok Bot](https://x.ai/bot): named teammates, reviewable skills, paused routines, and single-owner handoffs. Amesh Web v2 already ships the roster (PR #1062) with **no model dispatch**. This PRD adds execution by spawning the flocode2 `0.1.0` binary (`flocode -p --output streaming-json`), a standalone loopback web UI for dogfood, and Muse Spark 1.2 verification through flocode2's OpenAI-compat client. Three deliverable PRs: amesh, flocode2, flobot.

## Problem Statement

Operators can create bots and hand off work, but nothing runs. Test-run writes `test-run: no external effect`. Routes advertise GLM 5.2 / Qwen 3.8 / Muse Spark 1.2 / local MLX while `~/.flocode/config.toml` is missing and the registry disagrees with the catalog on Qwen. There is no web surface that streams a real harness turn.

## User Stories

1. **As an operator**, I install flobot on the Agents rail, create Chief (muse) and Coder (qwen), and send a prompt to Coder. **Acceptance:** a `flocode` child starts, NDJSON events appear in the UI, pairing isolation holds, no API key appears in JSON.
2. **As an operator**, I test-run a paused routine. **Acceptance:** one `--intent ask` child runs; enable stays blocked until that run records `LastRun`; the child cannot apply_patch.
3. **As a harness developer**, I dogfood on `:4120` without pairing. **Acceptance:** standalone UI creates a bot and streams a GLM or Qwen turn; logs capture tool names and errors.
4. **As a reviewer**, I enable verify on a CODE run. **Acceptance:** after a landed write, flocode2 calls Muse via openai bindings and emits `kind=verify` (`passed|failed|skipped`). Missing Muse key skips, does not panic.
5. **As an operator**, I leave yolo off. **Acceptance:** mutating tools in headless CODE are denied with a visible `err`/`perm` event; checking yolo skips Ask+Guard only.

## Functional Requirements

- FR1: Exec PATH `flocode` (must be flocode2 0.1.0). Never import amesh into flocode2. Never import flocode2's Go module into amesh (donor module path collision).
- FR2: `POST /runs` on both standalone flobot and amesh plugin. Body `{bot_id, prompt, intent?, yolo?, verify?, worktree?}`.
- FR3: Stream NDJSON kinds `content,thinking,tool_start,tool_end,perm,plan,err,done,verify`.
- FR4: Route pins: glm, qwen (3.8 with 3.6 fallback after `/v1/models` probe), muse, local (typed error if unpinned).
- FR5: Muse key from `~/.accounts/meta.ai` → child `MODEL_API_KEY` only.
- FR6: Test-run = ask-intent `BOT_RUN`. Enable still requires LastRun.
- FR7: Standalone web on `127.0.0.1:4120`.
- FR8: flocode2 `--verify` + `verify.RunWithPin` using openai client.
- FR9: Instrument flocode2 slog around tools, codec repair, provider errors. Capture logfiles; file bugs; fix; redeploy binary (max 10 attempts).
- FR10: Three PRs on feature branches, never `main`.

## Non-Functional Requirements

- Loopback bind only for standalone UI.
- Child timeout = flocode2 turn budget (default 10m); test-run clamp 60s.
- No secrets in store JSON, NDJSON, LastNote, HTML, git.
- Tests: store ownership, executor fake-binary NDJSON, amesh pairing 403, verify skip-without-key.
- Headless mutating tools remain denied without yolo.

## Success Criteria

- A GLM or Qwen headless turn completes with `kind=done` through the standalone UI.
- Amesh plugin `/runs` test passes with a fake flocode.
- Muse verify either `passed/failed` or clean `skipped: missing key` — never a panic or leaked key.
- Irregularities (Qwen 3.8 vs 3.6/Gemma, missing config, verify-off) recorded in `research/04-dogfood-log.md`.
- Three open PRs: amesh, flocode2, flobot.

## Constraints & Assumptions

- flocode2 tagged `v0.1.0` on `impl/harness-v1`. PATH binary is that build.
- amesh-web is already running on 18765.
- Mesh identity `local-agent.json` is absent; do not block on it.
- CCPM lives in this repo under `.claude/` (donor skill is read-only from `~/dev/flocode/.claude/skills/ccpm`).

## Out of Scope

- Cloud VM, computer-use, iOS, Discord gateways, Skills Hub, skill_manage, teach-by-demo recording.
- Persistence of roster across amesh-web restart.
- Real catalog signing.
- Dynamic plugin host.
- Editing `~/dev/flocode` or `~/.opencode/bin/opencode`.
- Merging to main.

## Dependencies

- flocode2 v0.1.0 binary + openai-compat provider.
- amesh-web pairing + existing FlobotStore.
- Mesh GLM `:13038`, Qwen node, Meta Model API.
