# Learnings — flobot Code Studio campaign

Recorded during the "make it insane" push (research → implementation). Add new entries at the top.

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
