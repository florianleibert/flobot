# Synthesis — building a better Grok Bot in flobot

**Date:** 2026-11-04
**Inputs:** research rounds 7–26 (20 rounds, four tracks), plus the codebase inventory.
**Method:** each round reads primary sources (vendor docs, specs, source code) and then
compares the finding against what flobot actually has, with file:line citations. Every
round ends in a "Gap vs flobot today" table; this document consolidates those tables
and turns them into an ordered build plan.

Web content in the rounds is marked UNTRUSTED CONTENT (data only).

---

## 1. The thesis

Grok Bot is not a better chat box. It is a **named teammate with memory that compounds,
skills you teach it, routines that fire without you, and an inbox that tells you when it
needs you.** Everything else — the model, the streaming, the UI — is table stakes.

flobot already has the right *shape*: bots, skills, routines, runs, a review gate, a
desktop shell. What it lacks is the four properties that make a bot feel like a
colleague rather than a command runner:

| Property | Grok Bot | flobot today |
|---|---|---|
| Memory that compounds | dated work log + shared facts, chat-editable | one clipped note (`store.go:312`) |
| Skills you can teach | teach-by-demonstration → reviewable skill | free-text body, hand-authored only |
| Routines that fire | plain-language schedule + event triggers + trust rules | cron string + webhook binding |
| An inbox that tells you | three attention states + per-bot notification policy | run status only |

And flobot has one advantage Grok Bot does not: **it runs on your Mac, against your
files, with your models.** That is the axis to win on — not by copying the cloud
product's surface, but by making the local agent trustworthy enough to leave running.

## 2. The constraint that shapes every milestone

`go.mod` is three lines. There is no `require` block and no `go.sum`. **flobot has zero
third-party dependencies**, and its persistence is JSON files written atomically at
`~/.flobot/*.json` with a leak scanner (`state.go:242–252`) that refuses to write
anything containing `"owner"`, `llm_`, `api_key`, or `secret`.

This is not incidental — it is the product's security posture. Consequences:

- **Round 23's FTS5 recommendation cannot be taken literally.** FTS5 requires SQLite,
  which requires a driver, which breaks the zero-dependency property. The *design* of
  round 23 (three tiers, bounded always-in-prompt memory, dated log, retrieval on
  demand) survives; the *engine* must be a small in-process inverted index over the
  existing JSON stores. That is ~200 lines and no dependency.
- **Round 22's Temporal model cannot be taken literally either.** No workflow engine.
  What transfers is fire identity, the timeout split, and heartbeat checkpoints — all
  of which are fields on existing structs.
- **Every milestone below is additive to existing types.** No schema migration story is
  needed because there is no schema; there are structs and JSON.

## 3. The consolidated gap table

Ranked by *value to the "feels like a colleague" property*, not by implementation ease.
Source round in parentheses.

### Tier 1 — without these it is not a Grok Bot analogue

| # | Capability | flobot today | Round |
|---|---|---|---|
| 1 | Bounded memory with **visible usage** and an error when full (not a silent clip) | `clip(note)` at `store.go:312`, silent | 10, 23 |
| 2 | **Dated work log** per bot — what it did, when | none | 7, 10 |
| 3 | **Agent-editable memory** via a tool (`add`/`patch`/`remove`), server-authoritative | HTTP only; agent cannot curate | 10, 23 |
| 4 | **Skill contract**: when-to-use / procedure / pitfalls / verification | free-text body (`store.go:50`) | 8, 11 |
| 5 | **Progressive disclosure** — skill index in prompt, body on demand | whole body injected | 11 |
| 6 | **Skill from a run** (teach-by-demonstration → reviewable draft) | none | 8, 11 |
| 7 | **Attention states** — needs-you / running / done, derived | run status only | 7 |
| 8 | **Post-turn learning loop** — background review that proposes memory/skill updates | none | 10 |

### Tier 2 — what makes it safe to leave running

| # | Capability | flobot today | Round |
|---|---|---|---|
| 9 | **Env allowlist** for the child process | full env + injected credentials (`executor/credentials.go`) | 24 |
| 10 | **Per-run worktree** (exists as a concept, not enforced per run) | `Routine.Worktree` only | 24 |
| 11 | **Seatbelt profile** per run (`sandbox-exec`) | none | 24 |
| 12 | **Keychain secret store** instead of plaintext `~/.accounts/*` | plaintext file | 24 |
| 13 | **Output scrubbing** before events are persisted/broadcast | none | 24 |
| 14 | **Approval gate** with plain-language rules and provenance-aware authorisation | none found | 9, 13 |
| 15 | **Permission precedence** deny > ask > allow; bash subcommand grammar; path/symlink rules | none | 13 |
| 16 | **Pre-dispatch validation** — a misconfigured routine never spends tokens | executor spawns and fails | 12 |
| 17 | **Global emergency stop** + recursion guard + per-routine rate limits | stop one run only | 12 |

### Tier 3 — durability and correctness

| # | Capability | flobot today | Round |
|---|---|---|---|
| 18 | **Fire identity / idempotency key** for routine fires | none | 12, 22 |
| 19 | **Three-way timeout split** (start-to-close / heartbeat / schedule-to-start) | one timeout + first-token/stall watchdogs (`executor.go:132–137`) | 22 |
| 20 | **Heartbeat with progress payload** → resume from checkpoint | none; SIGTERM only | 22 |
| 21 | **Cooperative cancel** (SIGTERM → grace → SIGKILL), ambiguous-delivery state | SIGTERM only | 22 |
| 22 | **Stream drop is visible**, not silent EOF; heartbeat comments; `id:`/`Last-Event-ID` or `?since` parity | silent drop (`server.go:1374`) | 26 |
| 23 | **Token-level deltas** so long answers do not look like hangs | message-granular (`executor.go:33`) | 26 |

### Tier 4 — product polish that closes real HIG/spec gaps

| # | Capability | flobot today | Round |
|---|---|---|---|
| 24 | **Dock badge** with unread run count (HIG's foreground substitute) | nothing when focused (`desktop/main.js:305`) | 25 |
| 25 | **Coalesced notifications** (HIG forbids duplicates per event) | one per run-end → storm under concurrency | 25 |
| 26 | **Notification `id`/`groupId`/actions**, `failed` listener (signing trap) | none; failure swallowed by `catch {}` | 25 |
| 27 | **Tray / menu-bar presence** so a windowless app is reachable | none on macOS | 25 |
| 28 | **Route decision recorded** + fallback chain + capacity-aware filtering | `Bot.Route` pin only (`routes.go:53`) | 17 |
| 29 | **Eval loop**: replay runs, deterministic scorers, operator verdict capture | none; labels thrown away | 18 |
| 30 | **Prompt cache discipline** — stable prefix ordering for local MLX routes | prompt rebuilt per run, memory first | 16 |

### Explicitly deferred (rounds say *don't build yet*)

- MCP transports and OAuth (rounds 20–21): correctly absent; build the tool *contract*
  first, transport later.
- WebSocket (round 26): every UI action already has a REST endpoint.
- Outbound webhook actions / egress allowlist (round 19): inbound-only is defensible
  until there is an outbound use case.
- Group chat with @-routing (round 7): the handoff bus covers the real need; group chat
  is a UI feature with no engine behind it.
- Enterprise control surface (round 9): scoped out deliberately.

## 4. Milestones

Each milestone ships something usable on its own and has an acceptance check that can
be run. Ordering is by dependency: M1 unlocks M2's learning loop; M3 must precede M4
because you do not leave an unsandboxed agent on a schedule.

### M1 — Memory that compounds (Tier 1 items 1–3)

**Files:** `internal/store/store.go`, `internal/store/state.go`, new `internal/memory/`.
**Change:** replace `Bot.Memory string` with a bounded memory record:
`{Facts string, Usage int, Cap int}` plus `Log []LogEntry{At, RunID, Summary}`.
Surface usage as a percentage in the bot JSON. When full, return an error the agent can
act on instead of clipping.
**Acceptance:** a test that fills memory past the cap gets an error naming the usage;
a test that appends a log entry and reloads from disk sees it; `assertNoLeaks` still
passes.

### M2 — Skills as contracts, and skills from runs (Tier 1 items 4–6)

**Files:** `internal/store/store.go` (`Skill`), new `internal/skills/`, `internal/server/`.
**Change:** add `WhenToUse`, `Procedure`, `Pitfalls`, `Verification` to `Skill` while
keeping `Body` as the fallback for existing rows. Inject only name+when-to-use into the
prompt; fetch the body when the model asks. Add "save this run as a skill" which drafts
a skill from the run's events and requires approval before it becomes live.
**Acceptance:** prompt assembly for a bot with five skills contains five one-line
entries and zero bodies; a drafted skill is inert until approved.

### M3 — Sandbox and secrets (Tier 2 items 9–13)

**Files:** `internal/executor/executor.go`, `internal/executor/credentials.go`, new
`internal/sandbox/`.
**Change:** build the child environment from an explicit allowlist plus declared
credentials only. Write a Seatbelt profile per run (`deny default`, allow the run's
worktree read/write, allow outbound only to the model endpoint). Move secrets to the
Keychain via `security find-generic-password`. Scrub known secret values out of events
before they are stored or published.
**Acceptance:** a run whose child prints `$AWS_SECRET_ACCESS_KEY` produces an event with
the value redacted; a child that tries to read outside its worktree fails; a child that
tries to reach a non-model host fails.

### M4 — Routines you can trust unattended (Tier 2 items 14–17, Tier 3 items 18–21)

**Files:** `internal/schedule/`, `internal/server/server.go`, `internal/store/store.go`.
**Change:** pre-dispatch validation (route pinned, skill exists, tools parse) before
spawning; fire identity so a duplicate webhook does not double-run; three timeouts;
heartbeat events carrying progress; cooperative cancel with grace period; global stop;
recursion guard; per-routine rate limit.
**Acceptance:** firing the same webhook id twice runs once; a routine with an unpinned
route fails validation with zero tokens spent; a cancelled run reaches `stopped` within
the grace window and its checkpoint is restorable.

### M5 — Streaming that does not lie (Tier 3 items 22–23)

**Files:** `internal/server/server.go`, `internal/executor/executor.go`, `web/app.js`.
**Change:** publish a terminal drop frame before closing a slow subscriber and have the
client resume with `?since=`; add a heartbeat comment every ~15 s; add a `delta` event
kind with server-side coalescing into ~50 ms windows and a larger hub buffer.
**Acceptance:** killing the client mid-run and reconnecting yields no duplicate and no
missing event; a slow reader sees an explicit drop frame rather than EOF; a long answer
renders incrementally.

### M6 — The app tells you what happened (Tier 4 items 24–27)

**Files:** `desktop/main.js`, `desktop/preload.js`, `web/app.js`.
**Change:** dock badge from unread run count; coalesce notifications within a window;
add `id`/`groupId`, actions ("Open diff", "Stop"), and a `failed` listener that logs;
add a Tray with pause-all and open-window.
**Acceptance:** three runs finishing within two seconds produce one notification;
clicking it opens that run's diff; an unsigned build logs the notification failure
instead of swallowing it.

### M7 — Measure it (Tier 4 items 28–30)

**Files:** new `internal/eval/`, `internal/routes/routes.go`.
**Change:** record the routing decision on each run; replay historical runs through
deterministic scorers; capture an operator verdict (good/bad) on a run and keep it;
order prompts stable-prefix-first so local MLX routes reuse cache.
**Acceptance:** replaying ten stored runs reproduces their recorded outcomes; a verdict
survives restart; prompt assembly for the same bot twice differs only after the stable
prefix.

## 5. What "better" means, stated so it can be checked

flobot beats Grok Bot when all five of these are true:

1. A bot remembers what it did last week without being told. *(M1)*
2. You can teach it something once and it uses it next time without re-explaining. *(M2)*
3. You can leave it on a schedule and it cannot read your SSH keys or phone home. *(M3)*
4. A routine that fires twice runs once, and a cancelled routine leaves nothing half-done. *(M4)*
5. When you come back to the Mac, the app tells you what happened without you having watched. *(M5, M6)*

Everything else in the twenty rounds is either supporting detail for those five or an
explicit decision not to build something.

## 6. Round index

| Round | File | Track |
|---|---|---|
| 7–9 | `07`–`09` | Grok Bot product surface (docs.x.ai) |
| 10–12 | `10`–`12` | Hermes Agent / OpenClaw: memory, skills, scheduler, security |
| 13–15 | `13`–`15` | Harness engineering: Claude Code hooks/permissions, Codex config/trust, OpenCode/Amp agent records |
| 16–18 | `16`–`18` | Local serving (mlx-lm), routing (RouteLLM), eval harnesses (Inspect) |
| 19–21 | `19`–`21` | Webhook auth schemes, OAuth/MCP authorization, MCP tool contract |
| 22–24 | `22`–`24` | Durable execution (Temporal), memory tiers (FTS5), macOS sandboxing/secrets |
| 25–26 | `25`–`26` | Desktop UX (HIG + Electron), streaming surfaces (WHATWG SSE) |

Earlier rounds (`01`–`06`) are prior-cycle notes kept for continuity.
