# Research round 17 — Routing: how to pick a model per task without guessing

**Date:** 2026-11-04
**Track:** D — local serving, routing, evaluation
**Sources (fetched 2026-11-04):**
- https://github.com/lm-sys/RouteLLM (README, Apache-2.0; paper arXiv:2406.18665)
- https://ampcode.com/manual — Model Routing, Decision Models
- flobot's `internal/amesh/` mesh model discovery

---

## 1. RouteLLM's core abstraction is one number

Every router implements exactly one method:

> "`calculate_strong_win_rate`, which takes in the user prompt and returns the win rate for the strong model conditioned on that given prompt — if this win rate is greater than user-specified cost threshold, then the request is routed to the strong model. Otherwise, it is routed to the weak model."

So the whole routing problem reduces to: **a scalar quality estimate per request, compared against a user-set threshold.** Not a classifier with N classes, not a learned policy over a model set — a single calibrated probability and a knob.

That is the right shape for flobot because it is *explainable*: every routing decision can be shown as "estimated win-rate 0.34 vs threshold 0.20 → weak route."

## 2. The five routers ordered by cost

| Router | Mechanism | Cost | Notes |
|---|---|---|---|
| `mf` | matrix factorisation on preference data | cheapest | **recommended**; "very strong and lightweight" |
| `sw_ranking` | weighted Elo, votes weighted by similarity to the prompt | cheap | needs embeddings |
| `bert` | BERT classifier on preference data | medium | needs embeddings |
| `causal_llm` | LLM classifier tuned on preference data | expensive | — |
| `random` | baseline | free | for ablation only |

Reported results: "reduce costs by up to 85% while maintaining 95% GPT-4 performance on widely-used benchmarks like MT Bench"; "same performance as commercial offerings while being >40% cheaper."

**The generalisation claim that matters most:**

> "While these routers have been trained on the gpt-4-1106-preview and mixtral-8x7b-instruct-v0.1 model pair, we have found that these routers generalize well to other strong and weak model pairs as well. Therefore, you can replace the model pair used for routing without having to retrain these models!"

That is the licence flobot needs: a router trained on cloud pairs transfers to local pairs. flobot does not need to train anything to ship routing.

## 3. Threshold calibration is the product surface

> "The threshold used for routing controls the cost-quality tradeoff… we recommend calibrating thresholds using a sample of your incoming queries, as well as the % of queries you'd like to route to the stronger model."

```
python -m routellm.calibrate_threshold --routers mf --strong-model-pct 0.5
For 50.0% strong model calls for mf, threshold = 0.11593
```

And the honest caveat: "because we calibrate the thresholds based on an existing dataset, the % of calls routed to each model will differ based on the actual queries received. Therefore, we recommend calibrating on a dataset that closely resembles the types of queries you receive."

**The product translation:** the user does not set a threshold. The user sets *"send about 20% of my work to the big model"* and flobot calibrates the threshold from that user's own run history. That is a comprehensible control with a defensible implementation.

## 4. The `router-<name>-<threshold>` model-string convention

Routing config rides in the model identifier: `model = "router-mf-0.11593"`. One string carries both router choice and threshold, so any OpenAI-compatible client can route without new API surface.

flobot's route pins should adopt this: a route id that encodes strategy and knob, resolvable by the executor without a separate lookup table.

## 5. Amp's two-surface split — routing vs decision models

Amp separates:

- **Model Routing** — a config surface mapping task types to models.
- **Decision Models** — models whose *job* is to decide (which model, which tool), distinct from models that produce output.

The second is the important distinction. A decision model is:
- small and cheap (it runs on every request)
- judged by decision quality, not output quality
- replaceable independently of the working models

flobot already has a natural home for this: the local mesh has small models that are fast and cheap. A 3–4B local model classifying "is this task mechanical or does it need judgement" is a decision model.

## 6. The composite router flobot should build

Combining RouteLLM's scalar + Amp's decision-model split + round 16's capacity facts:

```
route(request) =
  1. hard constraints (filter)
     - bot pin (operator-owned, not overridable by agent)
     - routine pin (round 12: agent cannot re-point)
     - capacity: model fits RAM/wired limit on target node
     - capability: needs tools? needs vision? needs long context?
  2. decision model (cheap local)
     - estimate win-rate scalar s ∈ [0,1]
  3. threshold (calibrated from this user's history at a chosen strong-model %)
     - s > t → strong route, else weak route
  4. fallback chain
     - if strong route unavailable (node down, model evicted) → next in chain
  5. record the decision
     - (s, t, chosen route, reason) written to the run record
```

Step 5 is what makes it debuggable and what makes step 3 calibratable: without recorded decisions there is no data to calibrate on.

## 7. What must never be routed by a model

From rounds 9 and 12: **approval decisions and policy decisions must not be model-routed.** Hermes makes this explicit ("inference pins are user-owned"; the agent cannot point a job at a different model). RouteLLM-style routing applies to *work*, never to *policy*.

## 8. Gap vs flobot today

| Capability | flobot today | Gap |
|---|---|---|
| Route pin per bot | yes (`Bot.Route`) | — |
| Route pin per routine | yes | — |
| Win-rate estimate | none | missing |
| Threshold calibration from history | none | missing |
| Capacity-aware filtering | none | missing |
| Decision model | none | missing |
| Fallback chain | none observed | missing |
| Recorded routing decision | none | missing |
| Route id encoding strategy+knob | plain string | missing |
