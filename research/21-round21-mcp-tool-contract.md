# Research round 21 — MCP as a tool protocol: the parts worth implementing, and the parts to skip

**Date:** 2026-11-04
**Track:** E — integrations, webhooks, OAuth, MCP
**Sources (fetched 2026-11-04):**
- https://modelcontextprotocol.io/specification/2025-06-18/server/tools
- https://modelcontextprotocol.io/specification/2025-06-18/server/index (capability model)
- flobot's inventory: zero MCP support

---

## 1. The tool record, and the one field with a security rule attached

```
name        unique identifier
title       optional human-readable display name
description human-readable description of functionality
inputSchema JSON Schema for parameters
outputSchema optional JSON Schema for structured output
annotations optional properties describing tool behavior
```

> "For trust & safety and security, clients **MUST** consider tool annotations to be untrusted unless they come from trusted servers."

Annotations are the hints that tell a client "this tool is read-only" or "this tool is destructive." The spec says: **do not believe them from an untrusted server.** That is the correct posture and it generalises: any self-description a tool provides about its own safety is a claim, not a fact. flobot's permission grammar (round 15) must classify tools by *what they can do*, not by what they say they do.

## 2. The human-in-the-loop requirement is stated as SHOULD, with three specifics

> "For trust & safety and security, there SHOULD always be a human in the loop with the ability to deny tool invocations. Applications SHOULD:
> - Provide UI that makes clear which tools are being exposed to the AI model
> - Insert clear visual indicators when tools are invoked
> - Present confirmation prompts to the user for operations, to ensure a human is in the loop"

Three concrete UI obligations. flobot's run view shows events but does not show **which tools are exposed to the model before the run starts**. That is the first item and it is the one that builds trust: the operator should be able to see the tool manifest a bot will have, before it acts.

## 3. Client-side security checklist — verbatim, because it is a good spec

> "Clients SHOULD:
> - Prompt for user confirmation on sensitive operations
> - **Show tool inputs to the user before calling the server, to avoid malicious or accidental data exfiltration**
> - Validate tool results before passing to LLM
> - Implement timeouts for tool calls
> - Log tool usage for audit purposes"

The second item is the sharpest: **show the arguments before the call.** Exfiltration happens through arguments — a tool call that sends your SSH key as a parameter looks like a normal call unless you see the arguments. An approval card that shows only "run bash" is useless; one that shows the command is the whole point.

The third item — "validate tool results before passing to LLM" — is the inbound half of round 9's provenance rule.

## 4. Server-side checklist

> "Servers MUST:
> - Validate all tool inputs
> - Implement proper access controls
> - Rate limit tool invocations
> - Sanitize tool outputs"

Rate limiting *tool invocations* (not just HTTP requests) is a detail flobot needs: a runaway agent loop calling one tool 400 times should be throttled at the tool boundary.

## 5. Two error channels, and why the distinction matters

| Channel | Mechanism | Use for |
|---|---|---|
| Protocol error | JSON-RPC `error` object (`-32602` unknown tool, invalid args) | the *call* was malformed |
| Tool execution error | result with `isError: true` + content | the call was fine, the *work* failed |

The distinction is what lets an agent behave correctly: a protocol error means "fix your arguments"; an execution error means "the API rate-limited you, try later or differently." Conflating them makes agents retry malformed calls forever or give up on transient failures.

flobot's executor has one channel: the child process exits nonzero or not. That is a protocol-error-shaped signal for everything. A structured result envelope with an explicit `isError` field is the fix.

## 6. Structured output with an output schema

> "If an output schema is provided: Servers MUST provide structured results that conform to this schema. Clients SHOULD validate structured results against this schema."

And: "For backwards compatibility, a tool that returns structured content SHOULD also return the serialized JSON in a TextContent block."

Dual representation (structured field + serialised text) is a compatibility pattern worth copying for flobot's run results: machine-readable `structuredContent` plus a text rendering for the transcript.

## 7. Content types — what a tool result may contain

`text`, `image` (base64 + mimeType), `audio`, `resource_link` (URI + name + mimeType), embedded `resource` (URI + inline text/blob).

With annotations per item: `audience` (`["user"]`, `["assistant"]`, both), `priority` (0–1), `lastModified`.

The `audience` annotation is quietly important: it lets a tool return content intended only for the human (a warning, a diff to review) that should not be fed back to the model. flobot's event stream has no such distinction — everything is either shown or not shown.

## 8. Pagination and change notification

`tools/list` supports `cursor`/`nextCursor`. `listChanged: true` capability means the server emits `notifications/tools/list_changed`.

For flobot: if tools can appear and disappear mid-run (a skill loads, an MCP server reconnects), the model's tool manifest goes stale. Either refresh at safe points or declare tools fixed for the duration of a run. The latter is simpler and matches Hermes' frozen-snapshot reasoning (round 10 §2): **stability beats freshness inside a run.**

## 9. What flobot should implement vs skip

**Implement (high value, low cost)**
1. A **tool manifest** concept: a declared list of tools with name/title/description/inputSchema, shown in the UI before a run.
2. **Arguments in the approval card** — never approve an opaque call.
3. **Two error channels** in the result envelope (`isError`).
4. **Per-tool rate limiting** at the invocation boundary.
5. **Audience annotation** on result items so human-only content is not fed back.
6. **Result schema validation** before the result enters context.
7. **Tool usage audit log** with redaction policy (round 9 §5).

**Skip**
1. JSON-RPC framing (flobot's children speak CLI; no need).
2. DCR / AS discovery (round 20).
3. Resources / prompts / sampling / elicitation capabilities.
4. Pagination (a local tool list is small).
5. `listChanged` — declare tools fixed per run instead.

## 10. The strategic point

MCP is a *transport* spec for a problem flobot does not have yet: heterogeneous third-party tool servers. flobot's tools are its own child processes and its own skills.

But MCP's **security checklist** is transport-independent and it is better than anything flobot has. The value of this round is not "implement MCP"; it is "adopt MCP's client obligations as flobot's tool contract":

- show the manifest before the run
- show arguments before the call
- validate results before they enter context
- two error channels
- per-tool rate limits
- audit every invocation
- never trust a tool's self-description

That list is implementable in flobot today with no new protocol.

## 11. Gap vs flobot today

| Capability | flobot today | Gap |
|---|---|---|
| Tool manifest shown pre-run | none | missing |
| Arguments in approval | n/a (no approval) | missing |
| Structured result envelope | exit code only | missing |
| Two error channels | none | missing |
| Per-tool rate limit | none | missing |
| Audience annotation | none | missing |
| Result schema validation | none | missing |
| Tool audit log | raw event stream | no redaction policy |
| MCP transport | none | correctly skipped |
