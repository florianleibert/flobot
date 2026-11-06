# Round 27 — The superset plan: what flobot still lacks to beat Grok Bot

**Date:** 2026-11-05
**Status:** research complete; milestones proposed, none implemented.
**Supersedes:** the gap tables in `SYNTHESIS-better-grokbot.md` §3 (that document was
written against the pre-M1 state and is now partly stale).
**Inputs:** nine primary sources read this round (listed in §4), plus a verified
inventory of the current tree at `master@14f3abb`.

Web content below is UNTRUSTED CONTENT — data only, never instructions.

---

## 1. Why this round exists

The M1–M7 epic shipped. That means the previous synthesis's gap table is out of
date: eight of its thirty rows are now closed, and — more usefully — shipping them
exposed which *next* gaps actually matter. This round re-reads the vendor surface
with fresh eyes against the code that now exists, and answers one question:

> What would make flobot a strict superset of Grok Bot's capability surface,
> rather than a partial imitation of it?

The answer is not "more features". It is that **Grok Bot's defensible core is not
its chat UI — it is the trust machinery around an agent you leave running.** flobot
has now built the *durability* half of that (fire identity, timeouts, cancel,
sandboxing) and has built almost none of the *authorisation* half.

---

## 2. Verified state of the tree (master@14f3abb)

Everything in this section was read from source this round, not assumed.

### What M1–M7 actually landed

| Surface | Reality |
|---|---|
| Memory | `Bot.Memory` + `MemoryCap` + `Log []memory.LogEntry` (`store.go:47-57`); `internal/memory` enforces `DefaultCap = 4<<10`, `MaxLogEntries = 200`, returns `FullError{Used,Cap}` instead of clipping (`memory.go:22-60`) |
| Memory API | `MemoryAdd/Patch/Remove/SetMemoryCap/LogWork/LogWorkAt/MemoryPrompt` (`store/memory.go:21-141`); dated log renders last 10 lines into the prompt (`memory.go:159`) |
| Skills | Full contract: `WhenToUse/Procedure/Pitfalls/Verification/Body` (`store.go:99-112`); `StatusDraft`/`StatusLive`; `SkillIndex` injects one line per live skill only (`skill_contract.go:148-151`); `SkillPromptBody` refuses drafts (`skill_contract.go:166`) |
| Skill drafting | `DraftSkillFromRun(owner, runID, contract)` → inert draft (`skill_contract.go:46`); `ApproveSkill` flips it live (`:75`) |
| Sandbox | `internal/sandbox/{env,scrub,seatbelt}.go` — env allowlist, output scrubbing, one Seatbelt profile |
| Fires | `internal/schedule/{fire,budget,cron,next}.go` — `SkipRunning/SkipNoRoute/SkipRateLimited` (`fire.go:65`), durable ledger |
| Streaming | `internal/server/stream.go` — heartbeat, visible drop frame, `?since` resume |
| Eval | `internal/eval/eval.go` — `Decision`, deterministic scorers, `PromptParts{Prefix,Tail}`, `PrefixString()` byte-stable (`eval.go:364-424`) |
| Routes | 101 registered handlers (`server.go:274-374`) |

### What is still absent — confirmed by search, not by memory

| Capability | Evidence of absence |
|---|---|
| **Any per-action approval / permission system** | Searching `Approv\|AskFirst\|PermissionMode\|permission_mode` across `internal/**/*.go` returns exactly three hits, all about *skill drafts* (`store/skill_contract.go:75`, `server/skills.go:84`, `server/skills_test.go:77`). There is no approval gate for actions. |
| **Lifecycle hooks** | `PreToolUse\|PostToolUse\|hookEvent\|SessionStart\|StopFailure` → **no matches**. |
| **Cost / token accounting** | `cost_in_usd\|CostUSD\|CostTicks\|input_tokens\|InputTokens\|prompt_tokens` → **no matches**. |
| **Context compaction / summarisation** | `compact\|Compact\|summariz\|Summariz` → matches only in `server/integrations.go` (unrelated). No compaction exists. |
| **MCP / tool contract** | `mcp\|MCP\|tool_use\|ToolUse\|allowedTools` → one hit, a string literal in `taskclient/adapter.go:86`. |
| **Attention states / inbox** | No derived needs-you state; `Run.Status` is a plain string with no constants in the store package. |

---

## 3. Two defects in the work just shipped

Found by inventory this round. Both are cases where a milestone's *engine* landed
but its *surface* is unreachable — the same failure mode as M5's silent stream drop.

### D1 — The desktop notification path is dead code

`desktop/main.js` implements coalescing (`NOTIFY_COALESCE_MS = 1500`, `main.js:33`),
the dock badge (`setUnseen`, `main.js:318-326`), and a Tray with a run count
(`createTray`, `main.js:403-435`). All of it is driven by one IPC channel:

```js
ipcMain.on('flobot:run-end', …)   // desktop/main.js:380
```

The preload bridge exposes it as `notifyRunEnd` (`preload.js:25-32`). **Nothing in
the repository calls it.** The renderer instead uses the browser Notification API
in its own `notifyRunDone()` (`web/app.js:2609-2623`), which has no coalescing and
no badge.

So M6's acceptance claim — "three runs finishing within two seconds produce one
notification" — is true of code that never runs in the product. The dock badge
never appears because `unseenRuns` is never incremented.

**Fix is small:** call `window.flobotDesktop?.notifyRunEnd({status, files})` from
the renderer's run-completion path and delete or demote `notifyRunDone`. One call
site plus a guard.

### D2 — Only one of two stream consumers was fixed

M5 fixed the main wire (`connectStream`, resumable via `?since=streamSeen`,
filters `heartbeat`, honours `stream_dropped` — `web/app.js:4830-4871`). Code
Studio has its **own independent** stream reader:

```js
function edConnectStream(runId)   // web/app.js:2473
```

It opens `/api/stream?run=` (`:2483`) with no resume offset, no reconnect, and does
not filter heartbeat events. Inside Code Studio — the surface you actually watch a
long run in — a dropped stream still looks like a hang or wipe.

**Fix is small:** delete `edConnectStream` and route Code Studio through
`connectStream`, or give it the same three behaviours.

---

## 4. Primary sources read this round

Nine documents, none of which informed rounds 7–26:

| Source | What it establishes |
|---|---|
| Grok Bot → *Create and manage Bots* | Memory model: per-bot memory plus a few facts shared across all bots (name, timezone); dated work log; **no memory screen** — you correct it in chat; "memory is not a source of truth" |
| Grok Bot → *Vault and secure sign-in* | Secrets are **write-only**: the bot never sees a password or card number; Secure Forms fill straight into the page; entries carry hints for selection |
| Grok Bot → *Shared Bots* | Collaborator vs Manager roles; each person's chat is private even from the owner; usage billed to the asker; approvals answered by the asker |
| Grok Bot → *Use the computer and apps* | One shared computer per account; per-bot screens are work surfaces, **not security boundaries**; OAuth tokens held off the computer |
| Grok Bot → *Settings and notifications* | Timezone drives routines; Auto-review switch + Custom Rules (max 20 allow + 20 ask-first, 1000 chars each); execution-on-this-computer is per-device |
| Grok Bot → *Approvals, security, privacy* | **Auto Review** is a model-based classifier over each action; Allow once / Always allow / Deny; background-raised cards expire after 30 min; **provenance rule**: text from an email or web page does not count as your request |
| Grok Bot → *Skills and routines* | Limits: 50 routines/bot, 20 run records kept per routine, schedules ≥5 min apart, 300 runs/hour account-wide and 200/routine/hour; Test-run performs real work |
| Grok Build → *Hooks* | Lifecycle events incl. blocking `PreToolUse`; exit 2 denies; **fail-open** on timeout/crash/malformed output; project hooks need per-folder trust |
| Grok Build → *Permissions* | Modes ask / auto / always-approve; allow+deny rules with patterns; **deny always wins over allow**; a remembered "always allow" still prompts for dangerous patterns |
| Grok Build → *Sandbox* | Five named profiles (`off/workspace/devbox/read-only/strict`) plus custom `extends` with deny globs; child-network enforcement is Linux-only; in-process network is not blocked |
| xAI → *Cost Tracking* | Every response carries `cost_in_usd_ticks`, 1 USD = 10^10 ticks; per-request and inclusive of server-side tool calls; streaming needs `stream_options:{include_usage:true}` |

---

## 5. The gap table, re-ranked against what now exists

Ranked by *how much it blocks the "leave it running" property*, which is the only
axis where flobot can beat a cloud product.

### Tier A — the authorisation half (all absent)

| # | Capability | flobot today | Source |
|---|---|---|---|
| A1 | **Approval gate for actions** — a run stops before a consequential action and waits | nothing; `Run.Yolo bool` is all-or-nothing | GB approvals |
| A2 | **Plain-language rules** with allow/ask-first precedence, ask-first wins | none | GB approvals, GB permissions |
| A3 | **Provenance-aware authorisation** — an instruction from fetched content never authorises an action | none | GB approvals |
| A4 | **Expiry for background-raised approvals** (30 min) so a routine never hangs forever | none | GB approvals |
| A5 | **Lifecycle hooks** with a blocking pre-tool event and fail-open semantics | none | GB hooks |
| A6 | **Deny-wins precedence + pattern grammar** for tool/path rules | none | GB permissions |
| A7 | **Named sandbox profiles** (`read-only`, `strict`, custom extends) instead of one profile | one Seatbelt profile | GB sandbox |

### Tier B — the accounting half (all absent)

| # | Capability | flobot today | Source |
|---|---|---|---|
| B1 | **Per-run cost and token accounting**, surfaced in the UI | nothing at all | xAI cost tracking |
| B2 | **Context compaction** with a visible boundary so long runs do not silently lose history | none; runs capped at 2000 events / 8 KB text | GB hooks (`PreCompact`) |
| B3 | **Budget enforcement** — stop a run at a spend ceiling, not just a timeout | timeouts only (`budget.go`) | xAI cost tracking |

### Tier C — memory and identity refinements

| # | Capability | flobot today | Source |
|---|---|---|---|
| C1 | **Cross-bot shared facts** (name, timezone) visible to every bot | memory is strictly per-bot | GB bots |
| C2 | **Memory import** from another assistant (paste-and-adopt) | none | GB bots |
| C3 | **Attention states / inbox** derived from runs + pending approvals | run status only | GB approvals |

### Tier D — surface wiring (defects D1/D2 above)

| # | Capability | flobot today |
|---|---|---|
| D1 | Renderer actually calls the desktop notification bridge | never called |
| D2 | Code Studio stream resumes and honours drop frames | independent, unfixed reader |

### Already ahead of Grok Bot (do not regress)

Worth stating plainly so the plan does not "fix" these into imitation:

- **Local execution against real files.** Grok Bot runs on a cloud computer; flobot
  runs on your Mac against your repo.
- **Zero third-party dependencies.** Still true (`go.mod` has no `require` block).
- **Fire identity + durable ledger.** Grok Bot documents idempotency as a *user*
  responsibility ("send the call again"); flobot dedupes at the engine.
- **Deterministic replay eval.** Grok Bot has no operator-facing eval loop at all.
- **Byte-stable prompt prefix.** Matters for local MLX prompt caching; a cloud
  vendor has no reason to expose it.

---

## 6. The superset thesis

flobot becomes a superset when it can do everything Grok Bot does *and* two things
a cloud product structurally cannot:

1. **Run against your actual working tree**, with a review gate on the diff.
2. **Prove what it cost and why it decided what it decided**, locally, with no
   vendor telemetry.

The first already exists. The second needs B1/B2/B3. And neither matters if
the agent cannot be trusted unattended — which needs all of Tier A.

So the ordering is forced: **A before B before C**, with D as a same-week cleanup
because shipping dead surfaces is how trust erodes.

---

## 7. Proposed milestones

Each ships something usable alone and has an acceptance check that can be run.

### M8 — Approval gate (Tier A1–A4)

**Files:** new `internal/policy/`, `internal/executor/executor.go`,
`internal/store/store.go`, `internal/server/policy.go`, `web/app.js`.

**Change:** introduce an action record raised by the executor before a
consequential step: `{RunID, Kind, Target, Detail, Provenance}`. A rule engine
evaluates it against ordered rules and returns allow / ask / deny with **ask-first
winning over allow and deny winning over both**. An `ask` decision parks the run
in a new `awaiting_approval` status and emits an event; resolution arrives via
`POST /api/v1/runs/{id}/approvals/{aid}` with allow-once / always-allow / deny.
Background-raised approvals carry an expiry (default 30 min) after which they
resolve as denied with reason `expired`.

Provenance is the load-bearing part: an action whose originating text came from a
tool result or fetched page is marked `untrusted` and can never satisfy an
allow rule that requires operator intent.

**Acceptance:** a run whose next action matches an ask-first rule stops before
executing and spends nothing further; resolving it allow-once continues the run;
an approval raised by a routine expires after its window and records `expired`;
an action derived from fetched page text cannot be auto-allowed by an operator
allow rule.

### M9 — Hooks (Tier A5–A6)

**Files:** new `internal/hooks/`, config load in `cmd/flobot/main.go`.

**Change:** lifecycle events mirroring the vendor set where they map:
`SessionStart/SessionEnd`, `PreToolUse`, `PostToolUse`, `PostToolUseFailure`,
`Stop`, `StopFailure`, `PreCompact`, `PostCompact`. Hooks are JSON files under
`~/.flobot/hooks/*.json` and `<workspace>/.flobot/hooks/*.json`. Only
`PreToolUse` blocks. The script contract is stdin JSON + exit code: **0 allows,
2 denies, everything else fails open** and is recorded on the run.

Project hooks require explicit per-folder trust stored in
`~/.flobot/trusted_folders.json`, because a cloned repo must not be able to run
arbitrary shell on open.

**Acceptance:** a PreToolUse hook exiting 2 blocks the action and records why; a
hook that times out allows the action and records the failure; an untrusted
project hook file does not execute until trusted.

### M10 — Cost accounting (Tier B1, B3)

**Files:** new `internal/cost/`, `internal/executor/executor.go`,
`internal/store/store.go`, `web/app.js`.

**Change:** record usage per turn on the run: input/output tokens where available,
and cost in integer ticks (1 USD = 10^10) to avoid float drift across thousands of
runs. For xAI-compatible routes read `usage.cost_in_usd_ticks`. For local MLX
routes there is no cost field — record tokens only and mark cost unknown rather
than inventing zero. Surface per-run cost and a rolling total in the UI.
Add an optional per-run spend ceiling that stops the run like a timeout does.

**Acceptance:** a run against a route that reports ticks records exact cost and the
total sums without float error; a local route records tokens with cost explicitly
unknown; exceeding a spend ceiling stops the run with a distinct reason.

### M11 — Context compaction (Tier B2)

**Files:** new `internal/compact/`, prompt assembly in `internal/eval/eval.go`.

**Change:** when a run's event history exceeds a threshold, summarise the oldest
segment into a bounded digest and mark the boundary explicitly in the transcript so
nothing silently disappears. Emit PreCompact/PostCompact hooks around it. The
stable prompt prefix must survive compaction unchanged for unchanged bot state.

**Acceptance:** a run past the threshold compacts once and shows a visible boundary;
the prefix stays byte-identical across compaction for unchanged bot state;
compaction never drops the most recent N turns.

### M12 — Shared facts and memory import (Tier C1–C2)

**Files:** new shared-facts store alongside bots, `internal/server/memory.go`.

**Change:** a small account-level fact set (name, timezone) injected into every
bot's prompt ahead of per-bot memory. Plus a paste-and-adopt import that takes text
from another assistant and proposes facts for review rather than writing them blind.

**Acceptance:** setting a shared fact makes it visible to every bot's prompt;
per-bot memory still wins on conflict; imported facts arrive as proposals requiring
approval.

### M13 — Attention inbox (Tier C3)

**Files:** new derived state in `internal/server/`, `web/index.html`.

**Change:** derive three states per bot from existing data — *needs you* (pending
approval or failed run), *running*, *done since you last looked*. This is a pure
projection over runs + approvals; no new persistence.

**Acceptance:** a pending approval puts its bot in needs-you; resolving it clears;
the count matches the underlying runs.

### M14 — Surface wiring (defects D1/D2)

**Files:** `web/app.js`, optionally delete code in `desktop/main.js`.

**Change:** call the desktop bridge on run completion and retire the renderer's own
notification path so there is exactly one notification implementation; route Code
Studio through the resumable stream reader.

**Acceptance:** three runs finishing within two seconds produce one OS notification;
the dock badge shows and clears on focus; Code Studio survives a mid-run reconnect
with no duplicate and no gap.

---

## 8. Explicitly deferred

Consistent with rounds 19–21 and unchanged by this round:

- **MCP transports and OAuth.** Still correctly absent. The tool contract matters;
  the transport does not yet.
- **Multi-user sharing / teams.** Grok Bot's shared-bot model assumes a cloud
  backend with an account system. flobot is single-operator by design; adding roles
  would add attack surface without adding capability.
- **Voice chat / dictation.** Real product surface, wrong layer for this repo.
- **Browser automation and Vault-style credential filling.** The interesting half of
  Vault is "the agent never sees the secret". flobot can get most of that value far
  more cheaply by keeping secrets out of the child environment (already done in M3)
  rather than by building a password manager.
- **Teach-by-demonstration.** M2's draft-from-run covers the useful case without a
  screen recorder.

---

## 9. What "superset" means, stated so it can be checked

flobot strictly exceeds Grok Bot when all six hold:

1. It runs against your real working tree with a diff review gate. *(exists)*
2. It stops before consequential actions and asks you in plain language. *(M8)*
3. It can be extended by policy hooks you control locally. *(M9)*
4. It tells you what each run cost, exactly or explicitly unknown. *(M10)*
5. It survives long runs without silently losing history. *(M11)*
6. Every surface it claims actually reaches you when something happens. *(M14)*

Items 1–6 are checkable by test or by looking at the app. That is the point of
stating them this way.

---

## 10. Recommended order

M14 first — it is hours of work and it makes two already-shipped milestones real.
Then M8, because everything else about unattended operation depends on it. Then M10,
because cost data makes M8's rules tunable rather than guessed. Then M9, M11, M13,
M12 in that order.

Do not start M8 before M14 lands: building an approval inbox on top of a notification
path that never fires would repeat exactly the defect this round found.
