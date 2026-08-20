# PRD: Flobot — Local-First Analogue of x.ai/bot

## Executive Summary

**Product:** Flobot — a local-first analogue of https://x.ai/bot implemented in this repository.

**Vision:** Provide a standalone, local-first multi-agent orchestration system where a user can define and persist named teammates, assign skills, execute runs via `flocode`, and monitor streaming execution in a local web UI. No hosted backend, no secrets persisted to disk.

**Current State:** This repo already contains scaffolding for `store`, `executor`, `server`, and `web`. Remaining gaps must be implemented so the system is end-to-end functional.

**Scope of this PRD:** Implement the remaining gaps inside this repository only to enable:
1. Persist/load roster to `~/.flobot/state.json`
2. Prepend a skill body to a run
3. Append one CCPM log line per child run
4. Actually run Meta Spark Muse 1.2 through `flocode` for `verify`

**Execution Engine:** `flocode` (flocode2 0.1.0) resolved via `PATH`, spawned with `-p --output streaming-json`. No direct import of `github.com/florianleibert/flocode`.

**Web UI:** Standalone server bound to `127.0.0.1:4120` streaming NDJSON with defined kinds.

**Models Pinned:**
- GLM 5.2
- Qwen 3.8
- Muse Spark 1.2 (key sourced from `~/.accounts/meta.ai` as `MODEL_API_KEY` only)

---

## Problem

### Problem Statement
Users who want an `x.ai/bot`-like multi-agent experience currently rely on a hosted, cloud-dependent product. There is no local-first, file-system-backed alternative that allows transparent control over teammates, skills, pipelines, and model execution while keeping secrets off-disk and working entirely through a local `flocode` installation.

### Who is Affected
- Developers who want to run, inspect, and verify multi-agent workflows locally
- Users who require no secrets persisted and explicit control over state in `~/.flobot/state.json`

### Current Pain Points
- No local analogue for persistent named teammates (`glm`, `qwen`, `muse`)
- No local streaming UI for observing agent thinking, tool use, and permissions
- No clean integration of Skills (`SKILL.md`) and CCPM pipeline (`flocode pipeline` A→E) into runs
- No verified path to execute Muse Spark 1.2 via `flocode` without leaking credentials

### Goals
- Deliver a local-first system that feels like `https://x.ai/bot` but runs entirely on-device via `flocode`
- Ensure roster persistence is reliable, secret-free, and schema-correct
- Enable skill prepending, CCPM logging, and verified Muse Spark execution

### Success Metrics
- Roster survives restart via `~/.flobot/state.json`
- Web UI on `127.0.0.1:4120` streams all NDJSON kinds without drop
- Skill body correctly prepended when skill is selected for a run
- Exactly one CCPM log line appended per child run via `flocode pipeline`
- `verify` run successfully executes Muse Spark 1.2 through `flocode` using `MODEL_API_KEY` sourced at runtime

---

## User Stories

### US-1: Persist and Load Named Teammates Roster
**As a** user
**I want** my named teammates `glm`, `qwen`, `muse` to persist in `~/.flobot/state.json`
**So that** my roster survives restarts and is available to the UI and executor

**Acceptance Criteria:**
- [ ] `store` writes roster to `~/.flobot/state.json` on create/update/delete of teammates
- [ ] `store` loads roster from `~/.flobot/state.json` on startup; if file missing, initializes with empty roster or defaults without error
- [ ] Persisted JSON contains entries for `glm`, `qwen`, `muse` with pinned models: GLM 5.2, Qwen 3.8, Muse Spark 1.2 respectively when roster is initialized
- [ ] Persisted JSON contains **no** `secrets` field or secret values
- [ ] Persisted JSON contains **no** `Owner` JSON field at any level
- [ ] File permissions are restrictive (e.g., 0600) where applicable
- [ ] Corrupted `state.json` is handled gracefully with error surfaced to UI/logs, not crash

### US-2: Standalone Web UI with Streaming NDJSON
**As a** user
**I want** a standalone web UI on `127.0.0.1:4120` that streams execution
**So that** I can observe agent progress in real time

**Acceptance Criteria:**
- [ ] `server` binds exclusively to `127.0.0.1:4120` (not 0.0.0.0)
- [ ] UI connects to NDJSON stream endpoint and renders events incrementally
- [ ] All NDJSON `kind` values are supported and rendered: `content`, `thinking`, `tool_start`, `tool_end`, `perm`, `plan`, `err`, `done`, `verify`
- [ ] `verify` kind is visually distinct and confirms Muse Spark 1.2 execution succeeded through flocode
- [ ] Stream handles disconnect/reconnect without requiring server restart
- [ ] No secrets or API keys are rendered in UI, logs, or network payloads

### US-3: Prepend Skill Body to Run
**As a** user
**I want** to select a Skill and have its body prepended to the run prompt
**So that** agents operate with the correct skill context

**Acceptance Criteria:**
- [ ] Skills are defined as `SKILL.md` files and discoverable by the system
- [ ] When a run is started with a skill, the full `SKILL.md` body is prepended to the prompt/context sent to `flocode`
- [ ] If no skill selected, run proceeds without prepending and without error
- [ ] Skill prepending is visible in `plan` or `thinking` NDJSON event for debuggability (without leaking secrets)
- [ ] `test-run` intent is `ask-intent` only and does not execute skill side-effects

### US-4: Append CCPM Log Line Per Child Run
**As a** user
**I want** CCPM workflow A→E via `flocode pipeline` to log progress
**So that** each child run is traceable

**Acceptance Criteria:**
- [ ] Each child run spawned via `flocode pipeline` appends **exactly one** log line to the CCPM log
- [ ] Log line contains child run identifier, pipeline stage (A→E), status, and timestamp
- [ ] No duplicate lines per child run, even on retry/error
- [ ] Log append is atomic and does not corrupt existing log
- [ ] Log location is inside this repository (or designated local log path), not in `~/dev/flocode` or `../flocode2`

### US-5: Verify Run with Muse Spark 1.2 via Flocode
**As a** user
**I want** to run `verify` using Muse Spark 1.2 through `flocode`
**So that** I can confirm the Muse integration works end-to-end

**Acceptance Criteria:**
- [ ] `executor` spawns `flocode` from `PATH` with flags `-p --output streaming-json`
- [ ] For Muse teammate, executor resolves Muse Spark 1.2
- [ ] Muse API key is read at runtime from `~/.accounts/meta.ai` and exposed to the child process **only** as env var `MODEL_API_KEY`
- [ ] Key is never written to `~/.flobot/state.json`, never logged, never printed, never returned via API/UI
- [ ] `verify` run completes and emits `verify` NDJSON kind with success status on valid key
- [ ] On missing/invalid key, emits `err` kind with actionable message and no key disclosure

### US-6: Safe Execution and Isolation
**As a** developer
**I want** execution to be constrained to this repository
**So that** upstream flocode installations are not mutated

**Acceptance Criteria:**
- [ ] No code edits `~/dev/flocode`
- [ ] No code edits `../flocode2`
- [ ] All changes are contained inside this repository
- [ ] `executor` never imports module `github.com/florianleibert/flocode`; it spawns the binary via PATH only

---

## Functional Requirements

### FR-1: Store / Roster Persistence
- FR-1.1: Implement `store` to read/write `~/.flobot/state.json`
- FR-1.2: Schema must support teammates array/map with `name` in `{glm, qwen, muse}`, `model` pinned, and optional metadata; must **not** include `Owner` field
- FR-1.3: Must guarantee no secrets are persisted; any secret fields stripped before write
- FR-1.4: Provide `Load()` and `Persist()` (or equivalent) with file locking to prevent concurrent write corruption

### FR-2: Executor - Flocode Spawning
- FR-2.1: Executor must resolve `flocode` via `PATH` lookup, not hardcoded path
- FR-2.2: Spawn command is `flocode -p --output streaming-json [args]`; args include prompt/skill-prepended input
- FR-2.3: Must not import `github.com/florianleibert/flocode` as Go/Python module; process spawn only
- FR-2.4: Must stream stdout as NDJSON, parse per-line JSON, and forward to server/web layer
- FR-2.5: Must handle `STREAMING-JSON` output parsing errors gracefully, emitting `err` kind
- FR-2.6: Env injection for Muse: read `~/.accounts/meta.ai` -> set `MODEL_API_KEY` in child env only; never persist or log value

### FR-3: Skill Handling
- FR-3.1: Skills are `SKILL.md` files in repo (or configured skills directory)
- FR-3.2: Skill loader reads full markdown body and prepends to run input before passing to `flocode`
- FR-3.3: Prepend format: `skillBody + "\n\n---\n\n" + userPrompt` (or equivalent deterministic separator)
- FR-3.4: `test-run` mode must be `ask-intent` only: determine intent without executing tools/side-effects

### FR-4: CCPM Pipeline Integration
- FR-4.1: Integrate `flocode pipeline` for CCPM stages A→E
- FR-4.2: On each child run completion, append one line to CCPM log (e.g., `ccpm.log` or equivalent in repo)
- FR-4.3: Log line format: `<timestamp> <pipeline_id> <stage A-E> <child_run_id> <status>`
- FR-4.4: Ensure idempotency: if child run retried, still only one line per logical child run id

### FR-5: Server / Web
- FR-5.1: Server starts standalone HTTP server on `127.0.0.1:4120`
- FR-5.2: Serves static web UI and NDJSON stream endpoint (e.g., `/api/stream` or `/events`)
- FR-5.3: NDJSON stream `Content-Type: application/x-ndjson` with kinds: `content`, `thinking`, `tool_start`, `tool_end`, `perm`, `plan`, `err`, `done`, `verify`
- FR-5.4: Web UI must render all kinds with distinct styling; `thinking` collapsed by default, `tool_start`/`tool_end` paired, `perm` requires user acknowledgment mock, `verify` shows success/failure badge
- FR-5.5: No secrets exposed via API responses or client-side logs

### FR-6: Model Configuration
- FR-6.1: Pin `glm` to GLM 5.2
- FR-6.2: Pin `qwen` to Qwen 3.8
- FR-6.3: Pin `muse` to Muse Spark 1.2
- FR-6.4: Model selection must be persisted in `state.json` per teammate and respected by executor

### FR-7: Verify Flow
- FR-7.1: Implement `verify` command/intent that triggers a Muse Spark 1.2 run through flocode
- FR-7.2: Verify must succeed end-to-end when `~/.accounts/meta.ai` contains valid key
- FR-7.3: Verify must emit `verify` NDJSON event on completion; downstream UI displays result

---

## Non-Goals

- NG-1: No hosted/cloud sync or multi-device roster sharing; local-first only
- NG-2: No support for models beyond pinned GLM 5.2, Qwen 3.8, Muse Spark 1.2 in v1
- NG-3: No editing or vendoring of upstream `~/dev/flocode` or `../flocode2` (flocode2 0.1.0); treat as external binary
- NG-4: No import of `github.com/florianleibert/flocode` module
- NG-5: No persistence of secrets, API keys, or Owner field in `state.json` or logs
- NG-6: No binding web UI to public interfaces (0.0.0.0) or deploying to external host
- NG-7: No inventing, generating, or hardcoding API keys; keys sourced only from `~/.accounts/meta.ai`
- NG-8: No `git push` to `origin main` or `origin master` as part of any feature or automation
- NG-9: No general-purpose skill marketplace or remote skill fetching; only local `SKILL.md`
- NG-10: No replacement of `flocode pipeline` CCPM implementation; integration only via CLI

---

## Constraints

### C-1: Repository Boundaries
- All implementation changes must stay inside this repository
- Do not edit `~/dev/flocode`
- Do not edit `../flocode2`

### C-2: Execution Constraints
- Must spawn `flocode` via `PATH` (flocode2 0.1.0)
- Must use flags `-p --output streaming-json`
- Never import module `github.com/florianleibert/flocode`
- CCPM must use `flocode pipeline` for stages A→E

### C-3: Persistence Constraints
- Roster persists in `~/.flobot/state.json`
- Persisted state must contain named teammates `glm`, `qwen`, `muse`
- Persisted state must have **no secrets**
- Persisted state must have **no Owner JSON field**

### C-4: Networking Constraints
- Standalone web UI must bind to `127.0.0.1:4120` only
- Streaming format is NDJSON with exact kinds: `content`, `thinking`, `tool_start`, `tool_end`, `perm`, `plan`, `err`, `done`, `verify`

### C-5: Model & Credential Constraints
- Pins enforced: GLM 5.2, Qwen 3.8, Muse Spark 1.2
- Muse key sourced from `~/.accounts/meta.ai` as `MODEL_API_KEY` only (env injection to child process)
- Never print API keys to stdout, stderr, logs, UI, NDJSON, or error messages
- Never log `MODEL_API_KEY` value; redact if env inspection is required
- Never invent API keys or use placeholder/hardcoded keys

### C-6: Git Safety Constraint
- **Never `git push origin main` or `git push origin master`**
- No automation, script, or instruction may push to `main`/`master`; use feature branches and PRs only
- CI/hooks must block direct pushes to `main`/`master` if present

### C-7: Skill & Test Constraints
- Skills are `SKILL.md` files; skill body must be prepended to run input verbatim
- `test-run` is `ask-intent` only; no tool execution or side effects during test-run

### C-8: Logging Constraint
- Append exactly one CCPM log line per child run
- Log must not contain secrets or API keys

### C-9: Security & Privacy Constraint
- Never print API keys in any output, including debug, verbose, or error paths
- Ensure file permissions on `~/.flobot/state.json` and `~/.accounts/meta.ai` reads do not leak via world-readable copies
