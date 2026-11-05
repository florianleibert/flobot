# Research round 8 — Skills, routines, and the trigger grammar

**Date:** 2026-11-04
**Track:** A — Grok Bot product surface (primary sources)
**Sources (fetched 2026-11-04):**
- https://docs.x.ai/grok-bot/skills-routines-and-automations.md
- https://docs.x.ai/grok-bot/use-cases.md
- https://docs.x.ai/grok-bot/troubleshooting.md (§A routine did not run)

---

## 1. The two primitives, stated plainly

> "A **skill** is a reusable set of instructions for how to do a task. A **routine** tells one Bot when to run a workflow: on a schedule, or when something happens."

Note the asymmetry that matters for architecture:

- **Skill = account-scoped library.** "Skills are available across your Bots." "Private skills are one library shared by all your Bots." A skill is *not* owned by a bot; a bot must have it *enabled* ("enable it for the current Bot under **Private skills**").
- **Routine = bot-scoped.** A routine belongs to exactly one bot. Deleting the bot deletes its routines.

flobot today: `Skill` is bot-scoped (`BotID`), and `Routine` is bot-scoped. The skill scoping is the wrong way round relative to Grok Bot and relative to how people actually work — you write a method once and let several bots use it.

## 2. The six-part skill contract

They state exactly what a useful skill contains:

1. When to use it
2. Required inputs and access
3. The sequence of work
4. How to validate the result
5. What to return
6. What requires approval

Items 4 and 6 are the ones most agent products omit, and they are exactly what makes an unattended run trustworthy. A skill without a validation clause cannot be tested; a skill without an approval clause cannot be automated safely.

**Skill publishing:** team publish with `Sync` (push later edits to the team copy), `Copy link`, `Unpublish`. A skill **requires a description before it can be published** — "because teammates and their Bots use the description to decide when it applies." That is progressive disclosure driven by a required field: the description is the retrieval key.

## 3. Teach-by-demonstration

Record one browser workflow from the computer view (≤10 minutes, no microphone audio) → the Bot turns it into a **draft** skill → you review → test on a safe example → then schedule.

Their own caveat is the important part: "The learned skill is a draft. Add decision rules, failure handling, and approval boundaries that may not be obvious from one example."

This is the single highest-leverage onboarding feature in the product: it converts tacit knowledge into an artifact without asking the user to write a prompt. flobot has no equivalent, but flobot has something better to record from — its own run transcripts. A "distill this run into a skill" action over `runs` + `events` is cheaper than screen recording and captures tool calls, not pixels.

## 4. The trigger grammar (complete enumeration)

From `skills-routines-and-automations.md` §Create a routine and §Trigger work from an event:

| Trigger class | Concrete forms | Notes |
|---|---|---|
| Schedule | plain-language schedule ("every weekday at 8:00 AM", "every 2 hours"), resolved to a stored schedule shown back in words | min interval **5 minutes**; time zone from Settings → Bot → Timezone (auto-detect or pinned) |
| Slack | bot mentioned / word-or-phrase mentioned / reaction added / any message; one channel or all of Slack | "A Slack trigger is part of a routine, not the Slack app" |
| GitHub / Linear / Sentry / PagerDuty | event-type triggers | "Cursor account integrations start these routines; they're separate from the Slack or GitHub apps and may need their own connection" |
| Email | mail to the bot's own `mail.grokbot.com` address | requires Agent Email enabled by admin |
| Webhook | `POST` URL + `Authorization: Bearer <key>` | body JSON forwarded to the routine instruction |

Semantics that are easy to get wrong and that they specify:

- **Only new activity counts.** "events from before the trigger existed don't start a run."
- **A new routine does not run at creation.** "If you set up 'every day at 8:00 AM' at 8:05, the first run is tomorrow at 8:00 AM."
- **`200` means accepted, not finished.** "A `200` response means Grok Bot accepted the call and started a run, not that the Bot has finished."
- **Rate limits:** 300 runs/hour across all routines, 200 runs/hour per routine; exceeding either fails with `Automation run rate limit exceeded`; **not retried automatically**.
- **Retention:** 20 most recent run records per routine; 50 routines per bot.
- **Run states:** `Running`, `Succeeded`, `Failed` (+ reason), `No runs yet`.
- **Unattended-usage circuit breaker:** "Grok Bot may ask whether to keep routines running after a long period away and pause them if there is no response."
- **Guidance against broad listeners:** "Avoid broad listeners such as 'every new message.' They create noise, consume usage, and increase the chance of acting on irrelevant input."

## 5. "Design routines for trust" — their eight rules

1. Automate preparation before execution.
2. Have the Bot draft, reconcile, or recommend first.
3. Require approval for sending, purchasing, deleting, publishing, or changing production systems.
4. Include a no-data and stale-data policy.
5. Make retries idempotent where possible.
6. Tell the Bot where to report partial completion.
7. Re-test after a website, app, or source format changes.
8. (from §Test before enabling) Stop at the intended approval point; make failure states explicit.

Rule 4 — *no-data and stale-data policy* — is the one almost every agent product misses. Their example prompt encodes it inline: "If the source data is unavailable, report the failure instead of using old data."

## 6. Test-before-enable

`Test` runs the routine immediately with real side effects: "A test run performs real work. It can navigate websites, change files, and call connected tools." Not every routine offers it.

The review checklist they give after a test run is a ready-made eval rubric:

- Did it select current inputs?
- Does the output meet the required format?
- Does every action have a source or audit trail?
- Did it stop at the intended approval point?
- Are failure states explicit?

## 7. KEEP / TOSS for flobot

**KEEP**
- Skill = account library + per-bot enablement (invert flobot's current scoping).
- The six-part skill contract as a *template* in the skill editor, with validation and approval clauses required before a skill can be attached to a routine.
- Description required before publish/share — used as the retrieval key.
- "Distill this run into a skill" instead of screen-recording teach-by-demonstration.
- Plain-language schedule input, stored schedule echoed back in words for confirmation.
- Explicit "new routine does not fire at creation".
- Per-routine and global hourly rate limits with a distinct error code.
- Unattended-usage circuit breaker (ask after long absence, pause on silence).
- No-data / stale-data policy as a first-class field on a routine.
- The five-question post-test review checklist as an eval rubric.

**TOSS**
- Trigger-as-prose ("ask the Bot for the trigger you want") — flobot should keep structured triggers.
- Test runs with unguarded real side effects.
- Rate-limit failures that are silently not retried.

## 8. Gap vs flobot today

| Capability | flobot today | Gap |
|---|---|---|
| Skill scope | bot-scoped (`Skill.BotID`) | no shared library, no per-bot enable |
| Skill contract | free-text body | no validation/approval clauses |
| Skill from a run | none | missing |
| Schedule grammar | cron + every (`internal/schedule`) | no plain-language input; no echo-back confirmation |
| Event triggers | integrations webhooks only | no Slack/GitHub/Linear/Sentry/PagerDuty/email classes |
| Rate limits | none observed | missing |
| Run history retention | full runs table | no per-routine cap/policy |
| No-data policy | none | missing |
| Unattended circuit breaker | none | missing |
| Post-test review rubric | none | missing |
