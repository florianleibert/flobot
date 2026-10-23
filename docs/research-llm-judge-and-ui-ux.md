# Research digest: LLM-as-a-judge + UI/UX design

Two research tracks, distilled for the flobot / Code Studio work. Papers verified via arXiv/ACM fetches where noted.

## Track 1 — Judging with an LLM

### Foundations

**Judging LLM-as-a-Judge with MT-Bench and Chatbot Arena** (Zheng et al., NeurIPS 2023)
https://arxiv.org/abs/2306.05685
The seminal paper. Introduces pairwise comparison and single-answer grading as judge protocols, and names the canonical biases:
- **Position bias** — judges prefer the first answer in pairwise mode; swap orders and average.
- **Verbosity bias** — longer answers win even when not better.
- **Self-enhancement bias** — judges favor their own model family's outputs.
Key mitigation: vary position, control for length, use a stronger judge than the judged.

**G-Eval: NLG Evaluation using GPT-4 with Better Human Alignment** (Liu et al., EMNLP 2023)
https://arxiv.org/abs/2303.16634
Human-formalized criteria: instead of "rate this 1-10", build an explicit evaluation *form* (criteria + step-by-step scoring chain) and use token probabilities of the score tokens as expected value — better calibrated than raw sampled scores. Core lesson: **the rubric is the interface to the judge**; a well-structured form beats a vague prompt.

### Surveys and reliability

**A Survey on LLM-as-a-Judge** (Gu et al., 2024, v6 2025)
https://arxiv.org/abs/2411.15594
Comprehensive survey organized around "how can reliable judge systems be built": consistency, bias mitigation, scenario adaptation, plus a benchmark for evaluating judges themselves. The taxonomy to cite when designing a judge loop.

**Can We Trust LLM Judges: Capability-Dependent Biases and Multi-Judge Ensemble** (Microsoft)
https://www.microsoft.com/en-us/research/publication/can-we-trust-llm-judges-a-study-of-capability-dependent-biases-and-multi-judge-ensemble-for-bias-calibration/
Judge bias depends on judge-vs-candidate capability gap; a **multi-judge ensemble** (diverse models voting) calibrates bias away better than any single judge.

**Rating Roulette: Self-Inconsistency in LLM-As-A-Judge Frameworks** (Findings of EMNLP 2025)
https://aclanthology.org/2025.findings-emnlp.1361.pdf
Same judge, same input, different runs → different scores. Quantifies self-inconsistency; motivates multiple samples + aggregation (median/majority) for stable verdicts.

**Auto-Prompt Ensemble for LLM Judge** (2025)
https://arxiv.org/abs/2510.06538
Ensembles over *prompts* rather than models — cheaper bias calibration than multi-model ensembles.

### Rubrics

**LLM-R: Multidimensional, Calibrated Approach to Automated Evaluation** (ACL 2024)
https://aclanthology.org/2024.acl-long.745v1.pdf
Multidimensional rubrics with calibration against human judgments.

**Autorubric: A Unifying Framework for Rubric-Based LLM Evaluation** (2026)
https://arxiv.org/abs/2603.00077
Unifying framework for rubric-based evaluation; rubrics as first-class, learnable objects.

**Judging LLM-as-a-Judge: Concerning Rubric Artifacts** (OpenReview)
https://openreview.net/attachment?id=jBcsGPKNeV&name=pdf
The counterpoint: rubrics themselves inject artifacts — judges anchor on rubric wording. Rubric design is a measurement-design problem, not just prompt engineering.

### Practical checklist for a judge loop (synthesized)
1. Pairwise > absolute scoring where possible; swap positions and average.
2. Explicit structured rubric (G-Eval style form), but test for rubric artifacts.
3. Multiple samples per judgment; report median + spread, not a single score.
4. Diverse judge ensemble (or prompt ensemble) for bias calibration.
5. Calibrate against a human-labeled golden set before trusting the judge.
6. Judge must be meaningfully stronger than the judged system.

## Track 2 — Designing amazing UIs / novel UX

### Foundational frameworks

**Guidelines for Human-AI Interaction** (Amershi et al., CHI 2019) — the canonical 18 guidelines
https://dl.acm.org/doi/10.1145/3290605.3300233
18 guidelines across 4 phases (initial encounter, ongoing interaction, error recovery, sustained engagement), validated across 20 Microsoft products. E.g. "make clear how well the system can do what it can do", "remember recent interactions", "update and adapt cautiously", "make clear why the system did what it did". Still the best checklist for AI product UX.

**Human-Centered AI (Shneiderman 2020) + UX 3.0 (Xu 2024) + HCAI-MF (Xu et al. 2025)**
HCAI: AI as capability augmenter, not replacement; reliability/safety/trust with human agency. UX 3.0 extends to AI-mediated, ecosystem experiences. HCAI-MF operationalizes into requirements + workflow + governance.

**A Framework of UX Principles for Human-AI Agent Interaction in the Workplace** (Paimann et al., 2026)
https://arxiv.org/abs/2607.19941
Empirically derived (workshop + expert review + meta-analysis + survey + interviews) **eight UX principles** for human-agent interaction in enterprise. The most directly applicable framework for agent-control UIs like flobot: controllability, explainability, trust calibration, oversight.

### Generative / novel UI

**Generative UI: LLMs are Effective UI Generators** (Leviathan et al., Google, 2026)
https://arxiv.org/abs/2604.09577
Model generates the *interface*, not just content. With right prompting + tools, LLMs robustly produce custom UIs; humans overwhelmingly prefer them over markdown walls of text; comparable to expert-crafted in ~50% of cases. Releases PAGEN dataset of expert-crafted UIs. Directly relevant to flobot's run-diff / mission UIs.

**Generative and Malleable User Interfaces with Generative and Evolving Task-Driven Data** (CHI 2025)
https://arxiv.org/abs/2503.04084
UIs that users (or models) can reshape at runtime — end-user programming meets generative AI. The "malleable software" thread: interfaces as living artifacts, not fixed designs.

**Survey of UI Design and Interaction Techniques in Generative AI Applications** (Luera et al., 2024; extended in Foundations and Trends 2025)
https://arxiv.org/abs/2410.22370
Systematic survey of GenAI interaction techniques — the map of the design space.

**Architecting Novel Interactions with Generative AI Models** (UIST 2023)
https://dl.acm.org/doi/10.1145/3586182.3617431
How to structure apps around generative models' capabilities.

### Agentic UX patterns (practitioner literature)

**Agentic UX Patterns** (Dustin Kirk) — https://agenticuxpatterns.com/
Trust/transparency patterns for agent UIs: show the agent's plan, expose tool calls, make autonomy levels explicit, always offer interrupt/undo.

**Designing for Agentic AI: Practical UX Patterns for Control, Consent** (Smashing Magazine, 2026)
https://www.smashingmagazine.com/2026/02/designing-agentic-ai-practical-ux-patterns/

### How this maps to flobot Code Studio (synthesis)
- **Diff review pane** = Amershi "support efficient dismissal" + agentic-UX undo/revert — already built (apply/reject/revert).
- **Runs tab with review state** = trust calibration: make agent outcomes inspectable and dated.
- **Collapsible tool blocks** = "make clear why the system did what it did" — visibility without noise.
- **Next uplevels the literature points to:**
  1. Confidence/uncertainty surfacing on runs (Amershi G2, judge-calibration literature).
  2. Plan visibility before/during runs (agentic-UX plan pattern; Paimann controllability).
  3. Generative UI for run results — render mission outcomes as purpose-built cards instead of text (Leviathan et al.).
  4. Judge loop for run quality: pairwise compare run vs. intent with a rubric, multi-sample, calibrated — G-Eval + MT-Bench mitigations.
