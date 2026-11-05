# Research round 11 — Hermes skills: progressive disclosure, conditional activation, trust

**Date:** 2026-11-04
**Track:** B — Hermes Agent / OpenClaw self-evolving skills
**Sources (fetched 2026-11-04):**
- https://hermes-agent.nousresearch.com/docs/user-guide/features/skills
- https://raw.githubusercontent.com/NousResearch/hermes-agent/main/README.md
- https://agentskills.io (open standard referenced by both)

---

## 1. Progressive disclosure — the three-level loading contract

| Level | Call | Returns | Cost |
|---|---|---|---|
| 0 | `skills_list()` | `[{name, description, category}]` | ~3k tokens |
| 1 | `skill_view(name)` | full SKILL.md + metadata | varies |
| 2 | `skill_view(name, path)` | one specific reference file | varies |

The agent sees only names + descriptions until it commits. This is the same idea as Grok Bot's "description is required before publish, because teammates and their Bots use the description to decide when it applies" — but Hermes makes the cost explicit (~3k tokens for the whole index).

**Large sources become knowledge-base skills:** a book/spec/docs corpus is not crammed into one file. `/learn` authors "a lean SKILL.md carrying the source's core mental models plus an index, with one distilled file per chapter or topic under `references/`". Reference files cost nothing until needed. Re-running `/learn` on the same topic **folds into the existing skill** rather than duplicating.

That is the correct answer to "how do I give an agent a 400-page manual": an index plus lazy leaves, not a summary and not a context dump.

## 2. SKILL.md format — the frontmatter is the contract

```yaml
---
name: my-skill
description: Brief description of what this skill does
version: 1.0.0
platforms: [macos, linux]           # optional OS restriction
metadata:
  hermes:
    tags: [python, automation]
    category: devops
    fallback_for_toolsets: [web]     # conditional activation
    requires_toolsets: [terminal]
    config:                          # non-secret settings → config.yaml
      - key: my.setting
        description: "What this controls"
        default: "value"
---
# Skill Title
## When to Use
## Procedure
## Pitfalls
## Verification
```

The four body sections are the house standard: **When to Use / Procedure / Pitfalls / Verification**. Compare Grok Bot's six-part contract (when / inputs / sequence / validate / return / approval). Union of the two is the right template:

1. When to use
2. Required inputs and access
3. Procedure
4. **Pitfalls** (Hermes)
5. **Verification** (both)
6. What to return (Grok Bot)
7. What requires approval (Grok Bot)

`Pitfalls` is the section nobody else has, and it is where self-improvement accumulates: the review loop appends known failure modes as they're discovered.

## 3. Conditional activation — skills that appear only when relevant

| Field | Behaviour |
|---|---|
| `fallback_for_toolsets` | hidden when listed toolsets **available**; shown when missing |
| `fallback_for_tools` | same, per tool |
| `requires_toolsets` | hidden when listed toolsets **unavailable**; shown when present |
| `requires_tools` | same, per tool |

Use case they name: "a curl-based search skill declaring `fallback_for_toolsets: [web]` stays hidden while the web toolset is available and appears only when it is disabled."

This is a capability-aware skill index — the skill catalogue is a function of the current tool set. flobot has a real version of this problem: a bot pinned to a local model with no web tool should not see web skills at all.

## 4. Platform gating

`platforms: [macos]` hides the skill from the system prompt, `skills_list()`, and slash commands on incompatible platforms. Relevant to flobot: macOS-only skills (AppleScript, Reminders, Home) should be gated the same way.

## 5. Secure setup on load

```yaml
required_environment_variables:
  - name: TENOR_API_KEY
    prompt: Tenor API key
    help: Get a key from https://developers.google.com/tenor
    required_for: full functionality
```

- Missing value → asked **securely, only when the skill is actually loaded**, and only in the local CLI.
- "Messaging surfaces never ask for secrets in chat — they tell you to use `hermes setup` or `~/.hermes/.env` locally instead."
- Once set, declared env vars are **automatically passed through to `execute_code` and terminal sandboxes** so the skill's scripts can use `$TENOR_API_KEY` directly.

That last sentence is the whole credential architecture in one line: a skill declares what it needs, the runtime injects it into the sandbox, the model never handles the value. flobot's `internal/executor/credentials.go` passes env vars to the child already — this is the missing declaration layer.

## 6. Directory layout and precedence

```
~/.hermes/skills/            # single source of truth, read-write
├── <category>/<skill>/
│   ├── SKILL.md             # required
│   ├── references/  templates/  scripts/  examples/  assets/
├── .hub/{lock.json,quarantine/,audit.log}
└── .bundled_manifest
```

Precedence when names collide: **project → local → `skills.create_dir` → `external_dirs`**. The shadowed copy is logged as `shadowed`.

Project-local skills: `<project-root>/.hermes/skills/` and `<project-root>/.agents/skills/`, where project root is the nearest ancestor containing `.git` (worktrees and submodules count).

**Trust gate for project skills:** "Skills are procedure documents the agent follows, so Hermes does not auto-load them from arbitrary cloned repos." First run in a repo with project skills prints:

```
◆ 3 project skill(s) found in /home/you/myproject but not loaded — run `hermes skills trust` to enable them.
```

Trusted roots persist in `skills.trusted_project_dirs`. That is a correct security posture for a feature that would otherwise be remote-code-execution-via-git-clone.

## 7. Bundled-skill update safety via content hashes

`.bundled_manifest` maps skill name → content hash at sync time (`origin hash`). On each sync:

- local hash == origin hash → unmodified → safe to overwrite with the new bundled version, record new origin hash
- local hash != origin hash → **treated as user-modified and skipped forever**

Runtime caches (`__pycache__`, `.pytest_cache`, `.mypy_cache`, `.ruff_cache`, adjacent `.pyc`) are excluded from the hash so running a helper script doesn't mark a skill modified.

Escape hatch for the sharp edge (copy-pasting a fresh bundled copy makes it look modified forever): `hermes skills reset <name>` clears the manifest entry and re-baselines; `--restore` also replaces the local copy.

This is exactly the mechanism flobot needs for its own bundled skill catalogue: users will edit bundled skills, and updates must never stomp them.

## 8. Trust levels and scanning

| Level | Source | Policy |
|---|---|---|
| `builtin` | ships with Hermes | always trusted |
| `official` | `optional-skills/` in repo | built-in trust, no third-party warning |
| `trusted` | known registries (`openai/skills`, `anthropics/skills`, `huggingface/skills`, `NVIDIA/skills`, `wandb/skills`, `qdrant/skills`, `pinecone-io/skills`, `makenotion/skills`, `Airtable/skills`, `Shopify/Shopify-AI-Toolkit`) | more permissive |
| `community` | everything else | non-dangerous findings overridable with `--force`; **dangerous verdicts stay blocked** |

Scanner checks: data exfiltration, prompt injection, destructive commands, supply-chain signals. Install records source URL, exact content hash, scanner version, findings, timestamp in `.hub/lock.json`. Quarantine dir before install.

Optional second opinion: NVIDIA SkillEvaluator Tier 1 (deterministic, keyless) — PII detection, unicode smuggling, script lint, license compliance, static scan via SkillSpector. **Advisory only**, findings printed with file:line before confirmation.

**The critical rule:** `--force` overrides caution/warn findings but **never** a dangerous verdict.

## 9. Update lifecycle with drift detection

- `hermes skills check` — which installed hub skills changed upstream
- `hermes skills update` — reinstall only those
- Locally-edited skills (on-disk hash != install-time hash) are **skipped** so your changes are never silently overwritten; `--force` replaces them.
- Orphaned/invalid install entries are reported, never auto-removed.

## 10. Skill bundles

Chain up to 5 leading `/skill` tokens in one message; parsing stops at the first non-skill token so `/ocr-and-documents ~/.hermes/cache/scan.pdf` doesn't swallow the path. For repeated combinations, prefer a **bundle** — "same effect under one short command."

## 11. Agent-managed skills (`skill_manage`)

Actions include create / edit / patch / write_file / remove_file / delete. Gated by `skills.write_approval`. This is the mechanism behind "autonomous skill creation after complex tasks" and "skills self-improve during use."

## 12. KEEP / TOSS for flobot

**KEEP**
- Three-level progressive disclosure with an explicit index budget.
- Knowledge-base skills: lean SKILL.md + index + lazy `references/`.
- `/learn` from a directory, URL, pasted notes, or *the conversation you just had*; re-learn folds into existing skill.
- Frontmatter contract incl. `platforms`, `requires_toolsets`, `fallback_for_toolsets`.
- Seven-section body template (union of Grok Bot + Hermes).
- `required_environment_variables` declaration → runtime injects into sandbox; model never sees value.
- Precedence project → local → external; shadowed copies logged.
- Project-skill trust gate (`hermes skills trust`) — never auto-load from a clone.
- Content-hash manifest so bundled-skill updates never stomp user edits.
- Trust levels + scanner + "dangerous verdicts cannot be overridden."
- Drift-aware update that skips locally-edited skills.

**TOSS**
- The GitHub-tap registry model (flobot is local-first; a directory is enough).
- Skill bundles as a separate concept initially — chaining covers it.

## 13. Gap vs flobot today

| Capability | flobot today | Gap |
|---|---|---|
| Skill storage | DB row with free-text body (`Skill.BotID`) | no filesystem tree, no references/, no scripts/ |
| Progressive disclosure | whole body injected | no index/leaf split |
| Conditional activation | none | missing |
| Platform gating | none | missing |
| Env declaration | none | missing |
| Project-local skills | none | missing |
| Trust gate | none | missing |
| Bundled-skill updates | none | missing |
| Skill creation from a run | none | missing |
