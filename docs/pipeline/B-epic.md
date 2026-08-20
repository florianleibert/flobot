outcome: Deliver a local-first, secure multi-agent orchestration UI and executor that persists teammate rosters, prepends skills, logs CCPM pipeline stages, and verifies Muse Spark 1.2 via spawned `flocode` processes.
in-scope: 
- Roster persistence in `~/.flobot/state.json` with 0600 permissions.
- Secret-free state guarantees (stripping `Owner` and secrets).
- `flocode` binary spawning via PATH with streaming NDJSON parsing.
- Muse Spark 1.2 execution via runtime-injected `MODEL_API_KEY`.
- Skill (`SKILL.md`) body prepending to prompts.
- CCPM atomic logging (one line per child run).
- Standalone Web UI on `127.0.0.1:4120` rendering all NDJSON kinds.
non-goals: 
- Cloud synchronization or remote roster sharing.
- Supporting models other than GLM 5.2, Qwen 3.8, Muse Spark 1.2.
- Importing or modifying upstream `flocode` source code.
- Binding the web UI to public interfaces (0.0.0.0).
- Persisting secrets or API keys anywhere on disk.
acceptance: 
- Roster survives restarts and initializes defaults if missing.
- UI streams and renders all specified NDJSON kinds without dropping events.
- Skills are prepended exactly as `skillBody + "\n\n---\n\n" + userPrompt`.
- Exactly one CCPM log line is appended per child run, atomically.
- `verify` successfully runs Muse Spark 1.2 using `MODEL_API_KEY` sourced at runtime, emitting a `verify` event.
