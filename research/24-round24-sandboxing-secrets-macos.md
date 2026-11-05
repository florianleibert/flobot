# Research round 24 — Sandboxing and secrets on macOS: what is actually available

**Date:** 2026-11-04
**Track:** F — durable execution, memory, sandboxing
**Sources (fetched 2026-11-04):**
- Apple `sandbox-exec(1)` / Seatbelt profile language (as documented in the macOS man pages and the public `sandbox` profile grammar)
- https://developer.apple.com/documentation/security/keychains (Keychain Services)
- Hermes' container-bypass rule and env-declaration model (rounds 11–12)
- Codex's `shell_environment_policy.include_only` (round 14)
- flobot's `internal/executor/credentials.go` (plaintext `~/.accounts/meta.ai`)

---

## 1. The four isolation levels available on a Mac, ranked by cost

| Level | Mechanism | Isolation | Cost | flobot fit |
|---|---|---|---|---|
| 0 | nothing — child inherits everything | none | free | current state |
| 1 | **env allowlist + cwd + resource limits** | no filesystem/network boundary | trivial | **do first** |
| 2 | **`sandbox-exec` Seatbelt profile** | filesystem + network + process | low | **the real answer** |
| 3 | per-run worktree + level 2 | + no cross-run file leakage | low | combine with 2 |
| 4 | container / VM | full | high | out of scope |

Level 1 is not security, it is hygiene: it stops accidental leakage. Level 2 is the only real boundary available without a VM, and it is deprecated-but-functional — Apple has kept `sandbox-exec` working for over a decade while discouraging its use.

## 2. Level 1 — the env allowlist, which flobot needs regardless

Codex's model (round 14 §6) is the right one:

```toml
shell_environment_policy.include_only = ["PATH", "HOME", "LANG"]
```

An **allowlist**, not a denylist. flobot currently passes the full environment plus injected credentials (`executor/credentials.go`). Consequences:

- every secret in the operator's shell is visible to every bot
- `AWS_*`, `GITHUB_TOKEN`, `OPENAI_API_KEY`, `SSH_AUTH_SOCK` all leak
- a malicious skill can read them from `os.environ` trivially

The fix is a five-line change with large impact: build the child env from an explicit allowlist plus the per-run declared credentials, nothing else.

## 3. Level 2 — a Seatbelt profile per run

`sandbox-exec -f profile.sb <command>` applies a Scheme-like policy. The primitives flobot needs:

```
(version 1)
(deny default)                                  ; deny-by-default
(allow process-exec (literal "/usr/bin/env"))   ; explicit binaries
(allow process-fork)
(allow sysctl-read)
(allow file-read* (subpath "/usr/lib")
                  (subpath "/System/Library")
                  (subpath "/private/tmp/flobot-run-XYZ"))
(allow file-write* (subpath "/private/tmp/flobot-run-XYZ"))
(allow network-outbound (remote ip "127.0.0.1:*"))   ; local model server only
(deny network-outbound (remote ip "169.254.169.254:*"))  ; metadata endpoint
```

The three properties that matter:

1. **`(deny default)` first.** Allow-by-default profiles are worthless.
2. **`file-read*` scoped to the worktree + system paths.** The child cannot read `~/.ssh`, `~/.aws`, or `~/.accounts`.
3. **`network-outbound` scoped to the local model server.** This is the SSRF mitigation from round 19 §6, and it is the *only* place it can be enforced reliably — application-level URL checks lose to DNS rebinding.

### The operational caveats

- `sandbox-exec` is deprecated. It still works, but Apple could remove it. Mitigation: make the sandbox backend pluggable (`none` / `seatbelt` / `container`) so a future replacement is a config change.
- Profiles are per-run, generated from a template with the worktree path substituted. Generating them from user input is an injection risk — paths must be validated (no quotes, no parens) before substitution.
- A too-tight profile produces confusing failures ("operation not permitted" from deep inside a toolchain). The profile needs an escape hatch: a per-bot `sandbox: relaxed` level that widens to `(allow file-read* (subpath home))` while keeping network denied.
- Node/Python toolchains need `/private/var/folders` (temp) and often `/dev/null`, `/dev/urandom`, `mach-lookup` for DNS.

## 4. Level 3 — per-run worktree

flobot already has `internal/workspace` with worktrees. The rule to add: **every run gets its own worktree, and the sandbox profile's only writable path is that worktree.** Then:

- two concurrent bots cannot corrupt each other's files
- a failed run leaves a disposable directory
- the diff of what a run changed is computable (`internal/diff` already exists)

Amp's "Orbs" (round 15 §2) is the cloud version of exactly this: the isolation unit is the thread, not the agent.

## 5. Secrets: Keychain, and the invariant that matters more than the store

The invariant from round 9 §2: **the secret value never enters the model context.** Not redacted after the fact — never admitted.

Three mechanisms, in order of preference:

| Mechanism | Model sees | Use for |
|---|---|---|
| Keychain lookup at spawn, injected as env | nothing | API tokens, PATs |
| Keychain lookup at spawn, injected as a file the tool reads | nothing | multi-line credentials (service account JSON) |
| Masked input form → written straight to Keychain | nothing | user-entered secrets |
| Secret in the prompt | **the value** | never |

flobot today reads a plaintext file (`~/.accounts/meta.ai`) and passes it as env. Two changes:

1. Move to Keychain (`security find-generic-password -s flobot/<integration> -w` or the Security framework via cgo).
2. Scope per integration *and* per bot: bot A's GitHub token is not bot B's.

### The scrubbing requirement

Even with perfect injection, secrets leak into logs: a tool echoes its env, an error message includes a URL with a token, a stack trace prints a header. So the event log needs **output scrubbing**: every event body passes through a redactor that replaces any known secret value with `[REDACTED:<name>]`.

Hermes does exactly this for Team Secrets: "output secret-scrubbed" (round 9 §6). The redactor must be applied at *write* time, not read time, or the plaintext is already on disk.

## 6. The honest disclosure flobot must ship

Two statements, both from primary sources:

1. **"All bots share one machine and one user account. Do not use separate bots as a security boundary."** — Grok Bot says this about its cloud computer (round 9 §3); it is equally true of flobot's Mac.
2. **"MCP servers run with the same privileges as the client."** — MCP's security guidance (round 20 §4).

flobot should state both in its own docs and show the first in the UI when a second bot is created. Under-promising here is what makes the sandbox work credible.

## 7. The layered plan for flobot

**Step 1 (hours):** env allowlist + per-run worktree + output scrubbing of known secrets.
**Step 2 (days):** Keychain-backed secret store, per-integration and per-bot scoping; replace the plaintext file.
**Step 3 (days):** Seatbelt profile generation per run with three levels (`none` / `standard` / `relaxed`); network outbound restricted to the configured model endpoint.
**Step 4 (later):** pluggable sandbox backend so a container can be swapped in.

Each step is independently useful and independently shippable. Step 1 alone closes the largest hole.

## 8. Gap vs flobot today

| Capability | flobot today | Gap |
|---|---|---|
| Env allowlist for child | full env passed | missing |
| Per-run worktree | worktrees exist, not per-run | partial |
| Seatbelt profile | none | missing |
| Network egress restriction | none | missing |
| Keychain secret store | plaintext file | missing |
| Per-bot secret scoping | none | missing |
| Output scrubbing | none | missing |
| Privilege-parity disclosure | none | missing |
