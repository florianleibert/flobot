# flobot

Local-first named teammates that **exec the flocode2 binary** (`flocode -p --output streaming-json`). Standalone dogfood UI on loopback **127.0.0.1:4120**. This module is `github.com/florianleibert/flobot`. It never imports `github.com/florianleibert/flocode` or `github.com/florianleibert/amesh`.

**Phase 2 (this repo):** execution + roster + web. State persists to `~/.flobot/state.json` (0600, atomic rename); runs and messages are capped (newest 200 runs / 500 messages kept). Muse key is read from `~/.accounts/meta.ai` into the child env as `MODEL_API_KEY` and never appears in JSON, NDJSON, or HTML.

## Run

```bash
go test ./...
go build -o flobot ./cmd/flobot
./flobot serve --addr 127.0.0.1:4120
# open http://127.0.0.1:4120
./flobot run --route glm --prompt "hello" --intent ask
```

Bind addresses that are not loopback are rejected. Binary lookup: `FLOCODE_BIN`, else `PATH` `flocode`, else `$HOME/dev/flocode2/flocode`.

### Argv contract

```
flocode -p PROMPT --model PIN --intent INTENT --output streaming-json
        [--yolo] [--verify] [--worktree NAME] [--continue-until]
```

Route pins (flocode config holds base URLs, not this API): `glm→glm-5.2`, `qwen→qwen-3.8`, `muse→muse-spark-1.2`, `local` is an error until pinned.

Test-run is `--intent ask`, yolo off, verify off, 60s timeout. Default CODE timeout is 10 minutes. Headless CODE without `--yolo` denies mutating tools (flocode2).

### HTTP (standalone, actor=`local`)

| Method | Path |
| ------ | ---- |
| GET | `/` (web UI) |
| GET | `/health` |
| GET | `/api/v1/roster` |
| POST | `/api/v1/bots` `{name,role,route}` |
| POST | `/api/v1/bots/{id}/remember` `{note}` |
| POST | `/api/v1/handoff` `{from,to,body,run}` |
| POST | `/api/v1/skills` `{name,body}` |
| POST | `/api/v1/routines` `{bot_id,skill_id,trigger}` |
| POST | `/api/v1/routines/{id}/test-run` |
| POST | `/api/v1/routines/{id}/enable` |
| POST | `/api/v1/runs` `{bot_id,prompt,intent,yolo,verify,worktree}` |
| GET | `/api/v1/runs` |
| GET | `/api/v1/runs/{id}` |
| GET | `/api/v1/runs/{id}/events` |
| GET | `/api/v1/health/models` |
| GET | `/api/v1/mesh/models` |
| GET | `/api/stream` (SSE; `?run=` replays a run) |
| POST | `/api/v1/runs/{id}/stop` |

`GET /api/v1/health/models` resolves `glm`/`qwen` endpoints from the live amesh catalog and probes their `/v1/models` (8s budget); `muse` probes `https://api.meta.ai/v1`; `local` reports `unpinned` (no pinned binary). Keys are never returned.

### Amesh mesh models

flobot connects to the local amesh-web (default `127.0.0.1:18765`, override with `AMESH_WEB_ADDR`) and reads `GET /api/v2/mesh/models`. Every **ready** mesh model becomes a selectable flobot route `mesh/<model-id>`:

```bash
./flobot run --route mesh/glm-5.2-fp8 --prompt "hello" --intent ask
curl -s http://127.0.0.1:4120/api/v1/mesh/models   # list ready mesh routes
```

- `POST /api/v1/bots` accepts `route: "mesh/<id>"` — per-bot model choice from the mesh (Hermes-style "your models, your hardware").
- A run on a mesh route generates a flocode config pinning that model (base_url = the mesh node endpoint) and passes it to the child via `FLOCODE_CONFIG`. The user's `~/.flocode/config.toml` is never touched; the generated file carries no secrets and is deleted when the child exits.
- Mesh routes use flocode's `generic-openai` profile (OpenAI-compatible mesh endpoints).
- An amesh-web that is down or a hub that is unreachable is a rendered state (`ok:false` + reason), never a crash.

### Handoff runs (Grok Bot crews)

`POST /api/v1/handoff` with `run: true` writes the bus message **and** spawns a BOT_RUN for the recipient bot with the handoff body as its prompt — the recipient acts on the work item, not just receives it. Coder-role recipients get intent `code`; everyone else `ask`. The response includes the spawned `run`.

### Teammate memory (Grok Bot G1)

`POST /api/v1/bots/{id}/remember` `{note}` stores a short role note on a bot. The note is prepended to every subsequent run's prompt for that bot, so a teammate remembers its instructions across runs. Memory is prompt text, never a secret, and is capped at 8 KiB.

### Routine scheduling (cron = fresh agent)

Enabled routines with an `every <duration>` trigger (e.g. `every 5m`, `every 2h`) fire on a 30s scheduler tick. Each fire is a **fresh** flocode child with the skill body prepended — no state carries between fires. Enabling a routine never fires it immediately; the first fire comes one interval after enable. Fires publish a `routine_fire` event on `/api/stream`.

Packages: `internal/routes`, `internal/store`, `internal/executor`, `internal/server`, `internal/amesh`, `internal/schedule`, `cmd/flobot`, `web/`.

---

# Phase 1 analysis — Agents-rail harness plugin (amesh-web)

**Project Status (amesh plugin v1):** Shipping (unreleased, flag-gated, in-memory alpha)  
**PR:** #1062  
**Commit:** fa62d64a  
**Component:** `amesh-web` (webv2)  
**Rail:** Agents  
**Release Timeline:** 2026.07.13.1+  

Amesh Web v2 shipped the roster with **no model dispatch**. Phase 2 execution lives in this standalone repo (and a sibling amesh PR). The analysis below is the v1 plugin contract.

---

## Executive Summary

**Flobot** is a signed plugin on the **Agents** rail in Amesh Web v2. It is a multi-agent team-orchestration harness with:

- **Named teammates** (bots) with pinned model routes
- **Reviewable skills** (how-to playbooks)
- **Routines** (paused-by-default, test-run-before-enable automation)
- **Handoff bus** (single-owner-per-stage message queue for work items)
- **Built-in routes** (GLM orchestrator, Qwen coder, Muse planner, local tools executor)

**Critical design constraint:** Everything is **in-memory only**—nothing persists across `amesh-web` restart. Amesh plugin v1 was roster metadata only. Phase 2 execution is this repo (`cmd/flobot serve`) exec'ing flocode2.

---

## Architecture & Components

### 1. Core Store: `go/internal/helper/flobot.go`

The `FlobotStore` is the in-memory authority for the v2 plugin. It manages four entity types:

#### **FlobotBot** — Named Teammate
- **ID:** Unique identifier (prefixed "bot-")
- **Name:** User-friendly name ("Chief", "Coder", etc.)
- **Role:** Semantic role ("chief-of-staff", "coder", "specialist")
- **Route:** Model route ID ("muse", "glm", "qwen", "local")
- **Memory:** Short role note (not another agent's private store)
- **Owner:** Session ID (pairing_session, json:"-" for privacy)
- **CreatedAt:** RFC3339 timestamp

**Invariants:**
- One owner per bot (no sharing between pairing sessions)
- Owner field never leaks to JSON (json:"-")
- Route must exist in `DefaultFlobotRoutes()`
- Name is trimmed and required

**Methods:**
- `CreateBot(owner, name, role, route)` → FlobotBot, error
- `ListBots(actor)` → []FlobotBot (filtered by owner)
- `Remember(id, actor, note)` → FlobotBot, error (update bot's Memory field)
- `ownedBot(id, actor)` → FlobotBot, error (private: ownership check)

#### **FlobotSkill** — Reviewable How-To
- **ID:** Unique identifier (prefixed "skill-")
- **Name:** Skill name
- **Body:** Markdown or plaintext description
- **Version:** Version counter (starts at 1)
- **Owner:** Session ID (json:"-")

**Design notes:**
- Never live from a raw recording—must be manually written/reviewed
- No execution in this release—just metadata
- Version for future diffing/changelog support

**Methods:**
- `CreateSkill(owner, name, body)` → FlobotSkill, error
- `ListSkills(actor)` → []FlobotSkill (filtered by owner)

#### **FlobotRoutine** — Paused Automation Rule
- **ID:** Unique identifier (prefixed "rtn-")
- **BotID:** Bot that runs this routine
- **SkillID:** Skill being invoked
- **Trigger:** Trigger description ("manual", "on_message", etc.)
- **Paused:** Always true on creation; enable requires test-run
- **LastRun:** RFC3339 timestamp (empty until test-run)
- **LastNote:** Result/status from last run
- **Owner:** Session ID (json:"-")

**Safety model:**
- New routines always start `Paused: true`
- Test-run is required before enable (guard against unreviewed automation)
- Enable fails if `LastRun == ""`

**Methods:**
- `CreateRoutine(owner, botID, skillID, trigger)` → FlobotRoutine, error
- `ListRoutines(actor)` → []FlobotRoutine
- `TestRun(id, actor)` → FlobotRoutine, error (sets LastRun, LastNote)
- `EnableRoutine(id, actor)` → FlobotRoutine, error (clears Paused)

#### **FlobotMessage** — Work Item Bus
- **ID:** Unique identifier (prefixed "msg-")
- **From:** Sender bot ID
- **To:** Recipient bot ID or "@all" for broadcast
- **Group:** Optional grouping (for filtering/batching)
- **Body:** Work item description
- **OwnerBot:** Single-owner-per-stage invariant
- **CreatedAt:** RFC3339 timestamp

**Bus semantics:**
- `OwnerBot` encodes **single ownership per stage**: at most one bot owns a work item at a time
- Handoff changes ownership; Message sets owner to recipient (or sender if broadcast)
- Broadcast (`To: "@all"`) is not allowed for handoff (no single owner for a broadcast)
- Messages persist to `~/.flobot/state.json` (newest 500 kept)

**Methods:**
- `Message(owner, from, to, group, body)` → FlobotMessage, error
- `Handoff(owner, from, to, body)` → FlobotMessage, error
- `ListMessages(actor)` → []FlobotMessage (filtered: visible if actor owns any bot involved)
- `appendMessage(owner, from, to, group, body, ownerBot)` → FlobotMessage, error (internal)

**Critical bug fix (PR #1062):**
The original `Handoff` released the store lock after `append`, then rewrote `s.messages[len-1]` to update `OwnerBot`. A concurrent `Message` in the gap moved the slice index, so the rewrite clobbered the wrong message (~9% reproduction rate in a 2-writer race test).

**Fix:** Build the message with its final `OwnerBot` before the lock, append exactly once under one critical section.

### 2. Default Routes: Documented Model Combination

- **GLM (Orchestrator):** Amesh mesh deployment of GLM 5.2 FP8; coordinates team flow
- **Qwen (Coder):** Qwen 3.8 27B FP8 on a mesh node; repo-aware coding
- **Local (Tools):** MLX-quantized Qwen 3-coder on this Mac; read-only system calls
- **Muse (Chief-of-Staff):** Meta's Muse Spark 1.2 via their API; 1M-context planning; **API key never leaves this Mac, never appears in JSON**

### 3. Web API: `go/cmd/amesh-web/flobot.go`

HTTP handlers for Agents pane and API consumers. All mutating routes require:
1. Valid `X-Amesh-Pairing-Session` header
2. Prior install via `POST /api/v2/plugins/install` with signed digest

**Key endpoints:**
- `GET /api/v2/plugins/flobot` — Full roster snapshot
- `POST /api/v2/plugins/flobot/bots` — Create bot
- `POST /api/v2/plugins/flobot/handoff` — Handoff work
- `POST /api/v2/plugins/flobot/skills` — Create skill
- `POST /api/v2/plugins/flobot/routines` — Create routine
- `POST /api/v2/plugins/flobot/routines/{id}/test-run` — Test routine
- `POST /api/v2/plugins/flobot/routines/{id}/enable` — Enable routine

### 4. Catalog Integration

**Placeholder digest (same contract as Gmail):**
`sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee`

**Catalog entry:**
- ID: "flobot"
- Rail: "Agents"
- Kind: "plugin"
- Digest: FlobotCatalogDigest
- Signed: true

**Plugin ordering:** Gmail always first in catalog; flobot is second (Agents rail).

### 5. Web v2 UI: `webv2/src/App.tsx` + React Components

**State in App component:**
```
- flobotBots: FlobotBot[]
- flobotRoutes: FlobotRoute[]
- flobotMessages: FlobotMessage[]
- flobotDraft: string (default: "Chief")
- flobotError: string
```

**Key functions:**
- `refreshFlobot()` — Fetch current roster
- `onInstallFlobot()` — Install flobot plugin
- `onCreateFlobotBot()` — Create new bot
- `onHandoffFlobot()` — Send work handoff

---

## Design Decisions & Rationale

### In-Memory Store
**Decision:** No persistence; everything clears on `amesh-web` restart.
**Rationale:** Flobot v1 is an experiment for multi-agent patterns; persistence adds burden; sessions are ephemeral.

### No Execution (amesh v1)
**Decision:** Amesh plugin v1 routes, routines, and test-run were **roster metadata only**.
**Rationale:** Execution is Phase 2: this standalone module spawns `flocode` and streams NDJSON. The amesh plugin host duplicates a thin `os/exec` wrapper in a sibling PR so amesh does not import this module.

### Single-Owner-Per-Stage Bus
**Decision:** Every message has an `OwnerBot` field; handoff moves ownership.
**Rationale:** Prevents work item ambiguity; enables handoff semantics; simpler than multi-owner graphs.

### Pairing-Session Isolation
**Decision:** Each `pairing_session` has its own bots, skills, routines, messages.
**Rationale:** Web v2 architecture; no cross-session leakage; operator privacy.

### API-Key Privacy (Muse Route)
**Decision:** Muse Spark 1.2 API key never leaves this Mac; never appears in plugin JSON.
**Rationale:** Muse is deployed outside mesh; browser could be intercepted.

---

## Testing

### Unit Tests: `go/internal/helper/flobot_test.go`

1. **TestFlobotInstallRequiresSignedCatalogDigest** — Digest mismatch/unsigned → fails
2. **TestFlobotIsAgentsPluginSecondAfterGmail** — Catalog ordering verified
3. **TestFlobotRosterOwnershipAndNoSecrets** — Ownership checks, JSON privacy
4. **TestFlobotHandoffDoesNotClobberConcurrentMessages** — **Race condition: concurrent Message + Handoff (500 iterations)**
5. **TestFlobotHandoffRejectsBroadcast** — Handoff to `@all` fails
6. **TestFlobotMessagesAreInvisibleToStrangers** — Cross-session isolation
7. **TestDefaultFlobotRoutesDocumentTheCombination** — All 4 routes present

### Integration Tests: `go/cmd/amesh-web/flobot_test.go`

1. **TestWebPluginsAPIIncludesFlobotOnAgentsRail** — Catalog endpoint
2. **TestFlobotBotsRequireInstallAndPairing** — 403 before install
3. **TestFlobotHandoffAndRoutineTestRun** — Full workflow

---

## Known Limitations & Follow-Up Work

### v1 (This Release)
- [x] Roster management
- [x] Handoff bus
- [x] Session isolation
- [x] UI for roster + handoff
- [x] Execution (Phase 2, this repo: `feat/execution-plugin`)
- [ ] Persistence (Phase 2)
- [ ] Real signing (placeholder digest)
- [ ] Skills & routines UI (API-only)

### Phase 2 / Future
1. Execution layer (dispatch to models, capability routing, streaming)
2. Persistence (PostgreSQL schema, audit trail)
3. Advanced routing (fallback routes, dynamic selection)
4. UI completeness (skill viewer, routine scheduler, thread view)
5. Security (routine signing, audit log, capability delegation)

---

## File Map

### Backend (Go)
| File | Purpose |
|------|---------|
| `go/internal/helper/flobot.go` | FlobotStore, entity types, business logic |
| `go/internal/helper/flobot_test.go` | Unit tests |
| `go/cmd/amesh-web/flobot.go` | HTTP handlers |
| `go/cmd/amesh-web/flobot_test.go` | Integration tests |

### Frontend (TypeScript/React)
| File | Purpose |
|------|---------|
| `webv2/src/App.tsx` | State, effects, handlers |
| `webv2/src/lib/types.ts` | Type definitions |
| `webv2/src/lib/api.ts` | API utilities |

### Documentation
| File | Purpose |
|------|---------|
| `docs/operator/flobot.md` | User guide |
| `CHANGELOG.md` | Release notes |

---

## PR Summary (#1062)

**Author:** Florian Leibert  
**Merged:** 2026-08-19  

**Commits:**
1. feat(webv2): add flobot as the signed Agents harness plugin
2. fix(webv2): append handoff bus messages once under the store lock
3. fix(webv2): surface flobot server errors and reload the roster
4. fix(webv2): render flobot install failures where the user is looking

---

## How to Use Flobot (Quick Start)

```bash
# 1. Install flobot
POST /api/v2/plugins/install
{"id": "flobot", "digest": "sha256:eeee...", "signed": true}

# 2. Create a bot
POST /api/v2/plugins/flobot/bots
{"name": "Chief", "role": "chief-of-staff", "route": "muse"}

# 3. Create a skill
POST /api/v2/plugins/flobot/skills
{"name": "sysinfo", "body": "Read-only uname and sysctl"}

# 4. Create a routine (starts paused)
POST /api/v2/plugins/flobot/routines
{"bot_id": "bot-xxx", "skill_id": "skill-yyy", "trigger": "manual"}

# 5. Test-run
POST /api/v2/plugins/flobot/routines/rtn-zzz/test-run

# 6. Enable
POST /api/v2/plugins/flobot/routines/rtn-zzz/enable

# 7. Send a handoff
POST /api/v2/plugins/flobot/handoff
{"from": "bot-aaa", "to": "bot-bbb", "body": "count Go files"}

# 8. View messages
GET /api/v2/plugins/flobot
```

---

## Glossary

| Term | Definition |
|------|-----------|
| **Flobot** | Signed Agents-rail plugin for multi-agent team orchestration |
| **Bot** | Named teammate with a pinned model route |
| **Skill** | Reviewable how-to playbook |
| **Routine** | Automation rule (paused-by-default, test-run-before-enable) |
| **Handoff** | Work-item transfer between bots |
| **Message** | Bus item (work description, sender, recipient, owner) |
| **Route** | Model endpoint + role (e.g., GLM orchestrator) |
| **Pairing Session** | Bearer token for browser session; isolates roster |
| **Single-Owner-Per-Stage** | Invariant: every message has exactly one `OwnerBot` |
| **In-Memory Store** | Everything lost on restart; no PostgreSQL yet |
| **Placeholder Digest** | Catalog checksum (sha256:eeee...) pending signing |
| **Web v2** | Amesh's React-based web UI |
| **Agents Rail** | Plugin category (vs. Mail, FloCode, etc.) |

---

**Last Updated:** 2026-08-19  
**Status:** Ready for review, integration, and Phase 2 planning
