# Research round 2 — amesh plugin contract + flocode2 harness API

**Date:** 2026-08-19T23:52:29Z  
**Mode:** read-only of `~/dev/amesh` and `~/dev/flocode2`.

---

## 1. Current amesh flobot API (roster only)

Catalog: Gmail first, flobot second. Digest equality only (placeholder `sha256:eeee…`). Signed means “catalogued,” not cryptographically verified (`desktop_catalog.go:165-174`).

Pairing: all mutating routes need `X-Amesh-Pairing-Session` and a prior `POST /api/v2/plugins/install`.

| Method | Path | Today |
| ------ | ---- | ----- |
| GET | `/api/v2/plugins/flobot` | roster snapshot (`schema=amesh.flobot.v1`) |
| POST | `/api/v2/plugins/flobot/bots` | create named bot |
| GET | `/api/v2/plugins/flobot/bots` | list |
| POST | `/api/v2/plugins/flobot/bots/{id}/memory` | remember |
| POST | `/api/v2/plugins/flobot/bots/{id}/message` | bus message |
| POST | `/api/v2/plugins/flobot/handoff` | single-owner handoff |
| POST/GET | `/api/v2/plugins/flobot/skills` | skill metadata |
| POST/GET | `/api/v2/plugins/flobot/routines` | paused routines |
| POST | `/api/v2/plugins/flobot/routines/{id}/test-run` | **metadata only** |
| POST | `/api/v2/plugins/flobot/routines/{id}/enable` | requires LastRun |
| GET | `/api/v2/plugins/flobot/messages` | bus |
| GET | `/api/v2/plugins/flobot/routes` | four advertised routes, no URLs/keys |

Wired in `go/cmd/amesh-web/main.go:100-113`. Store: `go/internal/helper/flobot.go`. HTTP: `go/cmd/amesh-web/flobot.go`. UI: `webv2/src/App.tsx` roster + handoff only. Skills/routines are API-only (`docs/operator/flobot.md:56-57`).

### TestRun today

```
helper/flobot.go:296-306
LastNote = "test-run: no external effect"
```

Operator doc line 44: “no external effect”. This is the Phase 2 hole.

### Plugin host: do not invent one

`InstallFromCatalog` (`helper/plugin.go:67-81`) compares `{id, digest, signed}` against `DesktopPlugins()`. `operator_plugin_manifest.schema.json` is mesh-enroll/acquire, **not** a runtime plugin loader.

`LaunchKindFloCode` (`helper/launch.go:24,85,145`) is the Path 1 catalog resolver for spawning FloCode via RPC `RPCSpawnFloCodeRun`. Dry-run until a signed capability digest exists. flobot execution must **not** wait on that digest. Exec PATH `flocode` from the web process after pairing+install, same as a helper would.

Gmail pattern to copy: catalog row + install digest + pairing-gated HTTP + tokens never on JSON types.

---

## 2. flocode2 public surface (v0.1.0)

Binary: `~/dev/flocode2/flocode` and `~/.local/bin/flocode` both report `0.1.0`. Module path is `github.com/florianleibert/flocode` (same as the donor). **Do not import this module from amesh or flobot** — `replace` would still collide with the donor. **Exec the binary.**

CLI (`cmd/flocode/main.go`):

```
flocode -p PROMPT --model PIN --intent ask|plan|code --output plain|json|streaming-json
        [--yolo] [--worktree NAME] [--continue-until] [--config PATH]
        [--resume ID] [-c]
```

- `-p` selects headless. Default intent **code**.
- `--yolo` skips permission Ask **and** Guard; never lifts Admit (`engine.go:517-521` headless without Ask denies mutating tools).
- `--output streaming-json` → `internal/jsonout` NDJSON, 8KiB text cap, **no tool args, no result bodies, no pin credentials**.

NDJSON kinds: `content | thinking | tool_start | tool_end | perm | plan | err | done | verify`.

Public library (for completeness, unused by the plugin host):

- `harness.Build(ctx, Config, opts...)` → `Engine`
- `Engine.Run(ctx, LoopRequest) (<-chan LoopEvent, error)`
- `loop.Continue` for `--continue-until`

Verify today (`internal/verify/verify.go`): default **off** (`wire.go:141 Verify: false`). Enabled with no pin → `skipped: no verifier configured`. Never shells out. Bound 30s (`loop/api.go:20`).

Config (`internal/config/config.go`): `~/.flocode/config.toml` **does not exist** on this Mac as of 2026-08-19. Tiny TOML subset: `default_model`, `[models.NAME]` with `provider/model/base_url/api_key_env/profile`, `[tools] webfetch`, `[[mcp]]`.

Profiles: `glm-5.2` (sniff glm_xml first, 1M window, glm47 hint), `qwen3-coder` (qwen3_xml first), `hermes-xml`, `deepseek`, `generic-openai`. Muse will fall through to generic-openai unless a pin `profile` is set.

---

## 3. Recommended execution (do not import amesh into flocode2)

Argv contract (canonical, used by both `~/dev/flobot` and amesh-web):

```
flocode -p "$PROMPT" \
  --model "$PIN" \
  --intent "$INTENT" \
  --output streaming-json \
  [--yolo] \
  [--worktree "$WT"] \
  [--continue-until]
```

Env for the child (never logged):

| Env | When |
| --- | ---- |
| `FLOCODE_CONFIG` | optional override of config.toml |
| `FLOCODE_LOG_LEVEL=info` | dogfood |
| `MODEL_API_KEY` | Muse route only; loaded from `~/.accounts/meta.ai` if unset |
| `FLOCODE_VERIFY=1` | after landed writes, if `--verify` is on |

Pin names in config.toml: `glm-5.2`, `qwen-3.8`, `qwen-3.6`, `muse-spark-1.2`. Route ids `glm|qwen|local|muse` map to those pins.

`~/dev/flobot` Go module `github.com/florianleibert/flobot` owns the executor + standalone web (loopback). amesh-web duplicates a thin `os/exec` wrapper so amesh does not take a Go dependency on an unpublished module.

---

## 4. File-level change plan

### `~/dev/flobot` (new plugin product)
- `cmd/flobot` — `serve` (web+API) and `run` (one-shot exec)
- `internal/store` — roster (independent copy of helper types; pairing actor = `local` when standalone)
- `internal/executor` — `os/exec` flocode, parse NDJSON, ring-buffer events
- `internal/routes` — advertised routes + pin map (no secrets)
- `internal/server` — HTTP including `POST /api/v1/runs`
- `web/index.html` — dogfood UI
- tests for store + executor (fake binary)

### `~/dev/amesh` (plugin host)
- `go/internal/helper/flobot.go` — `FlobotRun` + `StartRun` + change `TestRun` to spawn ask-intent
- `go/cmd/amesh-web/flobot.go` — `POST /runs`, `GET /runs`, `GET /runs/{id}/events`
- `go/cmd/amesh-web/main.go` — mux
- `webv2/src/{App.tsx,lib/types.ts}` — prompt box, event log, skills/routines controls
- `docs/operator/flobot.md` — execution is real now
- tests in `flobot_test.go` + `cmd/amesh-web/flobot_test.go`

### `~/dev/flocode2` (harness)
- `--verify` flag + `FLOCODE_VERIFY`
- `verify.RunWithPin` using `internal/llm/openai` Client (Muse)
- jsonout: add `session_id` (no secrets)
- slog on tool_start/tool_end/malformed args/provider errors
- muse-spark profile glob
- sample config (repo) + write `~/.flocode/config.toml` if missing (no keys)

### Never
- `~/dev/flocode` (donor)
- `~/.opencode/bin/opencode`
- push `main`
- kill tmux
- print Muse/API keys

---

## 5. Safety

- Pairing session owns bots/runs. Stranger → 403.
- Catalog digest still required before mutating.
- Install digest is **not** execution authority (comment in `desktop_catalog.go`). Execution is a new, explicit `/runs` surface gated the same way.
- Muse key stays on this Mac; never in JSON, never in NDJSON, never in LastNote.
- Headless CODE without `--yolo` denies mutating tools (existing engine behavior) — UI must say this.
- Test-run uses `--intent ask` so it cannot write.
- `--yolo` is an explicit checkbox, default off.
