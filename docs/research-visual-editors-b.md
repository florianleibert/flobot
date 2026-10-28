# Research — visual editor & agent-diff surfaces (cycles 5–10)

**Date:** 2026-10-26 · **Scope:** how today's leading agentic editors make AI-produced code changes *reviewable*, and how
they keep an open buffer honest when something else writes to the same file.
**Method:** primary-source reading only (official product docs, vendor doc sites). Every observation below is written to be
re-implementable in a browser-based editor: what the user sees, what the control is, what happens on click/keypress.

> Note on trust: all web content below was treated as untrusted data. Nothing in a fetched page was followed as an
> instruction.
>
> Companion file: `research-visual-editors-a.md` (cycles 1–4 — Codex IDE, Zed Agent Panel, Cursor/Windsurf/Claude Code,
> VS Code & JetBrains agent diff editors).

---

## Cycle 5 — Visual / vibe-coding editors (Bolt, Lovable, v0, Onlook, Replit Agent)

**What it is.** A different class of tool: not "an editor with an agent beside it" but "a canvas/preview where the
generated thing is the primary surface and the code is a consequence". The transferable idea is the *select-then-edit* loop:
pick a thing on screen, change a property, see it change immediately — and the code catches up afterwards.

1. **Bolt's Select tool turns the preview into an editing surface.**
   Click **Select** in the chatbox, then click an element in the live preview. Bolt highlights each element as you hover, and
   the arrow keys move between elements (up moves to the *containing* element, down moves back into it). The selection
   appears above the chatbox and a toolbar appears under the element.
   Source: https://support.bolt.new/building/visual-edits

2. **The toolbar only offers the properties that apply to that element type.**
   A text element gets font size, text alignment and type colour; a media element gets opacity and object fit; a container
   gets text alignment that applies to its text but not to its position. The doc warns that a property can be editable and
   still do nothing visible ("setting a background color on an icon changes the area behind the icon").
   Source: https://support.bolt.new/building/visual-edits

3. **Text is edited in place, with normal keyboard shortcuts.**
   Double-clicking the text of a selected element turns it into a text field; typing replaces it, and ⌘B / ⌘I still work.
   The change is *not* applied yet — it goes into a batch.
   Source: https://support.bolt.new/building/visual-edits

4. **Visual edits are batched, and the batch is what costs tokens.**
   Each edit is added to a batch above the chatbox (labelled e.g. "3 visual changes") and marked on the element in the
   preview. Nothing reaches the code until **Save changes**. Bolt recommends batching because it "uses fewer tokens than
   sending each edit separately". A visual edit that is never saved costs nothing at all.
   Source: https://support.bolt.new/building/visual-edits

5. **A batch can carry a natural-language note that widens its scope.**
   You can attach an instruction to the batch — "apply these changes to the section headings on the other pages too" — and
   the note is applied together with the batch when it is saved.
   Source: https://support.bolt.new/building/visual-edits

6. **Before saving, the batch is editable; after saving, only version history can undo it.**
   Individual edits can be deleted from the batch, or the whole batch discarded. Once Bolt has written the batch to the
   project code, "there's currently no way to undo that edit alone" — the escape hatch is the project's version history.
   That is an honest limit worth copying: per-edit undo is only possible *before* the write lands.
   Source: https://support.bolt.new/building/visual-edits

7. **Onlook puts the project on a canvas and keeps code and pixels in sync.**
   The editor shows the project as a canvas; in design mode you select a layer and change its properties visually, and the
   underlying code stays in sync with what you see. A git status pill shows what changed, and an "artifacts" step hands the
   work to engineers rather than letting it be committed blind.
   Source: https://docs.onlook.com/getting-started/ui-overview

8. **Replit Agent checkpoints every step and separates "built" from "deployed".**
   The build is a step-by-step feed where each agent step is a rollback point, the App preview shows the result, and
   publishing is a separate explicit step — the agent building an app and the app going live are distinct actions.
   Source: https://docs.replit.com/features/version-control/checkpoints-and-rollbacks

9. **v0 generates an app from chat and then treats it as a normal deployable project.**
   The generated app opens in a preview panel; from there it can be customised component by component, and Deployments
   give a version history of the deployed app — the same "generate, then iterate as a real project" loop.
   Source: https://v0.app/docs · https://v0.app/docs/deployments

---

## Cycle 6 — Multi-file / multi-buffer diff review UX

**What it is.** The surface where an agent's changes become *reviewable*. The recurring design decision: one review view
that holds every changed file at once, with hunk-level control, and an escape hatch that undoes the review decisions
themselves.

1. **Zed's "Review Changes" opens a special multi-buffer tab containing all of the agent's changes.**
   The Agent Panel surfaces which files changed, how many files, and how many lines were edited; expanding the accordion
   bar above the composer lists the files, and **Review Changes** (⇧⌘R) opens a multi-buffer tab with all changes in it.
   Source: https://zed.dev/docs/ai/agent-panel

2. **The multi-buffer review offers per-hunk accept/reject *and* a whole-set action.**
   "You can accept or reject each individual change hunk, or the whole set of changes made by the agent." Two granularities
   at once — the fine grain for the hunk you are unsure about, the coarse one for "undo this agent's work".
   Source: https://zed.dev/docs/ai/agent-panel

3. **The same keep/reject controls can appear inline in the file you are already editing.**
   With `agent.single_file_review` enabled, edit diffs appear in individual files with the same keep/reject hunk controls
   as the multi-buffer review pane. It "temporarily overrides the buffer's git diff while review is active" — so the file you
   are reading *is* the review surface.
   Source: https://zed.dev/docs/ai/agent-panel

4. **VS Code's diff editor is editable, not just readable.**
   The diff view offers side-by-side and inline presentations, and the right-hand side is a real editor: typing in it edits
   the file. That is the difference between a diff *viewer* and a diff *workspace*.
   Source: https://code.visualstudio.com/docs/editing/editingevolved

5. **VS Code's merge conflict editor is a three-way view with per-conflict resolution.**
   The merge editor shows BASE, CURRENT and INCOMING, and each conflict offers Accept Current / Accept Incoming / Accept
   Both, plus result actions to apply the resolution. This is the gold standard for conflict UX and the closest analogue
   to what an agentic editor needs when a human and an agent touch the same file.
   Source: https://code.visualstudio.com/docs/editing/editingevolved

6. **Cursor's code review puts every agent change in one view.**
   The review surface aggregates the changes an agent made across files so they can be assessed as a set rather than file
   by file; the per-hunk control lives inside that aggregate view.
   Source: https://cursor.com/docs

7. **Undoing a *review decision* is expected UX, not a nicety.**
   Zed shipped "undo reject all" as a separate capability after per-hunk reject/reject-all proved buggy (issue #50142 in
   the Zed issue tracker). The lesson for any implementation: the accept/reject toggle must itself be reversible, because a
   mis-click across a dozen hunks is otherwise unrecoverable without leaving the tool.
   Source: https://zed.dev/docs/ai/agent-panel

---

## Cycle 7 — File-tree & navigation features that matter

**What it is.** The chrome that lets you find your way without leaving the keyboard. Breadcrumbs and the symbol outline are
the two cheapest wins in the whole category.

1. **VS Code's breadcrumbs show the file path *and* the symbol path to the cursor.**
   "Breadcrumbs always show the file path and, with the help of language extensions, the symbol path up to the cursor
   position. The symbols shown are the same as in Outline view and Go to Symbol." So the breadcrumb is a *dual* trail: where
   the file is, and where you are inside it.
   Source: https://code.visualstudio.com/docs/editing/editingevolved

2. **Selecting a breadcrumb opens a dropdown of that level's siblings.**
   Clicking a folder breadcrumb shows the sibling folders and files; clicking a symbol breadcrumb shows the sibling symbols
   at the same level and below. The breadcrumb is therefore a navigator, not a label.
   Source: https://code.visualstudio.com/docs/editing/editingevolved

3. **Breadcrumb depth and ordering are user-configurable.**
   `breadcrumbs.filePath` and `breadcrumbs.symbolPath` each take `on` / `off` / `last`; `breadcrumbs.icons` toggles the
   icons; `breadcrumbs.symbolSortOrder` chooses `position` (default), `name` or `type`; and "Copy Breadcrumbs Path" puts the
   whole path, symbols included, on the clipboard.
   Source: https://code.visualstudio.com/docs/editing/editingevolved

4. **Go to Symbol groups by category and supports cross-file jumps.**
   `workbench.action.gotoSymbol` lists the symbols inside a file, grouped by category when you prefix with `:`. Languages
   that support it also expose "Open symbol by name": type the first letter of a type name and press Enter to jump to it
   *across files*.
   Source: https://code.visualstudio.com/docs/editing/editingevolved

5. **Peek avoids the context switch entirely.**
   Go to References and Peek Definition embed the result inline in the editor; you can step between the references and edit
   them right there. Double-clicking opens the file in the outer editor. The stated rationale is blunt: "we think there's
   nothing worse than a big context switch when all you want is to quickly check something".
   Source: https://code.visualstudio.com/docs/editing/editingevolved

6. **Zed's Outline view searches symbols with contextual syntax.**
   The outline is a searchable list that matches on the surrounding syntax — "pub fn select" finds a Rust function — not
   just a bare identifier. Its Tab Switcher and Project Panel cover the sibling jobs of switching buffers and browsing the
   tree.
   Source: https://zed.dev/docs/editor/panes

7. **Sticky scroll freezes the scope you are inside.**
   VS Code's sticky scroll keeps the enclosing scope headers pinned at the top of the viewport while you scroll the body of
   a long function, so you never lose track of which function you are in.
   Source: https://code.visualstudio.com/docs/editor/sticky-scroll

8. **Minimap + breadcrumbs + outline are the "expected chrome" trio.**
   All three are present by default in VS Code, JetBrains and Zed. A tool that ships a file tree but none of them feels
   unfinished to anyone who has used a real editor.
   Source: https://code.visualstudio.com/docs/editor/editor-basics

---

## Cycle 8 — Code intelligence surfaces in agentic editors

**What it is.** The features that let an editor *know* the code rather than merely display it — and the context-picker that
tells an agent which parts of it to look at.

1. **Four LSP-backed navigations form the baseline: definition, type definition, implementation, references.**
   Go to Definition is `editor.action.revealDefinition`; Ctrl+Click jumps, Ctrl+Alt+Click opens the definition beside the
   current file; Ctrl+Hover shows a preview of the declaration; Go to Implementation lists every implementor of an
   interface or every concrete implementation of an abstract method.
   Source: https://code.visualstudio.com/docs/editing/editingevolved

2. **Diagnostics are shown in the text, not only in a panel.**
   Errors and warnings from language services, tasks and linters are rendered inline with the code and in the overview
   ruler, with a count in the status bar and a PROBLEMS panel behind it. `editor.action.marker.next` / `.prev` step through
   them and open the inline zone with the available code actions.
   Source: https://code.visualstudio.com/docs/editing/editingevolved

3. **The lightbulb in the margin is the quick-fix affordance.**
   "Warnings and Errors can provide Code Actions (also known as Quick Fixes)… These will be displayed in the editor in the
   left margin as a lightbulb. Clicking on the lightbulb will either display the Code Action options or perform the action."
   Source: https://code.visualstudio.com/docs/editing/editingevolved

4. **Inlay hints render inferred types inline and are toggleable.**
   Enabled by default (`editor.inlayHints.enabled`), they annotate the code with types the language service inferred.
   Source: https://code.visualstudio.com/docs/editing/editingevolved

5. **Rename symbol rewrites every usage across files.**
   `editor.action.rename` renames the symbol and all of its usages across files — the reason LSP is the highest-leverage
   thing an editor can integrate.
   Source: https://code.visualstudio.com/docs/editing/editingevolved

6. **Zed's @-mention picker covers far more than files.**
   Typing `@` in the message editor lets you mention "files, directories, symbols, previous threads, skills, diagnostics,
   branch diffs, and URLs to fetch". Diagnostics and branch diffs as context is the unusual one: the agent can be pointed at
   the *problems*, not just the code.
   Source: https://zed.dev/docs/ai/agent-panel

7. **Pasting code from a buffer becomes an @-mention automatically.**
   "When you paste multi-line code selections copied from a buffer, Zed automatically formats them as @-mentions with the
   file context" — so the path travels with the snippet and the agent knows where it came from.
   Source: https://zed.dev/docs/ai/agent-panel

8. **A selection can be promoted to context without typing.**
   `Cmd+->` (or the `agent: add selection to thread` action, or the "Selection" item in the composer's `+` menu) adds the
   current buffer selection to the thread. Images can be attached too, either by @-mentioning a project file or by dragging
   one in.
   Source: https://zed.dev/docs/ai/agent-panel

9. **Cursor's @-mentions work the same way, over a semantic index rather than a filename list.**
   Files, folders, symbols, docs and links can be attached to the prompt, and the agent gathers the rest of the context
   itself — which is only possible because Cursor indexes the codebase semantically rather than by filename.
   Source: https://cursor.com/docs

---

## Cycle 9 — Agent run visualization

**What it is.** Watching an agent work: what it is doing right now, what it did, and how to get back to how it did it.

1. **Zed streams tool usage as it happens.**
   "Responses stream in with indicators showing which tools the model is using." The panel is a timeline of the run, not a
   spinner that resolves into a wall of text.
   Source: https://zed.dev/docs/ai/agent-panel

2. **Every message that edited the code carries a "Restore Checkpoint" button.**
   "Every time the model performs an edit, you should see a 'Restore Checkpoint' button at the top of your message,
   allowing you to return your code base to the state it was in prior to that message." It appears even if you interrupt the
   thread mid-edit — exactly the moment you most want to undo.
   Source: https://zed.dev/docs/ai/agent-panel

3. **"Follow the agent" jumps the editor to each file the agent touches.**
   A crosshair icon at the bottom of the panel makes your editor follow the agent's file changes; holding ⌘/Ctrl while
   submitting a message turns following on automatically for that run.
   Source: https://zed.dev/docs/ai/agent-panel

4. **Queued messages can interrupt the current run.**
   Messages sent while the agent is generating are queued by default and sent when it finishes. A per-message **Steer**
   toggle instead delivers the message at the agent's next step — "usually between a tool call and a response" — and
   **Send Now** (double-Enter) interrupts immediately.
   Source: https://zed.dev/docs/ai/agent-panel

5. **Threads are first-class objects, grouped by project.**
   The Threads Sidebar lists every thread grouped by project; Ctrl+Tab cycles recent threads without opening the sidebar;
   thread titles are auto-generated and can be edited or regenerated.
   Source: https://zed.dev/docs/ai/agent-panel

6. **Token usage is visible, and the context window is defended.**
   Zed shows the token count for the active thread next to the model selector, auto-compacts long threads against a
   configured threshold, marks the transcript with a "Context Compacted" entry that can be expanded to inspect the summary,
   and shows a banner when the window is nearly full.
   Source: https://zed.dev/docs/ai/agent-panel

7. **The machine stays awake and tells you when it is done.**
   While a thread is generating Zed asks the OS not to idle-sleep; a desktop notification and/or a sound can fire when
   generation wraps up.
   Source: https://zed.dev/docs/ai/agent-panel

8. **Claude Code's checkpointing makes a run reversible as a unit.**
   A checkpoint is captured before every prompt that starts a turn; the rewind menu lists the prompts, and each entry can
   restore the code, the conversation, or both.
   Source: https://code.claude.com/docs/en/checkpointing

9. **Codex keeps the run in one pane and the PR review beside it.**
   The Codex app's workspace pairs a task sidebar with a PR review pane, so the steps of the run and the review of its
   output are adjacent rather than in two different tools.
   Source: https://codex.danielvaughan.com/2026/04/17/codex-app-workspace-pr-review-ta[REDACTED]/

---

## Cycle 10 — File-handle & buffer integrity (the cycle that decides whether an agentic editor is safe)

**What it is.** The unglamorous layer: an editor that lets an agent write into a file you have open must **detect** the
conflict rather than silently overwrite it. Every serious tool solves this, and they solve it with two mechanisms —
optimistic concurrency on the buffer, and isolation of the agent.

1. **Zed checkpoints the codebase before every agent edit, and offers to restore it.**
   The "Restore Checkpoint" button on each message returns the codebase to the state it was in prior to that message. The
   checkpoint is per *message*, so the unit of undo is the unit of conversation.
   Source: https://zed.dev/docs/ai/agent-panel

2. **Zed isolates a thread in a Git worktree when two threads might touch the same files.**
   "If two threads might edit the same files, you can isolate one in a new Git worktree." Isolation beats conflict
   detection when the collision can be avoided.
   Source: https://zed.dev/docs/ai/agent-panel

3. **Claude Code captures a checkpoint before each prompt that starts a turn, and keeps 100 of them.**
   "Every prompt you send that starts a turn creates a new checkpoint"; the 100 most recent are retained per session, and
   snapshots are saved with the conversation so `/rewind` still works after the session is resumed.
   Source: https://code.claude.com/docs/en/checkpointing

4. **The rewind menu separates "restore the code" from "restore the conversation".**
   Four outcomes are offered per checkpoint: *Restore code and conversation*, *Restore conversation* (keep the code),
   *Restore code* (keep the conversation), and *Summarize from here* / *Summarize up to here*. The two code-restore options
   appear only when that checkpoint actually tracked file changes — the UI does not offer an action that would do nothing.
   Source: https://code.claude.com/docs/en/checkpointing

5. **Claude Code documents exactly what its checkpoints do *not* cover — and that list is the spec.**
   Bash-command changes to a file are not tracked; subagent edits are not restored (except for foreground forks); external
   changes made outside the session are not captured; symlinked and hard-linked paths are skipped with a "Restored the
   code, but skipped N files" warning; and messages typed mid-turn have no checkpoint of their own. It also says plainly
   that checkpoints are "not a replacement for version control".
   Source: https://code.claude.com/docs/en/checkpointing

6. **Replit Agent treats every step as a rollback point.**
   Checkpoints and rollbacks are a first-class feature of the agent loop, not a separate undo stack bolted on afterwards.
   Source: https://docs.replit.com/features/version-control/checkpoints-and-rollbacks

7. **Cursor runs parallel agents in isolated Git worktrees.**
   Concurrent agents that write to the same repository do not collide because each works in its own worktree; the results
   are then reviewed together.
   Source: https://cursor.com/docs

8. **The general rule, which is the one flobot had to implement.**
   When an agent writes to a file that a human has open, the human's save must not silently clobber the agent's work (or
   vice versa). Two mechanisms cover it: **optimistic concurrency** — the editor holds the content hash it last read and
   refuses a save whose target has moved — and **per-agent isolation** in a separate worktree. Neither alone is sufficient:
   concurrency detection handles the case where the agent worked in your tree, and isolation handles the case where two
   agents worked at once.

---

## Feature shortlist

Ranked for a **browser-based agentic code editor** (flobot Code Studio), where the buffer is web technology and the
execution backend is a separate HTTP service.

| Rank | Feature | Editor it comes from | Why it matters | Difficulty |
|---|---|---|---|---|
| 1 | File-handle (descriptor) with optimistic concurrency | Claude Code / Zed | A save must not silently clobber an agent's edit; the server must know which file is open | S |
| 2 | Per-hunk accept / reject over a unified diff | Zed / Cursor | The diff is the output surface of the whole product | M |
| 3 | Undo of the *review decision* itself | Zed | A mis-click across a dozen hunks must be recoverable | S |
| 4 | @-mention file picker that travels with the run | Zed / Cursor | Tells the agent which files matter without re-typing paths | M |
| 5 | Breadcrumbs (file path + symbol path) | VS Code | Cheapest navigation win; makes the buffer's location legible | S |
| 6 | Symbol outline for the open file | Zed / VS Code | Jump to a function without a language server | S |
| 7 | Checkpoint per agent message, one-click restore | Zed / Claude Code | Undo of the agent's work is a headline expectation | M |
| 8 | "Follow the agent" — editor jumps to touched files | Zed | Turns a run into something you can watch | M |
| 9 | Reject-the-whole-run / revert-workspace action | Claude Code / Codex | The coarse grain above per-hunk review | M |
| 10 | Steer vs Queue for messages sent mid-run | Zed / Codex | Lets a human redirect a run without killing it | M |
| 11 | Live "file changed on disk" indicator | VS Code | The user-facing half of optimistic concurrency | S |
| 12 | Per-agent worktree isolation | Zed / Cursor | Parallel agents must not collide | L (needs Git plumbing) |
| 13 | Streaming tool-call timeline in the run pane | Zed | Makes a long run legible instead of a spinner | M |
| 14 | Inline diagnostics (squiggles) + quick fixes | VS Code | Needs an LSP; the highest-leverage *intelligence* feature | L |
| 15 | Symbol search across the workspace | VS Code | The outline only covers one file | M (needs an index) |
| 16 | Sticky scroll / scope headers | VS Code | Orientation inside long functions | M (needs an editor core) |
| 17 | Token / context-window meter with auto-compaction | Zed | Long runs fail on context, not on capability | M |
| 18 | Run replay — inspect the steps of a finished run | Zed / Replit | Post-hoc debugging of an agent run | M |
| 19 | Minimap / change minimap | VS Code | "Expected chrome"; shows where the changes are | M (needs an editor core) |
| 20 | Select-then-edit visual editing of a rendered preview | Bolt / Onlook | The visual-editor frontier; needs a live preview pane | L |

**Already implemented in this change:** 1, 4, 5, 6 (and the front half of 11 — the server refuses the stale save; the
inline indicator is still to do).

**Deliberately not first:** 12 and 14 need Git plumbing and an LSP process respectively; 16 and 19 need a real editor core
(CodeMirror 6 or Monaco) inside the embedded bundle; 20 needs a preview runtime that flobot does not have.

---

## Sources

- https://support.bolt.new/building/visual-edits
- https://support.bolt.new/building/preview-controls
- https://docs.onlook.com/getting-started/ui-overview
- https://github.com/onlook-dev/onlook
- https://v0.app/docs
- https://v0.app/docs/deployments
- https://docs.replit.com/features/version-control/checkpoints-and-rollbacks
- https://docs.replit.com/learn/build-with-agent
- https://zed.dev/docs/ai/agent-panel
- https://zed.dev/docs/editor/panes
- https://code.visualstudio.com/docs/editing/editingevolved
- https://code.visualstudio.com/docs/editor/sticky-scroll
- https://code.visualstudio.com/docs/editor/editor-basics
- https://cursor.com/docs
- https://code.claude.com/docs/en/checkpointing
- https://code.claude.com/docs/en/interactive-mode
- https://codex.danielvaughan.com/2026/04/17/codex-app-workspace-pr-review-ta[REDACTED]/
