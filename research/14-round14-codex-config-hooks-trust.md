# Research round 14 — Codex: layered config, trust scoping, and the hooks plane

**Date:** 2026-11-04
**Track:** C — harness engineering
**Sources (fetched 2026-11-04):**
- https://developers.openai.com/codex/config-advanced
- https://developers.openai.com/codex/hooks.md
- https://raw.githubusercontent.com/openai/codex/main/docs/config.md

---

## 1. Config layering with an explicit precedence chain

Codex loads, in order: **system → user (`~/.codex/config.toml`) → profile (`~/.codex/<name>.config.toml`) → project (`.codex/config.toml`, walked root→cwd, closest wins) → CLI flags (`-c key=value`)**.

Profiles are a *layer file*, not a nested table:

```toml
# ~/.codex/deep-review.config.toml
model = "gpt-6.1-sol"
model_reasoning_effort = "medium"
approval_policy = "on-request"
model_catalog_json = "/Users/me/.codex/model-catalogs/deep-review.json"
```

"Because the profile file is a layer above your base user config and below project and CLI config, it only needs the values that differ from your base config."

They deprecated the old `[profiles.name]` table and the top-level `profile =` selector in 0.134.0 and now warn on it. Lesson: **nested profile tables rot; flat overlay files don't.**

## 2. Project config is gated on trust — and has a denylist of keys it may never set

> "For security, Codex loads project-scoped config files only when the project is trusted. If the project is untrusted, Codex ignores project `.codex/` layers, including `.codex/config.toml`, project-local hooks, and project-local rules. User and system layers remain separate and still load."

And even in a **trusted** project, these keys are ignored with a startup warning:

```
openai_base_url, chatgpt_base_url, apps_mcp_product_sku,
model_provider, model_providers, notify, profile, profiles,
experimental_realtime_ws_base_url, otel
```

Rationale stated: project config "can't override settings that redirect credentials, alter host-owned app request metadata, change provider auth, select config profiles, or run machine-local notification/telemetry commands."

This is the cleanest statement of a rule flobot needs: **a repo may configure how the agent works in that repo, but never where credentials go, where telemetry goes, or which provider is used.** A malicious repo must not be able to exfiltrate by pointing `base_url` at its own server.

Relative paths inside a project config resolve relative to the `.codex/` folder containing it — not the cwd. Subtle and correct.

## 3. Project root detection is configurable

Default: nearest ancestor containing `.git`. Override:

```toml
project_root_markers = [".git", ".hg", ".sl"]
project_root_markers = []   # treat cwd as root; don't search parents
```

flobot's workspace descriptor already does something like this; making the marker list explicit is a one-line improvement.

## 4. Hooks: four locations, one schema, one representation per layer

Loadable from `hooks.json` **or** inline `[hooks]` tables, at `~/.codex/` (user) and `<repo>/.codex/` (project):

```toml
[[hooks.PreToolUse]]
matcher = "^Bash$"

[[hooks.PreToolUse.hooks]]
type = "command"
command = '/usr/bin/python3 "$(git rev-parse --show-toplevel)/.codex/hooks/pre_tool_use_policy.py"'
timeout = 30
statusMessage = "Checking Bash command"
```

Rules:
- "Project-local hooks load only when the project `.codex/` layer is trusted. User-level hooks remain independent of project trust."
- "If a single layer contains both `hooks.json` and inline `[hooks]`, Codex loads both and warns. Prefer one representation per layer."
- Default timeout 600 s.
- Output field `systemMessage` is "surfaced as a warning in the UI or event stream."
- Failure semantics are stated precisely: "An explicit supported denial can block an action, but a `PreToolUse` callback error, timeout, or malformed response can fail the hook without blocking the tool."

That last sentence is the honest version of a subtle truth: **a broken policy hook must not silently become an allow.** Codex documents that it *can* fail open on error — which is why they also offer managed-hooks-only mode.

## 5. Managed hooks override everything

```toml
# requirements.toml ONLY
allow_managed_hooks_only = true
```

"Ignore user, project, and session hook configs while still allowing managed hooks from requirements and managed config layers. This setting is only supported in `requirements.toml`; putting it in `config.toml` does not enable managed-hooks-only mode."

The "this key only works in the managed file" pattern is how you make an override that users cannot defeat by editing their own config. flobot's analogue: a policy file that a bot cannot rewrite.

## 6. Sandbox / approval / provider knobs worth noting

- `approval_policy = "on-request"` (plus `never`, `untrusted`, etc.) — approval policy as a named enum in config, not a boolean.
- `sandbox_workspace_write.network_access = true` — network is off by default inside the write sandbox.
- `shell_environment_policy.include_only = ["PATH","HOME"]` — **env allowlist**, not denylist. The child gets only what's named.
- `model_providers.<id>` with `base_url`, `env_key`, `http_headers`, `env_http_headers`, `supports_standalone_web_search`.
- Reserved provider ids: `openai`, `ollama`, `lmstudio`, `amazon-bedrock`, `amazon-bedrock-runtime`, `gcp`.
- `openai_base_url` to point the built-in provider at a proxy/router without defining a provider.
- `hide_agent_reasoning` / `show_raw_agent_reasoning` — reasoning visibility as an explicit toggle, with the caveat that some models don't emit it.
- `history.persistence = "none"`, `history.max_bytes` with oldest-first compaction.
- `notify = ["python3", "/path/notify.py"]` — external program on `agent-turn-complete`, receiving one JSON argument with `type`, `thread-id`, `turn-id`, `cwd`, `input-messages`, `last-assistant-message`.
- `tui.notification_method: auto|osc9|bel`, `tui.notification_condition: unfocused|always`.
- `file_opener: vscode|cursor|windsurf|vscode-insiders|none` — clickable citations rewritten to editor URIs.
- `project_doc_max_bytes`, `project_doc_fallback_filenames` — AGENTS.md discovery with explicit byte budget.

## 7. The `notify` payload is a mini event contract

```json
{"type": "agent-turn-complete", "thread-id": "...", "turn-id": "...",
 "cwd": "...", "input-messages": ["..."], "last-assistant-message": "..."}
```

One JSON arg, stable field names, currently one event type. flobot already emits events; formalising a single stable notification payload (with a documented field set and an explicit "currently only these types" note) is cheap and unlocks desktop toasts, webhooks and CI without an SDK.

## 8. KEEP / TOSS for flobot

**KEEP**
- Flat overlay profile files instead of nested profile tables.
- Project config gated on trust + a **denylist of keys a project may never set** (provider/base_url/auth/telemetry/notify).
- Relative paths in project config resolve against the config's own directory.
- Configurable project-root markers.
- Hooks from a single schema at user and project scope; project hooks require trust.
- Warn when two representations of the same hook layer coexist.
- Explicit statement of hook failure semantics (denial blocks; error/timeout may not).
- A managed-only override key that is honoured only in the managed file.
- Env **allowlist** (`include_only`) rather than denylist for child processes.
- Sandbox network off by default.
- Reasoning visibility as an explicit toggle.
- History persistence off/capped with oldest-first compaction.
- A stable one-JSON-arg notification payload.
- Clickable citations via a configurable editor URI scheme.

**TOSS**
- Windows sandbox / WSL specifics.
- Enterprise managed-config merge rules.

## 9. Gap vs flobot today

| Capability | flobot today | Gap |
|---|---|---|
| Config layering | env vars + store rows | no layered files |
| Project-scoped config | workspace descriptor only | no trust gate, no key denylist |
| Hooks | none | missing |
| Managed-only override | none | missing |
| Env allowlist for child | passes full env + credentials | no allowlist |
| Sandbox network control | none | missing |
| Notification payload | SSE events only | no stable external contract |
| Editor URI citations | none | missing |
