# Research round 10 — Hermes Agent: bounded memory, frozen snapshot, background review

**Date:** 2026-11-04
**Track:** B — Hermes Agent / OpenClaw self-evolving skills
**Sources (fetched 2026-11-04):**
- https://hermes-agent.nousresearch.com/docs/user-guide/features/memory
- https://raw.githubusercontent.com/NousResearch/hermes-agent/main/README.md (MIT, Nous Research)
- https://hermes-agent.nousresearch.com/docs/user-guide/features/skills (see round 11)

---

## 1. The two-file memory model, with hard character budgets

| File | Purpose | Limit |
|---|---|---|
| `MEMORY.md` | agent's personal notes — environment facts, conventions, lessons | **2,200 chars (~800 tokens)** |
| `USER.md` | user profile — preferences, style, expectations | **1,375 chars (~500 tokens)** |

Stored in `~/.hermes/memories/`, injected into the system prompt as a **frozen snapshot at session start**. Total memory budget ≈ **1,300 tokens per session, fixed**.

This is the single most important number in the whole design. Grok Bot's memory is unbounded and invisible; Hermes' is ~1.3k tokens and always visible in the prompt header with a usage percentage:

```
══════════════════════════════════════════════
MEMORY (your personal notes) [67% — 1,474/2,200 chars]
══════════════════════════════════════════════
```

Entries separated by `§`. The agent sees its own capacity and consolidates proactively.

## 2. The frozen-snapshot pattern — and why

> "The system prompt injection is captured once at session start and never changes mid-session. This is intentional — it preserves the LLM's prefix cache for performance."

Writes persist to disk immediately; tool responses show live state; the *prompt* only updates next session. That is a deliberate trade: cache stability over immediacy. Consequence they document honestly: "a fact saved in the current session is visible to the next session, not to another session that was already running."

flobot re-reads `Bot.Memory` per run, so it has no cache problem but also no stable prefix. If flobot ever runs multi-turn agent loops in-process, the frozen-snapshot pattern is the right answer.

## 3. Memory-full is an error, not a silent eviction

> "Memory does not auto-compact: when a write would exceed the limit, the memory tool returns an error instead of silently dropping entries."

The error payload is instructive — it tells the model exactly how much to free and hands it the current entries:

```json
{"success": false,
 "error": "Memory at 2,100/2,200 chars; adding this entry (250 chars) would exceed the limit by 153 chars. Retry as ONE 'operations' batch that removes or shortens (patch) stale entries from current_entries below to free at least 153 chars AND adds this entry — the limit is checked only on the batch result.",
 "current_entries": ["..."], "usage": "2,100/2,200"}
```

Two design wins: (a) the limit is checked **on the batch result**, so remove+add in one call works; (b) the model is forced to make the eviction decision, not a heuristic.

## 4. Tool surface: `add` / `patch` / `remove`, no `read`, no `replace`

- `add`, `patch` (replace one span inside an entry), `remove` (substring match).
- **No `read`** — memory is already in context.
- **`replace` was removed**: "It overwrote the whole matched entry, and models used it as a find-and-swap, which wiped the other facts in multi-fact entries." A `replace` call now returns an error pointing at `patch`.

That is a shipped-and-reverted API, documented with the reason. Lesson: models misuse whole-entry replace; span-level patch with unique-substring matching is the correct primitive.

- Every call requires a top-level `target`: `"memory"` or `"user"`. One target per batch.
- Substring matching must be unique; ambiguous matches error with `closest_entries`.

## 5. Security scanning on write

> "Memory entries are scanned for injection and exfiltration patterns before being accepted, since they're injected into the system prompt. Content matching threat patterns (prompt injection, credential exfiltration, SSH backdoors) or containing invisible Unicode characters is blocked."

Memory is a prompt-injection vector by construction. Scanning on write is mandatory, not optional. Also: exact duplicates rejected.

## 6. The background review — the actual learning loop

After each turn, a **fork** replays the conversation and may save memory entries or patch skills. Key engineering details:

| Concern | Their answer |
|---|---|
| Cost | runs on main model by default (warm prompt cache = cheap reads); can route to a cheaper model (`auxiliary.background_review`) at ~3–5× lower cost |
| Cache parity | same-model review keeps system prompt, tools, snapshot byte-identical so it reuses the parent's prefix; different-model review replays a **digest** (recent turns verbatim + summary of older) |
| Reasoning effort | same-model review **inherits** parent effort; cannot be overridden (would break cache parity). Different-model route honours `reasoning_effort` |
| Cost cap | `max_input_tokens` caps the **sum** of replayed input tokens across all tool iterations of one review; default = 75% of resolved context window, capped at 600k |
| Local GPU | `defer: auto` — reviews on the managed local llama-server are **queued until the machine is idle** (`defer_max_age_s: 1800`), because a fork would occupy the GPU your next prompt needs |
| Coalescing | queued reviews coalesce per session; a preempted review is re-queued not discarded |
| Tool scope | review gets memory + skill-management + read-only file tools by default; extra tools opt-in by name via `extra_tools` |
| Observability | fork usage persisted with `task='background_review'`; completion line in `agent.log` |

The local-GPU deferral is directly relevant to flobot: flobot runs on a Mac against local MLX models via amesh. A background learning pass must not steal the GPU from the user's next prompt.

## 7. Consent gates: `write_approval`

Two independent switches:

- `memory.write_approval: true` → foreground writes prompt inline in CLI; everywhere else (messaging, scripts, background review) writes are **staged** for `/memory pending`, `/memory approve <id>`, `/memory reject <id>`.
- `skills.write_approval: true` → skill writes always stage; you review a one-line gist inline and the full diff out-of-band (`/skills diff <id>`), because "a SKILL.md is far too large to read in a chat bubble."

**Staged-write pinning:** "A staged patch or remove records the full entry it targets… if it changed after the write was staged, the write is refused and stays pending." That is optimistic-concurrency control on a memory write. Copy this.

Notification verbosity: `display.memory_notifications: off | on | verbose`, per-platform.

## 8. Session search as the second tier

- All sessions in SQLite (`~/.hermes/state.db`) with **FTS5** full-text search.
- Returns **actual messages**, no LLM summarization, no truncation.
- Three calling shapes: discovery / scroll / browse.
- ~20 ms query, ~1 ms scroll, **free** (no LLM calls).

The comparison table they publish is the cleanest statement of the two-tier architecture anywhere:

| | Persistent memory | Session search |
|---|---|---|
| Capacity | ~1,300 tokens | unlimited |
| Speed | instant (in prompt) | ~20 ms FTS5 |
| Cost | tokens in every prompt | free |
| Use case | key facts always available | "did we discuss X?" |
| Management | curated by agent | automatic |

flobot has SQLite already and stores runs/events/messages. Adding FTS5 over messages is a small change with large payoff — and it means memory can stay small.

## 9. The `/journey` learning timeline

A timeline of everything learned — skills + memory entries plotted over time, with a scrubber that replays the build-up. Three surfaces: CLI (`hermes journey`), TUI (`/journey`), desktop (Star Map panel). Nodes are addressable and prunable:

- `hermes journey list` → node ids (`skill name`, `memory:<source>:<index>:<fingerprint>`)
- `hermes journey delete <node>` → skills archived (restorable), memory chunks removed
- `hermes journey edit <node>` → opens SKILL.md or memory chunk in `$EDITOR`

A skill appears only once it has a *learning signal*: created in this profile, created by background review, or used at least once. Bundled/hand-written-never-used skills stay off the timeline.

This is a genuinely novel UX idea worth stealing: **make learning visible and reversible**. It answers "what has this thing learned about me?" without a settings table.

## 10. Failure modes they document (all instructive)

1. **Claimed-but-not-saved.** "I've added that to my memory" with no tool call. "Small local models (roughly under 30B parameters) and models with weak tool-calling do this often — they produce the confirmation without the tool call." Fix: stronger model for setup; once entries exist a smaller model reads them fine.
2. **Staged but unapproved** writes silently never land.
3. **Wrong profile** — memory is per profile.
4. **Frozen snapshot** — current-session saves invisible until next session.
5. **Session-boundary dependency.** The whole loop assumes sessions end: "If you never reset, a chat can run for weeks as a single session: convenient, but it grows expensive… and the learning loop of forget → recall from memory → search past sessions almost never gets to fire." Prescription: `/new` at natural boundaries.

Failure mode #1 matters enormously for flobot because flobot targets local models. A memory system that depends on reliable tool-calling will silently no-op on a 30B model. flobot must verify writes server-side (the store write is the source of truth; the model's claim is not).

## 11. KEEP / TOSS for flobot

**KEEP**
- Hard character budgets with visible usage percentage in the prompt header.
- Memory-full returns an actionable error; agent must free space in the same batch.
- Span-level `patch` + `remove`; **no whole-entry replace**.
- Write-time injection/exfiltration/unicode scanning.
- Duplicate rejection.
- Background review as a post-turn fork with its own model route, token cap, and idle-deferral on local GPUs.
- Staged writes with optimistic-concurrency pinning + gist/diff approval split.
- FTS5 session search as the unlimited second tier.
- `/journey`-style learning timeline with per-node edit/delete.
- Honest documentation of "claimed but not saved" for small models.

**TOSS**
- Frozen snapshot if flobot's runs are one-shot (it mostly is — re-read per run is fine).
- Two-file split as *files*; flobot should keep it in SQLite with the same budgets.

## 12. Gap vs flobot today

| Capability | flobot today | Gap |
|---|---|---|
| Memory budget | one capped note (`clip(note)` in `store.go:312`) | no char budget surfaced to model, no usage % |
| Memory tool | none — memory edited via HTTP only | agent cannot curate its own memory |
| Memory-full handling | silent clip | silent truncation is worse than an error |
| Injection scanning | none | missing |
| Post-turn learning | none | missing |
| Staged writes / approval | none | missing |
| Session search | none (runs table only instr.) | missing FTS5 |
| Learning timeline | none | missing |
