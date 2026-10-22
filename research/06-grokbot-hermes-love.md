# Research round 6 — What people LOVE about Grok Bot and Hermes (pick 3 of each)

**Date:** 2026-10-14
**Mode:** x-agent xsearch (`~/dev/hydra/x-agent/xsearch_grokbot_hermes_love_20261014.py` → `data/grokbot-hermes-love-20261014.json`), 10 queries, 2026-05-01→2026-10-14. Public posts only, no secrets.

---

## Grok Bot — top 3 loved things

### G1. Named specialist teammates that hand off to each other (crews)
The single most-loved thing. Users build a chief-of-staff + specialists hierarchy and the bots route work among themselves without the human relaying.

- @gippp69 2026-10 (crew overnight job): "grok bot crews are f**king insane… 6 bots, 612 finished tasks, 47 blocked actions, 89 rejected handoffs and 0 irreversible moves made without me." Every seat is *intentionally incomplete*: scout never decides, quant never acts, exec only acts on a timestamped PASS, scribe attaches evidence to every handoff.
- @0xRafy 2026-09-05 (224 likes): "Most people use Grok Bot as an agent. The real power starts when Grok Bot manages the agents" — objective → decompose → route → parallel agents → track → resolve → synthesize.
- @tchchia 2026-10-08: bots "can TALK to EACH OTHER… too real."
- @emigal 2026-10-12 (855 likes): "my main Bot can manage other bots for me… can create other bots on the fly."

**flobot already has this shape** (bots + handoff bus + single-owner-per-stage). The loved detail flobot lacks: *evidence attached to every handoff* and *seats that are intentionally incomplete* (a bot that can research but never act).

### G2. Routines that run unattended, test-first, with approval gates
- @Cyb3rCricket 2026-10-13: the loved order is do-it-once-in-chat → save as skill → test → *then* schedule. "A routine is only as good as the method behind it." Keep approval on for anything that sends/buys/deletes/publishes.
- @Anushkaa1407 2026-08-20: "stays on when your laptop is closed. VM runs in the cloud 24/7… routines run on schedule without you."
- @grok (official guidance, echoed by users): "Test tasks once, save as skills, then schedule as routines."

**flobot already has this** (paused-by-default routines, test-run = `--intent ask`, enable requires LastRun). The loved detail flobot lacks: *scheduled firing* (cron) — each fire a fresh process.

### G3. Zero-setup managed experience with connectors
- @emigal 2026-10-12: "it has the most available connectors… replaced 100% of my Hermes usage & automations… runs 100% of my Cursor cloud agents, reviews PRs, merges for me."
- @MiaAI_lab 2026-08-12 (333 likes): "Grok Bot wins if you hate setup — tools + apps already wired, nothing you need to keep turned on locally."
- @david_may 2026-10-09: moved most Claude Code communication to grok bots; bots review UX in their computer and pass prompts to Claude Code to fix.

**flobot's analogue is not a cloud VM** (explicit non-goal). The local-first version of "already wired": the amesh mesh *is* the connector layer — models, nodes, and peers already enrolled. Connecting flobot to amesh (this round) is the local-first answer to G3.

---

## Hermes — top 3 loved things

### H1. Self-evolving skills (the agent remembers *how* it solved something)
- @akshay_pachaar 2026-05-17 (411 likes): "the most powerful feature in Hermes… memory handles facts, and skills handle procedures." Agent saves a working approach as SKILL.md after trial-and-error; next session it follows the proven procedure. A background Curator merges overlapping skills and archives (never deletes) unused ones.
- @milesdeutscher 2026-08-02 (1019 likes): free Skills Hub, 90,000+ community skills across 200+ categories.
- @aiedge_ 2026-09-11 (396 likes): "a f*cking goldmine."

**flobot keeps the human-review fence** (skills are never auto-executed from a raw recording — round 1 TOSS list), but the loved loop is: hard problem solved once → proposed as a *draft* skill → human reviews → enabled. A "propose skill from this run" action is the flobot-shaped version.

### H2. Full local control — your hardware, your models, no lock-in
- @MiaAI_lab: "Hermes wins when you want full control — runs locally, on your hardware, you pick the model, including local models."
- @EXM7777 2026-08-25 (1544 likes): "if you want a system that grows with you until it's YOUR thing and not just a tool you rent… Hermes, every time."
- @kidtsang 2026-10-12: "Local deployment can also enhance privacy and reduce reliance on credits."
- @BkashJosi 2026-10-12: "The local hero: runs on your machine, never sends a byte out" + "Hermes can already split by job" — a different model per bot.

**flobot is already this** (loopback-only, exec local binary, keys never leave the Mac). The loved detail this round unlocks: *per-bot model choice from the amesh mesh catalog* — any model any peer is serving becomes a flobot route.

### H3. Extensibility — build your own AI team (MCP, cron, per-job models)
- @marc_bara 2026-09-13 (527 likes): 15 days, 50+ capabilities added; the key one was a custom MCP server so Hermes interacts directly with his own task/invoice system.
- @tonysimons_ 2026-09-02 (278 likes): "Grok Bot is 'I want an AI team. Make this easy.' Hermes is 'I want to BUILD my AI team.'"
- @witcheer 2026-10-10 (948 likes): a six-month setup — memory keeps facts, nightly cron reviews sessions and tidies skills, script-only cron takes backups.

**flobot's version**: skills + routines + handoff are the extension surface, and the amesh mesh is the model layer. Cron-style routine firing (G2) and skill proposals (H1) are the two extension points to build next.

---

## What this round ships (mapping loves → code)

| Love | flobot change |
| ---- | ------------- |
| G3 zero-setup connectors / H2 your models, your hardware | `internal/amesh`: client for amesh-web `GET /api/v2/mesh/models` (127.0.0.1:18765); every ready mesh model becomes a selectable flobot route `mesh/<id>` |
| H2 per-bot model choice | `POST /api/v1/bots` accepts `mesh/<id>` routes; runs on a mesh route get a generated flocode config (pin = mesh model, base_url = mesh node) via `FLOCODE_CONFIG`, so no secrets and no edits to the user's config |
| G1/G2/H1/H3 | already in the roster shape (handoff, routines, skills); recorded here as the backlog |

Mesh rows carry no credentials — the amesh private catalog is signed and the mesh nodes serve on the private network; flocode's `generic-openai` profile handles OpenAI-compatible mesh endpoints.
