# Integrations — let an outside service fire your routines

An **integration** is an inbound event source: Gmail, Slack, GitHub, Telegram,
Discord, Linear, Stripe, or anything else that can POST to a URL. When the
event arrives, every routine bound to that integration fires, and the event
becomes the agent's context.

```
provider  ──POST──▶  http://127.0.0.1:4120/api/v1/integrations/<id>/fire
                        │  authenticate (secret / HMAC signature)
                        │  normalise the payload into one readable line
                        ▼
                     routine(s)  ──▶  run  ──▶  agent (bot + skill)
```

Everything stays on the machine: the secret lives in
`~/.flobot/integrations.json` (0600), the receiver binds to loopback, and no
provider credential is ever handed to flobot — a provider signs with a secret
*you* choose and paste into flobot, so flobot never holds the provider's own
credentials.

---

## 1. The connect flow

**Integrations → Connect a service** opens the gallery. Pick a provider:

1. **Name it** — `work inbox`, `repo hooks`, `pager`.
2. **Pick the authentication** — the scheme the provider can actually
   produce (see §2).
3. **Create** — flobot shows the **webhook URL** and the **secret exactly
   once**. Copy both into the provider.
4. **Send a test event** — flobot fires a realistic sample payload at its own
   receiver and tells you how many routines fired. This is the proof the
   chain works before the provider ever calls.
5. **Bind a routine** — pick the routine from the dropdown on the card. Only
   enabled routines fire (same gate as the scheduler).

If you lose the secret, **rotate** it on the card: the old secret dies
immediately and the new one is shown once. Rotation is the fix for a leaked
secret — you do not have to recreate the integration and rebind its routines.

### What the panel shows

| Field | Meaning |
|---|---|
| status chip | `live` (last event fired), `idle` (never fired), `rejected` (last event was refused), `no routine` (nothing bound) |
| URL | the absolute webhook URL — copy it with one click |
| secret | the last four characters only; the secret itself is never listed |
| fires | how many events started a run |
| last | when it last fired, relative |
| routines | the bound routines; `×` unbinds one |
| Activity | every event, newest first, filterable — `fired`, `rejected`, `ignored`, `error` |

---

## 2. Authentication schemes

The provider decides which scheme you can use. Every check is constant-time,
and the signed schemes also enforce a **5 minute timestamp window** so a
captured request cannot be replayed later.

| Scheme | What the provider sends | Fits |
|---|---|---|
| `bearer` | `Authorization: Bearer <secret>` | curl, n8n, Zapier, Make, Zapier webhooks, your own script |
| `query` | `?key=<secret>` in the URL | providers that POST to a fixed URL and cannot set headers (Slack incoming webhooks, Zapier catch hooks, Telegram `secret_token`) |
| `hmac` | `X-Hub-Signature-256: sha256=<hex>` over the raw body | GitHub, Linear (any HMAC-SHA256 sender) |
| `slack` | `X-Slack-Signature: v0=<hex>` over `v0:<timestamp>:<body>` | Slack apps (signing secret) |
| `stripe` | `Stripe-Signature: t=<ts>,v1=<hex>` over `<ts>.<body>` | Stripe endpoints |
| `standard` | `webhook-id` / `webhook-timestamp` / `webhook-signature: v1,<base64>` over `<id>.<ts>.<body>` | standard-webhooks senders (secret is `whsec_…`) |

The default for a provider is pre-selected in the connect sheet; you can
switch it, and the sheet tells you where the credential goes.

---

## 3. Provider recipes

### Gmail — new mail fires a routine

No Gmail API key ever touches flobot. A Google Apps Script watches the mailbox
and POSTs to the receiver.

1. **Integrations → Gmail → create**.
2. Copy the **Apps Script** tab from the connect sheet (or read
   [integrations-gmail.md](integrations-gmail.md) for the annotated version).
3. Paste it into <https://script.google.com>, set the URL, the integration id
   and the secret, deploy as a web app, add a 1-minute trigger.
4. Bind a routine. The next matching mail fires it.

The agent sees: `Gmail: <subject> — <snippet>`.

### Slack — messages, mentions and reactions

1. Create a Slack app, enable the Events API, set the **Request URL** to
   flobot's webhook URL.
2. Paste the app's **signing secret** into flobot's secret field (scheme
   `slack`), or use `query` if you use an incoming webhook instead.
3. Subscribe to the events you care about, bind a routine.

The agent sees: `Slack: <message text>`.

### GitHub — issues, pull requests, releases, checks

1. Repo → **Settings → Webhooks → Add webhook**.
2. Payload URL = flobot's webhook URL, content type `application/json`.
3. Choose a secret and paste **the same value** into flobot (scheme `hmac`).
4. Select the events, bind a routine.

The agent sees: `GitHub: opened owner/repo #42 Fix the login (by ana)`.

### Telegram — chatops from your phone

1. Create a bot with @BotFather, keep the bot token.
2. `POST https://api.telegram.org/bot<TOKEN>/setWebhook` with
   `{"url": "<flobot url>?key=<secret>"}` — the **secret token** is Telegram's
   way of keeping the URL private (scheme `query`).
3. Bind a routine: every message to the bot fires it.

The agent sees: `Telegram: <message text>`.

### Discord — interactions and channel events

1. Developer portal → **General Information → Interactions Endpoint URL** =
   flobot's webhook URL.
2. Paste the **bot token** as the secret (scheme `bearer` or `query`).
3. Bind a routine.

The agent sees: `Discord: <content>`.

### Linear — issue events

1. Linear → **Settings → Webhooks → Add webhook**, paste flobot's URL.
2. Set a **signing secret** and paste the same value into flobot (scheme
   `hmac`).
3. Bind a routine.

The agent sees: `Linear: Issue Fix the login`.

### Stripe — payments, refunds, disputes

1. Stripe → **Developers → Webhooks → Add endpoint**, paste flobot's URL.
2. Copy the **endpoint signing secret** into flobot (scheme `stripe`).
3. Bind a routine.

The agent sees: `Stripe: charge.succeeded Pro plan`.

### Webhook — anything else

Your script, a cron job, a device, a workflow tool. Create a **Webhook**
integration, copy the URL and secret, and POST JSON. The body becomes the
agent's context:

```bash
curl -X POST "http://127.0.0.1:4120/api/v1/integrations/<id>/fire" \
  -H "Authorization: Bearer <secret>" \
  -H "Content-Type: application/json" \
  -d '{"text":"disk 7 is at 91%"}'
```

---

## 4. What the agent receives

flobot normalises each provider payload into one readable line and hands it to
the routine as context, together with the routine's skill:

| Provider | Context |
|---|---|
| Gmail | `Gmail: <text>` or `from … — <subject> — <snippet>` |
| Slack | `Slack: <text>` |
| Telegram | `Telegram: <text>` |
| Discord | `Discord: <content>` |
| GitHub | `GitHub: <action> <repo> #<n> <title> (by <sender>)` |
| Linear | `Linear: <type> <title>` |
| Stripe | `Stripe: <type> <description>` |
| Webhook | the `text` field, or `title — body`, or the compact JSON |

The context is prompt text. It is never a secret, and the secret is never
part of it.

---

## 5. Use cases worth wiring

The patterns that keep coming up, in rough order of how often people want them:

- **Triage** — a new issue, a new ticket, a new email: read it, classify it,
  draft a reply, open a PR.
- **Chatops** — talk to your crew from Telegram or Discord: every message
  fires a routine, the agent works, the run shows up in the UI.
- **On-call** — an alert from Alertmanager, Sentry or Grafana fires the
  on-call routine; the agent starts investigating with the alert as context.
- **Release** — a GitHub release or a deploy webhook fires the release-notes
  and announcement routine.
- **Billing** — a payment, refund or dispute fires the finance routine.
- **Inbox automation** — mail matching a query fires the "extract the invoice
  numbers and file them" routine.
- **Home / device** — a webhook from a camera, a sensor or a Home Assistant
  automation fires a routine that reacts locally.
- **CI** — a failed build fires the triage routine: read the log, open an
  issue, propose a fix.
- **Scheduled workflows** — n8n / Zapier / Make call the URL as one step in a
  longer workflow, so the agent is the "smart step" of an automation.
- **Secrets as capability** — the webhook secret *is* the permission. Hand it
  to a script, a phone, a colleague; rotate it when it leaks.

### 5.1 The kit: all ten, ready to run on this Mac

Those ten are not a wish list — they ship as a **kit**. **Integrations → Use
cases** lists them; picking one creates, in one step:

1. a **teammate** on the `local` route (this Mac's MLX model — no mesh hop,
   no API key, no cloud bill),
2. the **playbook** it follows (written for a local model: read-only tools, a
   bounded answer, an explicit "not enough context" instead of a guess),
3. the **routine** that binds the two, armed and ready, and
4. the **connection** — the provider integration with the right authentication
   scheme — with its secret handed out once.

Then you paste that one secret into the provider and the automation is live.
**Send a test event** in the same sheet fires the example event through the
whole chain — that is the proof, and it needs no provider.

Each activation also gives the routine **its own worktree**, so ten
automations can fire at the same time without fighting over one checkout —
flocode refuses to run two sessions in the same directory, and without the
worktree the second one dies with "already in use".

| Use case | Fires from | Scheme | What the agent does |
|---|---|---|---|
| Triage a new issue | GitHub | HMAC | classify the issue, name the next action |
| Chatops from your phone | Telegram | bearer | run the request, report the real output |
| On-call: investigate an alert | webhook | bearer | what is broken, two checks to run first |
| Release notes from a release event | GitHub | HMAC | one line, five highlights, upgrade notes |
| Reconcile a payment | Stripe | stripe | amount, purpose, duplicate check, one action |
| Extract facts from an email | Gmail | bearer | six fixed lines: subject, from, when, amount, due, action |
| React to a sensor event | webhook | bearer | what changed, the threshold, one safe command |
| Diagnose a failed build | webhook | bearer | failing stage, first error line, likely cause |
| Be the smart step in an automation | webhook | bearer | parse the input, output only the result |
| A bookmarked URL that runs a task | webhook | query | one bounded local task, then stop |

**What "local" needs.** The local route is pinned to a flocode model profile
that points at an MLX server on this Mac. Two things must be true:

1. **A local MLX endpoint serves the model the pin names.** For the shipped
   kit that is flocode's `local-qwen3-coder-30b-a3b-mlx` profile
   (`mlx-community/Qwen3-Coder-30B-A3B-Instruct-4bit` on
   `127.0.0.1:8059`):

   ```bash
   mlx_lm.server --model ~/.cache/huggingface/hub/models--mlx-community--Qwen3-Coder-30B-A3B-Instruct-4bit/snapshots/<hash> \
     --port 8059 --host 127.0.0.1
   ```

2. **flobot's local route points at that profile.** The pin *is* the profile
   name; `flocode models` lists them all:

   ```bash
   curl -X POST http://127.0.0.1:4120/api/v1/local/pin \
     -H 'Content-Type: application/json' \
     -d '{"pin":"local-qwen3-coder-30b-a3b-mlx"}'
   ```

If the endpoint is down the run fails with "endpoint unreachable" — the use
case itself is fine, the model is not. A local model loads cold, so runs are
given the full ten-minute budget instead of the sixty seconds a mesh model
gets.

**API**: `GET /api/v1/usecases` returns the kit;
`POST /api/v1/usecases/{id}/activate` (body `{"create_integration":true}` or
`{"integration_id":"int-…"}`) creates it and returns the routine, the run id
of the warm-up and — for a new connection — the secret, once.

---

## 6. HTTP API

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/v1/integrations` | list integrations with routines, events, URL, secret hint, status |
| `GET` | `/api/v1/integrations/providers` | the provider catalogue and the auth schemes |
| `POST` | `/api/v1/integrations` | create one; body `{kind, name, detail, auth_mode}` — returns the secret **once** |
| `POST` | `/api/v1/integrations/{id}/fire` | the webhook receiver |
| `POST` | `/api/v1/integrations/{id}/test` | fire a sample payload from the inside |
| `POST` | `/api/v1/integrations/{id}/rotate` | replace the secret; the new one is returned **once** |
| `GET` | `/api/v1/integrations/{id}/events` | the fire history, newest first |
| `DELETE` | `/api/v1/integrations/{id}` | remove it; bound routines are unbound |
| `POST` | `/api/v1/routines/{id}/integration` | bind (`integration_id`) or unbind (`""`) |

Fire response:

```json
{ "schema": "flobot.integration_fire.v1", "integration": "int-…", "fired": 1, "runs": ["run-…"] }
```

`fired: 0` with HTTP 202 means the event was received but nothing ran — no
routine is bound, or every bound routine is paused or busy. The event is still
recorded, so the Activity log shows exactly what happened.

---

## 7. Security notes

- The receiver binds to `127.0.0.1`. A provider that runs elsewhere needs a
  tunnel (`ssh -L 4120:127.0.0.1:4120`) or a local forwarder — nothing is
  exposed to the network by default.
- Secrets are 48 hex characters (or `whsec_…` for the standard scheme),
  generated from the system CSPRNG, stored 0600 in `~/.flobot/integrations.json`.
- The list API never returns a secret — only the last four characters.
- Credential comparisons are constant-time; signed schemes also reject
  timestamps outside a 5 minute window (replay protection).
- A rejected credential is recorded as a `rejected` event, so a provider
  misconfiguration is visible instead of silent.
- Rotate on suspicion. The old secret stops working immediately.

## 8. Troubleshooting

| Symptom | What it means | Fix |
|---|---|---|
| `401 invalid webhook credential` | the provider presents the wrong secret | rotate the secret in flobot, paste the new one into the provider |
| `401` right after a rotation | the provider still has the old secret | update the provider — the old one is dead by design |
| Events show `rejected` in the Activity log | same as above, but the provider keeps retrying | check the secret, then `test` the integration |
| Events show `no routine` / nothing fires | no routine is bound, or the routine is paused | bind a routine on the card, test-run it, then enable it |
| `202` with `fired: 0` | the event arrived, the routine did not run | the routine is paused, busy, or its bot is already running |
| The provider says the URL is unreachable | the provider runs on another machine | tunnel the port, or use a local script as the bridge |
| The agent ignores the event | the payload had no recognisable text | send `{"text": "…"}`, or check the normalised line in the Activity log |

## 9. Not yet (deliberately)

- **Outbound**: flobot cannot yet push *to* a provider (post a Slack message,
  open a GitHub issue). Today the agent does that itself with its own tools —
  the integration only receives events.
- **MCP**: an MCP server surface (tools instead of webhooks) is the natural
  next step; the provider catalogue and the event log are already shaped for
  it.
- **Delivery guarantees**: flobot answers `202` when an event arrived but
  nothing ran. It does not retry a provider's delivery, and it does not queue
  events while the routine's bot is busy — the event is recorded as
  `ignored`, so nothing is silently lost.

## 10. Where this came from

The provider list, the authentication schemes and the setup flow were
researched against GitHub: the standard-webhooks specification (the scheme
`standard` follows it exactly), Svix, GitHub, Slack, Stripe, Linear,
Telegram, Discord, n8n and the agent frameworks that ship their own webhook
and approval patterns. The panel's shape follows the operator's own hydra
stack: progressive disclosure, a proof step before the provider is trusted,
an event log as the receipt, and destructive actions that stay visible but
gated.

