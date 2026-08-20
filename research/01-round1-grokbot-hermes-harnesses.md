# Research round 1 — Grok Bot, Hermes, harness survey, hydra x-agent

**Date:** 2026-08-19T23:52:29Z  
**Mode:** public sources + local docs. No Grok Bot binary, no reverse-engineering, no secrets.  
**Did not run** `~/dev/hydra/x-agent/x-agent-v5.py --prds` (that path writes into `~/dev/flocode/docs/prd`). Used existing x-agent data dumps and live X/web/GitHub research instead.

---

## Sources

### Web
- https://x.ai/bot
- https://x.ai/news/introducing-grok-bot (2026-08-11)
- https://docs.x.ai/grok-bot/overview
- https://docs.x.ai/grok-bot/bots
- https://docs.x.ai/grok-bot/skills-routines-and-automations (cited via press + local ANALYSIS)
- https://hermes-agent.nousresearch.com/docs/user-guide/features/skills
- https://github.com/NousResearch/hermes-agent
- https://github.com/xai-org/grok-build
- Local: `/Users/floleibert/dev/flobot/{README,SPEC,ANALYSIS}.md`
- Local: `/Users/floleibert/dev/flocode2/docs/research/06-grokbot-hermes.md`
- Local hydra x-agent: `~/dev/hydra/x-agent/x-agent-v5.py`, `data/flocode-harness-reliability-xsearch-20260819.json`, `data/qwen38-r1-*.json`

### X (semantic, 2026-08-19)
- @elvissun 2026-04-17: Hermes self-authors skills; skill explosion / overlap is the long-tail failure. OpenClaw keeps a small governed corpus.
- @Texas_Found_Me 2026-08-14: Grok Bot coordinates specialists better; Hermes often takes the work itself instead of handing off.
- @mattragudo 2026-08-14: “Hermes is just the Harness” — do not conflate Bot product with harness.
- @Daniel_Farinax 2026-08-19: local-model-as-agent is the differentiator vs cloud Bot.

### Hydra x-agent (existing dumps, not a new `--prds` run)
- Tool-call parsers are the reliability hotspot: Qwen3.8 XML ≠ Hermes JSON; Qwen3.5/3.6 want `qwen3_xml`; GLM wants `glm47`. A harness that “fails the model” is usually a parser miss (`@suryaagentic` 2026-08-15).
- Thinking-mode stalls local Qwen (69s → ~2s when thinking is off).

---

## Product identities (do not conflate)

| Name | What it is | What flobot is |
| ---- | ---------- | -------------- |
| **Grok Bot** | Named cloud teammates on one shared VM. Desktop + iOS. Cursor-coupled. | Capability *shape* only: named bots, skills, routines, handoff. |
| **Grok Build** | Local TUI coding agent (`grok`). Open-sourced as xai-org/grok-build. | Already copied by flocode2. flobot *execs* that shape. |
| **Hermes Agent** | Python life+coding agent: 20+ gateways, Skills Hub, skill_manage, cron. | Steal L0/L1 skills, MEMORY caps, cron=fresh agent. Toss the rest. |
| **Hermes tool-call format** | `<tool_call>{json}</tool_call>` | flocode2 already sniffs this. Not a product. |

---

## KEEP (steal for the amesh plugin)

| Pattern | From | flobot contract |
| ------- | ---- | --------------- |
| Named teammate with a standing role + pinned route | Grok Bot | `FlobotBot` already in amesh helper |
| Skill = reviewable *how* (human-edited markdown, never live from a recording) | Grok Bot + Hermes SKILL.md | `FlobotSkill`; L0 list / L1 body on demand |
| Routine = *who + when*; starts paused; test-run before enable | Grok Bot | existing `FlobotRoutine`; Phase 2 test-run **execs** flocode `--intent ask` |
| Single-owner-per-stage handoff | Grok Bot docs | existing `OwnerBot`; append-once under lock (PR #1062) |
| Approval does not reverse work already done | Grok Bot | surface in run UI |
| Allow once / always / deny | Grok Build | flocode2 perm; plugin streams `perm` NDJSON |
| L0/L1 progressive disclosure | Hermes skills | do not dump every skill body into the prompt |
| MEMORY caps (short role note, not another agent's private store) | Hermes + existing flobot | `Memory` field stays a note |
| Cron = fresh agent | Hermes | each routine fire is a new `flocode -p` process, new session |
| Worktrees, not in-process SpawnChild | Grok Build / flocode2 | `--worktree` on CODE runs |
| NDJSON `streaming-json` | Grok Build / flocode2 | plugin parses stdout; never tool args/secrets |
| Ask / Plan / Code write fence | flocode2 | test-run = ask; handoff CODE = code |
| Parser-first reliability | x-agent dumps | pin GLM `glm-5.2` profile and Qwen `qwen3-coder` profile |

---

## TOSS (explicit non-goals)

- Persistent **cloud VM** / computer-use / shared cookies as a security boundary
- Cursor auth, SuperGrok Heavy packaging, iOS companion
- Teach-by-demo screen recording (≤10 min browser capture)
- Auto Review as a second model in front of every tool
- Hermes 20 messaging gateways (Discord/Telegram/WhatsApp as the primary surface)
- `skill_manage` agent-written skills without human review
- Skills Hub / ClawHub / 123 bundled skills on day one
- Dynamic plugin host inside amesh (`operator_plugin_manifest` is enroll/acquire only)
- Importing `amesh` into flocode2
- Running x-agent `--prds` into the donor `~/dev/flocode` tree

---

## Eight execution IDs

| ID | Name | Contract |
| -- | ---- | -------- |
| **E1** | `BOT_RUN` | `POST /runs` with `{bot_id, prompt, intent?, yolo?}`. Exec `flocode -p --output streaming-json --model <pin>`. Pairing session must own the bot. |
| **E2** | `ENV` | Workspace is the local repo (or `--worktree`). No cloud computer. Durable env = session.db + worktree path. |
| **E3** | `ACI` | Agent-computer interface = flocode2 tools (read/grep/glob/bash/apply_patch/webfetch) plus namespaced MCP. No GUI takeover. |
| **E4** | `PERM` | Default Ask on mutating tools. `--yolo` skips Ask+Guard, never Admit. Headless without Ask callback denies mutating tools. Stream `kind=perm`. |
| **E5** | `HANDOFF_RUN` | Handoff writes a bus message with `OwnerBot=to`, then may spawn `BOT_RUN` for the recipient. Broadcast `@all` is never a handoff. |
| **E6** | `SKILL_RUN` | Skill body is prepended as L1 context to the prompt. Skills are never auto-executed from a raw recording. |
| **E7** | `ROUTINE_FIRE` | Cron/event later. v1: test-run is a bounded `BOT_RUN` with `--intent ask`. Enable requires LastRun set. Each fire is a fresh process. |
| **E8** | `BOT_STATE` | Roster+memory in-memory on amesh-web (lost on restart). Run events live in the flobot/amesh process for the session. Secrets never on these types. |

---

## Implications for this build

1. The amesh plugin already ships the roster (PR #1062, commit `fa62d64a`). The gap is **execution**.
2. Execution is **exec the flocode2 binary** (`0.1.0` on PATH as `~/.local/bin/flocode`), not in-process `harness.Build` inside amesh (amesh must not import flocode2's `github.com/florianleibert/flocode` module — that path is the donor).
3. Standalone `~/dev/flobot` is the dogfood product + argv contract. amesh speaks the same argv.
4. Verification uses Muse Spark 1.2 via OpenAI-compat in flocode2's `internal/verify`, not a second cloud Auto Review product.
