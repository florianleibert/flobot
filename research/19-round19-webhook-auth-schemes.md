# Research round 19 — Webhook authentication: the six schemes and their exact wire formats

**Date:** 2026-11-04
**Track:** E — integrations, webhooks, OAuth, MCP
**Sources (fetched 2026-11-04):**
- https://www.standardwebhooks.com/ (spec + TSC roster)
- https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries
- flobot's own `internal/store/integrations.go` (already implements six schemes — see §5)

---

## 1. The problem Standard Webhooks exists to solve, in their words

> "Every webhook implementation needs to protect themselves and their users from **SSRF, spoofing, and replay attacks**." And: "every webhooks provider implements them differently and with varying quality… consumers need to have a different implementation for each provider."

Three named threats, three required mitigations. Any integration subsystem that does not address all three is incomplete:

| Threat | Mitigation |
|---|---|
| Spoofing | HMAC over the raw body with a shared secret |
| Replay | timestamp in the signed payload + a tolerance window |
| SSRF | never fetch a URL supplied in a payload without an allowlist |

## 2. GitHub's scheme — the reference implementation of "spoofing only"

Wire format:
- Header `X-Hub-Signature-256`
- Value `sha256=<hex>` where hex = HMAC-SHA256(secret, raw_body)
- Body handled as **UTF-8**; "Webhook payloads can contain unicode characters."
- Legacy `X-Hub-Signature` uses HMAC-SHA1 and "is only included for legacy purposes."

Two operational rules they state that are easy to miss:

1. **"Never use a plain `==` operator."** Use `secure_compare` / `crypto.timingSafeEqual` / `hmac.compare_digest`.
2. **"Make sure that the payload and headers are not modified before verification. For example, if you use a proxy or load balancer, make sure that the proxy or load balancer does not modify the payload or headers."** — i.e. verify against the *raw* bytes, before any JSON re-serialisation. This is the #1 real-world cause of signature failures.

They publish a test vector, which is the right practice and worth copying:

```
secret:  "It's a Secret to Everybody"
payload: "Hello, World!"
signature: 757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17
X-Hub-Signature-256: sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17
```

A published vector means an implementer can prove their verifier correct without a live endpoint. flobot should ship one per scheme.

**GitHub's gap:** no timestamp, therefore no replay protection. A captured delivery can be replayed forever. That is why the other schemes exist.

## 3. Standard Webhooks' scheme — spoofing + replay

Signed content: `<id>.<timestamp>.<body>`. Headers `webhook-id`, `webhook-timestamp`, `webhook-signature` (space-separated list of `v1,<base64>` signatures to allow rotation). Secret is `whsec_`-prefixed base64.

Three properties this buys that GitHub's does not:

1. **Replay window** via the timestamp.
2. **Multiple valid signatures** in one header → zero-downtime secret rotation.
3. **An id** for idempotency/dedupe on the receiver side.

flobot already implements this shape (round-19 §5), including the `whsec_` prefix and rotation.

## 4. The full scheme matrix flobot should support, with the exact signed string

| Scheme | Signed string | Header | Replay defence | Rotation |
|---|---|---|---|---|
| `bearer` | — | `Authorization: Bearer <secret>` | none | new secret |
| `query` | — | `?key=<secret>` | none | new secret |
| `hmac` (GitHub) | `<body>` | `X-Hub-Signature-256: sha256=<hex>` | **none** | new secret |
| `slack` | `v0:<ts>:<body>` | `X-Slack-Signature: v0=<hex>`, `X-Slack-Request-Timestamp` | timestamp tolerance | new secret |
| `stripe` | `<ts>.<body>` | `Stripe-Signature: t=<ts>,v1=<hex>` | timestamp tolerance | multiple v1 |
| `standard` | `<id>.<ts>.<body>` | `webhook-id`, `webhook-timestamp`, `webhook-signature: v1,<b64>` | timestamp tolerance | multiple v1 |

flobot implements all six (`internal/store/integrations.go:61–86`) with constant-time comparison (`Authenticate`, :391–399), one-shot rotation (`RotateSecret`, :434–445), and a non-reversible hint (`SecretHint`, :454–460). That is genuinely ahead of most products.

**What flobot is missing on this axis:**

1. **No published test vectors.** No way to prove a verifier correct without a live provider.
2. **No idempotency/dedupe on delivery id.** A provider retry creates a second run. Standard Webhooks gives you `webhook-id` precisely for this; flobot does not use it.
3. **No replay cache.** Timestamp tolerance bounds the window but does not prevent a replay *inside* it.
4. **No outbound webhooks.** Integrations are receive-only (confirmed in the inventory: no outbound tool calls to providers).

## 5. The inbound-only limitation is the strategic gap

The inventory is unambiguous: flobot's integrations are **triggers**, not **tools**. A GitHub webhook can start a routine; the routine cannot comment on the issue.

Grok Bot's equivalent (round 8) is explicitly two-directional: "GitHub / Linear / Sentry / PagerDuty event-type triggers" *and* connected apps the bot can act through. Hermes' skill env-declaration model (round 11) is the mechanism for the outbound half: a skill declares `required_environment_variables`, the runtime injects them into the sandbox, and the skill's scripts call the API.

So the outbound half does not need a bespoke connector framework. It needs:
1. A credential store with per-integration scoping (round 24).
2. A skill-declared env contract (round 11).
3. An approval gate on outbound side effects (round 9).
4. An egress allowlist so a skill cannot POST anywhere (round 22).

That is four subsystems flobot needs anyway, and together they give outbound integrations without a connector SDK.

## 6. SSRF — the threat nobody implements

Standard Webhooks names SSRF first and no provider doc explains it, because it is a *receiver* problem: if a payload contains a URL and your agent fetches it, an attacker who can send you a webhook can make your agent fetch `http://169.254.169.254/` or `http://localhost:8080/admin`.

flobot's exposure is severe and specific: an inbound webhook body becomes a prompt (`server/integrations.go:638–649` creates a run with the payload as prompt). A malicious payload can instruct the agent to fetch an internal URL, and the child process runs with full network access (inventory §23–24: "no egress control").

The mitigation ladder, cheapest first:
1. **Never let tool-result or webhook content authorise an action** (round 9's provenance rule).
2. **Egress allowlist on the child process** — deny RFC1918, link-local, loopback, and metadata endpoints by default.
3. **DNS-rebinding-safe resolution** — resolve, check the IP, then connect to that IP (not re-resolve).
4. **Refuse non-HTTP(S) schemes** in any URL-fetch tool.

## 7. KEEP / TOSS for flobot

**KEEP**
- All six auth schemes (already present).
- Constant-time comparison (already present).
- One-shot rotation + non-reversible hint (already present).
- **Add:** published test vector per scheme.
- **Add:** delivery-id dedupe using `webhook-id` / provider equivalents.
- **Add:** short replay cache keyed by signature.
- **Add:** verify against raw bytes before JSON parsing (document it).
- **Add:** egress allowlist + metadata-endpoint denial for child processes.
- Outbound via skill env declarations + approval gate + egress allowlist, not a connector SDK.

**TOSS**
- A bespoke per-provider connector framework.
- OAuth-per-provider as the primary auth path (see round 20).

## 8. Gap vs flobot today

| Capability | flobot today | Gap |
|---|---|---|
| Six auth schemes | ✓ integrations.go:61–86 | — |
| Constant-time compare | ✓ :391–399 | — |
| Rotation + hint | ✓ :434–445, :454–460 | — |
| Test vectors | none | missing |
| Delivery dedupe | none | missing |
| Replay cache | none | missing |
| Raw-bytes verification documented | unverified | must confirm |
| Outbound actions | none | missing |
| Egress allowlist | none | missing |
| SSRF defence | none | missing |
