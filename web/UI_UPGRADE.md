# flobot UI — 10x upgrade (implement)

## Scope
Dramatically upgraded the flobot agent-control web UI (`web/index.html`,
`web/app.js`, `web/styles.css`) on top of the existing Liquid Glass design
system. All changes are frontend-only; the Go backend and its API surface are
unchanged, and every pre-existing element ID + test contract is preserved.

## What shipped

1. **Event-kind filter chips** (live Run console + drawer transcript).
   One active filter per console; `tool_start`/`tool_end` collapse into a
   `tools` bucket; filtering replays buffered events instantly instead of
   being append-only. Lets an operator isolate errors, hide thinking noise,
   or watch only tool activity.

2. **Run detail drawer.** Click any run in RECENT RUNS to open a slide-in
   drawer with:
   - meta grid (status, intent, bot, route, yolo, verify, event count, created)
   - prompt block
   - **activity sparkline** — one bar per event bucket, colored by kind
     (tool/err/done/verify/thinking), turning a flat transcript into a shape
   - full transcript with its own filter chips
   - **Copy transcript** (clipboard) and **stream live** (jump to the live
     console for that run)

3. **⌘K command palette.** Fuzzy jump to any panel with keyboard navigation
   (↑/↓/Enter), mouse click, and per-item key hints.

4. **Keyboard shortcuts.** `1–7` jump to panels, `r` opens Run, `⌘K` palette,
   `Esc` closes overlays. Typing in inputs never triggers shortcuts.

5. **Auto model-health probe.** On load, probes `/api/v1/health/models` and
   shows a live `routes N/M up` line with a green/red LED in the sidebar
   footer; also fills the Model health table.

## Verification
- Test contract intact: `src="app.js"` (double-quoted), `href="styles.css"`,
  all 9 NDJSON kinds present in both `app.js` and `styles.css`.
- `node --check web/app.js` — no JS syntax errors.
- `go vet ./...` clean; `go test ./...` — all 10 packages pass.
- Rebuilt binary (9,623,682 bytes), relaunched Electron app; served content
  carries the new markers (`evFilters`, `drawer`, `palette`, `routeHealth`,
  `openDrawer`, `sparkline`, `probeRoutes`, `openPalette`, `applyFilter`,
  `chipf`, `drawer-panel`, `palette-box`).
- Live screenshot confirms new UI renders: sidebar `routes 1/4 up` LED line,
  Run count in nav, RECENT RUNS populated with a real run.
- Drawer data path verified against live API shapes (`GET /api/v1/runs/{id}`
  and `/events`).

## Security
No secrets added. All new rendering goes through the existing `sanitize()` +
`esc()` path; no API keys or credential material in any asset.
