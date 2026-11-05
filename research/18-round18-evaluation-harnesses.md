# Research round 18 — Evaluation: how to prove a bot got better

**Date:** 2026-11-04
**Track:** D — local serving, routing, evaluation
**Sources (fetched 2026-11-04):**
- https://inspect.aisi.org.uk/ (UK AI Security Institute + Meridian Labs, MIT)
- https://inspect.aisi.org.uk/agents.html, /tools.html, /scoring.html (doc index)
- Grok Bot's post-test review checklist (round 8) as the product-side rubric
- flobot's `docs/audit/verify-codex-parity.mjs`, `verify-context-surfaces.mjs`

---

## 1. Inspect's decomposition is the right one, and it is only three nouns

> "An Inspect evaluation is a **Task** that brings together three things: **Dataset** that provides labelled samples… **Solver** that produces an answer for each sample… **Scorer** that evaluates the output."

Everything else is a modifier on those three. For flobot this maps cleanly:

| Inspect | flobot analogue |
|---|---|
| Dataset | a set of saved run inputs (from `runs`/`messages`) or a fixture file |
| Solver | the bot + route + skills under test |
| Scorer | deterministic checks, or a model-graded rubric |

The value of adopting this vocabulary is that it forces the question "what is the dataset?" — which is where most in-house eval efforts die. flobot has an unusual advantage: **every run it has ever done is a labelled sample**, because the operator either accepted the result or corrected it. Accept/correct is a free preference label.

## 2. The pieces worth taking from Inspect

**Composable solvers.** `generate()` is one call; `react(prompt=..., tools=[bash(), todo_write()], attempts=3)` is a full agent loop. `attempts` is a first-class solver parameter — retry budget belongs to the eval, not the harness.

**Sandbox as a task field.** `sandbox="docker"` with a `Dockerfile` or `compose.yaml` alongside the task. The isolation level is declared per task, not globally. flobot's analogue: `sandbox: "worktree" | "none"`.

**Multi-agent primitives + agent bridges.** "the ability to run arbitrary external agents like Claude Code, Codex CLI, and Gemini CLI." An eval harness that can pit your harness against another harness on the same task is how you find out whether your scaffolding is actually better. flobot can do this directly: its executor already spawns an external CLI, so "run this task under flocode vs. under codex" is a config change.

**Tool approval as a testable surface.** Inspect has "Tool Approval" and "Tool Result Review" as documented tool features with an `Approvers` extension point. That means approval behaviour is *evaluated*, not just implemented: you can assert "this action asked for approval" and "this action did not."

That is the missing half of round 9's approval design. An approval system without approval tests will drift.

**Scanners over transcripts.** "Analysis explains how to read eval logs, extract data frames, and **scan transcripts for issues**." Post-hoc scanning of every transcript for a pattern (secret leakage, prompt-injection compliance, tool misuse) catches what per-run assertions miss.

**Caching and limits.** Model-response caching (avoid recompute), `--limit`, early stopping, parallelism, error handling. Standard but necessary.

**Two documentation artifacts for LLM assistance:** `llms.txt` (~2k token index) and `llms-guide.txt` (~185k full). Same progressive-disclosure pattern as skills (round 11). Note that this is now the third product shipping a two-tier doc index — it is a convention.

## 3. The scoring taxonomy flobot needs

Combining Inspect's scorers with Grok Bot's post-test checklist (round 8) gives a concrete rubric set:

| Scorer | Kind | Asserts |
|---|---|---|
| `includes` / `exact` | deterministic | the answer contains the required token |
| `model_graded_qa` | model | free-form answer matches target semantics |
| `format_conforms` | deterministic | output matches the declared return shape (Grok Bot skill contract item 5) |
| `sources_cited` | deterministic | every claim has a citation (Grok Bot checklist) |
| `stopped_at_approval` | deterministic | the run halted at the declared approval point (checklist) |
| `failure_explicit` | deterministic | on missing input, the run reported failure rather than proceeding (checklist) |
| `no_stale_data` | deterministic | inputs are newer than the declared staleness bound (checklist) |
| `idempotent_retry` | deterministic | re-running produces no duplicate side effect (checklist) |
| `no_secret_leak` | scanner | transcript contains no secret value |
| `no_injection_compliance` | scanner | transcript shows no action taken on tool-result instructions |

Six of those ten come straight from Grok Bot's own published checklist. They published their rubric; using it as an eval suite is free.

## 4. What flobot already has, and what it is missing

flobot ships two audit scripts (`docs/audit/verify-codex-parity.mjs`, `verify-context-surfaces.mjs`). Those are *conformance* checks — do the surfaces exist and match a spec. They are not behavioural evals.

The gap is therefore precise: flobot can prove its UI matches a spec, and cannot prove a bot did a task correctly. That is the difference between a code-quality gate and a product-quality gate.

## 5. The minimum viable eval loop for flobot

Ordered by cost/benefit:

1. **Record every run's routing decision + outcome + operator verdict** (accept/correct/abandon). Free labels.
2. **Replay**: re-run N historical inputs against the current bot config; diff outputs. Catches regressions from prompt/skill/model changes.
3. **Deterministic scorers** for the six checklist items above.
4. **Model-graded rubric** for the free-form remainder, using a *different* model than the one under test.
5. **Transcript scanners** for secret leakage and injection compliance.
6. **Cross-harness comparison**: same task under flocode and one external CLI.
7. **Calibration feedback**: feed accept/correct labels back into the router threshold (round 17).

Step 2 is the one that pays immediately and needs no new concepts: flobot already stores runs.

## 6. Gap vs flobot today

| Capability | flobot today | Gap |
|---|---|---|
| Task/dataset/solver/scorer vocabulary | none | missing |
| Replay of historical runs | none | missing |
| Deterministic scorers | none | missing |
| Model-graded rubric | none | missing |
| Transcript scanners | none | missing |
| Approval assertions | none | missing |
| Cross-harness comparison | none | missing |
| Conformance checks | two `.mjs` scripts | exists but not behavioural |
| Operator verdict capture | none | missing (labels are being thrown away) |
