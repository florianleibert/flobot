# Research round 9 — Approvals, Auto Review, Vault, and the trust boundary

**Date:** 2026-11-04
**Track:** A — Grok Bot product surface (primary sources)
**Sources (fetched 2026-11-04):**
- https://docs.x.ai/grok-bot/approvals-security-and-privacy.md
- https://docs.x.ai/grok-bot/vault.md
- https://docs.x.ai/grok-bot/security.md
- https://docs.x.ai/grok-bot/teams-and-enterprises.md
- https://docs.x.ai/grok-bot/shared-bots.md

---

## 1. Auto Review — a model-based gate, not a permission table

> "Auto Review checks each action your Bot is about to take, for example before it runs a command, uses a connected app, or uses its computer. Most actions run on their own."

The architecture is three layers, and the ordering is the whole design:

1. **Built-in safety checks** — "always apply, whatever your rules say." Not user-editable.
2. **Auto Review** — a model that classifies each proposed action as allow / ask.
3. **Custom Rules** — up to 20 `Allow automatically` + 20 `Ask first`, each ≤1000 chars, plain language ("reply to emails for me").

Resolution rule: **`Ask first` wins when both match.** `Allow automatically` "let matching actions proceed only when the automated review does not identify another reason to stop" — the rule is a *hint to the classifier*, not a bypass.

**The provenance rule is the sharpest idea on the page:**

> "Auto Review takes into account what you asked for in your own messages. Text from somewhere else, such as an email or a web page, doesn't count as your request."

That is prompt-injection defence expressed as an approval-input rule: only the human's own turn can authorise. Any content that arrived through a tool result is untrusted for authorisation purposes.

**Card vocabulary** (each label is a distinct semantic, not cosmetic):

| Card | Buttons | Meaning |
|---|---|---|
| default | `Allow once` / `Always allow` / `Deny` | `Always allow` writes an Allow rule and shows `Saved to Custom Rules` |
| terminal command | `Allow` / `Skip` / `Manage settings` | no rule offered |
| matched an `Ask first` rule | `Allow` / `Not now` / `Edit rule` | rule is editable from the card |
| no rule available | `Allow` only | no `Always allow` |
| Slack thread | `Allow once` / `Deny` / `View details` | only the asker may answer |
| group chat | as default minus `Always allow` | only the asker may answer; others see who it waits on |

Warnings attached to cards: `This can't be undone`, `This changes sign-in or security settings`, `This shares your data outside this chat`.

**Deny semantics:** "It won't try that action again or ask you about it until you send another message." Deny is scoped to the current turn, not permanent.

**Expiry, split by origin — the key durability rule:**

| Origin of the work | Card lifetime |
|---|---|
| a chat message you sent | waits indefinitely, no timer |
| a routine / incoming message / background work | **expires after 30 minutes** |
| a new message sent instead of answering | open card closes |
| changing Auto Review settings | open cards close |

Closed cards show `Expired`; if a rule was offered you can still press `Always allow next time`.

**Email gets a third tier:** chat-requested email sends after Auto Review; routine- or inbound-triggered email **waits as a draft until you press Send**; forwards and unreadable attachments always wait. So the matrix is (origin × action class) → {auto, approve-now, draft-and-wait}.

## 2. Vault — secrets the model never sees

Resolution order for a login: **Vault → connected password manager (1Password etc.) → Secure Form**. "Values from Vault and Secure Forms are filled into the page without being shown to the model."

- Secure Form: masked, excluded from transcript, not shown to model.
- Hardware security keys: usable from the desktop while `Use hardware security keys` is on; every use asks approval first.
- Human-only steps (SSO, passkey, CAPTCHA, device approval, card verification) → **computer takeover**: `Take over` → do it → `I'm done` → tell the bot to continue.
- Opt-out of convenience: leave `Securely save to Bot's vault` unchecked to force per-payment approval.

The invariant to copy: **the secret value never enters the model context.** Not redacted-after-the-fact — never admitted.

## 3. The shared-computer boundary — an honest negative

> "All of your Bots share one cloud computer assigned to your user account. Files, browser sessions, and command line credentials on that computer are available across your Bot roster. **Do not use separate Bots as a security boundary.**"

And: "Sharing a Bot is not a security boundary." A public share link copies configuration only.

flobot's equivalent: all bots run as the same OS user on the same Mac. Same honesty is required in flobot's docs — but flobot can actually do better here because it can give each bot its own workspace directory and its own credential scope (see round 24).

## 4. Local execution — a separate capability with its own ceiling

Cloud-computer work and local-computer work are **different permissions**. `Execution on this computer` per desktop: `Ask every time` (default) / `Always allow` / `Never allow`. Team admin can cap it; "when the team's policy is stricter than yours, the team's applies." First-run prompt: `Allow Grok Bot and all Bots to run commands on your local computer?` with `Always allow` / `Allow once` / `Never` / `Deny once`.

Known shipped bug they document: a bot may say "Local tools are turned off…" even when the setting is permissive. Documented as unfixable client-side. Worth noting as evidence that capability-ceiling logic is where these systems rot.

## 5. Audit and Action Recording — what they consider safe to log

From `teams-and-enterprises.md` §Action Recording, the sanitisation spec per event type:

| Event | Recorded | Stripped |
|---|---|---|
| shell command | the command, secret-scrubbed | secrets |
| browser navigation | `scheme://host/path` + title | query strings, credentials |
| computer use session | action count, screenshot count, duration | screenshots, clicks, typed text |
| everything else | ids, outcomes, durations | tool arguments, file paths, message content |
| skills read | which skill | — |
| handoffs to subagents/cloud agents | that it happened | content |

Events are tagged `cursor.surface=grok_bot` for OTel export. Off by default. "Recorded events do not appear on the Audit Log page" — two separate systems: admin/control-plane audit vs. agent action recording.

That split is worth copying: flobot's event log today is one stream. A product needs (a) an operator-facing action log with a documented redaction policy per event class, and (b) an admin/control-plane audit log.

## 6. Enterprise control surface — the full list, for scoping what flobot should *not* build

Access/identity: org enable switch, group access, SCIM 2.0, team icon.
Capabilities: Cloud Agents delegation on/off, public template sharing, Manage Shared Bots (access + auto-add), Page sharing (team/specific/off), Agent Email (`mail.grokbot.com`), connector policy (inherits Cursor MCP policy), Execution on Local Computer ceiling, Allow Local Egress Routing.
Rules/approvals: Team Rules (scoped to Cursor/Grok Bot/both), Enforce Auto-review (lock), team Auto-review rules, **Inference Hooks** (signed per-request webhook to a customer AI-security server that allows/denies before the model runs; starts in Shadow mode).
Computers/network: Network Controls (allow-all / defaults+allowlist / allowlist-only; "Apply to MCP servers"), Team Setup Scripts (manifests run on every computer), Team Secrets (env vars for setup scripts only; ≤100 secrets, ≤32 KB each, ≤96 KB total; output secret-scrubbed; reserved names rejected), Allow Sudo, Manage Member Computers (recreate/terminate/delete), Terminate Inactive Computers at 30 days.
Logging: Action Recording, Audit logs, OTel Export.
Groups: group settings **only widen**, except network policy which replaces (most restrictive wins).

flobot is a local-first single-user app. Almost none of this applies. The two pieces that *do* transfer: **Inference Hooks** (a pre-model allow/deny hook — flobot can implement it as a local policy script) and **Team Secrets → env-var injection with output scrubbing** (flobot can implement it as per-bot keychain scoping).

## 7. KEEP / TOSS for flobot

**KEEP**
- Three-layer gate: built-in checks → model review → plain-language rules; `Ask first` always wins.
- **Provenance rule:** only the operator's own message authorises; tool-result content never does.
- Card vocabulary with distinct semantics per case; warnings for irreversible / security-changing / data-sharing.
- Deny = scoped to this turn.
- Expiry by origin: interactive waits forever; background expires at 30 min.
- Email-style three-tier matrix (auto / approve / draft-and-wait) generalised to any outbound side effect.
- Vault invariant: secret values never enter model context.
- Takeover flow for human-only steps.
- Separate action log (redaction policy per event class) from control-plane audit log.
- Honest documentation of the shared-computer non-boundary.

**TOSS**
- Model-based Auto Review as the *only* gate (it is probabilistic; flobot should pair it with deterministic capability flags).
- Enterprise admin surface (out of scope for local-first).
- Approval cards invisible in group chats.

## 8. Gap vs flobot today

| Capability | flobot today | Gap |
|---|---|---|
| Approval gate | none found in codebase inventory | entire subsystem missing |
| Rule authoring in plain language | none | missing |
| Provenance-aware authorisation | none | missing |
| Secret handling | env vars passed to child (`internal/executor/credentials.go`) | no keychain scoping, no masked-input path |
| Action log with redaction policy | raw event stream | no per-class redaction spec |
| Control-plane audit log | none | missing |
| Takeover flow | none | missing |
| Expiry by origin | n/a | missing |
