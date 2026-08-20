# Research round 3 — models, verification, web UI

**Date:** 2026-08-19T23:52:29Z  
**Secrets:** `~/.accounts/meta.ai` exists, 49 bytes, single line, starts with `LLM_`. Key is **not** printed. User path `~/accounts/meta.ai` is wrong; actual path is `~/.accounts/meta.ai`.

---

## Routing table (advertised vs registry vs hydra)

| Route id | Role | Advertised model (amesh flobot) | Live source of truth | OpenAI base_url | Auth |
| -------- | ---- | ------------------------------- | -------------------- | --------------- | ---- |
| `glm` | orchestrator | `glm-5.2-fp8` | hydra kb + amesh registry `verda-b300-glm52/glm-5.2-fp8` | `http://10.30.0.127:13038/v1` (WG). Public alias `http://23.183.40.38:13038/v1` is the same proxy; on-box clients must not hairpin. | none in registry |
| `qwen` | coder | `qwen3.8-27b-fp8` | **split brain.** Catalog/UI advertise 3.8. `~/.amesh/registry.json` currently lists `qwen3.6-27b-fp8` at `qwen36-b300` and Gemma 4 at `http://23.183.40.72:8020/v1`. Hydra kb (`kb/models/qwen3.8-27b-aiter-mi300x.md`) says Qwen3.8 **took :8020** and replaced Gemma's row. Pin both; probe `/v1/models` at start. | Qwen 3.8: `http://23.183.40.72:8020/v1`. Qwen 3.6 fallback: `http://10.10.0.14:8000/v1` (`MASTER_LLM_*` in `~/.amesh/current-master.env`) | none / optional `OCR_LLM_API_KEY` |
| `local` | tools | `qwen3-coder-30b-a3b-mlx` | advertised on this Mac; no `local-agent.json`; no mlx OpenAI URL found | none — route is documented; executor returns a typed error until a pin exists | — |
| `muse` | chief-of-staff / verifier | `muse-spark-1.2` | Meta Model API | `https://api.meta.ai/v1` | `MODEL_API_KEY` from `~/.accounts/meta.ai` (child env only) |

### Live `/v1/models` probe (2026-08-19T23:52Z, 8s timeout)

| URL | HTTP | ids |
| --- | ---- | --- |
| `http://10.30.0.127:13038/v1/models` | 200 | `glm-5.2-fp8` |
| `http://23.183.40.38:13038/v1/models` | timeout | public hairpin; do not use from this Mac |
| `http://23.183.40.72:8020/v1/models` | 200 | **`qwen3.8-27b-fp8`** (registry Gemma row is stale; hydra kb was right) |
| `http://10.10.0.14:8000/v1/models` | timeout | Qwen 3.6 fallback **down** |
| `http://86.38.182.64:13000/v1/models` | timeout | unused |
| `https://api.meta.ai/v1/models` | 200 | `muse-spark-1.2-contributor`, **`muse-spark-1.2`**, `muse-spark-1.1` |

Pin `qwen` at 3.8 on `:8020`. Keep 3.6 as documented fallback but it is not live. Muse pin model id is exactly `muse-spark-1.2`.

`~/.amesh/model-endpoints/` is an **empty directory**. Registry `base_url_ref` values (`local_config:…`) have no corresponding files on disk. Do not invent a second registry.

`~/.flocode/config.toml` is **missing**. flocode2 `InitExample` points at `127.0.0.1:8080` — wrong for this mesh. Write a real pin file (env_key names only).

`~/.amesh/local-agent.json` is **absent**. Mesh client in flocode2 is `AbsentMesh()` — correct. flobot must not wait on mesh discovery to run.

### Pin names for `~/.flocode/config.toml` (no secrets)

```toml
default_model = "glm-5.2"

[models.glm-5.2]
provider = "openai-compat"
model = "glm-5.2-fp8"
base_url = "http://10.30.0.127:13038/v1"
profile = "glm-5.2"

[models.qwen-3.8]
provider = "openai-compat"
model = "qwen3.8-27b-fp8"
base_url = "http://23.183.40.72:8020/v1"
profile = "qwen3-coder"

[models.qwen-3.6]
provider = "openai-compat"
model = "qwen3.6-27b-fp8"
base_url = "http://10.10.0.14:8000/v1"
profile = "qwen3-coder"

[models.muse-spark-1.2]
provider = "openai-compat"
model = "muse-spark-1.2"
base_url = "https://api.meta.ai/v1"
api_key_env = "MODEL_API_KEY"
profile = "generic-openai"
```

Route map: `glm→glm-5.2`, `qwen→qwen-3.8` (fallback `qwen-3.6` if `/v1/models` disagrees), `muse→muse-spark-1.2`, `local→error until pin`.

If live `/v1/models` on `:8020` is still Gemma, pin `qwen` at 3.6 and record the irregularity. Do not silently call Gemma a Qwen coder.

---

## Verification (Muse via flocode2 openai bindings)

Today: `loop.Deps.Verify` is hardcoded `false`. `verify.Run` never HTTP-calls. Tests assert it does not exec Makefile.

Required:

1. CLI `--verify` and env `FLOCODE_VERIFY=1` set `Deps.Verify=true`.
2. `verify.RunWithPin(ctx, enabled, workspace, pin)` uses `internal/llm/openai.New(pin, client)` then a **non-streaming** chat completion: “Reply with PASS or FAIL and one sentence. Did the landed workspace change look complete?” Bound by existing 30s `verifyBound`.
3. Default verify pin = `muse-spark-1.2` from config when `--verify` is on.
4. Missing `MODEL_API_KEY` → `skipped: missing key` (typed `MissingKeyError`), never panic.
5. Never include file bodies that look like secrets. Cap prompt at 4KiB of `git status` / changed-path list only (workspace walk already stats Makefile/go.mod).
6. Result is advisory: landed turn still returns nil; NDJSON `kind=verify`.

flobot executor passes `--verify` on CODE runs that request it. Test-run (ask) does not verify.

---

## Web interfaces

### A. Standalone dogfood (this repo) — primary “get it working”

- Port **4120** (free; new project range 4120–4124). Loopback only.
- `GET /` static `web/index.html`
- Same roster + `POST /api/v1/runs` without pairing (actor=`local`)
- Event log from NDJSON
- Route health: `GET /api/v1/health/models` probes `/v1/models` and reports ids (no keys)

### B. amesh web v2 (plugin)

- amesh-web already listening `127.0.0.1:18765`
- Extend Agents → flobot card: prompt, yolo checkbox, event stream, skills/routines/test-run buttons
- Pairing still required
- `POST /api/v2/plugins/flobot/runs`

Browser tools are not in this agent’s tool list. Verify standalone UI with curl against :4120 and amesh API with httptest + live curl if pairing is available. Record the gap.

---

## Ports

| Project | Port | Service |
| ------- | ---- | ------- |
| amesh | 18765 | amesh-web (in use, pid amesh-web) |
| amesh | 18766 | amesh-helper |
| flocode2 | 4090 | design-site |
| flocode2 | 4091 | pprof |
| flobot | **4120** | standalone web (free) |
| flobot | 4121–4124 | reserved |

Register in `~/dev/flobot/.claude/ports.json` and `~/.claude/port-registry.json`. No conflict with 18765/4090.

---

## Logfiles to inspect during dogfood (max 10 attempts)

| Location | What |
| -------- | ---- |
| `~/.flocode/logs/` | donor-era logs (5710 files). New runs may not land here — flocode2 uses slog to stderr. |
| `~/.flocode/session.db` | flocode2 sessions (exists, 135168 bytes, updated 2026-08-19 15:16) |
| child stderr of executor | slog text; capture per-run |
| `~/dev/flocode2/docs/collab/agents.log` | prior collab |
| `docs/history/evidence/2026-08-19-*.md` | tag-gate stand-ins (several are tests, not live) |

Hotspots already known from v0.1.0:

- native envelope skipped `RepairArgs` (fixed in `5885cd1`)
- no config.toml → `ErrNoModel`
- verify always off
- jsonout has no `session_id`
- Qwen 3.8 advertised vs 3.6/Gemma on :8020
- thinking-mode stalls
- headless mutating tools denied without `--yolo`

Record each attempt: argv, pin, first/last NDJSON kinds, tool names, errors, wall time, any secret leak in logs (must be zero).
