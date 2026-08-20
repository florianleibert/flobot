# Remaining Phase-2 tasks

## T-persist
- files: internal/store/store.go, internal/store/persist.go, internal/store/store_test.go
- depends_on: []
- parallel: true
- acceptance: Store Save/Load ~/.flobot/state.json; Owner never in JSON; secrets never in file; TestPersistRoundTrip passes.

## T-skill-prompt
- files: internal/executor/executor.go, internal/server/server.go
- depends_on: []
- parallel: true
- conflicts_with: T-ccpm-log (executor.go)
- acceptance: test-run / skill run prepends skill body to the flocode prompt.

## T-ccpm-log
- files: internal/executor/executor.go, docs/ccpm-runs.md
- depends_on: []
- parallel: false
- conflicts_with: T-skill-prompt
- acceptance: each child run appends one line: RFC3339, pin, intent, result; no API keys.
