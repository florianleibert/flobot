# Research round 7 — Grok Bot's Bot model, memory, and roster

**Date:** 2026-11-04
**Track:** A — Grok Bot product surface (primary sources)
**Mode:** public first-party docs only (`docs.x.ai`, markdown endpoints). No binary, no reverse-engineering, no secrets.
**Sources (all fetched 2026-11-04):**
- https://docs.x.ai/grok-bot/overview.md
- https://docs.x.ai/grok-bot/bots.md
- https://docs.x.ai/grok-bot/get-started.md
- https://docs.x.ai/grok-bot/use-cases.md
- https://docs.x.ai/grok-bot/faq.md
- https://docs.x.ai/llms.txt (page index; every page is available as `.md`)

---

## 1. What a Bot *is* (the entity contract)

> "A Bot is a durable AI teammate with a name, a job, its own conversation, and working context that develops over time." — `bots.md`

Fields the product actually exposes:

| Field | Where it lives | Notes |
|---|---|---|
| name | Bot details | owner-only rename (`bots.md` §Change a Bot's name…) |
| picture (shape + color, generated, or uploaded ≤25 MB) | Bot details | `bots.md` |
| label ("Title" on phone) | Bot details | free text under the name |
| job / role | **not a field** — "Personal Bots no longer have a description or a **Bot settings** page; to change what a Bot does, tell it in chat." | `bots.md` §Edit a Bot |
| memory | server-side, agent-managed | see §3 |
| conversation | per-Bot, private | `bots.md` |
| routines | owned by the Bot | `skills-routines-and-automations.md` |
| skills | **account-wide library**, not per-Bot | see round 8 |
| apps / secrets / files | account-wide or shared-Bot-scoped | round 9 |

**Design decision worth stealing:** they *deleted* the structured "description" field for personal Bots and made chat the only way to change behaviour. The role is prose in memory, not a form. That is the opposite of flobot's `Bot.Role` string field — and it is cheaper to build and more expressive.

**Design decision worth rejecting:** "no model picker" (`faq.md` §Can I pick the model?). Grok Bot routes model choice server-side and hides it. flobot's whole premise is the opposite: per-bot pinned routes over your own mesh. Keep the picker.

## 2. The roster rules they state explicitly

From `bots.md` §Give each Bot a clear job — create a separate Bot only when the work has a distinct:

1. Goal or area of ownership
2. Set of tools and sources
3. Working style
4. Approval boundary
5. Recurring schedule

And §Organize a team of Bots:

> "Start with the smallest useful roster: 1. Give one Bot ownership of an end-to-end outcome. 2. Add another only when the work has a stable specialist role. 3. Put Bots in a group chat when the handoff itself needs to be visible. 4. Keep external actions behind a clear approval boundary."

Named anti-pattern: a **"General Helper"** bot "does neither" — gives no guidance and makes saved context non-reusable.

**Roster mechanics:** pin, sections (grouping), hide-from-sidebar (hides without pausing routines or deleting history), duplicate (carries profile/settings/skills/routines/avatar but **not** conversation history, memory, or attachments), delete (erases profile, conversations, memory, routines, attachments).

**Primary Bot:** one designated main point of contact that "checks in with you on its own and coordinates work with your other Bots". Replaceable. A shared Bot cannot be primary.

## 3. Memory — the most instructive part of the whole product

From `bots.md` §What a Bot remembers:

- Saves: how you like to work; rules you ask it to keep; facts about you (name, time zone); **"a dated log of what it has worked on with you"**.
- **Does not** save: logins, cards, contact info → those go to Vault.
- **The agent decides when to save, and does not ask first.** "It often mentions a save in its reply, but not every save shows up in the chat."
- Scope: per-Bot memory + a small set of **account-shared facts** (name, time zone). "When one Bot learns one of these, your other Bots know it too."
- Voice chats draw on earlier calls; group chats let your Bot use what you told it privately.
- **There is no memory screen.** "Ask in chat instead" — see it / correct it / remove it are all chat commands.
- Reset conversation keeps memory. Delete Bot deletes its memory.
- Import from another assistant: paste-the-transcript flow that lands in the primary Bot's memory.
- Explicit caveat: **"Memory is not a source of truth"** — keep changing facts in the source system; back safety boundaries with approvals and Auto-review rules "rather than memory alone".

**The three-layer shape to copy:** (a) per-agent private memory, (b) a tiny shared fact set across the roster, (c) everything else moves through files/handoffs/group chats rather than memory.

flobot today has `Bot.Memory` as one capped note prepended to every prompt (README §Teammate memory). That is layer (a) only, with no dated log and no shared layer.

## 4. Group chats — how multi-agent conversation is actually specified

From `chat-and-collaboration.md` §Start a group chat:

- 2–6 Bots per group.
- Direction primitives: plain message (Bots decide who answers), `@Bot`, **Reply** on a message (directs to that Bot), `@everyone` ("sparingly").
- Kickoff pattern they recommend is *one owner per stage*, written inline:
  > "@Researcher gather the source material and link every claim. @Writer turn the findings into a launch draft. @Reviewer check the draft against the sources and list only blocking issues. Do not publish anything."
- **Approval cards, secret/login requests, and email/Slack drafts do not appear in group chats.** So no group member can approve another's action.
- **Bot-to-group handoff messages are text-only** — "a Bot should send an image directly to another Bot when that teammate must inspect it."
- Replies can leak out of the group into private chats; the fix is an instruction ("Reply here in the group") optionally made durable via memory.
- **There is no switch to stop Bots messaging each other.** The documented answer is "tell them not to, and ask them to remember it."
- Every Bot reply counts toward usage, including inter-Bot messages.

That last point is a real product failure they shipped and documented as a limitation. flobot can do better with a first-class `can_message_peers` capability flag per bot.

## 5. Handoff semantics

From `chat-and-collaboration.md` §Let Bots hand work off:

> "A Bot can send an asynchronous message to another Bot. The receiving Bot wakes, handles the request, and can reply later."

When to hand off: one Bot owns a source system and another owns the deliverable; a specialist should review a draft; a blocker belongs to another role; a long job should continue without you coordinating.

Constraint table (who may message whom):

| Bot kind | Where you ask | Can message other Bots? | Acting for |
|---|---|---|---|
| Personal Bot | app / your own Slack DM | yes | you |
| Personal Bot | Slack channel thread | **no** | nobody (thread is shared) |
| Shared Bot | Slack thread or DM | yes, to that person's own Bots | the teammate who mentioned it |

> "Ask for a single owner at each stage. Parallel handoffs create duplicate work and noisy updates."

flobot already enforces single-owner-per-stage in the store. The missing half is *the recipient acting on the work item* — which flobot has (`handoff` with `run: true`) and Grok Bot describes only in prose.

## 6. Attention states — the inbox model

From `settings-and-notifications.md` §Understand attention states, the sidebar distinguishes exactly three:

1. **Needs attention** — a question, an approval, or a handoff
2. **Unread activity** — a new result
3. Working / typing status

Plus per-Bot notification switch; desktop takes precedence over phone while in use; phone notified ~3 minutes later if still unanswered.

This is a three-state machine flobot's UI does not model. It is cheap: derive from run state + pending approval + unread cursor.

## 7. KEEP / TOSS for flobot

**KEEP**
- Role-as-prose-in-memory instead of a structured role field (or keep both: `Role` for display, memory for behaviour).
- The five-criteria roster rule as onboarding copy in the "create bot" sheet.
- Three-layer memory: per-bot note + dated work log + tiny shared fact set.
- Memory is chat-editable and has no admin screen.
- Group chat with explicit `@`, Reply-to-direct, one-owner-per-stage kickoff.
- Three attention states in the sidebar.
- Hide-from-sidebar ≠ pause routines ≠ delete.
- Duplicate-a-bot semantics (copy config, not history).

**TOSS**
- No model picker (flobot's differentiator is the opposite).
- No switch to stop inter-bot messaging.
- Approvals invisible in group chats.
- Text-only handoffs.

## 8. Gap vs flobot today

| Grok Bot capability | flobot today | Gap |
|---|---|---|
| Durable named bot with memory that compounds | `Bot` + one capped note prepended to prompt | no dated work log; no shared facts; memory not chat-editable |
| Roster sections / pin / hide / duplicate | flat roster | all four missing |
| Primary bot that coordinates | none | missing |
| Group chat 2–6 bots with @/reply routing | handoff bus only (1:1) | no group surface |
| Three attention states | run status only | no "needs attention" derivation |
| Per-bot notification policy | none | missing |
