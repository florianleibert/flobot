# flobot — 10 Killer Use Cases

Not "what flobot could do." What flobot does *better than anything else on
your machine*, because it is **local-first, durable, and mesh-connected** —
an agent stack that lives in your repo, survives restarts, and never phones
home unless you say so.

Grounded in the real stack: SPEC.md capabilities C1–C6, the executor's
intent/verify/yolo/worktree mechanics, handoffs, teammate memory, the
scheduler/routines, and amesh mesh-model routing. The web UI lives at
`http://127.0.0.1:4120`.

---

## 1. The overnight repo surgeon

**You:** "Fix the flaky test in `internal/store`, then run the full suite."
**flobot:** works in an isolated worktree, keeps your main checkout clean,
runs the suite in verify mode, and leaves a diff + ledger. You wake up to a
reviewable patch, not a half-broken working tree.

*Why it kills:* durable environment + worktrees + verify/yolo = unattended
surgery with a clean landing zone. No cloud VM, no "did it actually run?"

## 2. Parallel specialists that never talk over each other

**You:** spawn `reviewer`, `tester`, `docs-writer` on one PR.
**flobot:** each is a durable named agent with its own memory and role; they
run concurrently, and handoff is an explicit, logged event — single owner per
stage, no two agents clobbering the same file.

*Why it kills:* this is the Grok Bot "bot → bot crew" shape, but local and
inspectable. The ledger shows exactly who owned what, when.

## 3. Teach-by-demo → one-command skills

**You:** do a release workflow once.
**flobot:** captures the ledger trace, compiles it into an editable skill
artifact (steps, inputs, tool calls, assertions), you test-run and publish.
Next release is one command.

*Why it kills:* your tribal knowledge stops living in a wiki and starts
living in a re-runnable, versioned artifact on disk.

## 4. Cron = fresh agent every morning

**You:** "Every 9am, run the standup-summary skill."
**flobot:** scheduler binds skill → named agent → cron; each fire is a fresh
agent run with its own ledger; test-run before enabling, pause/resume
anytime, retained run history.

*Why it kills:* scheduled automation that is inspectable and replayable —
not a black-box cron that silently breaks.

## 5. Event-triggered automation with narrow rules

**You:** "When `CHANGELOG.md` changes, run release-notes."
**flobot:** git-hook / file-watch / webhook connectors fire the routine; match
rules are narrow and explicit — no "run on anything."

*Why it kills:* automation that fires on *your* events, locally, with no
cloud middleware between the trigger and the work.

## 6. Teammate memory that actually persists

**You:** "Remember: we use table-driven tests and never `t.Skip`."
**flobot:** that preference lives in the agent's durable memory and shapes
every later task — per-agent, shared only via environment or explicit
handoff.

*Why it kills:* the agent gets *smarter about you* over time, on your disk,
not in some vendor's cloud profile.

## 7. The private assistant with zero cloud

**You:** run flobot entirely against local models / mesh identity.
**flobot:** memory, skills, routines, ledgers all live on your machine; no
external API call; offline degradation is graceful — local agents keep
working with no network.

*Why it kills:* an agent stack with real capability that does not require
you to hand your code to a third party. This is the differentiator.

## 8. The consequential-action gate

**You:** delegate a task that touches writes-outside-repo, sends, deletions,
or account mutations.
**flobot:** blocks for explicit Allow/Deny; decision logged with agent,
action, surface; pending approvals time out to **deny**.

*Why it kills:* you can let an agent be powerful *because* it cannot act
consequentially without you — trust by construction, not by hope.

## 9. Mesh-model routing: cheap triage, strong reasoning

**You:** route triage to a small local model, hard reasoning to a big one.
**flobot:** amesh mesh routing pins models per route/agent via web UI + HTTP
API — tune cost vs. quality per agent without changing your workflow.

*Why it kills:* you get frontier-grade reasoning where it matters and cheap
local inference everywhere else, in one app.

## 10. The always-on local crew (graceful offline)

**You:** leave flobot running on your machine (or an opted-in remote host).
**flobot:** durable agents keep working when you're away; routines fire on
schedule; everything degrades gracefully offline; remote sandbox is an
explicit opt-in escape hatch, never a requirement.

*Why it kills:* "always-on" without the always-on cloud bill or the data
residency compromise.

---

## The one-line pitch

> flobot is the local-first Grok Bot analogue: durable named agents,
> teach-by-demo skills, scheduled/event routines, per-agent memory, and a
> consequential-action approval gate — all running against your own mesh of
> models on your own disk.

## Scope honesty

- Built today: executor intents, verify/yolo modes, worktrees, handoffs,
  teammate memory, mesh-model routing (README).
- Spec-level gaps (flagged in SPEC.md): GUI-capable remote sandbox (C1.3),
  productized scheduler + event-connector registry (C4), ledger→skill
  compiler (C3.2).
