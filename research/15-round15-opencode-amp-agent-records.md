# Research round 15 — OpenCode + Amp: agent definitions as data, and the multi-model roster

**Date:** 2026-11-04
**Track:** C — harness engineering
**Sources (fetched 2026-11-04):**
- https://opencode.ai/docs/agents/
- https://ampcode.com/manual (docs index; Introduction)
- https://ampcode.com/ (product surface)

---

## 1. OpenCode: an agent is a config record, not a code path

Two agent kinds:

| Kind | Role | Examples |
|---|---|---|
| **primary** | the assistant you talk to; cycled with Tab | `build` (all tools), `plan` (read-only) |
| **subagent** | invoked by a primary or `@`-mentioned | `general` (all but todowrite), `explore` (read-only) |
| **hidden system** | not selectable, runs automatically | `compaction`, `title`, `summary` |

The hidden-system-agent category is worth stealing outright: **compaction, title generation and summarisation are agents with their own model and prompt**, not inline code. flobot generates run titles ad hoc; making them declared agents means they get model routing, temperature and permission policy for free.

### The full agent record

```jsonc
{
  "description": "Reviews code for best practices and potential issues",  // REQUIRED
  "mode": "primary" | "subagent" | "all",     // default "all"
  "model": "provider/model-id",
  "temperature": 0.1,
  "top_p": 0.9,
  "steps": 5,                                  // max agentic iterations
  "prompt": "{file:./prompts/build.txt}",
  "disable": false,
  "hidden": true,                              // hide from @ menu only
  "color": "#FF5733" | "accent",
  "permission": { ... },
  // any other key passes through to the provider as a model option:
  "reasoningEffort": "high", "textVerbosity": "low"
}
```

Three details with outsized value:

1. **`description` is required.** Same conclusion as Grok Bot ("a skill requires a description before it can be published") and Hermes (index = name + description). Three products, one invariant.
2. **Unknown keys pass through to the provider.** No schema churn to expose a new model knob (`reasoningEffort`, `textVerbosity`). flobot's route config should do this.
3. **`steps` with a defined terminal behaviour**: "When the limit is reached, the agent receives a special system prompt instructing it to respond with a summarization of its work and recommended remaining tasks." A step cap that ends in a *useful* message rather than a truncated failure.

### Model inheritance rule

> "If you don't specify a model, primary agents use the model globally configured while **subagents will use the model of the primary agent that invoked the subagent**."

Inheritance-by-invocation is the right default for flobot: a subagent spawned by a bot pinned to a local model should stay on that model unless explicitly overridden.

### Permission grammar — glob → action, last match wins

Keys: `read`, `edit`, `glob`, `grep`, `list`, `bash`, `task`, `todowrite`, `external_directory`, `webfetch`, `websearch`, `lsp`, `skill`, `question`, `doom_loop`.

Actions: `"ask" | "allow" | "deny"`, or an object of glob → action for fine-grained control:

```jsonc
"permission": {
  "bash": { "*": "ask", "git status *": "allow", "git log*": "allow", "grep *": "allow" },
  "edit": "deny",
  "webfetch": "deny"
}
```

> "Since the last matching rule takes precedence, put the `*` wildcard first and specific rules after."

Note this is **last-match-wins**, the opposite of Claude Code's deny>ask>allow precedence. Both are defensible; last-match-wins is more expressive but easier to get wrong. If flobot picks last-match-wins it must ship the same warning Claude Code ships.

Two more permission ideas:

- **`external_directory`** — a dedicated key for any tool that reads or writes outside the project worktree. flobot's workspace is a directory; this is the missing boundary.
- **`doom_loop`** — "Recovery prompts when an agent appears stuck." A stuck-agent detector as a *permission key* is an unusual but effective placement: it makes loop-breaking policy configurable per agent.

### Task permissions — subagent spawn control

```jsonc
"permission": { "task": { "*": "deny", "orchestrator-*": "allow", "code-reviewer": "ask" } }
```

> "When set to `deny`, the subagent is removed from the Task tool description entirely, so the model won't attempt to invoke it."

Removing denied subagents from the tool description — rather than letting the model try and fail — is the correct implementation of least privilege for a roster. It also saves prompt tokens. And: "Users can always invoke any subagent directly via the @ autocomplete menu, even if the agent's task permissions would deny it" — operator override preserved.

### Plan mode's default permission set, stated exactly

> "By default, Plan: Denies file edits, writes, and patches except Markdown plan files in `.opencode/plans/` and the `plans/` directory under OpenCode's data directory. These other edits are refused without prompting. Allows the question tool… Does not restrict the bash tool."

And the precedence trap they document: "Plan's defaults are applied before your top-level permission config, and the last matching rule wins. A global rule such as `"edit": "allow"` therefore also applies to Plan." So a global allow silently defeats plan mode — documented, with the explicit fix (set plan's permissions directly).

**Plan artifacts as files in the repo** (`.opencode/plans/`) rather than chat state is worth copying: plans become reviewable, diffable, committable.

### Child-session navigation

Subagents create child sessions with dedicated keybinds: `session_child_first`, `session_child_cycle`, `session_child_cycle_reverse`, `session_parent`. That is the UI answer to "where did my subagent go" — flobot's handoff bus needs the same: every handoff target is a navigable session, not a fire-and-forget message.

## 2. Amp: what a mature multi-model product actually ships

From the docs index (the full navigation tree is itself a specification of a mature agent product):

**Core surfaces:** Threads · Orbs (per-thread machines) · Projects · GitHub & Git · Integrations · Pricing
**Execution:** Modes & Models · Prompting · Designing UI · Tools · Sizes & Costs · Handling Secrets · Files & Attachments
**Multiplayer:** Collaborate · Workspaces · Groups · Multiplayer Space
**Extensibility:** Plugins · Decision Models · Global Plugins & Skills · Skills · MCP · AGENTS.md · Model Routing
**Clients:** CLI (Keybindings, Execute Mode, Spawning Orbs, Clone Repositories, Streaming JSON) · macOS · iPhone/iPad · Android · Puck · Slack Integration · Satellites
**Agent-to-agent:** Agent to Agent · Automations & Webhooks
**Enterprise:** MCP Registry Allowlist · Thread Visibility · Workspace Entitlements · Allowed Models · Minimal Data Retention

### The four ideas worth taking

1. **Orbs = per-thread machine.** "Machines that Amp creates per thread, with your code, tools, and plugins already set up. Send a prompt, close your laptop, and the agent keeps working." The unit of isolation is the *thread*, not the bot. flobot's unit is currently the bot; per-run worktrees are the local equivalent and flobot already has `internal/workspace`.

2. **Model Routing as a first-class config surface** (not per-message model choice). Combined with **Decision Models** — models whose job is to decide something (which model, which tool) rather than to produce output. That is exactly the shape flobot needs for its router: a cheap local model that classifies the task calibre and picks a route.

3. **Streaming JSON** as a documented CLI output mode. Machine-consumable agent output without an SDK. flobot's SSE stream is close; formalising it as a documented streaming-JSON contract makes it scriptable.

4. **"Opinionated: You're always using the good parts of Amp. If we don't use and love a feature, we kill it."** and **"No backward compatibility, no legacy features."** A stated product policy of deletion. For flobot this is the discipline that prevents the config surface from becoming Codex's.

### What Amp deliberately does *not* have

No local-model story in the docs index; no per-bot memory; no skills-as-files (Skills exist but as plugins). It is a cloud-frontier product. flobot's differentiation is not competing on any of these axes — it is: **local models, your machine, your data, per-bot routes over your own mesh.**

## 3. The cross-product invariants (three harnesses, same conclusions)

| Invariant | Claude Code | Codex | OpenCode | Grok Bot | Hermes |
|---|---|---|---|---|---|
| Agent/skill description is required and is the retrieval key | ✓ (15k token warning) | — | ✓ (required) | ✓ (required to publish) | ✓ (~3k index) |
| Progressive disclosure: index cheap, body lazy | ✓ | ✓ (AGENTS.md byte budget) | ✓ | ✓ | ✓ |
| Project-scoped config requires trust | ✓ | ✓ (+ key denylist) | ✓ | n/a | ✓ (`skills trust`) |
| Permission rules as glob → action with explicit precedence | ✓ deny>ask>allow | ✓ policy enum | ✓ last-match-wins | ✓ Ask-first wins | ✓ deny globs |
| Unattended mode that denies instead of hanging | ✓ `dontAsk` | ✓ `approval_policy` | ✓ `ask`→deny in cron | ✓ 30-min expiry | ✓ `cron_mode: deny` |
| Wrapper stripping before command matching | ✓ | — | — | — | ✓ |
| Step/turn cap with a useful terminal message | ✓ maxTurns | — | ✓ steps | — | — |
| Subagent spawn allowlist that hides denied agents from the prompt | ✓ | — | ✓ task perms | — | — |
| Model choice per agent, inherited by subagents | ✓ | ✓ profiles | ✓ inherit-on-invoke | ✗ (server-side) | ✓ per-job pin |

Five independent products converged on nine design decisions. Those nine are not opinions; they are requirements.

## 4. KEEP / TOSS for flobot

**KEEP**
- Agent as a data record with required `description`, `mode`, `model`, `temperature`, `steps`, `prompt`, `permission`, plus provider passthrough.
- Hidden system agents for compaction / title / summary.
- Subagent model = invoking agent's model unless overridden.
- Glob→action permission grammar; pick one precedence and document it loudly.
- `external_directory` as its own permission key.
- Stuck-loop detection as a configurable policy.
- Denied subagents removed from the tool description.
- Operator can always invoke any agent directly.
- Plan artifacts as files in `.opencode/plans/`.
- Child-session navigation keybinds for every spawned agent.
- Per-run isolation unit (worktree) rather than per-bot.
- Model routing + decision models as config surfaces.
- Streaming JSON output contract.
- A stated policy of deleting features.

**TOSS**
- Cloud machines / orbs / satellites (flobot is local).
- Multiplayer / workspaces / groups.
- Plugin API (skills cover it).

## 5. Gap vs flobot today

| Capability | flobot today | Gap |
|---|---|---|
| Agent as data record | bot row + role string | no mode/model/steps/permission record |
| Hidden system agents | inline code for titles | missing |
| Subagent model inheritance | n/a (no subagents) | missing |
| Permission grammar | none | missing |
| external_directory boundary | none | missing |
| Stuck-loop detection | none | missing |
| Spawn allowlist | none | missing |
| Plan artifacts as files | none | missing |
| Child-session navigation | handoff list only | no navigation |
| Streaming JSON contract | SSE only | not documented/stable |
