# The workflow — from "something happened" to "the agent answered"

This is the operating manual for flobot's integrations: the loop you run when
you wire a service to a routine, the checks that prove the wiring is real, and
the demo that proves the whole chain on this Mac.

`docs/integrations.md` is the reference (every endpoint, every provider, every
auth scheme). This document is the *path*: what you do, in what order, and
what "working" looks like at each step.

---

## The loop in one paragraph

1. A service posts an event to flobot's webhook URL.
2. flobot authenticates the caller, normalises the payload into one line of
   text, and starts a run for every routine bound to that integration.
3. The run carries the event as context plus the routine's playbook.
4. The teammate answers on this Mac, on the local route.
5. The answer lands in the run; the fire lands in the integration's event log;
   both show up in the Activity log.

Nothing in that loop needs a provider account, an API key, or the mesh. The
kit ships all ten automations pre-wired to the local route, so the only thing
you add is the secret.

---

## 0. Two things must be true first

**flobot is serving.**

```bash
curl -s http://127.0.0.1:4120/api/v1/roster | head -c 200
```

If that 404s or times out, start it:

```bash
flobot serve --addr 127.0.0.1:4120 --workspace ~/flobot-workspace/your-project
```

**A local model is serving.** The kit runs on the `local` route, which is
pinned to a flocode model profile that points at an MLX server on this Mac:

```bash
mlx_lm.server --model ~/.cache/huggingface/hub/models--mlx-community--Qwen3-Coder-30B-A3B-Instruct-4bit/snapshots/<hash> \
  --port 8059 --host 127.0.0.1
```

and the pin itself:

```bash
curl -s http://127.0.0.1:4120/api/v1/local
curl -X POST http://127.0.0.1:4120/api/v1/local/pin \
  -H 'Content-Type: application/json' \
  -d '{"pin":"local-qwen3-coder-30b-a3b-mlx"}'
```

A local model loads cold and streams slowly, so a local run gets the
ten-minute run budget and six-minute first-token/stall watchdogs — a mesh
model keeps the one-minute defaults.

> If the endpoint is down, the run fails with **"endpoint unreachable"**. The
> use case is fine; the model is not. This is the single most common cause of
> a "broken" automation.

---

## 1. Install the kit

**In the UI.** Open the **Integrations** tab. The **Use cases** gallery sits at
the top, above the provider tiles: ten cards, each with what fires it, what the
agent does, and a checklist of the steps still outside flobot. Pick one.

**From the API.** One call creates the teammate, the playbook, the routine, the
worktree and the connection:

```bash
curl -s -X POST http://127.0.0.1:4120/api/v1/usecases/triage/activate \
  -H 'Content-Type: application/json' \
  -d '{"create_integration":true}' | python3 -m json.tool
```

The response carries `bot`, `skill`, `routine`, `integration`, `url` — and
`secret`, **once**. That is the only time you ever see it.

**From the script.** All ten, fired, with the answers printed:

```bash
scripts/demo-kit.sh                 # install if missing, fire all ten
scripts/demo-kit.sh --fresh         # wipe the kit first, then reinstall
scripts/demo-kit.sh --only triage   # just one
scripts/demo-kit.sh --webhook-demo  # also prove the signed /fire path
```

Each activation gives the routine **its own worktree**. Ten automations firing
at once must not contend for one checkout — flocode refuses two sessions in
the same directory, and the second one would die with "already in use".

---

## 2. Connect the provider

Copy the **URL** and the **secret** from the integration card into the
provider. That is the whole ceremony.

| Provider | Where the secret goes | Scheme |
|---|---|---|
| GitHub | Settings → Webhooks → the webhook's *Secret* | `X-Hub-Signature-256: sha256=…` |
| Slack | Signing secret of the app | `X-Slack-Signature: v0=…` + timestamp |
| Stripe | Endpoint signing secret | `Stripe-Signature: t=…,v1=…` |
| Telegram | The bot token, as `?key=` | bearer/query |
| Gmail | The Apps Script's `SECRET` | bearer |
| Discord / Linear | The app secret | bearer |
| Anything else | The `Authorization` header | bearer |

The provider signs the request; flobot checks the signature in constant time
and refuses anything older than five minutes, so a captured request is worth
nothing later.

**Check it worked:** the integration card shows `fired: N` and the event log
lists each delivery with its status: `fired`, `rejected`, `ignored` or
`error`.

---

## 3. Fire the first event

Do this before you touch a real provider — it proves the whole chain without
one:

```bash
# the built-in sample for that provider
curl -s -X POST http://127.0.0.1:4120/api/v1/integrations/int-XXXX/test

# or your own payload
curl -s -X POST http://127.0.0.1:4120/api/v1/integrations/int-XXXX/test \
  -H 'Content-Type: application/json' \
  -d '{"event":"{\"text\":\"disk 7 is at 91%\"}"}'
```

In the UI that button is **Send a test event** on the integration card.

**What a good answer looks like** — `fired: 1` and a `runs` array with one id:

```json
{ "schema": "flobot.integration_test.v1", "fired": 1,
  "runs": ["run-2f1c…"], "preview": "Webhook: disk 7 is at 91%" }
```

`fired: 0` means the event arrived but nothing ran: no routine is bound, or
every bound routine is paused or busy. The reason is in the response and in
the event log.

---

## 4. Read the answer

```bash
curl -s http://127.0.0.1:4120/api/v1/runs/run-2f1c…/events
```

Events arrive as they happen: `content` while the model writes, then `done`.
An `err` event means the run failed — the text says why (model unreachable,
timeout, tool error).

The same answer is visible in the UI: the run's thread, and the Activity log
for the integration's own view of deliveries.

---

## 5. Make it yours

Every use case is four objects you can edit:

| Object | Where | What it controls |
|---|---|---|
| **Playbook** (skill) | the routine's skill | what the agent is told to do and to answer |
| **Routine** | the integration card | which playbook, which teammate, when it runs, whether it is paused, which tools it may use |
| **Integration** | the card | where events come from and how they are authenticated |
| **Teammate** (bot) | the roster | the name, the role, and the route (`local`) |

The playbooks shipped with the kit are written for a local 30B model:
read-only tools, a bounded answer, and an explicit "not enough context"
instead of a guess. If an answer is too vague, the playbook is the first thing
to tighten — say what to answer and in what shape, and what to say when the
event does not carry the fact.

To pause an automation without deleting it: `POST /api/v1/routines/{id}/pause`.
To arm it again: `POST /api/v1/routines/{id}/test-run` then
`POST /api/v1/routines/{id}/enable`.

**The tool policy.** A routine carries one: `tools: "none"` (the kit's
default — the child is started with no tools and can only answer in text), or
a comma-separated list such as `"read,glob,grep"`. Change it without touching
the playbook:

```bash
curl -X PATCH http://127.0.0.1:4120/api/v1/routines/<id> \
  -H 'Content-Type: application/json' -d '{"tools":"none"}'
```

Why it is not just a line in the playbook: on the local route the model's tool
calls are never executed, and a tool-happy 30B model asked to "answer in
plain text" sometimes returns the tool call as its answer. The policy is a
property of the run, not of the wording.

---

## 6. Build your own automation

The kit is a template, not a limit. The shortest path to your own:

1. `POST /api/v1/integrations` — `{kind, name, detail, auth_mode}`. You get the
   secret once.
2. `POST /api/v1/routines` — a teammate, a playbook (skill), a trigger, and
   `integration_id` to bind it.
3. `POST /api/v1/integrations/{id}/test` — fire your own payload and read the
   answer.

Or reuse an existing connection: `POST /api/v1/routines/{id}/integration` with
`{"integration_id":"int-XXXX"}` binds any routine to any integration, and an
empty string unbinds it. Several routines may share one integration; one
routine may listen to several.

---

## 7. Day two

- **Rotate a secret** when it leaks: `POST /api/v1/integrations/{id}/rotate`.
  The new secret is returned once; the old one stops working immediately.
  Update the provider right away — a provider retrying with the old secret
  shows up as `rejected` events, which is exactly how you notice.
- **A provider elsewhere?** The receiver binds to `127.0.0.1`. Use a tunnel
  (`ssh -L 4120:127.0.0.1:4120`) or a local forwarder.
- **Several events at once?** flobot does not queue: if the routine's
  teammate is already running, the event is recorded as `ignored` and the
  provider gets `202`. Nothing is lost silently — but if you need a queue,
  the routine belongs on a teammate that is not busy.

---

## 8. The demo, and what "working" means

`scripts/demo-kit.sh` is the acceptance test for this whole subsystem. It
installs the kit (idempotently — it reuses what is already wired), fires each
use case's own example event, waits for the run, and prints one line per use
case with the model's answer.

A green run looks like this:

```
  OK   triage       Triage a new issue                 bug — login is broken on Safari…
  OK   chatops      Chatops from your phone            …
  OK   oncall       On-call: investigate an alert       …
  ...
  10 of 10 use cases answered
```

The checklist for "it is implemented properly":

- [ ] `GET /api/v1/usecases` returns ten use cases.
- [ ] `GET /api/v1/roster` shows ten teammates, ten playbooks, ten routines —
      all on the `local` route, none paused.
- [ ] `GET /api/v1/integrations` shows ten integrations, each with at least one
      bound routine and a `url`; no secret in the payload (only a four-character
      hint).
- [ ] Every use case's example event produces an answer that follows its
      playbook: six fixed lines for the inbox, JSON only for the workflow step,
      a single safe command for the sensor.
- [ ] A request with a wrong credential is refused with `401` and recorded as
      `rejected`.
- [ ] A request with the right credential is accepted and starts exactly one
      run per bound routine.

`scripts/demo-kit.sh --webhook-demo` checks the last two for you: it fires a
correctly signed request at the real `/fire` receiver, and then fires one with
a wrong secret to prove it is refused.

---

## 9. Deliberately not there yet

- **Outbound**: flobot receives; it does not push back to a provider. The
  agent can do that itself with its own tools.
- **MCP**: an MCP surface (tools instead of webhooks) is the natural next
  step; the provider catalogue and the event log are already shaped for it.
- **Delivery guarantees**: no retries, no queue. An event that arrives while
  the routine is busy is recorded as `ignored`, not silently dropped.
