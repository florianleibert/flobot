# flobot / flocode2 dogfood log

**Started:** 2026-08-19T23:52:29Z  
**Binary:** `/Users/floleibert/.local/bin/flocode` → flocode2 `0.1.0`  
**Max attempts:** 10  
**Rule:** never log API keys, `LLM_*` tokens, or tool args.

## Baseline (before execution plugin)

| Check | Result |
| ----- | ------ |
| `flocode --version` | 0.1.0 |
| `~/.flocode/config.toml` | missing at research time |
| `~/.flocode/session.db` | 4 tables, 0 sessions |
| GLM `10.30.0.127:13038` | 200 `glm-5.2-fp8` |
| Qwen `23.183.40.72:8020` | 200 `qwen3.8-27b-fp8` |
| Qwen 3.6 `10.10.0.14:8000` | timeout |
| Muse `api.meta.ai` | 200 `muse-spark-1.2` (+ 1.1, contributor) |
| amesh-web `:18765` | listening (amesh-web pid 13171) |
| flobot `:4120` | free |

Known harness hotspots from v0.1.0 (pre-this-PR): native envelope skipped RepairArgs (fixed 5885cd1); verify hardcoded off; jsonout no session_id; no config.toml → ErrNoModel; headless mutating tools denied without --yolo.

## Attempts

| # | What | Result | Irregularities |
| - | ---- | ------ | -------------- |
| 1 | `flocode -p pong --intent ask --model glm-5.2 --output streaming-json` | `content=pong` `done` session_id set. Exit 0. Empty stderr. | none |
| 2 | same, `--model qwen-3.8` | `content=pong` `done`. Exit 0. | none. Live `/v1/models` on `:8020` is `qwen3.8-27b-fp8` (registry Gemma row is stale). |
| 3 | `--model muse-spark-1.2` with `MODEL_API_KEY` from `~/.accounts/meta.ai` | `content=pong` `done`. Key not in NDJSON. | none |
| 4 | flobot `POST /api/v1/runs` ask glm | status running→done, events `[content,done]`, text pong. No `LLM_` in JSON. | HTML served on `:4120`. Health probes: glm ok, qwen38 ok, qwen36 unreachable, muse ok. |
| 5 | GLM `--yolo --verify` write `hello.txt` | apply_patch ok, file correct. | **content split** into 6 NDJSON chunks. **verify failed: empty reply** — Muse spent `max_tokens=128` on hidden reasoning (`finish_reason=length`, `content=null`). slog: `tool_call_source source=envelope`. |
| 6 | retry verify with env key | apply_patch ok | Muse returned a paragraph (no PASS/FAIL token) about untracked files → classified failed. |
| 7 | clean workspace, `max_tokens=1024` | apply_patch ok | still `empty reply (finish_reason length)` — reasoning fills the whole budget unless `reasoning_effort` is set. |
| 8 | `reasoning_effort=low` + 1024 | **verify passed**. File `hello-from-flobot`. | thinking still 7 NDJSON lines. Content still chunked (UI coalesces). |
| 9 | Qwen 3.8 `--yolo` write `qwen.txt` | apply_patch ok, `qwen-ok`. | `verify skipped: verify off` still emitted on landed writes. No thinking events (Qwen quieter). |

### Hotspots filed/fixed
- Muse Chat Completions: `reasoning_effort=none` is 400; omit → model-determined reasoning can eat `max_tokens`. Fix: `low` + 1024.
- jsonout emits one `content` line per token delta. UI coalesces; harness keeps streaming-json token-level.
- `MODEL_API_KEY` must be injected for `--verify` even when the bot route is glm/qwen.
- Qwen 3.6 mesh node timed out; do not use as fallback tonight.

### Not verified
No browser MCP in this session. Web UI checked via curl (HTML, roster, runs, health). Desktop/mobile viewport not exercised.
