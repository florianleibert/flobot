# Research round 20 — OAuth 2.1 for local-first agents: what MCP got right and what flobot should skip

**Date:** 2026-11-04
**Track:** E — integrations, webhooks, OAuth, MCP
**Sources (fetched 2026-11-04):**
- https://modelcontextprotocol.io/specification/2025-06-18/basic/authorization.md
- https://modelcontextprotocol.io/docs/2025-06-18/tutorials/security/security_best_practices.md
- https://modelcontextprotocol.io/specification/2025-06-18/server/tools (round 21)

---

## 1. MCP's authorization is explicitly OPTIONAL and transport-dependent

> "Authorization is **OPTIONAL** for MCP implementations. When supported: Implementations using an HTTP-based transport **SHOULD** conform to this specification. Implementations using an **STDIO transport SHOULD NOT follow this specification, and instead retrieve credentials from the environment.**"

That single sentence is the most important line in the spec for flobot. flobot's tools are local child processes over stdio-equivalent pipes. **The correct credential model for a local stdio tool is environment injection, not OAuth.** OAuth is for remote HTTP servers.

So flobot's OAuth question decomposes cleanly:

| Tool kind | Credential model | OAuth needed? |
|---|---|---|
| local child process (flocode) | env injection, scoped per run | no |
| local MCP server (stdio) | env injection | no |
| remote MCP server (HTTP) | OAuth 2.1 + PKCE + DCR | yes |
| first-party API (GitHub, Slack, Gmail) | PAT or app token in keychain | usually no |

flobot has zero MCP support today (inventory: "mcp — zero matches repo-wide"). So OAuth is not on the critical path. It becomes relevant only if flobot adds remote MCP servers.

## 2. The subset of OAuth 2.1 MCP chose, and why each piece is there

Standards: OAuth 2.1 draft-13, RFC8414 (AS metadata), RFC7591 (dynamic client registration), RFC9728 (protected resource metadata), RFC8707 (resource indicators).

| Requirement | Level | Why |
|---|---|---|
| AS implements OAuth 2.1 with confidential + public client support | MUST | baseline |
| DCR support | SHOULD (both sides) | "Clients may not know all possible MCP servers… Manual registration would create friction" |
| Server implements Protected Resource Metadata | MUST | discovery without out-of-band config |
| AS provides AS Metadata | MUST | endpoint discovery |
| Client parses `WWW-Authenticate` on 401 | MUST | entry point to discovery |
| Client implements RFC 8707 `resource` param in **both** auth and token requests | MUST | audience binding |
| Client implements PKCE | MUST | code interception defence |
| Redirect URIs registered exactly; `state` verified | MUST / SHOULD | open-redirect defence |
| Tokens in `Authorization: Bearer`, never in query string | MUST | leak via logs/referrer |
| Server validates token audience | MUST | confused-deputy defence |
| **Token passthrough forbidden** | MUST NOT | confused deputy |

The discovery chain is a fixed four-step dance:

```
C → M: request, no token
M → C: 401 + WWW-Authenticate: ...resource_metadata="<url>"
C → M: GET <resource_metadata url>
M → C: { authorization_servers: [...] }
C → A: GET /.well-known/oauth-authorization-server
A → C: AS metadata
[ DCR if supported ] → [ PKCE auth code flow with resource param ] → token
```

## 3. The two prohibitions that matter more than the requirements

**Token passthrough is forbidden.** "If the MCP server makes requests to upstream APIs, it may act as an OAuth client to them. The access token used at the upstream API is a separate token, issued by the upstream authorization server. The MCP server **MUST NOT** pass through the token it received from the MCP client."

**Audience validation is mandatory.** "MCP servers MUST only accept tokens specifically intended for themselves and MUST reject tokens that do not include them in the audience claim."

Together these prevent the confused-deputy pattern where a proxy server launders a user's token into a third-party API that then trusts it as if the proxy had authorised it.

**flobot's direct analogue:** an integration's webhook secret must never be forwarded to a downstream service, and a run's model API key must never be visible to a tool the run invokes. The inventory shows flobot already does the second correctly — `MODEL_API_KEY` is injected once for direct routes only (`routes/routes.go:100`, `executor.go:171–172`) — but there is no general rule, only one special case.

## 4. The security-best-practices doc's real content

The fetched text was heavily truncated, but the surviving fragments name the attack classes and mitigations:

- **Confused deputy** via static client IDs on proxy servers → "MCP proxy servers using static client IDs MUST obtain user consent for each dynamically registered client before forwarding to third-party authorization servers."
- **Consent cookie security** — a per-client consent cookie is itself an attack surface.
- **Security control circumvention** by proxying through an MCP server.
- **Error-detail leakage** — "Error messages reveal information to attackers through error messages or OAuth flows"; "Internal network reconnaissance".
- **Session hijacking** — "if [an attacker has] a session ID, they cannot impersonate another user as the user ID is [bound separately]" — session ID must not be an identity.
- **Sensitive-path warnings** — "Display warnings for commands that access sensitive locations (home [directory]…)".
- **Privilege parity disclosure** — "Warn that MCP servers run with the same privileges as the client."
- **Failure caching** — "Cache recent failures to avoid repeated elevation loops for denied [requests]".
- **Least privilege / minimization** — "Proper minimization constrains compromise impact, improves audit clarity, and reduces consent churn."

Two of those are directly actionable for flobot and cost almost nothing:

1. **"Warn that MCP servers run with the same privileges as the client."** flobot's child processes run with the operator's full privileges (inventory §23). The UI should say so, once, plainly — the same honesty Grok Bot applies to its shared computer (round 9 §3).
2. **"Cache recent failures to avoid repeated elevation loops."** When an approval is denied, do not re-prompt on the next tick. flobot has no approval system yet, so this is a design constraint to build in from day one.

## 5. What flobot should actually build, in order

Given zero MCP and zero OAuth today, the pragmatic sequence:

**Phase 1 — credentials without OAuth.**
- macOS Keychain-backed secret store, scoped per integration and per bot.
- Env-injection contract: a skill or integration declares what it needs; the runtime injects into the child; the value never enters the prompt (round 11 §5, round 9 §2).
- Replace the plaintext `~/.accounts/meta.ai` file read (`executor/credentials.go:1`) with a keychain lookup.

**Phase 2 — outbound actions.**
- Egress allowlist per integration (which hosts may be called).
- Approval gate on outbound side effects (round 9).
- Delivery-id dedupe on inbound (round 19).

**Phase 3 — remote MCP, only if needed.**
- Implement the four-step discovery chain.
- PKCE + `resource` param.
- Refuse token passthrough by construction (each server gets its own token).
- DCR optional; hardcoded client id per server is acceptable for a single-user app.

**Phase 4 — local MCP over stdio.**
- No OAuth. Env injection only.
- Tool list from `tools/list`, with `listChanged` honoured.
- Per-server tool allowlist in the permission grammar (round 15).

## 6. The one thing flobot must not copy

MCP's authorization spec is written for a multi-tenant cloud where the client and server are operated by different parties. flobot is single-user, single-machine. Implementing DCR and full AS discovery for a local app would be pure ceremony.

The transferable parts are the *prohibitions* (no passthrough, audience binding, no tokens in query strings) and the *disclosures* (privilege parity warning, sensitive-path warnings). Those are cheap and they are where real incidents come from.

## 7. Gap vs flobot today

| Capability | flobot today | Gap |
|---|---|---|
| Keychain secret store | none — plaintext file | missing |
| Env-injection contract | one special case (MODEL_API_KEY) | no general mechanism |
| Egress allowlist | none | missing |
| Outbound actions | none | missing |
| MCP (any transport) | none | missing |
| OAuth | none | missing (correctly deferred) |
| Privilege-parity disclosure | none | missing |
| Failure caching for denials | n/a | build in from day one |
