# flobot — Grok Bot Analysis Workspace

**Created:** 2026-08-18 (workspace README timestamp).  
**Target product:** [https://x.ai/bot](https://x.ai/bot)  
**Mode:** Public product/architecture analysis only. No production binaries. No reverse-engineering. No competing app ship.  
**Canonical long-form research note:** [`/Users/floleibert/dev/flocode/analysis/research/grok-bot-public.md`](/Users/floleibert/dev/flocode/analysis/research/grok-bot-public.md)

---

## Purpose of this workspace

`~/dev/flobot` is a **spec and evidence folder** for understanding Grok Bot as a public product, then mapping which *capability shapes* (not brand/UI clones) could inform FloCode or adjacent research.

| In scope | Out of scope |
|---|---|
| Public pages, docs, pricing, use-case copy | Installing/running Grok Bot DMG |
| Capability taxonomy & FloCode gap matrix | Binary / network reverse-engineering |
| Spec sketches for analogous FloCode features | Shipping a Cursor/xAI competitor named flobot |
| Trust boundaries described in public docs | WireGuard/DNS/firewall changes for “bot cloud” |

---

## Executive summary

Grok Bot (early beta, announced **2026-08-11**) is positioned as a **team of always-on AI teammates** that:

1. Share one **persistent cloud computer** (browser + files + terminal + computer-use).
2. Are messaged like colleagues from **desktop + iOS**.
3. Run **in parallel**, coordinate via **DMs / group chats**, and hand off ownership.
4. Learn workflows via **teach-by-demo → skills → scheduled/event routines**.
5. Keep **per-Bot memory** while sharing logins/files on the account computer.
6. Specialize via **job templates** spanning sales, ops, finance, product, and engineering.

Eligibility (beta): **SuperGrok Heavy**, **Cursor Ultra**, **Cursor Teams Premium**. Distribution and auth are Cursor-coupled. Grok Bot **requires cloud data storage**.

FloCode is a **local-first TUI coding agent** with workthreads, subagents, mesh (amesh), Discord control, voice STT, permissions, and learning loops. The useful comparison is **capability analogy**, not product rivalry.

---

## Evidence base (primary)

| URL | Role |
|---|---|
| [x.ai/bot](https://x.ai/bot) | Product narrative, templates, pricing bullets, downloads |
| [Introducing Grok Bot](https://x.ai/news/introducing-grok-bot) | Launch post (2026-08-11): differentiation + internal examples |
| [docs overview](https://docs.x.ai/grok-bot/overview) | Formal “what’s different” + shared-computer model |
| [get started](https://docs.x.ai/grok-bot/get-started) | Install, first Bot, login takeover |
| [computer and apps](https://docs.x.ai/grok-bot/computer-and-apps) | Screens vs security boundaries; recover/reset |
| [skills & routines](https://docs.x.ai/grok-bot/skills-routines-and-automations) | Skill vs routine; teach; events; limits |
| [bots](https://docs.x.ai/grok-bot/bots) | Roster limits, memory, organize |
| [chat & collaboration](https://docs.x.ai/grok-bot/chat-and-collaboration) | Groups, @-mentions, handoffs |
| [use cases](https://docs.x.ai/grok-bot/use-cases) | Eight job templates with safe-start prompts |
| [approvals / security](https://docs.x.ai/grok-bot/approvals-security-and-privacy) | Auto Review, takeover, local-exec policy |
| [FAQ](https://docs.x.ai/grok-bot/faq) | Platforms, 24/7, shared computer, cost framing |

Secondary press may echo claims; **docs.x.ai and x.ai win conflicts** (notably: Linux desktop unsupported at launch per FAQ/get-started, despite some secondary articles listing Linux).

---

## Feature inventory (concrete)

### Own computer
- Persistent cloud VM; browser, FS, terminal, computer-use for non-API apps.
- **One computer per user account**, shared by all Bots (sessions, cookies, `/workspace` files).
- Per-Bot **screens** for parallel GUI work — **not** separate security domains.
- Operator preview + **takeover** for secrets/2FA/CAPTCHA.
- Optional gated execution on the user’s local Mac/Windows machine.

### Multi-bot parallel
- Named Bots with job + description; create specialists as work splits.
- Cap: **50 Bots + group chats** combined.
- Pattern: CoS + lane specialists.

### Routines / teach-by-demo
- **Skill** = how; **Routine** = who + when (schedule or supported event).
- Teach-a-task: ≤10 min browser recording → draft skill → test → schedule.
- Events: e.g. Slack/GitHub account integrations (narrow match rules).
- ≤50 routines/Bot; retain 20 recent runs; can pause after long absence.

### Group chat between bots
- Groups of 2–6; `@Bot`, `@everyone`; async Bot→Bot messages.
- Human watches handoffs; single-owner-per-stage guidance.
- Bot→group attachments currently text-only.

### Memory
- Per-Bot preferences / role / work summaries.
- Cross-Bot via shared computer + messages/handoffs.
- Docs: re-verify consequential facts from source systems.

### 24/7 cloud + surfaces
- Work continues with laptop/app closed.
- Desktop: macOS + Windows; mobile: **iOS 18+**.
- Sync same Bots/threads across devices.
- Not at launch: Linux desktop, Android, iPad.

### Job templates
Sales Outbound · Talent Scout · Paid Media · Expense Manager · Product Performance · Bug Reproduction · Account Health · Chief of Staff — each documented with connect list, safe-start prompt, and “approval before external action” posture.

### Pricing (public product page)
- Cursor Ultra **$200/mo**
- SuperGrok Heavy **$300/mo**
- Cursor Premium Teams **$120/seat/mo** (+ SSO, team marketplace, analytics)

---

## Positioning vs FloCode

```
Grok Bot  ──►  “Finish the swing in the real SaaS UI, 24/7, as a team of named bots”
FloCode   ──►  “Operate a coding agent with evidence, isolation, mesh, and review loops”
```

**Shared ideas:** multi-agent parallelism, handoffs, standing boundaries, connectors/MCP-ish tools, human approval for consequential acts.

**Different centers:**

| | Grok Bot | FloCode |
|---|---|---|
| Primary user | Knowledge worker + Cursor subscriber | Engineer / operator in a repo |
| Primary UI | Messenger + Agent Computer | TUI + slash commands + boards |
| Runtime home | Cloud VM | Local workspace (+ mesh workers) |
| Continuity product | Named Bot + routines | Sessions, WT ledger, goals, memory files |
| Trust story | Cursor account + Auto Review + takeover | Local-first permissions, yolo opt-in, mesh identity |

---

## Architecture implications (analogies for FloCode)

If FloCode wanted *similar capability shapes*, the design pressure points are:

1. **Durable remote environment** — GUI-capable sandbox with session persistence; decide shared-vs-isolated agents explicitly (Grok Bot chose shared-per-user).
2. **Durable named agents** — promote role configs into long-lived agent records (description = standing policy; chat = task).
3. **Inter-agent message bus** — visible DM/group semantics on top of workthread/mesh events.
4. **Skill artifact + scheduler** — compile verified workflows; cron/event triggers with test-run and pause.
5. **Demonstration capture** — optional computer-use recording *or* ledger→skill compilation (more FloCode-native).
6. **Multi-surface approvals** — Discord/mobile-friendly Allow/Deny for consequential tool classes.
7. **Secret path** — never chat-paste OTP/passwords; takeover or masked secret request (Grok Bot public pattern).

None of the above requires cloning Cursor’s product, auth, or VM stack.

---

## Gap matrix vs FloCode stack

| Capability | FloCode today (approx.) | Gap severity for “Grok-like teammate” UX |
|---|---|---|
| Cloud GUI computer | Local tools / MCP / amesh work execute | **High** — different runtime model |
| 24/7 without laptop | Needs a live host/process | **High** |
| Named multi-bot group chat | WT + task spawner + mesh chat | **Medium** — mechanics exist; messenger UX does not |
| Teach-by-demo | Experience reuse / LoRA / docs | **High** for browser demo; **Medium** if ledger→skill |
| Scheduled routines | Goals, killhouse, ad-hoc Discord | **Medium–High** as productized scheduler |
| Sales/ops templates | Eng pipelines | **Out of FloCode core scope** unless packaged separately |
| iOS companion | Discord + voice | **Medium** (control), **High** (computer preview parity) |
| Per-teammate memory UX | memory loader + session DB | **Medium** |
| Approvals taxonomy | permission chain | **Low–Medium** — extend consequential classes |
| Mesh identity / local models | Strong | FloCode **advantage** |
| Repo evidence (ledger/RCA/goals) | Strong | FloCode **advantage** |
| Local-first / no-cloud mode | Supported posture | FloCode **advantage** (Grok Bot requires cloud storage) |

---

## What “build” means here (spec contract)

A flobot “build” means producing **written artifacts**, not an installable agent fleet:

1. **This `ANALYSIS.md`** — living index + gap matrix.
2. **`grok-bot-public.md` in FloCode analysis/research** — evidence-backed deep dive.
3. Optional follow-ons (still docs-only unless a FloCode PRD is opened):
   - `capabilities.md` — normative capability IDs (COMPUTER, ROUTINE, GROUP_CHAT, …).
   - `trust-model.md` — shared-computer implications; approval taxonomy.
   - `flocode-mapping.md` — which gaps become FloCode issues vs explicit non-goals.

**Success criterion:** a reader can answer “what Grok Bot publicly is,” “how it differs from FloCode,” and “what analogous FloCode work would *mean*” without installing Grok Bot or claiming feature parity.

**Failure criterion:** treating flobot as a place to download Grok Bot, scrape private APIs, or ship a lookalike SaaS.

---

## Recommended FloCode-facing questions (for later PRDs, not this folder)

1. Do we want a **remote sandbox** profile at all, or is Discord+mesh+local enough for FloCode’s users?
2. Should workthreads grow **peer messaging UX**, or stay board/ledger-first?
3. Is **routine scheduling** worth productizing on top of goal/killhouse?
4. Should “experience reuse” graduate to **explicit skills** with human edit/review?
5. Which consequential action classes deserve standing **Require Approval** rules like Grok Bot’s Auto Review copy?

---

## Changelog

| Date | Change |
|---|---|
| 2026-08-18 | Initial public-product analysis; linked FloCode research note |
