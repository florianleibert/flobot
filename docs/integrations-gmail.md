# Gmail integration — fire flobot routines on new mail

flobot's Integrations panel connects external services to your routines. This
doc is the Gmail path: a tiny Google Apps Script watches a mailbox and POSTs
to flobot's webhook receiver whenever new mail arrives, which fires every
routine bound to that integration.

No Gmail API keys ever touch flobot. The Apps Script runs in Google's cloud
and only ever calls out to `http://127.0.0.1:4120` with the webhook secret
flobot generated.

The full catalogue — Slack, GitHub, Telegram, Discord, Linear, Stripe, any
webhook — is in [integrations.md](integrations.md).

---

## 1. Create the integration in flobot

1. Open flobot at `http://127.0.0.1:4120` → **Integrations** → **Connect a
   service** → **Gmail**.
2. Give it a name (e.g. `work inbox`) and a detail (e.g. `label: inbox`).
   The authentication scheme for Gmail is `Bearer` — the Apps Script can set
   headers, so it uses the secret directly.
3. Press **Create integration**. flobot shows the **webhook URL** and the
   **secret exactly once** — copy both now. The secret looks like `a1b2c3…`
   (48 hex chars). You will paste it into the Apps Script in step 3.
4. Press **Send a test event** to prove the chain works before Gmail ever
   calls: flobot fires a realistic sample payload at its own receiver and
   tells you how many routines fired. Zero means no routine is bound yet —
   that is step 2, not a failure.

The receiver is:

```
POST http://127.0.0.1:4120/api/v1/integrations/<id>/fire
Authorization: Bearer <secret>
Content-Type: application/json

{ "text": "new mail: <subject> from <sender>" }
```

The `text` field is handed to the routine's agent as context. It is prompt
text, never a secret.

## 2. Bind a routine to the integration

1. **Routines** → arm a routine (pick a teammate + a skill; the trigger can
   be `manual` — the integration is what fires it).
2. Back on **Integrations**, on the Gmail card use the **bind a routine…**
   dropdown and pick it. The card shows the bound routine as a chip; `×`
   unbinds it.
3. **Test-run** the routine once, then **enable** it. Only enabled routines
   fire (same gate as the scheduler).
4. Press **test** on the card to fire a sample event and watch it land in
   the **Activity** log.

## 3. Deploy the Apps Script

1. Go to <https://script.google.com> → **New project**.
2. Paste the script below.
3. Set the three constants at the top:
   - `FLOBOT_URL` — leave as `http://127.0.0.1:4120` (loopback).
   - `INTEGRATION_ID` — the integration id (from the flobot UI, e.g.
     `int-…`).
   - `SECRET` — the webhook secret you copied in step 1.
4. **Save**, then **Deploy → New deployment → Web app**:
   - Execute as: **Me**
   - Who has access: **Only myself**
5. Copy the deployment's **Web app URL** (ends in `/exec`).
6. In the script editor, open **Triggers** (clock icon) → **Add trigger**:
   - Function: `checkInbox`
   - Event source: **Time-driven**
   - Type: **Minutes timer**, every 1 minute.
7. Run `checkInbox` once manually to authorize Gmail access.

## The script

```javascript
/**
 * Gmail → flobot bridge.
 * Fires every routine bound to a flobot Gmail integration when new mail
 * matching QUERY arrives.
 */

var FLOBOT_URL = "http://127.0.0.1:4120";
var INTEGRATION_ID = "int-REPLACE_ME";
var SECRET = "REPLACE_ME";

// Gmail search query for what counts as "new". Narrow it — flobot only
// wants mail you actually want to automate on.
var QUERY = "is:unread newer_than:1d";

// How long to wait between fires for the same thread, so a long thread
// doesn't re-fire every minute.
var DEDUP_WINDOW_MIN = 15;

function checkInbox() {
  var threads = GmailApp.search(QUERY, 0, 5);
  for (var i = 0; i < threads.length; i++) {
    var thread = threads[i];
    if (!thread.isUnread()) continue;
    var first = thread.getMessages()[0];
    var key = "flobot_fired_" + thread.getId();
    var last = PropertiesService.getScriptProperties().getProperty(key);
    if (last && (Date.now() - Number(last)) < DEDUP_WINDOW_MIN * 60000) continue;

    var subject = first.getSubject() || "(no subject)";
    var from = first.getFrom() || "(unknown)";
    var snippet = (first.getPlainBody() || "").slice(0, 500);

    var fired = fire(subject, from, snippet);
    if (fired) {
      PropertiesService.getScriptProperties().setProperty(key, String(Date.now()));
      // Mark read so it doesn't re-fire on the next tick.
      thread.markRead();
    }
  }
}

function fire(subject, from, snippet) {
  var payload = {
    text: "New mail.\nSubject: " + subject + "\nFrom: " + from +
          "\n\n" + snippet
  };
  var options = {
    method: "post",
    contentType: "application/json",
    headers: { Authorization: "Bearer " + SECRET },
    payload: JSON.stringify(payload),
    muteHttpExceptions: true
  };
  var resp = UrlFetchApp.fetch(
    FLOBOT_URL + "/api/v1/integrations/" + INTEGRATION_ID + "/fire",
    options
  );
  return resp.getResponseCode() === 200;
}
```

## Notes & limits

- **Loopback only.** The receiver binds to `127.0.0.1`, so the Apps Script
  must run on the same machine as flobot (or you must tunnel). For a remote
  Mac, expose flobot over SSH (`ssh -L 4120:127.0.0.1:4120`) and point
  `FLOBOT_URL` at the tunnel.
- **Every minute is the floor.** Apps Script time-driven triggers can't run
  faster than once a minute; Gmail itself is near-real-time only via push
  (which needs a public endpoint). For sub-minute latency, use a generic
  webhook fired by a local watcher instead.
- **The secret is shown once.** If you lose it, press **rotate** on the
  card: the old secret stops working immediately and the new one is shown
  once. Paste the new one into the Apps Script. (Deleting the integration
  also works, but it unbinds the routines — rotating does not.)
- **The Apps Script keeps its own copy** of the secret, in the script
  constants above. Treat it like a password.
- **Secrets never reach flobot's disk except the store's own file**
  (`~/.flobot/integrations.json`, 0600). The Apps Script keeps its copy in
  Google's script properties.

## Other services

The same receiver works for anything that can POST JSON — with the
authentication scheme the provider supports. See
[integrations.md](integrations.md) for the Slack, GitHub, Telegram, Discord,
Linear, Stripe and generic-webhook recipes, the scheme table and the use
cases.
