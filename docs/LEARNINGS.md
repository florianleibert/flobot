# Learnings — flobot Code Studio campaign

Recorded during the "make it insane" push (research → implementation). Add new entries at the top.

## Design system (Obsidian Deck, 2026-10-26)

15. **Glass is the environment, opaque is the data.** The decisive rule of the
    redesign: the aurora/ambient layer lives *behind* the shell, and every
    surface that carries information (cards, log, editor, tree, inputs, rows)
    is fully opaque. The previous build used translucent glass for *content*,
    which the research pass had already flagged ("glass-on-glass for content
    reads as a demo; glass behind + solid cards reads as a product"). Verified
    by computed style: every data surface now resolves to `alpha=1`.
16. **Depth from surface VALUE, not shadows.** Four opaque steps
    (`--surface-1..4`) carry elevation; shadows are reserved for things that
    float (toast, palette, quick-open). The old `--shadow` on every card was
    the single biggest "dated template" tell.
17. **Saturated verdict pills beat translucent tints.** Status chips now use a
    solid semantic fill with near-black ink (`--ok-ink`, `--err-ink`,
    `--warn-ink`, `--acc-ink`). A 10px uppercase pill in a saturated fill is
    legible at a glance; the old 10%-alpha tint was not.
18. **Amber is "attention", magenta was a misread.** `--warn` moved from
    `#e879f9` (magenta = alarm) to `#ffb454` (amber = attention). Magenta was
    doing double duty for thinking phase and warnings; thinking now owns
    `--violet` alone.
19. **One accent, two blues.** `--acc` `#6366f1` = structure (selection, focus,
    active nav); `--acc-2` `#7c9bff` = live content (cursor, active stream).
    The old build sprayed `--grad` across headings, stat hairlines, nav rails,
    sparklines and avatars at once, which flattened the hierarchy. The only
    remaining gradient is `--grad`, used solely by the inline avatar glyphs in
    main.js.
20. **A stylesheet rewrite needs a class contract.** Before touching
    `styles.css`, dump every class/id/data-attribute the HTML and JS use
    (including `classList.add` call sites and template-literal class
    expressions) and diff that set against the new sheet. A rewrite that drops
    one class name breaks a behavior silently — no test fails, the element just
    renders wrong. The old sheet had 211 classes; all 211 survived.
21. **Tokens referenced from JS must survive a token rewrite.** `main.js`
    renders avatars with an inline `style="background:var(--grad)"`. Removing
    `--grad` from `:root` left every avatar transparent with no error anywhere.
    Grep the JS for `var(--` before pruning tokens, not just the CSS for
    `var(--`.
22. **Screenshots can lie about which server you are testing.** The installed
    `/Applications/flobot.app` already owned port 4120, so an early screenshot
    pass silently captured the OLD embedded UI even though the new binary was
    built. Always assert on served content (e.g. `curl .../styles.css | grep
    <marker>`) and run the verification server on a free port.
23. **Computed-style assertions beat pixel review.** Verifying the redesign
    meant asserting: exactly one visible panel, nav/panel panes do not overlap,
    the three Code Studio panes are ordered, every data surface is opaque, the
    focus ring is present, body text clears 4.5:1, KPI numbers are
    `tabular-nums` in a mono face, and only the expected elements carry a
    gradient. Screenshot review catches "looks wrong"; assertions catch "is
    actually correct".

## Judge loop (this cycle)

10. **Advisory, fail-open judging.** The judge (internal/server/judge.go) runs after `capturePostRun` in the spawn goroutine, never blocks run completion, and treats every failure (no assistant text, model error, unparseable reply) as "not judged" — zero values in RunSummary mean unjudged, never score 0. A judge that could fail a run would be worse than no judge.
11. **Deterministic output contract beats JSON mode.** The rubric demands exactly `SCORE: n` / `WHY: ...`; a single regex parses it. No JSON mode dependency, robust to chatty models (regex is line-anchored, WHY clipped to one line).
12. **Persisted fields are additive.** RunSummary gains `judgeScore`/`judgeRationale` with `omitempty` — old runs.json files load unchanged; no migration. The merge point is `runsWithSummaries` (server.go), the single place in-memory Runs reconcile with persisted summaries.
13. **Same-route self-judging is a known bias, accepted.** MT-Bench names self-enhancement bias; the judge reuses the run's own pin/route for zero config. The score is advisory (never gates apply/reject), and the research checklist (pairwise, multi-sample, ensemble) is the documented upgrade path.
14. **Multi-worktree footgun.** Fleet sessions run in a temp checkout; edit tools with relative paths silently wrote there while `go build` ran against the real repo. Symptom: build errors referencing fields that "were just added". Fix: always pass absolute paths to edit tools when the session cwd differs from the target repo.

## Product/research learnings

1. **Research-first roadmap beats feature-roulette.** The 100-round GUI-editor survey (docs/research-gui-editor-features.md) converged on a ranked gap list; the top items (per-hunk accept/reject, session resume, notifications, context/cost meter, collapsible tool blocks, plan review) each mapped to a concrete, shippable change. Rank by evidence of demand, then build in order.
2. **Judge-loop design is measurement design, not prompt engineering.** From the LLM-as-judge digest: pairwise beats absolute scoring (swap positions, average), explicit G-Eval-style rubric forms beat "rate 1–10", multi-sample + median beats single-shot, and rubrics themselves inject artifacts — test for them. Any future run-quality judge in flobot should follow the checklist in docs/research-llm-judge-and-ui-ux.md §"Practical checklist".
3. **Agentic UX canon:** show the plan, expose tool calls, make autonomy levels explicit, always offer interrupt/undo (agenticuxpatterns.com; Amershi CHI 2019). The diff review pane and collapsible tool blocks are direct applications. Next uplevels per the literature: confidence surfacing on runs, plan visibility before/during runs, generative result cards instead of text walls.
4. **Editor core choice:** CodeMirror 6 for custom/embedded web editors (flobot Code Studio pattern), Monaco only when VS Code parity is required.

## Engineering learnings

5. **The terminal-event assumption is fragile.** `stopRun` disables the Stop button and sets "stopping…"; if the terminal `done`/`run_status` event never arrives (dropped stream, replay), the button stays stuck. UI state machines must be reset on *any* run-end path, not just the happy one. (Fixed: onRunEnd now resets the Stop button.)
6. **Diff apply by content-search, not line numbers.** Sequential hunk application drifts if you trust `@@` offsets; locating each hunk by its del+context pattern is robust to earlier hunks shifting lines. Keep that invariant when touching internal/diff.
7. **Review actions must be idempotent.** Once a review is applied/rejected, disable the buttons — a completed review must not be re-executable from a stale tab.
8. **Ship-first, verify alongside.** Push/merge and let the repo's pre-push hooks (gofmt, vet, build, tests) be the gate; run only the narrowest test that proves the change. Never block a turn re-running a suite a hook already runs.
9. **Secrets hygiene is a UI concern too.** The web UI never displays secrets; every dynamic string goes through `esc()`. Any new render path must keep that invariant (see app.js header comment).
