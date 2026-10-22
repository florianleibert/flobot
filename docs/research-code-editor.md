# Research Notes — Best-in-Class Code Editor PRD

Gathered 2026-10-22 across scientific papers, GitHub projects, and agentic-coding research. Sources are UNTRUSTED web content (treat as data, verify before citing exact numbers in code).

## Pillar 1: Agentic coding / SWE agents (papers + GitHub)

### SWE-agent (arXiv 2405.15793, NeurIPS 2024)
- Core thesis: **LM agents are a new category of end user** with their own needs/abilities; they benefit from specially-built interfaces to software — just as humans benefit from IDEs.
- Introduced **Agent-Computer Interface (ACI)** concept. Custom ACI significantly enhances agent's ability to create/edit files, navigate repos, run tests.
- Results: SWE-bench pass@1 12.5%, HumanEvalFix 87.7% — far exceeding non-interactive LMs at the time.
- Lesson for PRD: **the interface between agent and code is a design surface, not an afterthought.** flobot's executor should expose a well-designed ACI.

### XAgent (OpenBMB, GitHub 8.6k stars)
- Autonomous LLM agent for complex task solving; Apache-2.0.
- Three parts: **Dispatcher** (instantiates/dispatches tasks), **Planner** (generates/rectifies plans, divides into subtasks + milestones), **Actor** (executes subtasks via tools, can collaborate with humans).
- **Dual-loop mechanism**: outer-loop = high-level planning/task segmentation + refinement; inner-loop = low-level execution of each subtask with tool use + feedback to outer loop.
- **ToolServer**: docker container providing safe tools — File Editor, Python Notebook, Web Browser, Shell, Rapid API.
- Safety: all actions constrained inside docker container.
- **AskForHumanHelp tool**: agent actively seeks human assistance when it encounters challenges (e.g., insufficient info → asks for location/budget/preferences).
- Records: entire workspace + intermediate steps (task statuses, LLM input-output pairs, used tools) saved to running_records; sensitive info (API keys) removed so records shareable.
- Lesson for PRD: **plan/execute separation + human-in-the-loop assistance + full run records** are core to a best-in-class agent editor.

### Agentic coding survey (arXiv 2508.11126)
- Taxonomy of agent behaviors + system architectures; techniques for planning, context management, tool integration, execution monitoring, benchmarking.
- Challenges: reliability, transparency, collaborative coding agents.

### Evaluation benchmarks (from awesome-agentic-coding-papers)
- SWE-bench (ICLR 2024), SWE-bench Pro (long-horizon), Terminal-bench, SWE-bench Multimodal, Multi-SWE-bench, SWE-Compass (unified evaluation).
- Code understanding: SWE-QA; Code localization: LocAgent, SweRank.
- Code edit: EDIT-Bench.

## Pillar 2: Copilot / inline suggestions engineering (GitHub engineering blogs)

### Unified 3-in-1 inline suggestions model (VS Code blog Sep 2026)
- Previously 3 separate models: **Completions** (ghost text), **NES** (next edit suggestions near cursor), **Long-distance NES** (edits farther away). Client orchestrated which to trigger.
- Unification: single model that can do all three; **diff-patch output format** expresses completions + window rewrite + cursor jump as a chain of edits in one response.
- Multi-patch responses → **caching subsequent patches** for fast tab-tab-tab experience.
- Input context: recently viewed snippets (no line numbers), current file with line numbers, recent edit history in unified diff format. Cursor location kept at END of prompt to maximize KV-cache prefix reuse.
- Evaluation stages: offline benchmarks → dogfooding → human eval at scale → controlled online A/B. Five benchmarks: STests, HumanEval (augmented), Output View Kind, Pseudo-Online Evaluation (POE), Qualitative Must-Haves (QMH canary).
- Client-side optimizations (5 joint): speculative decoding (completion at current line), ghost-text progressive reveal, diff-based edit rendering (parse patch → choose view kind), cache delay + debounce, cross-mode ignored-suggestion suppression.
- Key result: 10.1% drop in dismissals, 13% less ghost text, no key metric regressions. Ablation of ghost-text-resurfacing-as-NES swung dismissal rate by 26% (+15.9% → -10.1%).
- Lesson: **the end-to-end experience is the product** — model + client orchestration + rendering + caching must be designed together.

### GitHub Copilot inline suggestions responsible-use / model
- Ghost text at cursor; next edit suggestions; long-distance edits.

## Pillar 3: UX / HCI research on AI programming assistants

### Longitudinal study (arXiv 2605.23135) — "supervisory engineering work"
- 158→101→95 matched professional engineers, two questionnaires 6 months apart.
- 82% report spending less time writing code; shift from **creation to verification**.
- New work category: **supervisory engineering work** = direction, evaluation, correction of AI output.
- **Productivity-experience paradox**: productivity perceptions stable/positive (84% improved at both timepoints), yet proportion reporting worsened developer experience nearly doubled 14%→27%; flow state and cognitive load eroded while feedback loops improved.
- DevEx framework (Noda et al. 2023): **feedback loops, cognitive load, flow state**.
- Concerns ranked: quality, readability, maintainability, security, dependency, learning curve.
- Lesson for PRD: **design for the supervisory engineer** — the editor's job is to make direction/evaluation/correction of AI output as low-friction as possible; protect flow and cognitive load.

### Grounded Copilot (arXiv 2206.15000, OOPSLA'23)
- Grounded theory of how programmers interact with Copilot (20 participants, 4 languages).
- Main finding: interactions are **bimodal**:
  - **Acceleration mode**: programmer knows what to do next, uses assistant to get there faster.
  - **Exploration mode**: programmer unsure how to proceed, uses assistant to explore options.
- Lesson for PRD: the editor must support both modes — fast accept when you know the target; rich exploration when you don't.

### IntelliCode design exploration (Vaithilingam et al., ICSE'23)
- Underuse of AI code-recommendation tools; **discoverability is a major cause**.
- Design principles + new inline interfaces to increase usage.

### Expectation vs Experience (Vaithilingam et al., CHI'22)
- Copilot preferred by most for daily tasks but difficulties in understanding/editing/debugging generated code hindered effectiveness.

### Reading Between the Lines (Mozannar et al., CHI'24)
- Taxonomy of programmer activities with AI recommendation systems; models user behavior and costs.

### DevEx (Noda et al. 2023)
- Feedback loops (speed of info), cognitive load (mental effort), flow state (immersion/enjoyment).

## Pillar 4: flobot + flocode context (local ground truth)

### flobot
- Local-first named teammates app; loopback-only 127.0.0.1:4120; Go server embedded SPA; Electron shell supervision.
- Runs one-shot agent runs by spawning flocode binary (`flocode -p --output streaming-json`).
- State persists to `~/.flobot/state.json` (0600, atomic rename); capped 200 runs / 500 messages.
- Routes: roster/bots/runs/skills/routines/handoff/mesh/direct/local; SSE `/api/stream`.
- Muse key from `~/.accounts/meta.ai` → child env MODEL_API_KEY; never in JSON/NDJSON/HTML.
- Existing PRD: `docs/prd-code-editor.md` (flocode-backed editor).

### flocode
- Go terminal AI coding agent; `flocode serve` = HTTP API server (tasks/stream/cancel/result/backends).
- Task contract: project{repo,branch,worktree_mode,dirty_policy}, goal{kind,title}, permissions{allowed_paths}, return_contract{patch,test_report,...}.
- Result envelope: patch (unified-diff), tests (TestReport), provenance, confidence, requires_human_review.
- LSP manager + codequery are Go-internal APIs only (not exposed over HTTP yet).
- Gateway (`flocode gateway`): resumable engine sessions over authenticated local transport with SSE, idempotent turns, durable steer, permission/question responses.

### gh CLI
- Installed at /opt/homebrew/bin/gh v2.86.0; authenticated as florianleibert; scopes gist/read:org/repo/workflow.
- flobot repo lives at github.com/florianleibert/flobot.

## Synthesis → PRD design principles
1. **Agent-computer interface as first-class design surface** (SWE-agent).
2. **Plan/execute separation + human-in-the-loop assistance** (XAgent dual-loop).
3. **Supervisory engineering is the user's job** — optimize direction/evaluation/correction (longitudinal study).
4. **Bimodal interaction**: acceleration + exploration (Grounded Copilot).
5. **End-to-end experience is the product** — model + client orchestration + rendering + caching together (Copilot).
6. **Discoverability** drives adoption (IntelliCode).
7. **DevEx**: protect feedback loops, cognitive load, flow state.
8. **Full run records** with secrets scrubbed (XAgent running_records).
9. **Diff-patch as universal edit representation** (Copilot) — matches flocode's Result.patch.
10. **Safety boundary**: agent actions constrained; human approves writes (XAgent ToolServer + flocode permissions).

## Cycle 2 additions (2026-10-22, "more concrete" pass)

### Aider unified-diff engineering (aider.chat/docs/unified-diffs.html, benchmarks.html)
- Unified diff edit format raised GPT-4 Turbo "laziness benchmark" 20%→61%; 3X reduction in lazy "…add logic here…" comments.
- Four principles for edit formats: **FAMILIAR** (use formats the model saw in training, e.g. git diff), **SIMPLE** (no JSON escaping, no line numbers), **HIGH-LEVEL** (whole-function hunks, not surgical line edits — 30–50% fewer editing errors), **FLEXIBLE** (tolerate imperfect diffs when applying).
- Aider drops line numbers from hunk headers and treats each hunk as search/replace; flexible patching (hunk normalization, sub-hunk splitting, context-window variation) — disabling it causes a **9X increase in editing errors**.
- Function-calling/JSON edit formats performed *worse* than plain text across benchmarks.
- Lesson for PRD: flocode's `PatchInfo{format:"unified-diff"}` is the right representation; flobot's diff *apply* path must be flexible (fuzzy hunk matching), and diff review UI must render per-hunk.

### Cursor best practices (cursor.com/blog/agent-best-practices, Jan 2026)
- Agent harness = instructions + tools + model; tuned per model.
- **Plan Mode**: agent researches, asks clarifying questions, produces an editable plan (markdown), waits for approval. Plans saved to `.cursor/plans/`.
- Rules (`.cursor/rules/`) = static always-on context; Skills = dynamic on-demand; Commands (`.cursor/commands/`) = reusable workflows.
- **Review during generation**: live diff view while agent works; Stop to cancel and redirect.
- Post-run **Agent Review** pass: line-by-line analysis of proposed edits.
- **Worktree isolation** for parallel agents; Apply merges back.
- Run multiple models on same prompt, compare side by side.
- Lesson for PRD: plan-first toggle, live diff during run, review pass, per-run editable plan artifact.

### Cursor Tab online RL (cursor.com/blog/tab-rl, Sep 2025)
- Knowing **when not to suggest** matters as much as suggestion quality; policy trained to target accept rate (suggest only if P(accept) ≥ 25%).
- Result: 21% fewer suggestions, 28% higher accept rate.
- Lesson for PRD: any proactive UI (diagnostics, suggestions, notifications) needs a suppression policy — silence is a feature.

### Zed Agent Panel (zed.dev/docs/ai/agent-panel)
- **Checkpoints**: every model edit creates a restore point; restore works even after interrupting mid-edit.
- **Review Changes**: multi-buffer tab with all changes; accept/reject per hunk or whole set; optional inline single-file review overriding git diff.
- **Follow the agent**: editor jumps to each file the agent touches.
- **Queued messages + Steer**: messages sent during generation queue by default; "Steer" interrupts at next step boundary.
- Token usage display + automatic compaction; @-mentions for files/symbols/threads; thread history; prevent-idle-sleep while generating; OS notification + sound when done.
- Lesson for PRD: checkpoints per edit, follow-mode, steer semantics, completion notifications — all concrete, shippable features.

### Anthropic: How Claude Code is used in practice (anthropic.com/research/claude-code-expertise, Jun 2026)
- ~400k sessions, ~235k people, Oct 2025–Apr 2026.
- **Division of labor**: users make ~70% of planning decisions, Claude ~80% of execution decisions. "People decide what to build; the agent decides how."
- Typical session: ~4 turns; each prompt triggers ~10 agent actions (up to 100+); ~2,400 words of output per turn.
- **Returns to expertise**: expert sessions trigger 12 actions/3,200 words per prompt vs novice 5 actions/600 words; experts succeed >2X novices; novices abandon troubled sessions several times more often.
- Debugging share fell 33%→19% over 7 months; usage shifted to end-to-end agentic work.
- Lesson for PRD: the UI's primary input surface is the *prompt* (planning); the primary output surface is *review of execution*. Optimize prompt composition (context attachment) and diff review, not manual editing.

## Cycle 3 additions (2026-10-22, repo audit pass — "25 more searches")

### GitHub issues
- `gh issue list --state all` on florianleibert/flobot: **zero issues** (open or
  closed). Clean slate — no issue-driven constraints, no known-bug workarounds
  to design around.

### Architecture facts verified against source (PRD v3 changes)
- **Runs are ephemeral**: `internal/store/store.go:110` "Nothing persists
  across restart"; `state.go:39` "runs and events are never persisted".
  PRD v2's state.json schema violated this contract → v3 moves run summaries
  to a separate `~/.flobot/runs.json`.
- **`spawn()` at server.go:813 is the single run choke point** — used by
  createRun, handoff, testRun, and fireRoutine (scheduler). One snapshot hook
  covers every run source including scheduled routines.
- **`WithWorkDir` option already exists** (server.go:75) and flows to
  `executor.Request.Dir` — Phase 0's workspace root reuses it.
- **Event text capped at 8 KiB** (`clip`, store.go:747; executor.go:325) —
  diffs can never travel through run events; confirms git-snapshot diff
  source.
- **`sameOrigin` DNS-rebinding guard** (server.go:112) wraps all routes —
  new endpoints inherit it free.
- **`assertNoLeaks`** (state.go:244) scrubs persisted state — new persisted
  fields must avoid api_key/secret/llm_ strings.
- **`hub` pub/sub + SSE** (server.go:897) — `diff_ready`/`review_done`
  events publish through the existing hub; no new transport.
- **Stop semantics**: `stopped` map (server.go:53-57) distinguishes user-stop
  from error; stopped runs may have partial edits on disk → new open
  question Q6 (review partial vs auto-restore).
- **`executor.Request.Worktree`** already passes through to flocode —
  per-run worktree isolation is a small extension.
- **`docs/ccpm-runs.md`** run audit log already written by the executor
  (executor.go:459) — run summaries belong beside it, not in state.json.
- **Electron**: `Notification` API available; preload allowlist needs one new
  IPC channel for completion notifications.

## Cycle 4 additions (2026-10-22, top-20 OSS agentic-coding issue mining)

### Top-20 ranking (stars, via GitHub API)
opencode 219.9k · codex 130.4k · gemini-cli 107.5k · zed 92.0k · OpenHands
91.5k · MetaGPT 71.0k · cline 70.7k · open-interpreter 68.6k · goose 55.5k
· gpt-engineer 55.0k · aider 49.7k · continue 36.3k · gpt-pilot 33.6k ·
crush 28.7k · void 28.7k · onlook 27.0k · Roo-Code 24.3k · SWE-agent 20.6k
· qwen-code 28.6k · plandex 15.8k (also: tabby 33.9k, twinny 3.7k, Devon 3.4k).

### Recurring pain points (top issues by engagement, 12 repos sampled)

1. **File-edit/apply reliability is the #1 pain.** cline #4384 (47👍,
   "Fix File Editing Tool Reliability — replace_in_file, write_to_file, and
   Diff Failures"), void #611 (agent "cannot find the search anchor"), void
   #857, continue #7544 ("Error streaming edit diffs: token limit reached").
   → Directly validates Aider's fuzzy-apply findings; Code Studio's
   git-snapshot diff + fuzzy hunk apply sidesteps the whole class.
2. **Undo/checkpoints are demanded, not optional.** codex #9203 (96👍,
   "Please make /undo back"). Zed ships checkpoints per edit. → Code Studio
   checkpoints must be first-class with a one-click restore, not a safety net.
3. **IDE-integrated diff/approval is a top ask.** codex #2998 (67👍,
   "IDE-integrated diff / approval"); ACP integration asks in crush (#990,
   #2091) and qwen-code (#987). → Validates diff review as the core product.
4. **LSP integration.** codex #8745 (72👍, auto-detect + auto-install LSP).
   → Validates Phase 4; also suggests auto-detection over manual config.
5. **Sandboxing.** opencode #2242 (100👍, "Is there a way to sandbox the
   agent?"). → Code Studio's loopback-only + permission model is a
   differentiator; document it as such.
6. **Secrets in plaintext.** continue #1729 (32👍, "storing api keys in plain
   text"). → flobot already never persists keys; assertNoLeaks is a
   marketable guarantee.
7. **Context window / large-file management.** cline #4389 (23👍), continue
   #7544. → Context budgeting belongs in the prompt-composition surface.
8. **Terminal integration reliability.** cline #4356 (66👍, cross-platform).
9. **MCP support** is the most common integration ask (aider #3314, plandex
   #241, void #445, roo #8119 MCP OAuth). → Explicit non-goal for v1; revisit
   after Phase 4.
10. **Model-provider breadth** dominates low-signal issues (add model X).
    → flobot's route abstraction already solves this; not a differentiator.
11. **Plan/Build mode separation** (crush #1734) and **subagents/teams**
    (crush #431, opencode #12711) are active feature areas. → Plan-first
    toggle already in PRD; teams = future.
12. **Instructions/memory file ignored** (gemini-cli #13852, qwen-code #494).
    → Bot memory must be verifiably included (it already is: prompt prepend
    in createRun).

## Cycle 5 additions (2026-10-22, adversarial PRD review)

### flocode task API — verified via read-only subagent (file:line evidence)
- Endpoints confirmed: POST /api/v1/tasks (202 {task_id,status}), GET
  /tasks/{id}, POST /cancel, GET /result, GET /stream (SSE).
- Submit type is `task.Task` (not "SubmitRequest"): task_version
  "2026-04.v1", project{repo,branch,worktree_mode,dirty_policy}, goal,
  permissions, return_contract.
- Patch is `PatchInfo{format,sha256,changed_files,diff,applied}` — diff
  string at `.patch.diff`, omitempty for huge patches.
- **No SSE replay**: no event ids, no Last-Event-ID; only a dropped_events
  counter in status. Reconnect = fresh subscription.
- **No mid-run input**: cancel is the only POST to a running task.
- **Auth is loopback-host, not same-UID**: taskOwnerForRequest requires
  RemoteAddr AND Host header loopback (tasks.go:119-151). Same-UID
  middleware guards the separate mesh listener.
- **No key can travel in submit**: task.Task has no model/key field; serve
  holds its own config.
- **Worktree mode**: edits land in a separate worktree, return as
  PatchInfo; no merge-back exists — flobot's apply path IS the merge-back.
- LSP/codequery over HTTP: none (confirmed Go-API-only).

### flobot — verified directly
- spawn() call sites: server.go:236 (fireRoutine), 427 (handoff), 518
  (testRun), 771 (createRun). PRD's "server.go:813" was wrong (that was
  the spawn definition line, not a call site).
- HasRunningRun gates routines only (server.go:216); manual createRun is
  NOT gated — concurrent runs on one bot are possible today.
- web/embed.go: per-file //go:embed directives (index.html, app.js,
  styles.css) — new JS files need explicit embed lines + script tags.
- desktop/preload.js: no IPC channels at all (UI talks HTTP directly);
  notifications need a new channel or renderer-side Notification API.
- web/app.js: plain script (not module), single script tag in index.html.

### Design flaws found and fixed in PRD v4
1. Dirty-tree attribution: naive `git diff HEAD` attributes user's
   pre-existing edits to the agent → temp-index write-tree pre/post
   (GIT_INDEX_FILE) + diff-tree.
2. Untracked files: temp-index `git add -A` includes them in both trees,
   so agent-created files appear and user's untracked files don't leak.
3. Not-a-git-repo: review unavailable, fail-closed (was unspecified).
4. Workspace API had no create/delete/rename → added POST/DELETE/rename
   with .git/ protection.
5. AC-2a-b claimed "reconnect, no lost tail events" — impossible (no
   replay) → rewritten to poll-status-then-render-from-Result.
6. US-13 (mid-run Q&A) impossible over task API → scoped to executor
   path, flagged as flocode-side ask.
7. "same-UID enforcement" claim wrong → loopback-host enforcement.
8. PatchInfo shape and task.Task naming corrected throughout.
