Build flobot in this repository: a local-first analogue of https://x.ai/bot.

Named teammates glm, qwen, muse persist in ~/.flobot/state.json with no secrets and no Owner JSON field.
Execution: spawn PATH flocode (flocode2 0.1.0) with -p --output streaming-json. Never import module github.com/florianleibert/flocode.
Standalone web UI on 127.0.0.1:4120 streaming NDJSON kinds content,thinking,tool_start,tool_end,perm,plan,err,done,verify.
Pins: GLM 5.2, Qwen 3.8, Muse Spark 1.2. Muse key from ~/.accounts/meta.ai as MODEL_API_KEY only; never print keys.
Test-run is ask-intent only. Skills are SKILL.md. CCPM A→E via `flocode pipeline`.
Never git push origin main or master. Never invent API keys.

This repo already has store, executor, server, web. Implement remaining gaps so a user can: persist/load the roster, prepend a skill body to a run, append one CCPM log line per child run, and actually run Meta Spark Muse 1.2 through flocode for verify. Keep changes inside this repo. Do not edit ~/dev/flocode. Do not edit ../flocode2.
