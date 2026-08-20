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

<!-- filled during dogfood: argv, pin, NDJSON kinds, tools, errors, wall time, irregularities -->
