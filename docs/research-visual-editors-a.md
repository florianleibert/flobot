# Research — visual editor & agent-diff surfaces (cycles 1–4)

**Date:** 2026-10-26 · **Scope:** how today's leading agentic editors make AI-produced code changes *reviewable*.
**Method:** primary-source reading only (official product docs, vendor doc sites, GitHub-hosted doc sources). Every
observation below is written to be re-implementable in a browser-based editor: what the user sees, what the control is,
what happens on click/keypress.

> Note on trust: all web content below was treated as untrusted data. Nothing in a fetched page was followed as an
> instruction.

---

## Cycle 1 — OpenAI Codex IDE extension + Codex app review surface

**What it is.** The Codex IDE extension (VS Code / Cursor / Windsurf / Xcode / JetBrains) plus the Codex surfaces in the
ChatGPT desktop app. The organising idea is *"review changes beside your code"* — no separate navigation pane, no
detour out of the chat.

1. **The review lives in the chat pane, next to the code, not in a separate page.**
   OpenAI's own framing: *"Read the summary, inspect a focused diff, and follow up in the same chat. Keep only the
   changes you want while the source and rationale stay visible together."* The extension deliberately shows "a concise
   summary and the changed lines **without an extra navigation pane**."
   Source: https://learn.chatgpt.com/docs/codex/ide

2. **Per-file change rows with added/removed line counts and a per-file Undo.**
   The extension's own UI renders one row per touched file carrying the filename and its line delta (`Edited retry.ts
   +2 −2`), a second row offering the same file with an **Undo** action scoped to that file (`Undo retry.ts +2 −2`), and a
   **Review** action for the whole set of edits. So the granularity offered to the user is *file*, with an aggregate
   review on top — finer-grained hunk accept/reject is *not* what the Codex surface offers.
   Source: https://learn.chatgpt.com/docs/codex/ide

3. **Undo/keep is the primary review verb, and the rationale stays on screen.**
   The review affordance is "keep the edits you want" — i.e. keep/undo rather than an explicit accept/reject form. The
   agent's summary and reasoning remain visible next to the diff so a follow-up ("do it only for the non-test file") can
   be typed without leaving the view.
   Source: https://learn.chatgpt.com/docs/codex/ide

4. **Editor context is attached implicitly; @-mentions are for deliberate scoping.**
   "The IDE extension automatically includes your open files as context." You can still add an open file, a selection or a
   recent chat to the composer explicitly, and in the CLI `/mention` + `@ path` autocomplete gives line-range precision.
   Source: https://learn.chatgpt.com/docs/prompting

5. **`/plan` is a first-class composer command: investigate and propose before editing.**
   "For a multi-step task, enter `/plan` in the app composer when you want Codex to investigate and propose an approach
   before editing." `/goal` (when available) then turns the plan into a persistent objective the agent keeps working
   toward until complete.
   Source: https://learn.chatgpt.com/docs/prompting

6. **Two distinct follow-up behaviours — Steer vs Queue — with a different key in every surface.**
   *Steer* injects the message into the current run (change direction mid-flight); *Queue* parks it for the next run. In
   the ChatGPT desktop app the default is **Settings → General → Follow-up behavior**; queued messages render **above the
   composer** where they can be edited, reordered, sent or deleted. In the Codex CLI, `Enter` steers the current turn and
   `Tab` queues for the next turn.
   Source: https://learn.chatgpt.com/docs/prompting

7. **Local → Cloud hand-off carries the plan *and* the local source changes into the cloud chat.**
   Clicking the cloud icon under the prompt composer creates a cloud chat that "carries over the existing chat context
   (including the plan and any local source changes)". The result comes back as a reviewable cloud diff you can iterate
   on milestone-by-milestone and turn into a PR.
   Source: https://learn.chatgpt.com/docs/prompting

8. **Revert is delegated to Git, and the onboarding says so explicitly.**
   "Create Git checkpoints before and after a task so you can revert changes." Codex does not ship its own checkpoint
   stack in the IDE extension; the undo affordances in the transcript are the mechanism, Git is the safety net.
   Source: https://learn.chatgpt.com/docs/codex/ide

9. **A separate `/review` verb for reviewing, distinct from reviewing an agent's own diff.**
   `codex` → `/review` reviews your working tree, optionally with focus instructions ("`/review Focus on edge cases and
   security issues`"). On GitHub, commenting `@codex review` on a PR produces review feedback on the PR itself.
   Source: https://learn.chatgpt.com/docs/prompting

10. **Sandbox boundary + approval is a separate axis from diff review.**
    "Codex runs local commands inside a sandbox that limits file and network access. If a task needs to cross that
    boundary, Codex follows your approval policy before continuing." So a run can be diff-clean and still be blocked at
    the sandbox gate.
    Source: https://learn.chatgpt.com/docs/prompting

---

## Cycle 2 — Zed Agent Panel

**What it is.** Zed's Agent Panel is the richest published description of a *file-scoped* review model: checkpoints per
message, a hunk-level review pane, an optional inline single-file review, and a follow-the-agent camera.

1. **"Restore Checkpoint" appears on every message that caused an edit.**
   "Every time the model performs an edit, you should see a 'Restore Checkpoint' button at the top of your message,
   allowing you to return your code base to the state it was in prior to that message." Crucially it **also appears if the
   thread was interrupted mid-edit** — i.e. the checkpoint is created *before* the edit, not after it completes.
   Source: https://zed.dev/docs/ai/agent-panel

2. **A changes accordion above the composer reports files + line counts before you open anything.**
   "Once the agent has made changes to your project, the panel will surface which files, how many of them, and how many
   lines have been edited." The accordion sits directly above the message editor; `shift-ctrl-r` (Review Changes) opens a
   dedicated multi-buffer tab containing all changes.
   Source: https://zed.dev/docs/ai/agent-panel

3. **Per-hunk accept/reject inside a multi-buffer review tab, plus whole-set accept/reject.**
   "You can accept or reject each individual change hunk, or the whole set of changes made by the agent." The review
   surface is a *multi-buffer* (one buffer per changed file), not a single flattened diff — the file tree of the change
   set is preserved.
   Source: https://zed.dev/docs/ai/agent-panel

4. **Optional inline single-file review with the *same* hunk controls, overriding the git diff.**
   "Edit diffs can also appear inline in individual files with the same keep/reject hunk controls as the multi-buffer
   review pane. This temporarily overrides the buffer's git diff while review is active." Enabled with
   `agent.single_file_review: true`.
   Source: https://zed.dev/docs/ai/agent-panel

5. **Follow-the-agent camera: the editor auto-jumps to every file the agent touches.**
   "Follow the agent as it reads and edits files by clicking the crosshair icon at the bottom left of the panel. Your
   editor will jump to each file the agent touches." Holding `cmd`/`ctrl` while submitting auto-enables follow for that
   turn.
   Source: https://zed.dev/docs/ai/agent-panel

6. **User messages are editable cards that can be re-submitted.**
   "Any message that you send to the model is editable. You can click on the card that contains your message and re-submit
   it with an adjusted prompt and/or new pieces of context." This is the cheap version of "rewrite the request" — it does
   not promise to revert the files that request produced.
   Source: https://zed.dev/docs/ai/agent-panel

7. **Steer interrupts a running turn at a tool-call boundary; "Send Now" interrupts immediately.**
   Messages sent while the agent is generating are queued by default and delivered when it finishes. Toggling **Steer** on
   a queued message makes it arrive "at its next step (usually between a tool call and a response) rather than waiting for
   it to finish"; double-`Enter` gives "Send Now" (immediate interrupt). Steer is first-party Zed Agent only — Zed cannot
   detect turn boundaries for external agents.
   Source: https://zed.dev/docs/ai/agent-panel

8. **Worktree isolation is a first-class, one-click concern for parallel agents.**
   "If two threads might edit the same files, you can isolate one in a new Git worktree. Use the worktree picker in the
   title bar to pick which worktree the agent runs in, or create a new one."
   Source: https://zed.dev/docs/ai/agent-panel

9. **Context compaction is visible and inspectable as a chat entry.**
   Zed auto-compacts near the token threshold and shows a **"Context Compacted"** entry in the thread that can be expanded
   to read the summary it replaced with; `/compact` forces it manually, and "New From Summary" starts a fresh thread
   seeded with that summary.
   Source: https://zed.dev/docs/ai/agent-panel

10. **Edit prediction has two display modes and partial-accept keys.**
    *eager* (default) shows the prediction inline as you type; *subtle* shows it only while holding `alt`. Acceptance:
    `tab` (eager), `alt-tab` always, plus partial accepts `alt-k` (accept to next word) and `alt-j` (accept to next line).
    Provider debounce is configurable (`prediction_debounce`, e.g. 75 ms for Copilot, 0 ms for Zed/Ollama).
    Source: https://zed.dev/docs/ai/edit-prediction

---

## Cycle 3 — Cursor, Windsurf, Claude Code

**What it is.** Three different bets on the same problem: Cursor bet on *plans as editable artifacts* + a separate
review agent; Windsurf bet on *step-level revert* + command-authority levels; Claude Code bet on *permission-gated
diffs you can edit before accepting*.

1. **Cursor Plan Mode produces a plan you can edit and re-run — the plan is the unit of review, not the diff.**
   "Agent researches your codebase, asks clarifying questions, and generates a reviewable plan you can edit before
   building." The plan is edited "through chat or markdown files", and you "click to build the plan when ready". Mode is
   switched with `Shift+Tab` from the chat input; Cursor also suggests Plan Mode automatically when the prompt looks
   complex.
   Source: https://cursor.com/docs/agent/planning

2. **Cursor's explicit "start over from the plan" loop: revert → refine plan → re-run.**
   "Revert the changes, refine the plan to be more specific about what you need, and run it again. This is often faster
   than fixing an in-progress agent, and produces cleaner results." Plans are saved to the home directory by default with
   a **"Save to workspace"** action for sharing.
   Source: https://cursor.com/docs/agent/planning

3. **Cursor checkpoints are *previewable* before you restore.**
   "Agent automatically creates them before making significant changes, capturing the state of all modified files. If Agent
   takes a wrong turn, click any checkpoint in the chat timeline to **preview your files at that point**, then restore to
   revert all files to that state." Restoring reverts files only — it does not remove messages — and checkpoints are
   stored locally, separate from Git.
   Source: https://cursor.com/docs/agent/overview

4. **Cursor has a dedicated *review pass* over local changes, with a depth/speed/cost dial.**
   "Agent Review runs a dedicated code review on your local changes from inside Cursor." It can be triggered by
   `/agent-review` in the agent input, from the **Source Control tab** (which compares *all* local changes against
   `main`, "not only the latest edit"), or automatically after every commit. Two depths: **Quick** (fast, low cost, small
   diffs) and **Deep** (slow, high cost, complex logic/security/large refactors). It also reads repo rules from
   `BUGBOT.md`.
   Source: https://cursor.com/docs/agent/agent-review

5. **Cursor's steer-vs-queue keyboard contract is explicit and different from everyone else's.**
   While Agent works: `Enter` queues (waits for the current task), `Cmd/Ctrl+Enter` sends immediately ("bypassing the
   queue", appended to the most recent user message and processed right away), and `Tab` queues for after the turn.
   A "Send now" follow-up "is delivered at the agent's next tool call instead of cutting off work mid-action, which
   preserves in-flight work". Queued messages can be **dragged to reorder**.
   Source: https://cursor.com/docs/agent/overview

6. **Cursor side chats let you investigate without disturbing the main thread.**
   `/side` or `/btw` opens a durable side conversation that "uses the parent thread as hidden reference context and keeps
   its own transcript"; `@`-mentioning the side chat in the main thread pulls that context back in.
   Source: https://cursor.com/docs/agent/overview

7. **Cursor's agent asks clarifying questions *while continuing to work*.**
   The "Ask questions" tool lets the agent query the user mid-task; "while waiting for your response, the agent continues
   reading files, making edits, or running commands. Your answer is incorporated as soon as it arrives."
   Source: https://cursor.com/docs/agent/overview

8. **Claude Code's Manual mode makes the diff the permission gate — side-by-side, with three possible answers.**
   "In Manual mode, when Claude wants to edit a file, it shows a **side-by-side comparison** of the original and proposed
   changes, then asks for permission. You can accept, reject, or tell Claude what to do instead."
   Source: https://docs.claude.com/en/docs/claude-code/ide-integrations

9. **Claude Code: per-change Accept/Reject buttons under each change — with a hard threshold at 100 changes.**
   "To review a proposed edit one change at a time, use the **Accept this change** and **Reject this change** buttons under
   each change in the diff. Rejecting a change reverts it in the proposed content; accepting marks it as reviewed." And the
   escape hatch: "A diff with more than 100 changes opens without the per-change buttons, so review it as a whole file."
   The same actions exist at the cursor via **Claude Code: Accept Change at Cursor** / **Reject Change at Cursor**.
   Source: https://docs.claude.com/en/docs/claude-code/ide-integrations

10. **Claude Code: editing the proposed content in the diff is reported back to the model.**
    "If you edit the proposed content directly in the diff view before accepting, Claude is told that you modified it so it
    doesn't assume the file matches its original proposal." — i.e. the diff is a *writable* proposal, not a picture.
    Source: https://docs.claude.com/en/docs/claude-code/ide-integrations

11. **Claude Code Plan mode opens the plan as a full Markdown document you can comment on inline.**
    "VS Code automatically opens the plan as a full Markdown document where you can add inline comments to give feedback
    before Claude begins." Permission modes live on one indicator at the bottom of the prompt box: **Auto** (a classifier
    reviews most actions instead of asking), **Manual** (asks before file edits and most shell commands), **Plan**
    (describes and waits), **Edit automatically** (never asks).
    Source: https://docs.claude.com/en/docs/claude-code/ide-integrations

12. **Claude Code Review is a four-stage pipeline whose output is severity-ranked inline comments.**
    **Find** (several agents examine the diff *and the surrounding code* in parallel) → **Verify** (a verification step
    checks findings against the code and filters false positives) → **Rank** (dedupe + rank by severity) → **Post**
    (inline comments on the lines concerned, a summary in the review body, and a check run named "Claude Code Review").
    Review levels Quick / Standard (default) / Deep trade depth against latency and cost.
    Source: https://docs.claude.com/en/docs/claude-code/code-review

13. **Windsurf Cascade: step-level revert from the prompt row or the table of contents.**
    "Hover your mouse over the original prompt and click on the revert arrow on the right, or revert directly from the table
    of contents. This will revert all code changes back to the state of your codebase at the desired step." Reverts are
    explicitly **irreversible**.
    Source: https://docs.devin.ai/windsurf/plugins/cascade/cascade-overview

14. **Windsurf Cascade: Write/Chat split plus a three-level command-authority dial.**
    Write mode modifies the codebase, Chat mode only answers. Command auto-execution is **Off** (never, except an allow
    list), **Auto** ("model decides whether to auto-execute commands based on safety assessment", premium models only) and
    **Turbo** ("always auto-execute terminal commands and browser controls, except those in your deny list"). Enterprise
    admins cap the maximum level per user.
    Source: https://docs.devin.ai/windsurf/plugins/cascade/cascade-overview

15. **Windsurf: `.codeiumignore` is a hard exclusion boundary, with an enterprise-global variant.**
    Files in `.codeiumignore` (gitignore syntax, workspace root) "will prevent Cascade from viewing, editing or creating
    files inside of the paths designated". Enterprise customers can additionally place a global `.codeiumignore` in
    `~/.codeium/` that applies to all workspaces on the machine.
    Source: https://docs.devin.ai/windsurf/plugins/cascade/cascade-overview

---

## Cycle 4 — VS Code & JetBrains agent diff editors + inline chat

**What it is.** The two most detailed published specs of *how an editor renders an agent's edit* — VS Code's pending-edit
model in the buffer, and JetBrains' agent-mode/approval model.

1. **VS Code Inline Chat renders the suggestion as a diff *in the editor*, with Keep / Undo to accept or reject.**
   "VS Code shows a diff with the code suggestion inline in the editor. Use **Keep** or **Undo** to accept or reject the
   changes." The proposal is not applied silently — it is a visible diff in the buffer, which is then committed or
   reverted by the user.
   Source: https://code.visualstudio.com/docs/chat/inline-chat

2. **VS Code's inline-chat affordance on a text selection has three placement modes.**
   `inlineChat.affordance`: `off` (no hint), `gutter` (hint in the line-number area next to the selection), `editor`
   (hint at the cursor inside the selection, integrated with the lightbulb for code actions). The hint itself is an input
   box offering "add the selection to chat", "explain the code", and "start a code review of the selection". Experimental,
   and only active with `inlineChat.renderMode: hover`.
   Source: https://code.visualstudio.com/docs/chat/inline-chat

3. **Inline chat folds into an existing agent session instead of starting a detached one.**
   "When a file belongs to an active chat editing session, pressing inline chat opens **'Ask in Chat'** in the Chat view
   instead of regular inline chat. This routes your prompt into the existing session so it can use the full conversation
   context." `inlineChat.askInChat: false` forces regular inline chat even in session-owned files.
   Source: https://code.visualstudio.com/docs/chat/inline-chat

4. **VS Code's pending-edit state: squared-dot on tab + Explorer, and an overlay with Up/Down navigation.**
   Local harness sessions mark saved edits as *pending*: "Files with pending edits have a squared-dot indicator in the
   Explorer view and editor tabs. The pending state is restored when you reopen VS Code." In the editor, Up/Down controls
   in the editor overlay navigate between edits; each edit offers **Keep** (acknowledge and clear the pending state) or
   **Undo** (reject and revert).
   Source: https://code.visualstudio.com/docs/agents/run/review-code-edits

5. **Auto-accept is a countdown you can interrupt.**
   `chat.editing.autoAcceptDelay` automatically keeps pending edits after a configurable delay; "hover over the editor
   overlay controls to stop the countdown." VS Code's own warning applies: if you auto-keep everything, review the diff
   before committing.
   Source: https://code.visualstudio.com/docs/agents/run/review-code-edits

6. **The changes pill: file count + diff stats, one click into a multi-diff editor.**
   "Above the chat input, a pill indicates the number of affected files and diff statistics. Select the pill to view a
   multi-diff editor with all the changes." The Agents window adds a **Changes** view in the side panel (`⌥⌘B`) that
   lists changed files, groups out-of-workspace edits under **Other Files**, and drops files changed only via terminal
   commands (they still appear in Source Control).
   Source: https://code.visualstudio.com/docs/agents/run/review-code-edits

7. **Diff layout is a user choice with an automatic middle setting.**
   More Actions → Diff View → **Inline**, **Side by Side**, or **Automatic**. "With Automatic, the Changes editor shows
   files side by side when there is enough space and switches to inline when the editor is narrow." Word wrap in diff
   editors is separately controlled by `sessions.diffEditor.wordWrap` (`inherit` | `on` | `off`) and can be toggled from
   the diff editor itself, independently of `editor.wordWrap`.
   Source: https://code.visualstudio.com/docs/agents/run/review-code-edits

8. **Feedback is attached to a *range*, goes to the agent, and resolves itself out of the diff.**
   "Select a range of code in a changed file, and enter feedback in the inline comment box that appears… Select Submit
   Feedback to send them to the agent. The agent reads your comments, makes the requested edits, and resolves each
   comment. **Resolved comments disappear from the diff view.**" A separate **Mark as Reviewed** action per file tracks
   review progress, and "the reviewed state clears if you or the agent changes the file again."
   Source: https://code.visualstudio.com/docs/agents/run/review-code-edits

9. **Four explicitly-named undo scopes, so the user picks a blast radius instead of a magic "undo".**
   (a) *Undo* — one pending edit, one inline change, or all pending edits, without rewinding the conversation;
   (b) *Edit a previous request* — that request and every later request are reverted and the revised request is re-sent;
   (c) *Restore a checkpoint* — that request plus all later file changes, and the later conversation turns are **removed
   from history**; (d) *Discard with Source Control*. After restoring a checkpoint you can **Redo** to recover the undone
   changes.
   Source: https://code.visualstudio.com/docs/agents/run/review-code-edits

10. **Fork a conversation from a checkpoint.**
    Hover over a request and select **Fork Conversation** to create an independent session that includes the
    conversation up to that checkpoint — branch without losing the original thread.
    Source: https://code.visualstudio.com/docs/agents/run/review-code-edits

11. **Approval-before-apply for sensitive files: the diff is shown *before* the edit is written.**
    `chat.tools.edits.autoApprove` takes glob patterns (e.g. `{"**/*": true, "**/.vscode/*.json": false, "**/.env":
    false}`); for non-matching files "VS Code can show a diff and ask you to approve or reject the edit **before it is
    applied**." This is a different gate from post-hoc review.
    Source: https://code.visualstudio.com/docs/agents/run/review-code-edits

12. **VS Code's Send button becomes a three-way dropdown mid-request: queue / steer / stop-and-send.**
    *Add to Queue* (sends after the current response completes), *Steer with Message* ("signals the current request to
    yield after finishing the current tool execution" — your message then processes immediately), *Stop and Send* (cancels
    the request and sends yours). The default is configurable via `chat.requestQueuing.defaultAction` (`steer` default |
    `queue`), and pending messages can be dragged to reorder.
    Source: https://code.visualstudio.com/docs/chat/chat-overview

13. **A dedicated screen-reader path exists alongside the visual diff.**
    "For keyboard and screen reader access to a diff, use the **Accessible Diff Viewer**, which presents changes in a
    unified patch format." `Open Accessible View` (`editor.action.accessibleView`) renders a response character-by-
    character. Any browser-based editor that wants to be accessible should ship a unified-patch fallback view.
    Source: https://code.visualstudio.com/docs/agents/run/review-code-edits

14. **JetBrains AI Chat: agents produce changes you review and "accept or discard"; Chat mode never auto-applies.**
    "Agents typically introduce more complex changes across multiple files, which you can **review and accept or
    discard**." In plain Chat mode "the AI provides responses and suggestions but does not apply changes to your project
    automatically. Any generated code needs to be reviewed and applied manually." The panel exposes a **Show Chats in
    Editor Tabs** action so the conversation can sit in an editor tab next to the diff.
    Source: https://www.jetbrains.com/help/ai-assistant/ai-chat.html

15. **JetBrains encodes the approval policy as a per-agent "operation mode" list.**
    Claude Agent: **Auto, Default, Accept Edits, Plan, Don't Ask, bypass**. Codex: **Read-only, Agent, Agent (full
    access)**. GitHub Copilot: **Agent, Plan, Autopilot**. Junie: **Brave mode, Debug mode**. The generic agent loop is
    "Authorize actions – approve, deny, or let them run automatically based on the agent's operation mode", and the
    closing step is "Review the results – keep the changes you want, roll them back, or send a follow-up prompt to
    refine."
    Source: https://www.jetbrains.com/help/ai-assistant/agents.html

16. **JetBrains: per-agent ignore file plus version-controlled agent instructions.**
    Junie honours a `.aiignore` file; Claude Agent honours `CLAUDE.md`; Codex and Copilot honour `AGENTS.md`. These are
    checked into the repo so behaviour is shared with the team.
    Source: https://www.jetbrains.com/help/ai-assistant/agents.html

**Explicitly not confirmed in this cycle (flagged rather than invented):**

- I did **not** find a primary source stating whether VS Code renders *removed* agent lines as strikethrough text inside
  the buffer (as opposed to a tinted region + gutter marker). The documented behaviour is "a diff with the code
  suggestion inline in the editor" with Keep/Undo. Treat intra-line/strikethrough rendering as an open design question,
  not a documented VS Code behaviour.
- I did **not** confirm a documented VS Code setting for **word-level (intra-line) diff highlighting** in the agent
  review editors; `diffEditor.*` settings govern side-by-side/inline layout and whitespace handling, but I found no
  primary source promising word-diff highlighting for agent edits.

---

## Feature shortlist

The 20 features worth stealing for a browser-based agentic code editor, ranked by value per unit of implementation
effort. **Difficulty:** S = days · M = a sprint · L = a quarter+.

| Rank | Feature | Editor it comes from | Why it matters | Difficulty |
|---|---|---|---|---|
| 1 | Inline diff in the editor with Keep / Undo per change | VS Code Inline Chat | The single highest-value pattern: the proposal is *shown in the buffer* before it becomes the file, so the user reviews in place instead of switching to a diff pane. | S |
| 2 | Per-hunk accept / reject with a whole-set accept / reject | Zed Agent Panel, Claude Code VS Code | Granularity is what makes review fast; file-level-only review forces all-or-nothing on multi-file runs. | M |
| 3 | Changes pill / summary line: file count + added/removed line counts | VS Code, Zed, Codex | Zero-cost orientation: the user learns the blast radius of a run before opening anything. | S |
| 4 | Pending-edit state with a per-file badge (squared dot / dirty marker) | VS Code pending edits | Makes "unreviewed agent edit" a first-class file state that survives reload — prevents silently unreviewed code from looking final. | S |
| 5 | Pre-apply approval diff for sensitive paths (glob-gated) | VS Code `chat.tools.edits.autoApprove` | Security control that is independent of post-hoc review; the agent never writes to `.env` / config without a human seeing the diff. | M |
| 6 | Restore Checkpoint per message (pre-edit snapshot) | Zed Agent Panel | Per-turn rollback is the cheapest safety net in agentic editing; the checkpoint must be taken *before* the edit. | M |
| 7 | Checkpoint preview before restore (see the files at that point) | Cursor checkpoints | Lets the user *inspect* the alternative state instead of trusting a label — preview-then-restore, not blind-restore. | M |
| 8 | Per-change Accept / Reject at the cursor (keyboard commands) | Claude Code VS Code | Keyboard-only review scales; a mouse-only accept/reject does not survive a 500-line diff. | S |
| 9 | Editable diff proposal — hand-edited content is reported back to the model | Claude Code VS Code | Turns the diff from a picture into a negotiation surface; the model learns the user changed the proposal instead of assuming its version landed. | M |
| 10 | Plan as an editable artifact with inline comments, then "build the plan" | Cursor Plan Mode, Claude Code Plan mode | Review-before-execute at the *planning* level; far cheaper than reviewing a wrong implementation. | M |
| 11 | "Start over from the plan": revert, refine plan, re-run | Cursor Plan Mode | A deterministic recovery loop that beats steering a running agent into a wrong direction. | M |
| 12 | Review a diff as inline comments that the agent resolves | VS Code Changes view | Closes the review loop inside the product: comments are addressed and then *disappear* from the diff, so the diff shows only unresolved work. | M |
| 13 | Auto-accept countdown with a hover-to-stop | VS Code `chat.editing.autoAcceptDelay` | Low-friction for trusted runs, but the countdown keeps a human in the loop; hover-to-stop prevents a race. | S |
| 14 | Multi-diff / multi-buffer review tab (one buffer per changed file) | Zed, VS Code multi-diff editor | The change set needs a *tree*, not a flattened patch; flattening destroys the file-structure context reviewers rely on. | M |
| 15 | Steer vs Queue as two distinct, discoverable follow-up behaviours | Cursor, VS Code, Codex | Sending a message into a running turn vs parking it for later is the most-used interaction in agentic editing and is currently inconsistent across tools. | M |
| 16 | Reorderable queue rendered above the composer | Codex desktop app, Cursor | Queued messages are a mini backlog the user manipulates; drag-to-reorder is cheap and removes "why did it do that?" confusion. | S |
| 17 | Four explicitly-named undo scopes (edit / request / checkpoint / source control) | VS Code undo scopes table | Naming the blast radius beats a generic Undo; users pick the scope that matches their intent instead of guessing. | S |
| 18 | Follow-the-agent camera (auto-jump to the file being edited) | Zed Agent Panel | Replaces "where did it edit?" with continuous observation; cheap to implement and dramatically reduces the lag of following an agent's work. | S |
| 19 | Diff layout choice: Inline / Side-by-side / Automatic + independent word wrap | VS Code diff toolbar | One diff widget must serve a 30-column browser and a 200-column monitor; "Automatic" is the honest default. | M |
| 20 | Accessible unified-patch diff view + accessible view of responses | VS Code Accessible Diff Viewer | A screen-reader path is a hard requirement for a keyboard-only review workflow, and a unified patch is the easiest format to render faithfully in a browser. | S |

**Runner-ups not in the top 20 but worth a backlog slot:** Claude Code's four-stage review pipeline (find → verify →
rank → post) with severity-ranked inline comments; JetBrains' per-agent operation-mode matrix (Auto / Accept Edits /
Plan / Don't Ask / bypass) as a single approval-policy dropdown; `.aiignore`-style hard exclusion boundaries; Zed's
"Context Compacted" inspectable summary entry; Cursor's side chats (`/side`) for tangent investigation; Claude Code's
agent map (subagent tree with status, elapsed time, token count).

---

## Sources

OpenAI / Codex
- https://learn.chatgpt.com/docs/codex/ide
- https://learn.chatgpt.com/docs/prompting
- https://learn.chatgpt.com/docs/codex/ide.md
- https://learn.chatgpt.com/docs/codex/ (index; `/app`, `/desktop`, `/changelog` paths returned 404 during research)

Zed
- https://zed.dev/docs/ai/agent-panel
- https://github.com/zed-industries/zed/blob/main/docs/src/ai/agent-panel.md
- https://zed.dev/docs/ai/edit-prediction

Cursor
- https://cursor.com/docs/agent/overview
- https://cursor.com/docs/agent/planning
- https://cursor.com/docs/agent/agent-review

Windsurf / Devin
- https://docs.devin.ai/windsurf/plugins/cascade/cascade-overview

Claude Code / Anthropic
- https://docs.claude.com/en/docs/claude-code/ide-integrations
- https://docs.claude.com/en/docs/claude-code/code-review

Visual Studio Code / GitHub
- https://code.visualstudio.com/docs/agents/run/review-code-edits
- https://code.visualstudio.com/docs/chat/inline-chat.md
- https://code.visualstudio.com/docs/chat/chat-overview.md
- https://code.visualstudio.com/llms.txt

JetBrains
- https://www.jetbrains.com/help/ai-assistant/
- https://www.jetbrains.com/help/ai-assistant/ai-chat.html
- https://www.jetbrains.com/help/ai-assistant/agents.html
