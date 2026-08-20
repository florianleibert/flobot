# Using flocode to implement flobot (sample project)

**Date:** 2026-08-20  
**Goal:** fix flocode by treating flobot as a real coding-agent workload.

## What we did this pass

| Stage | Model | Intent | Result |
| ----- | ----- | ------ | ------ |
| Enable pipeline skill | operator | — | `.flocode/skills/pipeline` on |
| PLAN remaining tasks | muse-spark-1.2 | plan | **stall** after glob/read loop; no `plan_write`. First attempt: empty `base_url` because pins lived in `~/.flocode/config.toml` while the binary read `~/.config/flocode/config.toml`. |
| Write `docs/tasks.md` | glm-5.2 | code --yolo | **ok** `apply_patch` |
| T-persist Save/Load | qwen-3.8 | code --yolo --verify | **ok** tests pass, Muse verify pass |
| Wire persist + skill body + ccpm log | qwen-3.8 | code --yolo --verify | **iteration bound exceeded** (25) after useful patches; `go test ./...` still green; no verify event |

## Flocode bugs found (sample project)

1. **Config path split.** After `harden headless`, `Dir()` is XDG `~/.config/flocode`. Existing pins in `~/.flocode/config.toml` were ignored → `--model muse-spark-1.2` had empty `base_url`. **Fix:** `Path()` falls back to `~/.flocode/config.toml` when XDG file is missing.
2. **Muse stall.** Profile `generic-openai` inter-token stall 30s fires during hidden reasoning. Config `profile = "generic-openai"` on the Muse pin skipped model glob. **Fix:** `muse-spark` profile (180s/120s) and glob wins over generic-openai when the model name is Muse.
3. **Iteration bound 25** is too low for a 3-task multi-file implement prompt. `--max-iterations` exists; default still 25. Recorded, not changed this pass.
4. **PLAN never called `plan_write`** — Muse kept reading. Skill was enabled but not followed as a gate.

## Not done

- Re-implementing the original Grok-written executor/UI through flocode (that already shipped).
- x-agent `--prds` (writes into donor `~/dev/flocode`).
