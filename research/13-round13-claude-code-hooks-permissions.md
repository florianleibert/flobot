# Research round 13 — Claude Code: hooks as the policy plane, permissions as the capability plane

**Date:** 2026-11-04
**Track:** C — harness engineering
**Sources (fetched 2026-11-04):**
- https://code.claude.com/docs/en/hooks.md
- https://code.claude.com/docs/en/permissions.md
- https://code.claude.com/docs/en/sub-agents.md

---

## 1. The hook event list is a specification of the agent loop

The lifecycle diagram in their docs enumerates every place a harness can intervene:

**Session:** `Setup` → `SessionStart` → … → `SessionEnd`
**Per turn:** `UserPromptSubmit` → `UserPromptExpansion` (slash commands) → agentic loop → `Stop` / `StopFailure`
**Inside the agentic loop:** `PreToolUse`, `PermissionRequest`, `PostToolUse`, `PostToolUseFailure`, `PostToolBatch`, `SubagentStart`, `SubagentStop`, `TaskCreated`, `TaskCompleted`
**Compaction:** `PreCompact`, `PostCompact`
**MCP:** `Elicitation`, `ElicitationResult` (nested inside MCP tool execution)
**Side branches:** `PermissionDenied` (from `PermissionRequest` for auto-mode denials), `WorktreeCreate`, `WorktreeRemove`, `Notification`, `ConfigChange`, `InstructionsLoaded`, `CwdChanged`, `FileChanged`, `DirectoryAdded`
**Model:** `PreModelSwitch` (sequential, before a requested switch), `PostModelSwitch` (async, after)
**Display:** `MessageDisplay` (display-only, while assistant text streams)
**Teammates:** `TeammateIdle`

For flobot, which today has exactly one hook point (spawn a child process), this list is the roadmap. The three that matter most and are cheapest:

1. **`PreToolUse` with a deny verdict** — the approval gate.
2. **`PermissionDenied` with `retry: true`** — tell the model it may retry a denied call. Without this, a denial reads as a hard failure and the model gives up or works around it.
3. **`InstructionsLoaded` / `ConfigChange`** — auditability of what shaped the prompt.

## 2. Hook handler types and the matching grammar

Handler types: `command`, `http`, `mcp_tool`, `prompt`, `agent`. Timeouts default 600 s for command/http/mcp_tool, 30 s for prompt, 60 s for agent; lowered to 30 s on `UserPromptSubmit`/`PreModelSwitch`/`PostModelSwitch`, 10 s on `MessageDisplay`. `SessionEnd` hooks share a **1.5-second budget** (raisable to 60 s by setting a longer per-hook timeout).

Matching is two-stage:

```
PreToolUse fires
  → matcher checks for a Bash match
    → if condition checks for Bash(rm *)
      → both match ⇒ hook command runs, returns permissionDecision: deny ⇒ tool call blocked
      → either fails ⇒ hook skipped, tool call proceeds
```

The `if` field is the **cheap pre-filter**: "If the command had been `npm test`, the `if` check would fail and `block-rm.sh` would never run, avoiding the process spawn overhead." Without `if`, every handler in the matched group runs.

That two-stage design (cheap pattern match in the harness, expensive process spawn only on a hit) is the right shape for flobot: an approval hook must not fork a process on every tool call.

Matcher syntax quirks worth knowing:
- Most events accept `|` **or** `,` as alternation.
- `FileChanged` and `StopFailure` accept **only** exact-match letters/digits/`_`/`|`; a hyphen, space or comma keeps them on the regex path.
- MCP tools appear as ordinary tool names (`mcp__.*`), so one matcher covers built-ins and MCP alike.

Concurrency: "When an event fires, its matching hooks run in parallel. If more than 12 of them are prompt or agent hooks, 12 of those run at once and the rest start as running ones finish."

## 3. Hook fields that encode real product decisions

| Field | Behaviour |
|---|---|
| `once: true` | remove the hook after its first **successful** run. "A run that fails, blocks with exit code 2, or times out leaves the hook in place." Only honoured in skill frontmatter. |
| `asyncRewake: true` | run in background; on exit code 2, wake Claude and show stderr (or stdout) as a **system reminder** so it can react to a long-running background failure |
| `onFailure: "continue" \| "block"` | whether the triggering action proceeds when the hook fails |

`asyncRewake` is a genuinely good idea: a slow check (a full test suite, a security scan) runs off the critical path but can still *interrupt* the agent when it fails. flobot's executor is fully synchronous per run; this is the pattern for adding background verification without blocking.

## 4. Hook working-directory fallback

"Handlers run in the current directory with Claude Code's environment. If the current directory no longer exists — for example a worktree or temp directory that another shell deleted mid-session — Claude Code runs command hooks from the first of these that still exists: the directory the session started in, the project root, your home directory, or the system temp directory." A warning naming the fallback is written to the debug log.

flobot has exactly this failure mode with worktrees (`internal/workspace`). The fallback ladder is a two-line fix that prevents an entire class of "hook silently didn't run" bugs.

## 5. Permissions: precedence is deny > ask > allow

> "A broad deny rule like `Bash(aws *)` blocks every matching call, including calls that also match a narrower allow rule like `Bash(aws s3 ls)`. An allow rule can't carve an exception out of a deny rule. The same precedence applies between ask and allow: a matching ask rule prompts even when a more specific allow rule also matches the same call."

That is the opposite of most config systems (last-match-wins) and it is correct for security: specificity never weakens a restriction.

## 6. Permission modes

| Mode | Behaviour |
|---|---|
| default | prompt per rules |
| `acceptEdits` | file edits auto-accepted |
| `plan` | read-only; plan mode |
| `bypassPermissions` | no prompts |
| `dontAsk` | **auto-denies every call that would otherwise prompt**; explicitly allowed tools still work |

`dontAsk` is the mode flobot needs for routines: not "allow everything" and not "hang waiting", but "deny anything that would have needed a human, and let the model find another path." Note their exceptions: `AskUserQuestion`, MCP tools marked `requiresUserInteraction`, reads from network paths, and org-`ask` connector tools are denied **even if allowed**.

## 7. Bash rule matching — the subcommand rule

> "Put the `*` after the subcommand. In `git log --oneline main`, `git` is the program and `log` is the subcommand… Claude Code matches everything before the first `*` as written, so those words are what limit the rule: `Bash(git log *)` allows only `git log` commands, and `Bash(git *)` allows every git command."

And it warns at startup about an allow rule with a `*` before the subcommand (`Bash(git * main)`) — because that rule means something different from what the author intended.

**Wrapper stripping:** before matching Bash rules, these are stripped so `Bash(npm test *)` also matches `timeout 30 npm test`: `timeout`, `time`, `nice`, `nohup`, `stdbuf`, plus shell builtins `command`, `builtin`, and zsh's `noglob`. Not stripped: `command -v` (a lookup, not an execution) and zsh's `nocorrect`.

That wrapper list is nearly identical to Hermes' launcher list (round 12). Two independent implementations converged on it — it belongs in flobot verbatim.

**Unmatchable fields:** "You can't match a tool's primary content field this way: `command` for Bash… A rule like `Bash(command:rm *)` would be bypassable by a compound command, so Claude Code ignores it and emits a startup warning." Good example of refusing to ship a footgun.

## 8. Path rules and symlink resolution

- Deny rules apply "when either the requested path or the file it resolves to matches. A symlink that points to a denied file is itself denied."
- With `Read(./project/**)` allowed and `Read(~/.ssh/**)` denied, a symlink at `./project/key` → `~/.ssh/id_rsa` is blocked because **the target fails the allow rule and matches the deny rule**.
- When symlinks don't resolve (loop), the operation is **refused**, not allowed.
- When a tool opens an approved file it "confirms that the path still resolves to the location the permission check approved" — TOCTOU defence.
- A `Read` deny also blocks Edit/Write on the same path (including creating a new file there). NotebookEdit is not covered — documented gap.

The TOCTOU re-check is the detail most implementations miss and it is cheap.

## 9. Subagents: description budget as a first-class constraint

> "Those descriptions take up context, so keep them short. When the combined descriptions of your subagents, except the built-in ones, exceed 15,000 tokens, Claude Code shows a warning at startup with the total token count."

Same progressive-disclosure economics as Hermes' ~3k skill index. Both products independently discovered that **the agent roster's descriptions are a prompt budget line item**, and both surface usage.

Other subagent mechanics worth copying:

- **Per-subagent tool allowlist** (`tools: Read, Grep, Bash`) — "If no entry in the list resolves to a tool, the subagent usually fails to launch with an error naming the entries." Fail loud rather than spawn a toolless agent.
- **Background subagent tool filter**: apart from `Agent`/`ExitPlanMode`, "a background subagent keeps every MCP tool but only these built-in tools: Read, Grep, Glob, LSP, Bash, PowerShell, Edit, Write, NotebookEdit, WebFetch, WebSearch, TodoWrite, Skill, ToolSearch, EnterWorktree, ExitWorktree, Monitor, TaskStop, SendMessage, Artifact." Everything else is removed silently. So "the same definition can resolve to different tools in the foreground and the background."
- **Spawn allowlist**: only named subagent types may be spawned; "the agent sees only the allowed types in its prompt." Blocking specific agents while allowing all others uses `permissions.deny`.
- **Depth limit** on subagent spawning; in a fork the Agent tool stays listed but errors.
- **Plan-mode inheritance**: "When the main conversation is in plan mode, the subagent stays in plan mode whichever permissionMode you set" — except agent-team teammates.
- **Model substitution warning**: interactive sessions warn naming both the requested model and the model actually used.
- **Invalid config handling**: a name starting with `-`, containing `:`, or >256 chars → file skipped + debug log; unparseable YAML → no fields read + debug log. Never a hard crash.
- **Unknown fields ignored silently** — documented as such.

## 10. KEEP / TOSS for flobot

**KEEP**
- The full hook event taxonomy as flobot's target surface; start with PreToolUse / PostToolUse / PermissionDenied / Stop / SessionEnd / PreCompact.
- Two-stage matching (`matcher` then cheap `if`) so no process spawns on non-matches.
- Handler types incl. `http` (flobot can point a hook at its own server instead of forking).
- `asyncRewake`: background check that can interrupt on failure.
- Working-directory fallback ladder.
- Precedence deny > ask > allow; specificity never weakens restriction.
- A `dontAsk` mode for unattended runs (deny-what-would-prompt).
- Subcommand-aware Bash matching + startup warning for wildcard-before-subcommand.
- Wrapper stripping list.
- Refusal to support unmatchable content fields.
- Symlink-aware path rules + TOCTOU re-check + refuse-on-unresolvable.
- Read-deny implies write-deny on the same path.
- Description token budget with startup warning.
- Fail-loud on zero resolved tools; silent removal for background-only tools.

**TOSS**
- Agent teams / teammates (flobot's handoff bus covers this differently).
- Mods / plugin approval extensions.

## 11. Gap vs flobot today

| Capability | flobot today | Gap |
|---|---|---|
| Hook points | one (process spawn) | no pre/post tool hooks |
| Hook matching | none | missing |
| Permission precedence | none | missing |
| Unattended deny mode | none | missing |
| Bash rule grammar | none | missing |
| Path/symlink rules | none | missing |
| Subagent tool allowlist | none | missing |
| Description budget | none | missing |
