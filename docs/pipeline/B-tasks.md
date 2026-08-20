## T-persist
files: internal/store/state.go, internal/store/state_test.go
depends_on: none
parallel: true
acceptance: Reads/writes `~/.flobot/state.json` with 0600 permissions. Initializes `glm`, `qwen`, `muse` with pinned models if absent. Strips `Owner` and `secrets`. Handles corrupted JSON gracefully without crashing.

## T-executor
files: internal/executor/flocode.go, internal/executor/flocode_test.go
depends_on: none
parallel: true
acceptance: Resolves `flocode` via PATH. Spawns with `-p --output streaming-json`. Parses stdout as NDJSON and forwards maps to a channel. Does not import `github.com/florianleibert/flocode`.

## T-secrets
files: internal/executor/credentials.go, internal/executor/credentials_test.go
depends_on: T-executor
parallel: false
acceptance: Reads `~/.accounts/meta.ai` (plain text). Exposes value *only* to the child process env as `MODEL_API_KEY`. Never logs, prints, or persists the key. Returns error on missing file without leaking contents.

## T-skills
files: internal/skills/loader.go, internal/skills/loader_test.go
depends_on: none
parallel: true
acceptance: Discovers `SKILL.md` files. Prepends body to prompt as `skillBody + "\n\n---\n\n" + userPrompt`. Returns unmodified prompt if no skill selected.

## T-ccpm
files: internal/ccpm/logger.go, internal/ccpm/logger_test.go
depends_on: none
parallel: true
acceptance: Appends exactly one line per child run to `./logs/ccpm.log`. Format: `<timestamp> <pipeline_id> <stage A-E> <child_run_id> <status>`. Uses file locking for atomic appends. No duplicate lines per `child_run_id`.

## T-server
files: internal/server/server.go, internal/server/server_test.go
depends_on: T-executor
parallel: false
acceptance: Binds exclusively to `127.0.0.1:4120`. Serves static UI and `/api/stream` (NDJSON). Forwards executor events to connected clients. Handles client disconnect/reconnect without server crash.

## T-webui
files: web/index.html, web/app.js, web/styles.css
depends_on: T-server
parallel: false
acceptance: Connects to `/api/stream`. Renders `content`, `thinking` (collapsed), `tool_start`/`tool_end` (paired), `perm` (mock ack), `plan`, `err`, `done`, `verify` (badge). Never displays secrets or API keys.

## T-verify
files: internal/executor/verify.go, internal/executor/verify_test.go
depends_on: T-executor, T-secrets
parallel: false
acceptance: Triggers Muse Spark 1.2 run via `flocode`. Injects `MODEL_API_KEY` env var. Emits `verify` NDJSON kind on success. Emits `err` kind with actionable message on missing/invalid key.
