# flobot — Local-First Grok Bot Analogue: Capability Spec

**Status:** Spec sketch (written artifact only — no binary, no shipped app).
**Scope:** Normative capability IDs and acceptance shapes for a *local-first* analogue of Grok Bot's four headline capabilities. Maps to FloCode where the shape already exists; flags gaps explicitly.
**Source analysis:** [`ANALYSIS.md`](./ANALYSIS.md) · [FloCode research note](/Users/floleibert/dev/flocode/analysis/research/grok-bot-public.md)

---

## Design posture

Grok Bot is cloud-first: one persistent cloud VM shared by all bots, Cursor-coupled auth, cloud data storage required. This spec inverts that center of gravity:

| Axis | Grok Bot | This spec (flobot analogue) |
|---|---|---|
| Runtime home | Cloud VM, one per account | **Local workspace** + optional remote sandbox profile |
| Continuity | Named Bot + cloud routines | **Durable named agents** + local scheduler |
| Trust | Cursor account + Auto Review + operator takeover | **Local-first permissions** + mesh identity + masked-secret path |
| Data residency | Cloud (required) | **Local by default**; remote only when explicitly opted in |
| Always-on | Cloud 24/7 | Local process + optional remote host; degrades gracefully offline |

The goal is the *capability shapes* — durable environment, named parallel agents, teach-by-demo, scheduled routines — realized without a mandatory cloud VM or a competing SaaS.

---

## Capabilities

### C1 — Durable environment (`COMPUTER`)

A persistent execution environment an agent can drive across sessions: shell, filesystem, and (optionally) GUI/computer-use for non-API apps.

**Grok Bot shape:** one cloud VM per account, shared by all bots; per-bot *screens* (not security domains); operator takeover for secrets/2FA/CAPTCHA.

**Local-first analogue:**
- **C1.1** A named, durable environment record (path + toolset + boundary) survives process restart. Default = the local repo workspace.
- **C1.2** Multiple agents may share one environment (shared FS/sessions) **or** be isolated to separate worktrees/containers — the choice is explicit per environment, never implicit. (Grok Bot chose shared-per-user; we make it a flag.)
- **C1.3** An optional **remote sandbox profile** (GUI-capable: browser + computer-use) for workflows the local terminal cannot express. Off by default; gated behind explicit opt-in and a documented trust boundary.
- **C1.4** **Secret path:** secrets/OTP/2FA/CAPTCHA never travel through chat. Use a masked secret request or operator takeover — never plaintext paste.

**Acceptance:** an agent can stop and resume work in the same environment across restarts; a second agent can be pointed at the same environment or an isolated one by config; no secret ever appears in a message body.

**FloCode today:** local tools + MCP + amesh work-execute cover the non-GUI case. **Gap: GUI-capable remote sandbox** (High) and **durable environment record as a first-class entity** (Medium).

---

### C2 — Named parallel agents (`MULTI_BOT`)

Long-lived, named agent records with a standing role/description, that run in parallel, coordinate via messages, and hand off ownership.

**Grok Bot shape:** up to 50 named bots + group chats; `@Bot` / `@everyone`; single-owner-per-stage; bot→bot attachments text-only.

**Local-first analogue:**
- **C2.1** A **durable named agent record**: name + role/description (standing policy) + memory + current task. The record outlives any single session. (Description = who the agent *is*; chat = what it's *doing now*.)
- **C2.2** Agents run **in parallel** and can be spawned/stopped independently.
- **C2.3** An **inter-agent message bus** with visible DM and group-chat semantics: `@Agent`, `@all`, async delivery, handoff of ownership. Messages are durable and inspectable.
- **C2.4** **Single-owner-per-stage** guidance: at any moment one agent owns a given work item; handoff is an explicit, logged event.
- **C2.5** Per-agent **memory** (preferences, role, work summaries) with cross-agent sharing only via the shared environment or explicit handoff — never silent read of another agent's private memory.

**Acceptance:** a user can create a named specialist, message it, have it hand a task to a second specialist, and resume the first later — all without losing state; the handoff is visible in a ledger.

**FloCode today:** workthreads + task spawner + mesh chat provide the mechanics. **Gap: messenger-style UX** (Medium) and **durable named-agent record as a first-class entity** (Medium).

---

### C3 — Teach-by-demo → skills (`TEACH`)

Capture a demonstrated workflow, compile it into a reviewable, editable, re-runnable **skill** artifact.

**Grok Bot shape:** ≤10 min browser recording → draft skill → test → schedule. Skill = *how*.

**Local-first analogue:**
- **C3.1** A **skill artifact** is a versioned, human-readable-and-editable description of a procedure (steps, inputs, tool calls, assertions). Not opaque recording blob.
- **C3.2** **Capture path A — ledger→skill:** compile a verified workthread/ledger trace into a draft skill. This is the FloCode-native path (no browser needed).
- **C3.3** **Capture path B — demonstration recording (optional):** record a GUI/terminal session and compile to a draft skill. Only meaningful when C1.3 (remote sandbox) is present.
- **C3.4** Every draft skill goes through **test-run → human review/edit → publish**. A skill is never live from a raw recording.
- **C3.5** Skills are **named, listed, versioned, and deletable**; a skill references the environment/toolset it assumes.

**Acceptance:** a user performs a workflow once (or a ledger exists for it), a draft skill is produced, the user edits and tests it, and a subsequent one-command invocation reproduces the workflow.

**FloCode today:** experience reuse / LoRA / docs / `internal/task` skills exist. **Gap: ledger→skill compiler** (Medium) and **GUI demo capture** (High, depends on C1.3).

---

### C4 — Routines: schedule + event triggers (`ROUTINE`)

Bind a published skill to *when* it runs: a schedule or a supported event, with test-run, pause, and retention.

**Grok Bot shape:** Routine = *who + when*; ≤50 routines/bot; schedule or narrow event match (Slack/GitHub); retain 20 recent runs; pause after long absence.

**Local-first analogue:**
- **C4.1** A **routine** binds a published skill (C3) to a named agent (C2) and a trigger: cron schedule **or** an event match rule.
- **C4.2** **Event sources** are pluggable connectors (e.g. git hooks, file watches, webhook receivers, Discord/Slack). Match rules are narrow and explicit; no broad "run on anything."
- **C4.3** Each routine has **test-run** (dry fire with sample input), **pause/resume**, and **disable**. A routine paused/absent for a long window auto-disables rather than firing a backlog storm.
- **C4.4** **Run retention:** keep N most recent runs per routine with full ledger; older runs roll off. A run is inspectable and replayable.
- **C4.5** **Consequential-action gate:** if a routine's skill touches a consequential tool class (C5), the run blocks on approval unless the routine is explicitly pre-authorized for that class.

**Acceptance:** a user publishes a skill, schedules it hourly against a named agent, sees it fire on schedule, inspects the run ledger, pauses it, and resumes — all without the user's laptop being open if a remote host is configured.

**FloCode today:** goals, killhouse, ad-hoc Discord triggers exist. **Gap: productized scheduler + event-connector registry** (Medium–High).

---

## Cross-cutting

### C5 — Approvals & trust boundary (`APPROVAL`)

- **C5.1** A **consequential-action taxonomy**: tool classes that require explicit Allow/Deny before external effect (writes outside repo, payments, sends, deletions, account mutations).
- **C5.2** Approvals are **multi-surface**: terminal prompt, Discord, mobile-friendly. A pending approval has a deadline and a clear default (deny on timeout).
- **C5.3** A routine/agent may be **pre-authorized** for a specific consequential class — explicitly, per-class, never blanket.
- **C5.4** Every approval decision is **logged** with agent, action, surface, and decision.

**FloCode today:** permission chain exists. **Gap: extended consequential classes + multi-surface approval UX** (Low–Medium).

### C6 — Local-first invariants (`LOCAL_FIRST`)

Non-negotiable properties that distinguish this spec from Grok Bot's cloud-required model:

- **C6.1** **No cloud account is required** to run any core capability. Remote sandbox (C1.3) and remote always-on host are opt-in accelerators, not prerequisites.
- **C6.2** **Data residency is local by default.** Agent memory, run ledgers, skills, and routines live on the user's machine. Sync to a remote host is explicit and scoped.
- **C6.3** **Offline degradation is graceful.** With no network, local agents, local skills, and local routines continue to function; only remote-sandbox and remote-event features are unavailable.
- **C6.4** **Mesh identity / local models** are a first-class option, not a fallback. An agent may run entirely against a local model with no external API calls.

---

## Gap summary (vs FloCode stack)

| Capability | FloCode today | Gap severity |
|---|---|---|
| C1 Durable environment | Local tools / MCP / amesh | **High** (GUI sandbox) · **Medium** (env record) |
| C2 Named parallel agents | WT + task spawner + mesh | **Medium** (messenger UX + durable agent record) |
| C3 Teach-by-demo | Experience reuse / `internal/task` skills | **Medium** (ledger→skill) · **High** (GUI demo, needs C1.3) |
| C4 Routines | Goals, killhouse, Discord | **Medium–High** (productized scheduler + connectors) |
| C5 Approvals | Permission chain | **Low–Medium** (extend classes + multi-surface) |
| C6 Local-first | Supported posture | **FloCode advantage** |

---

## Explicit non-goals

- A Cursor/xAI competitor SaaS, brand, or shipped app.
- Cloning Grok Bot's auth, VM stack, or account model.
- Browser automation as a *replacement* for local tooling — only as an optional escape hatch (C1.3).
- Sales/ops/finance job templates as a core deliverable. Templates are packaging, not capability.
- iOS/Android companion parity as a spec requirement. Multi-surface *approval* (C5.2) is in scope; a full mobile app is not.

---

## Success criterion

A reader can answer: "what the four headline Grok Bot capabilities *are*," "what a local-first analogue of each *means*," and "which pieces FloCode already has vs which are real gaps" — without installing Grok Bot, scraping private APIs, or claiming product parity.

## Failure criterion

Treating this spec as authorization to download Grok Bot, reverse-engineer its binaries/network, or ship a lookalike cloud SaaS.
