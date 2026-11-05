# Research round 12 — Hermes automation + the eight-layer security model

**Date:** 2026-11-04
**Track:** B — Hermes Agent / OpenClaw self-evolving skills
**Sources (fetched 2026-11-04):**
- https://hermes-agent.nousresearch.com/docs/user-guide/features/cron
- https://hermes-agent.nousresearch.com/docs/user-guide/security

---

## Part 1 — The scheduler, which is far more rigorous than Grok Bot's

### 1.1 Model resolution at fire time (three-tier, user-owned)

`per-job pin` → `cron.model` in config → main agent model at fire time.

Critical rule: **"The agent cannot point a job at a different model — inference pins are user-owned."** The `cronjob_manage` tool can `pinned=true` only when the user asks. Per-job `reasoning_effort` (`none|minimal|low|medium|high|xhigh|max|ultra`) is **deliberately not exposed** to the agent tool for the same reason.

For flobot this maps directly: a routine's route pin is operator-owned; a bot must not be able to re-point its own routine at a different (e.g undistilled, expensive) model.

### 1.2 Pre-dispatch configuration validation — "a misconfigured job never spends tokens"

Before constructing any agent machinery, the scheduler validates:

- provider API key resolves (skipped for unpinned job when a `fallback_providers` chain exists; a pinned job is always checked)
- attached skills are ready — no missing required env vars, commands, or credential files
- delivery targets are known and have gateway credentials (`local`/`origin` never checked)
- every MCP server named in the job's `enabled_toolsets` resolved to ≥1 tool for this profile

Distinction they draw: a server that connected earlier and is only reconnecting after a network blip **does not block** (job runs with the tools that did resolve; logged once per outage). A server that never connected for this profile, or is parked on a permanent error like revoked credentials, **blocks**.

On failure: `last_status = blocked_config`, **one** alert delivered (not every tick), no LLM call. Next healthy run clears the state so a future break alerts again. The verdict names the profile and `HERMES_HOME` it read.

That last detail — naming which config root produced the verdict — is the fix for the single most common "but it works in my shell" bug class.

### 1.3 Admission recovery — durable fire identity

> "Agent-backed runs retain their fire identity until output and a durable delivery handoff are recorded. The scheduler reconciles unfinished receipts on each tick, including after an owner restart."

Rules:
- A known terminal result is handed to the delivery queue **without running the agent again**.
- Duplicate handoffs and bookkeeping use the **original fire identity**.
- Delivery interrupted **after a send claim** stays `unknown` and is **not retried automatically** (at-most-once on the ambiguous side).
- An interrupted execution, or a prepared fire that cannot be found, stays **paused for operator review**.
- Recovery never creates another admission for it.
- Paused jobs are not automatically resumed even when their completed result is recovered.

This is the correct at-least-once/at-most-once split: idempotent up to the send claim, then fail-safe to `unknown` rather than double-send. flobot's executor has no fire identity at all — a crash mid-run loses the run.

### 1.4 The `wakeAgent` gate — $0 pre-run decisions

A job may attach a `script`. If the script's final stdout line is `{"wakeAgent": false}`, **the agent is never invoked**. Omitted → default true. The script can pass `context` through to the agent.

Three documented recipes:
- **file-change gate** — compare file mtime to last successful tick
- **external-flag gate** — drop a file from a deploy hook / CI
- **SQL-count gate** — query your own DB for new rows

They explicitly warn *not* to query Hermes' own `state.db` (internal schema).

This is the single best cost-control idea in the whole survey. flobot polls cron every tick and always spawns a flocode child. A pre-flight gate would let a "check my inbox every 5 min" routine cost nothing until something changed.

### 1.5 `context_from` — job chaining without a DAG engine

```python
cronjob(action="create", name="daily-digest", schedule="every day 7am",
        context_from=["ai-news-fetch", "github-prs-fetch"],
        prompt="Write the daily digest using the outputs above.")
```

Referenced jobs' most recent **completed** outputs are injected above the prompt. Caveat stated: "chaining reads the most recent completed output — it does not wait for upstream jobs that are running in the same tick." That is honest about what it is: data dependency on last-known-good, not orchestration.

### 1.6 Catch-up window — precise semantics

A missed cron occurrence runs late only inside a catch-up window: **half the job's period, clamped to 2 min–2 h**. `--grace-minutes` overrides per job (0–1440); `0` = never catch up. Applies only to cron expressions — `once` and `interval` schedules have no missed wall clock.

Resume semantics: "A recurring job whose slot came due while it was paused keeps that slot due, so the next tick fires one catch-up run… instead of silently jumping to the next occurrence."

### 1.7 Cancel vs pause vs remove — three distinct verbs

| Verb | Effect on schedule | Effect on in-flight run |
|---|---|---|
| `pause` | stops future fires | keeps running |
| `cancel` | **untouched** | stops current run; books a `cancelled` row with reason |
| `remove` | deletes job | cancels in-flight run, booked `"job removed while running"` |

Cancel details: script jobs get SIGTERM to the process group escalating to SIGKILL after grace, backgrounded grandchildren included. Agent jobs are interrupted at their next checkpoint. "The cancel is a **request**, not a kill." A cancelled run sends no failure notice and does not count toward the failure streak. "A cancel cannot undo side effects the run already performed — it stops the run, it does not roll it back."

Also: `--paused --paused-reason "Awaiting review"` creates a canary with no create-then-pause race, storing `enabled: false, state: paused, next_run_at: null` in the first locked write.

### 1.8 Global emergency stop

`hermes pause [--reason ...]` — no scheduled fire starts through any door: the built-in ticker skips dispatch, the hosted-scheduler webhook answers **503 with `Retry-After: 60`** so it redelivers after resume, and the misfire sweep stays idle. In-flight runs are never killed. Explicit manual runs still execute.

### 1.9 Recursion guard

> "Cron-run sessions cannot recursively create more cron jobs. Hermes disables cron management tools inside cron executions to prevent runaway scheduling loops."

Opt-in via `cron.allow_agent_scheduling: true`, with two safety properties: one flat user-owned table (no special ownership), and `deliver: origin` from inside a cron run resolves at create time to the creating job's concrete target so a spawned job "can never point its output at a session that no longer exists."

Self-removal pattern is explicitly supported: a job may call `cronjob(action="remove", job_id=<its own id>)` and still deliver its final response.

### 1.10 Other details worth copying

- **Per-job timezone**, DST-stable: "0 9 * * * fires at 09:00 in that zone, and stays at 09:00 across that zone's DST changes." Changing the zone recomputes `next_run_at` immediately.
- **Per-job `workdir`**: injects that dir's `AGENTS.md`/`CLAUDE.md`/`.cursorrules`, and binds terminal/file tools to it. "Each agent run binds its workdir to that run's unique task identity" so concurrent runs don't leak paths.
- **Per-job `enabled_toolsets`**: "carrying browser, delegation into every tiny 'fetch news' job bloats the tool-schema prompt on every LLM call." If set on the job it wins; else cron-platform config; else built-in defaults. If the config can't be read, **the run fails with a recorded error instead of quietly running with every tool.**
- **Prompt injection scanning at creation and update time**: "Prompts containing invisible Unicode tricks, SSH backdoor attempts, or obvious secret-exfiltration payloads are blocked."
- **Self-contained prompts**: BAD "Check on that server issue"; GOOD "SSH into server 192.168.1.100 as user 'deploy', check if nginx is running…". Cron runs in a fresh session with no chat context.
- **Storage**: plain JSON on disk (`~/.hermes/cron/jobs.json`) with atomic writes; malformed entries repaired on load with type-only warnings.
- **Output**: `~/.hermes/cron/output/{job_id}/{timestamp}.md`.
- **Manual run is an extra fire**: "the scheduled run still happens and the repeat count is not spent."
- **Failure UX**: distinct statuses for delivery failure vs run failure; alert once then remind on cooldown; `hermes cron doctor` fleet health check.

## Part 2 — The eight-layer security model

They enumerate it explicitly:

1. **User authorization** — allowlists, DM pairing
2. **Dangerous command approval** — human-in-the-loop
3. **File write safety** — denylist + optional write sandbox
4. **Container isolation** — Docker/Modal/Singularity
5. **MCP credential filtering** — env isolation for MCP subprocesses
6. **Context file scanning** — prompt injection detection in project files
7. **Cross-session isolation** — sessions can't reach each other's data; cron storage hardened against path traversal
8. **Input sanitization** — working-dir params validated against an allowlist

### 2.1 Approval modes — per-surface policy, not one global switch

```yaml
approvals:
  mode: smart            # smart | manual | off
  timeout: 300
  cron_mode: deny        # deny | approve
  single_query_mode: deny
  unattended_mode: deny
  mcp_reload_confirm: true
  destructive_slash_confirm: true
```

| Mode | Behaviour |
|---|---|
| `smart` | auxiliary LLM assesses risk; low-risk auto-approved **for that command only**; genuinely dangerous auto-denied; uncertain escalates to manual |
| `manual` | always prompt |
| `off` | equivalent to `--yolo` |

The per-surface keys are the important design: cron/one-shot/webhook have **no human to answer**, so instead of blocking for the full timeout they **deny instantly** ("the agent must find another path"). Exception: an API session whose client *can* answer (`POST /v1/runs/{id}/approval`) still gets the card.

That is exactly flobot's situation for routines: an unattended routine must fail fast with an actionable error, not hang for five minutes.

### 2.2 YOLO mode with honest visual state

Three activations (`--yolo`, `/yolo` toggle, `HERMES_YOLO_MODE=1`), scoped precisely (session / process / profile), persisted with the session, and always paired with two persistent visual reminders (red banner + `⚠ YOLO` status fragment). `/new` and `/resume` restore approvals.

### 2.3 The hardline blocklist — a floor below YOLO

Refuses regardless of YOLO, `mode: off`, cron approve mode, or "allow always". Trips *before* the approval layer. No override flag.

Covered: `rm -rf /` and variants, `rm -rf --no-preserve-root /`, bash fork bomb, `mkfs.*` on mounted root, `dd if=/dev/zero of=/dev/sd*`, piping untrusted URLs to sh at rootfs top level.

**Fails closed on unparseable quoting**: "The floor also fails closed on a command whose shell quoting cannot be parsed… the error says malformed executable payload." Quoting is judged as written, so valid escapes inside a quoted pattern are not malformed.

### 2.4 User deny rules — YOLO with exceptions

```yaml
approvals:
  deny:
    - "git push --force*"
    - "*curl*|*sh*"
    - "dd if=* of=/dev/*"
```

fnmatch globs, case-insensitive, matched against whole command text **and** individual executable candidates, over the same normalized/deobfuscated variants the dangerous-pattern detector uses (so `git pu""sh --force` doesn't slip past). Executable candidates match both literal path and basename; `/usr/bin/sudo *` does not become a rule for every binary named sudo.

Supported launchers: `sudo env command exec nohup setsid time nice timeout stdbuf ionice chrt taskset chroot`. Shell `-c` payloads inspected recursively.

**And they publish the threat model honestly:** "Deny rules are a shell-command policy, not a complete shell interpreter or an OS capability sandbox… Do not use a basename deny rule as a guarantee that a capability cannot be reached by other means. For containment, use OS permissions and an isolated backend."

### 2.5 Approval timeout fails closed

No response within timeout → denied by default. "An expired prompt cannot be reopened: the pending entry is discarded and the agent is told not to retry on its own within that turn." A timeout is **not** counted as a denial, so asking again is never penalized.

### 2.6 The trigger list (partial, but instructive)

Recursive delete, root-path delete, world-writable chmod (incl. recursive long-flag form), recursive chown to root, mkfs, dd if=, writes to block devices, SQL DROP/DELETE-without-WHERE/TRUNCATE, overwrite of `/etc/`, systemctl stop/restart/disable/mask, kill -9 -1, pkill -9, fork bombs, `<shell> -c`, `<interp> -e/-c`, `curl|sh`, process substitution execution, tee/redirect to `/etc/`, `~/.ssh/`, `~/.hermes/.env`, `xargs rm`, `find -exec rm`, cp/mv into `/etc/`, `sed -i /etc/`, self-termination of hermes/gateway, docker lifecycle verbs incl. global flags, docker daemon redirect (`-H`, `DOCKER_HOST`, `context use`), podman remote redirect.

Two notable entries: **self-termination prevention** (`pkill hermes`, `gateway run &`) and **daemon redirect detection** (`docker -H`, `DOCKER_HOST=`) — both are "the agent breaks its own supervisor" hazards.

### 2.7 Container bypass rule

"When running in docker, modal, daytona or vercel_sandbox backends… dangerous command checks are skipped because the container itself is the security boundary." But user-defined deny rules apply to **all** backends "before any backend-specific approval shortcut."

### 2.8 execute_code has its own gate

"A script run by execute_code can call subprocess, os or shutil directly, and those calls never pass through the patterns above." So messaging/API sessions ask **once for the whole script** before it starts; a denied or unanswered script never runs. Local sessions follow the interactive rule instead (Python-level ops unprompted; only the script's `terminal()` calls ask).

The exemption requires both a viewer-submitted turn *and* an attached viewer who can answer approvals when the script starts.

That is the correct shape for flobot too: any tool that can execute arbitrary code needs a gate at the *script* level, not just at the command level.

### 2.9 Supervised-gateway lifecycle restriction

A non-overridable guard against stopping/restarting the gateway from inside its own supervised process — including name-derived kills (`pgrep python | xargs kill`) because "a supervised gateway is literally a python process." Kills scoped to an agent-owned process (`process(action="kill")`, explicit PID) pass.

On macOS specifically: executed `launchctl submit` / `launchctl bootstrap` are restricted regardless of label; read-only `launchctl print` is not a lifecycle operation.

flobot has exactly this hazard: it runs as an Electron app supervising a Go server which spawns flocode children. A bot that runs `pkill flobot` kills its own supervisor.

### 2.10 Context-file injection scanning with a documented exception

Project files (`AGENTS.md`, etc.) matching injection patterns are blocked:
```
[BLOCKED: AGENTS.md contained potential prompt injection (prompt_injection). Content not loaded.]
```

But your own `SOUL.md` in `HERMES_HOME` is treated as operator-authored: a scanner hit logs a warning naming the pattern and loads anyway, listed as `⚠ SOUL.md … loaded`. Rationale: an identity file that *documents* an attack phrase (security guidance) must keep working.

The exception does **not** extend to a profile-distribution-supplied SOUL.md — third-party copies still block.

That asymmetry (operator-authored = warn-and-load; third-party = block) is the right policy and flobot should copy it verbatim for its own prompt/context files.

### 2.11 Supply-chain advisory checking

A curated catalog of known-compromised package versions (e.g. the mistralai 2.4.6 poisoning) checked at CLI startup banner, `hermes doctor`, and gateway startup. Each advisory has a stable id; `hermes doctor --ack <id>` persists dismissal in `config.security.acked_advisories`. "Old advisories are intentionally not removed from the catalog — leaving them in place keeps fresh installs warned about historically poisoned versions that might still be cached in a private mirror."

Stdlib-only, one `importlib.metadata.version()` lookup per advisory so it's safe on every startup boundary.

## Part 3 — KEEP / TOSS for flobot

**KEEP (scheduler)**
- Pre-dispatch config validation with `blocked_config`, one alert, zero tokens spent.
- Durable fire identity + admission recovery; ambiguous-after-send stays unknown.
- `wakeAgent` pre-flight gate (flobot can implement as an optional shell pre-check per routine).
- `context_from` last-known-good chaining.
- Catch-up window = half period clamped to 2 min–2 h; per-job grace override; resume keeps the due slot.
- Three distinct verbs pause / cancel / remove with exact in-flight semantics.
- Global emergency stop with 503+Retry-After for external fire sources.
- Recursion guard on agent-managed scheduling.
- Per-job timezone (DST-stable), workdir (bound per run), enabled_toolsets (fail-closed if unreadable).
- Prompt-injection scanning of routine prompts at create/update.
- Operator-owned model pins; agent cannot re-point.

**KEEP (security)**
- Per-surface approval policy (`cron_mode`, `unattended_mode`) with instant deny where no human exists.
- Hardline blocklist below YOLO with no override flag; fail-closed on unparseable quoting.
- User deny rules as fnmatch globs over normalized variants + executable candidates.
- Approval timeout fails closed; timeout ≠ denial.
- Whole-script gate for code execution.
- Self-termination / supervisor-kill guard.
- Operator-authored context files warn-and-load; third-party block.
- Published threat model for what deny rules do *not* cover.

**TOSS**
- Eight-layer model verbatim (flobot has no gateway/messaging surface).
- YOLO as a first-class mode (flobot should keep approval always-on for routines).
- Container backends (out of scope for local-first).

## Part 4 — Gap vs flobot today

| Capability | flobot today | Gap |
|---|---|---|
| Pre-dispatch validation | none — executor spawns and fails | missing |
| Fire identity / recovery | none | missing |
| Pre-run cost gate | none | missing |
| Job chaining | none | missing |
| Catch-up window | none observed | missing |
| Cancel vs pause vs remove | stop only | missing distinctions |
| Global emergency stop | none | missing |
| Recursion guard | none | missing |
| Per-routine timezone | uses server TZ | missing |
| Per-routine toolset limits | none | missing |
| Approval policy per surface | none | missing |
| Hardline blocklist | none | missing |
| Deny rules | none | missing |
| Supervisor-kill guard | none | missing |
| Context-file injection scan | none | missing |
