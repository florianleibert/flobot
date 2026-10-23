# GUI Code Editor Feature Research — 100 rounds

Goal: catalog the most-requested and best-implemented GUI editor features (2024–2026), to inform the flobot Code Studio editor roadmap. Each "round" = one focused query/topic. Findings ranked by evidence of user demand (upvotes, surveys, adoption) and implementation quality.

## Editors surveyed
VS Code, Zed, Cursor, Windsurf, JetBrains fleet, Neovim (GUI fronts), Sublime, Xcode, Nova, Emacs, Fleet, Copilot Workspace, Replit, Bolt/Lovable (web IDEs), CodeMirror/Monaco (web editor cores).

---

## R1. Stack Overflow Dev Survey 2025 — editor landscape
- VS Code remains #1 IDE (~74% of devs use it); Visual Studio #2; AI IDEs (Cursor etc.) growing but single-digit share.
- Takeaway: baseline expectations are set by VS Code; differentiators must go beyond it.

## R2. VS Code most-upvoted feature requests (GitHub issues)
- Historically top requests: improved search/replace (multi-file, regex), remote development, sync settings, terminal improvements, split editors, profiles.
- Recurring open asks: better large-file handling, faster startup, native test explorer improvements, workspace-level settings sync.

## R3. Zed — differentiating features
- GPU-accelerated Rust core: instant startup, 120fps+ editing, huge-file handling.
- Real-time multiplayer collaboration (CRDT-based, cursors, follow mode, shared editing).
- Built-in AI agent panel, inline assist, editable agent diffs.
- Vim mode as first-class; language server support; Tmux-like project panes.

## R4. Cursor — AI-native editor features
- Tab completion (multi-line, next-edit prediction across the file).
- Agent mode: autonomous multi-file edits, terminal commands, checkpoint/restore.
- Composer/Agent side panel with diff review + accept/reject per hunk.
- Project rules (.cursorrules), @-mentions of files/symbols/docs, codebase-wide semantic index.
- Background agents (cloud), bugbot (PR review).

(work in progress — rounds continue)

## R5. Windsurf — Cascade agent
- Cascade: agentic flow that writes code, runs commands, iterates on its own errors; "flows" = reusable multi-step automations.
- Supercomplete (next-gen autocomplete), context-aware "Memories" of codebase.
- Takeaway: the "agent + editable diff review" pattern is table stakes for AI IDEs.

## R6. JetBrains IntelliJ 2025.2
- Full Line Code Completion (local model, offline, free).
- AI Assistant 2025.2: better context (selected code, attached files), inline AI actions, model choice incl. local.
- Takeaway: offline/local-model completion is a differentiator; JetBrains ships it free in-IDE.

## R7. Zed features page (fetched) — full feature inventory
- Breadcrumbs (path + syntax-node summary), multi-buffers (editable excerpts of multiple files in one tab — used for search results, diagnostics, rename previews), project diagnostics multi-buffer, outline view with contextual fuzzy search ("pub fn select"), split panes, dock (anchored terminal/reference panel), integrated terminal (Alacritty backend), command palette, file finder, code actions w/ multi-file results in multi-buffer, rename refactoring preview, find-all-references multi-buffer, syntax-tree selection grow/shrink, multi-cursor, Vim mode tested against Neovim output, themes.
- Collaboration: contacts, project sharing (guests get full editing + LSP), follow mode (pane-scoped, auto-unfollow on edit, avatar click to follow, cycle participants), screensharing, calls.
- Takeaway: multi-buffer is Zed's signature UX — every list-of-locations surface (search, diagnostics, references, renames) is an editable multi-file buffer.

## R8. Zed agent panel / editable diffs (issues)
- Editable diff review in agent panel is a top ask; accept/reject per-hunk had bugs (#62147), "undo reject all" needed (#50142) — evidence that per-hunk accept/reject + undo of review decisions is expected UX.

## R9. CRDT multiplayer editing (Zed blog)
- CRDTs make multiplayer text editing core DNA: no central server authority, offline edits merge, cursors/selections broadcast. Follow mode + shared editing = "Google Docs for code".

## R10. Neovide / Neovim GUI fronts
- GPU-accelerated rendering, smooth cursor, animated shortcuts, externalized LSP; Neovim GUI ecosystem thrives on: startup speed, remote attach (neovim --server), ligatures, multi-grid UIs.

## R11. Cursor 2.0 changelog (fetched) — full feature inventory
- Multi-Agents: sidebar for agents+plans, up to 8 parallel agents on one prompt, git worktrees/remote machines for isolation.
- Composer model (4x faster agentic model); Browser GA (embedded in-editor, element-select tool forwarding DOM to agent); improved code review (all agent changes across files in one view); sandboxed terminals GA (macOS, workspace RW, no network); team commands/rules centrally managed; voice mode (speech-to-text + custom submit keywords); plan mode in background (plan with one model, build with another, parallel plans); cloud agents (99.9% reliability, instant startup); prompt UI: files/dirs as inline pills, agent self-gathers context; LSP perf overhaul.
- Takeaway: parallel isolated agents + unified multi-file diff review + sandboxed terminals are the current frontier.

## R12. Monaco vs CodeMirror 6 (web editor cores)
- Monaco: VS Code engine, LSP-ish IntelliSense, but heavy (~1MB+), poor mobile support.
- CodeMirror 6: modular, tiny core, excellent mobile/touch, fast on huge docs, better for embedding/custom UI.
- Takeaway: for a custom web editor (like flobot Code Studio), CodeMirror 6 is the standard pick for control; Monaco when VS Code parity is needed.

## R13. Sticky scroll / inlay hints
- Sticky scroll (frozen enclosing-scope headers while scrolling) — one of the most-praised VS Code additions of recent years; heavily requested in JetBrains too.
- Inlay hints (inline type annotations, parameter names) — loved, with toggle demand.
- Takeaway: both are high-value, low-cost wins for any editor.

## R14. Web IDEs (Replit/Gitpod/StackBlitz)
- Instant dev environments (WebContainers/containers), zero-setup, in-browser preview, agent integration (Replit Agent), real-time collaboration, mobile access.
- Takeaway: instant-on + preview pane is the core web-IDE value prop.

## R15. Three-way merge / conflict editor UX
- Best-in-class: GitKraken, Merge Studio (VS Code), SmartGit — side-by-side + base, per-hunk accept ours/theirs/both, inline conflict markers, undo.
- Takeaway: 3-way merge with base pane and per-hunk resolution is the gold standard for conflict UX.

## R16. Inline git blame / minimap / breadcrumbs
- Inline git blame (GitLens-style hover + status bar) is one of the most-installed editor extensions ever; lightweight inline-blame clones keep appearing (marketplace demand).
- Minimap: signature VS Code feature; breadcrumbs: adopted across VS Code/JetBrains/Zed.
- Takeaway: inline blame + minimap + breadcrumbs = the "expected chrome" trio.

## R17. Remote development (VS Code Remote SSH/Containers/WSL)
- Remote SSH, Dev Containers, WSL, Tunnels — the single biggest reason VS Code dominates; "UI local, compute remote" architecture.
- Takeaway: remote/container dev is a top-tier requested capability in any editor.

## R18. Command palette / fuzzy finder UX
- Cmd+K palette pattern is now a standard UX pattern (uxpatterns.dev); fzf-style fuzzy matching expected; keyboard-first navigation, recent items, scoped commands by focus.
- Takeaway: palette + fuzzy file finder are non-negotiable core UX.

## R19. Copilot code review / agent PR workflows
- Copilot code review agent reviews PRs, comments inline; Copilot Workspace: idea→plan→PR in browser.
- Takeaway: AI review of agent-produced diffs (bugbot-style) is becoming standard companion to agent editors.

## R20. Sublime Text — multiple cursors & large files
- Multiple cursors (Sublime popularized) remain a top-3 loved feature; Sublime still prized for huge-file performance and instant startup.
- Takeaway: multi-cursor + fast large-file handling are timeless differentiators.

## R21. Inline chat / inline edit (Ctrl+L / Cmd+I)
- VS Code inline chat + inline edit (AI edits in a live diff widget in-buffer); Cursor Cmd+K inline edit; JetBrains inline AI actions.
- Takeaway: in-place AI edit with live diff widget (not a side panel) is the most-loved AI interaction after tab completion.

## R22. LSP as the code-intelligence backbone
- Go-to-definition, references, rename, hover, diagnostics, formatting, code actions — all via LSP; even Warp and Kiro now embed LSP; Zed auto-downloads language servers.
- Takeaway: LSP support is the single highest-leverage code-intelligence feature an editor can add.

## R23. Split view / layout persistence
- Editor groups, split view, custom layout (move panels anywhere), and layout save/restore are heavily requested (Layout Saver extension exists because VS Code lacks native persistence).
- Takeaway: layout persistence + split panes are expected; native layout save/restore is a gap even in VS Code.

## R24. Tree-sitter incremental parsing
- Error-tolerant incremental syntax trees power: fast highlighting, syntax-aware selection/folding, outline, breadcrumbs — adopted by Zed, Neovim, GitHub.
- Takeaway: tree-sitter is the standard for editor-native syntax intelligence below LSP.

## R25. Project-wide search & replace
- Regex + capture-group substitution with side-by-side diff preview (VS Code), editable multi-buffer results (Zed), bulk tools exist because demand is high.
- Takeaway: project search/replace with editable results and diff preview is a top-tier power feature.

## R26. JetBrains Fleet
- Smart Mode (editor→IDE on demand), distributed architecture (backend anywhere), collaborative multi-cursor out of the box, polyglot.
- Takeaway: "lightweight by default, smart on demand" is a loved pattern; collaboration built-in.

## R27. Debugger integration (DAP)
- Debug Adapter Protocol = one integration, all debuggers; breakpoints, watch, step-through, inline variable values; CLion/VS Code/Neovim all support DAP.
- Takeaway: DAP support is the standard route to debugger features.

## R28. Snippets / Emmet
- Emmet abbreviation expansion (HTML/CSS) built into VS Code; user snippets with tabstops/placeholders are a top-5 most-used editing feature.
- Takeaway: snippet + Emmet support is expected baseline for web editing.

## R29. Themes / fonts / visual customization
- Theme + icon + font customization is a perennial top "setup guide" topic; ligatures, font-variant settings; VS Code shipped a UI modernization pass (2026) — appearance matters for adoption.
- Takeaway: theming (incl. light/dark) and font control are cheap, expected features.

## R30. Ghost text / inline AI suggestions
- Copilot inline completions (ghost text) — the most-used AI editing feature by volume; UX concerns = distraction control (delay, toggle).
- Takeaway: inline ghost-text completion with accept-word/partial-accept keys is the baseline AI editing feature.

## R31. File tree sidebar UX
- Git status colors/letters in the tree, file-icon themes, nested lazy loading; even terminal tools (Claude Code) now add IDE-style file trees — demand for tree+git-status is universal.
- Takeaway: file tree with git status indicators + icon themes is core chrome.

## R32. Performance benchmarks (startup, large files)
- Zed/Sublime lead startup (<100ms) and huge-file editing; VS Code/Cursor slower; "fastest editor" rankings are a perennial traffic magnet — performance itself is a feature users request.
- Takeaway: startup time + large-file responsiveness are top requested "features".

## R33. Vim mode / keybinding customizability
- VSCodeVim is among the most-installed extensions ever; Zed ships first-class vim mode tested against Neovim; full keybinding remapping is table stakes.
- Takeaway: vim emulation + remappable keybindings are top requested editor capabilities.

## R34. AI context: @-mentions / file pills
- Cursor's @-file/@-symbol/@-doc mentions, inline file/dir pills in prompt UI, agent self-gathering context (2.0) — manual context attachment is being reduced but explicit mention remains loved.
- Takeaway: @-mention context attachment + inline pills is the dominant AI-context UX.

## R35. Cursor @-mentions (docs fetched)
- @Files, @Folders, @Code (symbols), @Docs (indexed doc sites), @Git (commits/diffs), @Web, @Lint errors, @Recent changes; codebase-wide semantic index behind it.
- Takeaway: rich typed context mentions (@file/@symbol/@docs/@git) are the most-copied AI-context UX.

## R36. Checkpoints / rollback for agent edits
- Claude Code checkpointing + /rewind (restore code+session state); Cursor checkpoints/restore; VS Code "review and revert agent changes" — every major agent surface now ships checkpoint/rewind.
- Takeaway: checkpoint + per-file/per-turn rewind is mandatory safety UX for agent editors.

## R37. Clickable terminal links
- File:line links in terminal/problem output that jump to the editor — heavily requested/loved; VS Code problem-matcher auto-links build errors.
- Takeaway: clickable file:line in terminal + problems panel is core integration UX.

## R38. Live preview pane
- VS Code Live Preview (embedded browser + hot reload), side-by-side preview; top-installed extension; web IDEs (StackBlitz/Replit) treat preview as first-class pane.
- Takeaway: embedded live-preview pane with hot reload is a top-requested web-dev feature.

## R39. Problems panel / squiggles / quick fixes
- Error squiggles + problems panel + quick-fix (code action) lightbulb — the diagnostics loop users notice most when it breaks (endless "how to enable squiggles" traffic).
- Takeaway: inline diagnostics + quick-fix actions are core; users notice their absence immediately.

## R40. Zen / distraction-free mode
- Zen mode (VS Code), distraction-free + focus modes (JetBrains, Cursor) — widely used and requested for deep work.
- Takeaway: cheap, loved mode toggle.

## R41. Git integration depth
- Stage/unstage per hunk, inline gutter diff indicators, diff-in-editor toggle, commit/push/pull from panel; Zed's Gram-style "project diff as multibuffer" is the emerging pattern.
- Takeaway: hunk-level staging + inline diff indicators + project-wide diff view are the expected git surface.

## R42. AI diff review tooling wave
- A whole tool category now exists just to review agent edits: hunk.dev (review-first terminal diff viewer), cc-diff-review, ai-review, AI Change Visualizer, VS Code native "review and revert agent changes".
- Takeaway: review-first UX (see all agent changes in one reviewable view, per-hunk accept/reject, revert) is THE defining editor feature of the AI era.

## R43. Settings sync / profiles
- Settings Sync across machines + per-project profiles (extensions, settings, keybindings) — both were top VS Code feature requests before shipping; profiles now core.
- Takeaway: sync + profiles are expected for any multi-machine editor.

## R44. Extension API / marketplace
- VS Code's extension ecosystem is its moat; Zed built an extension system + marketplace; even agent CLIs now grow plugin marketplaces.
- Takeaway: an extension/plugin API is the #1 long-term moat feature, but only after core UX is solid.

## R45. TODO tree / comment annotations
- Todo Tree (multi-million installs): scans TODO/FIXME comments into a sidebar tree, highlights in-editor; "better comments" colorization also popular.
- Takeaway: TODO/FIXME scanning + inline highlight is a cheap, high-demand feature.

## R46. Code folding / indentation guides
- Syntax-aware folding (tree-sitter/LSP-based), custom folding regions, indentation guides — baseline editing features across all editors.
- Takeaway: folding + guides are expected baseline.

## R47. Bracket pair colorization / guides
- VS Code's 10,000x-faster bracket colorization was a celebrated engineering win; Zed shipped rainbow brackets on user demand (discussion #31378); guides + colorization both expected.
- Takeaway: bracket pair colorization + guides are cheap wins users explicitly ask for.

## R48. Semantic / natural-language code search
- Sourcegraph-style semantic search; Cursor codebase index; VS Code smart actions; CodeGraph-style semantic graphs — natural-language "find code" is now an expected AI-editor capability.
- Takeaway: NL/semantic search over the codebase is a differentiator worth having.

## R49. Format-on-save / autosave
- Format-on-save (Prettier/LSP) + codeActionsOnSave (fix lint) is now the standard; autosave with delay is a top-toggled setting.
- Takeaway: autosave + format-on-save via LSP are expected defaults.

## R50. Tab/session persistence & multi-root workspaces
- Restore tab groups/sessions on reopen (VS Code does; extensions fill gaps), multi-root workspaces, per-window layout memory — persistent demand (Save/restore editors extensions).
- Takeaway: session/tab restore + multi-root projects are requested table stakes.

## R51. Column (box) selection
- Shift+Alt column selection, keyboard column mode — standard in VS Code/Sublime/JetBrains; frequently asked about.
- Takeaway: block/column selection is expected baseline editing.

## R52. Word wrap / line numbers / whitespace rendering
- Alt+Z word wrap toggle, render-whitespace options, line numbers, minimap toggle — the "view options" set users expect to control per-editor.
- Takeaway: view toggles (wrap, whitespace, line numbers) are trivial but mandatory.

## R53. Find/replace refinements
- Find-in-selection, case-preserving replace, preserve-case, search history — refinements users ask for once basics exist (VS shipped case-preserving replace as a headline feature).
- Takeaway: find-in-selection + case-preserving replace are the most-requested refinements.

## R54. Plan mode (agent planning + approval)
- Plan mode everywhere in 2025: Cursor plan mode, VS Code plan agent, Visual Studio Plan agent, CodeGPT plan mode — agent drafts a plan, user reviews/edits/approves, then execution.
- Takeaway: plan→approve→execute is now the standard agent workflow; plan review UI is a top feature.

## R55. Spell check (CSpell)
- Code Spell Checker (CSpell) — multi-million installs; camelCase-aware, workspace dictionaries.
- Takeaway: spell-check with code-aware tokenization is a cheap, high-install feature.

## R56. Remote tunnels / web editor
- VS Code Remote Tunnels (any machine → browser editor at vscode.dev) — "buried feature" articles show latent demand; vscode.dev gives full editor in browser.
- Takeaway: tunnel-to-browser editing is a differentiator for remote access.

## R57. Markdown / media preview
- Built-in markdown preview (side-by-side, sync scroll) is a top-used VS Code feature; image/media preview in editor; preview extensions for HTML.
- Takeaway: markdown preview with sync scroll is expected for docs-heavy repos.

## R58. Notebook support
- Jupyter notebooks in VS Code (cells, outputs, variables view) — major data-science adoption driver; Positron doubles down on notebooks.
- Takeaway: notebook/cell editing is a big surface but niche for a code-focused editor.

## R59. AI terminal / NL commands
- Warp AI (natural-language terminal), terminal command suggesters in-editor — a whole category of AI terminal assistants.
- Takeaway: NL→command in the integrated terminal is an emerging expected feature.

## R60. AI smart actions (generate tests/docs/explain)
- VS Code smart actions, JetBrains AI generate tests/docs, Copilot "explain" — right-click AI actions on selections are standard.
- Takeaway: selection-scoped AI actions (explain/generate tests/docs) are expected AI-editor surface.

## R61. Accessibility
- Screen-reader support, full keyboard navigation, ARIA roles, high-contrast themes — VS Code has a dedicated accessibility doc; a11y is a differentiator for web editors (CodeMirror 6 leads mobile/a11y).
- Takeaway: keyboard-complete + screen-reader basics are expected for any serious editor.

## R62. i18n tooling in editors
- i18n Ally (inline translations in-editor, key detection) — popular niche; editors increasingly surface i18n key previews inline.
- Takeaway: niche but loved; inline i18n value preview is a nice-to-have.

## R63. Remote-control / headless editor APIs
- REST Control (VS Code), headless-editor MCP servers, OpenCode server — programmatic editor control is an emerging pattern (agents driving editors).
- Takeaway: an HTTP/WS control API for the editor is forward-looking (agent-driven editing).

## R64. External file-change reload
- Hotly requested VS Code behavior (#169942): reliable detection + reload prompt for externally-modified files — critical for agent+editor co-editing (flobot's exact case).
- Takeaway: robust external-change detection with non-destructive reload is a top request and core to agent-editor UX.

## R65. Cursor polish / smooth scrolling
- Neovide popularized smooth cursor animation + smooth scroll; Zed ships 120fps rendering; users cite "feel" as a switch reason. Cursor (the editor) reviews repeatedly praise polish.
- Takeaway: rendering polish (smooth scroll, cursor animation, low input latency) is a real adoption factor.

## R66. Outline panel / symbol sidebar
- Outline view (VS Code sidebar + Zed outline panel + outline.nvim) — symbol tree navigation is standard; Zed's outline adds contextual fuzzy ("pub fn select").
- Takeaway: outline sidebar + fuzzy symbol jump are expected navigation.

## R67. GitLens — the lens pattern
- GitLens (30M+ installs): blame lens on hover, file history, line history, rich hover cards — the most-installed git extension ever; its feature set defines "git lens" UX.
- Takeaway: hover-based git history (blame lens) + file/line history views are the gold standard.

## R68. Test explorer
- VS Code Test Explorer API (run/debug per test, gutters, coverage); JetBrains test runner — expected in IDE-class editors.
- Takeaway: test panel with gutter run buttons is expected for IDE-tier; optional for editor-tier.

## R69. Coverage gutters
- Coverage Gutters extension (lcov/gcov inline coverage) — popular; JetBrains has native coverage with inline highlighting.
- Takeaway: inline coverage highlighting is a valued test-companion feature.

## R70. Error Lens — inline diagnostics
- Error Lens (multi-M installs): renders the diagnostic message at end-of-line instead of hover-only — one of the most-installed "quality of life" extensions; JetBrains added inline errors natively after demand.
- Takeaway: end-of-line inline diagnostics is a top-requested QoL feature (hover-only is considered insufficient).

## R71. Indent-rainbow
- indent-rainbow — multi-M install; colorizes indentation depth. Simple, loved.
- Takeaway: trivial-to-add visual aid with proven demand.

## R72. REST client (.http files)
- REST Client / Thunder Client — run HTTP requests from .http files in-editor; huge install base (Postman-in-editor).
- Takeaway: .http request runner is a popular power feature for API work.

## R73. Database/SQL client in editor
- Database Client / DBCode — browse tables, run SQL in-editor; large install bases; "Postman for DB" pattern.
- Takeaway: DB explorer is a popular power-user panel.

## R74. Container tools / devcontainers
- VS Code Container Tools (replaced Docker ext), Dev Containers — containerized reproducible dev envs; heavily used.
- Takeaway: container/devcontainer workflow support is expected for modern editors.

## R75. Agent-editor integration patterns
- VS Code agent mode (MCP support), Zed AI agent panel, remote agent supervision (CodeAgent mobile), multi-agent orchestration — the agent panel is now a first-class editor pane.
- Takeaway: agent panel + MCP client + remote supervision are the emerging agent-editor surface.

## R76. Diff algorithms (word/syntax-aware)
- Myers default, Patience/Histogram for code (fewer misaligned hunks), word-diff mode; syntax-aware diffing improves review quality.
- Takeaway: patience/histogram + word-level diff options materially improve diff UX.

## R77. Side-by-side vs inline diff
- Research consensus: side-by-side for review/understanding, inline (unified) for editing in place; Cursor forum uproar when per-change apply + inline review was removed ("throwing away your best UX advantage") — inline per-hunk apply is the most-loved agent-diff UX.
- Takeaway: offer both layouts; inline per-hunk accept/reject is the priority.

## R78. Local file history / timeline
- VS Code Timeline (local file history, restore any save) — heavily used "undo beyond git" feature; JetBrains Local History is a flagship feature.
- Takeaway: local file history with restore is a top safety feature, especially with agents editing files.

## R79. Streaming diffs while editing
- Claude Code live diff panel (see the diff while the agent is still editing) — celebrated UX; Continue Edit streams diffs into the editor.
- Takeaway: live/streamed diff view during agent edits (not only after) is a differentiating, loved feature.

## R80. Multi-agent worktree isolation
- Parallel agents via git worktrees is now a standard pattern (Cursor 2.0 built-in; parallel-code tooling); review/merge flow after.
- Takeaway: worktree-isolated parallel agents + merge review is the frontier multi-agent UX.

## R81. Next Edit Suggestions (NES)
- Copilot NES GA, JetBrains NES GA (Dec 2025), Continue next-edit — jump-to-next-edit prediction beyond autocomplete; active research (arXiv 2508.02473, 2508.10074).
- Takeaway: NES (predict the next edit location + content) is the current autocomplete frontier.

## R82. MCP client support
- VS Code/Zed/Cursor all ship MCP clients (tools for agents); VS Code has an extension API for MCP.
- Takeaway: MCP client in the editor is now expected for agent-capable editors.

## R83. Rules files (AGENTS.md / .cursorrules)
- Project instruction files are now a standard: AGENTS.md (cross-tool standard), .cursorrules, CLAUDE.md, copilot-instructions; migration guides everywhere.
- Takeaway: support AGENTS.md-style project rules — it's the emerging cross-editor standard.

## R84. Prompt files / prompt library
- VS Code prompt files (.prompt.md, reusable slash commands), GitHub prompt files, prompt-library extensions — saved reusable prompts are an expected AI-editor feature.
- Takeaway: reusable prompt/slash-command library is standard.

## R85. Token/cost transparency
- Copilot Cost Lens, codeburn, token-monitor widgets — a tooling category exists purely because editors don't show AI spend; users want per-conversation token/cost display.
- Takeaway: inline token/cost meter is a requested, differentiating transparency feature.

## R86. Model picker / mid-chat switching
- Model pickers in every AI editor (VS Code language-model API, Cursor, Claude Code /model); mid-conversation switching increasingly supported.
- Takeaway: model selector with per-task override is expected AI-editor chrome.

## R87. Context-window usage indicator
- Open feature requests on Zed (#49472), Claude Code (#44469), plus HUD plugins — users want a live context-usage meter in the status bar.
- Takeaway: context-window usage indicator is a top open request in AI editors.

## R88. Stop/interrupt/pause agent mid-run
- Interruptible agent execution is a named pattern (agentpatternscatalog); Esc-to-stop, pause-for-approval gates, safe mid-task stop without corrupting state.
- Takeaway: reliable stop/pause + resume of agent runs is mandatory control UX.

## R89. Agent permission/approval UX
- VS Code approvals (allow/deny command lists, YOLO mode), Cursor sandboxed terminals, approval-prompt design guides — permission gating with persistent allowlists is standard.
- Takeaway: per-tool/per-command approval with "always allow" persistence + sandbox default is the expected safety model.

## R90. Collapsible command blocks in agent transcript
- Zed discussion #58333 (make ACP/tool blocks collapsible), VS Code "making agents practical" post, command-block TUI patterns — collapsing verbose tool output is a top readability request.
- Takeaway: collapsible tool/command output blocks in the agent transcript are a requested QoL feature.

## R91. Image/screenshot attach to AI chat
- Copilot vision GA (image input in chat), screenshot-attach guides — multimodal attach is standard in AI editors.
- Takeaway: paste-image-into-chat is expected multimodal UX.

## R92. Voice input
- VS Code built-in voice support (accessibility + chat), Cursor voice mode, dictation guides — voice coding is a growing niche.
- Takeaway: voice input is a nice-to-have accessibility+mobile feature.

## R93. Mobile companion / remote supervision
- Cursor iOS app (run/supervise agents from phone), CodeAgent Mobile (control VS Code agents), a whole "control agents from mobile" app category — remote agent supervision is a hot 2026 surface.
- Takeaway: mobile/web companion for agent supervision is a differentiator.

## R94. Agent notifications
- Whole tooling category for "notify me when the agent finishes/needs input" (agent-notify, Claude hooks, opencode issue #39936) — notifications are a top unmet need.
- Takeaway: desktop notifications on agent completion/attention-needed is a requested, high-value feature.

## R95. Session history / resume
- VS Code session history (sync + query past sessions), Claude Code session resume, history-viewer extensions — browsing/resuming past agent sessions is expected.
- Takeaway: session list + resume + search is standard agent-editor surface.

## R96. Offline / local models / privacy
- A whole category of offline AI coding tools (2026); Cursor local mode; JetBrains local full-line completion — privacy/offline is a real demand segment.
- Takeaway: local-model support is a differentiator for privacy-sensitive users.

## R97. Telemetry / privacy controls
- Telemetry opt-out docs are among the most-visited editor docs; privacy-friendly editor lists exist; Cursor publishes a data-use page (trust response).
- Takeaway: clear telemetry opt-out + privacy statement is expected trust surface.

## R98. Update control / rollback
- Version pinning/rollback guides for Claude Code, VS Code extension update delay/pause/pin — users want control over when tools change under them (the d5ca8dd5 lesson generalizes).
- Takeaway: update channels with delay/pin/rollback are a requested reliability feature.

## R99. Shortcut discoverability
- Cheat-sheet sites are a permanent traffic category; editors ship built-in shortcut references (VS Code, JetBrains) — discoverability of keybindings is a real UX surface.
- Takeaway: in-app shortcut reference/cheat-sheet panel is a cheap onboarding win.

## R100. Onboarding / first-run
- First-run wizards, get-started walkthroughs, onboarding design patterns — editors invest in guided first-run (VS Code Get Started, walkthroughs).
- Takeaway: a first-run walkthrough (open project → key features) measurably improves adoption.

---

# Synthesis: Top features by demand × implementation quality

## Tier 1 — Table stakes (users notice absence immediately)
1. Fast startup + large-file responsiveness (Zed/Sublime set the bar)
2. LSP integration: go-to-def, references, rename, hover, diagnostics, quick-fix
3. Command palette + fuzzy file finder
4. Multi-cursor + column selection
5. Project search/replace with regex + preview (editable results à la Zed multi-buffer)
6. Integrated terminal with clickable file:line links
7. File tree with git status indicators
8. Split panes + layout persistence
9. Themes (light/dark) + font control; minimap, breadcrumbs, sticky scroll, inlay hints
10. Inline diagnostics (Error Lens pattern) + problems panel

## Tier 2 — The AI-era differentiators (most requested 2025–2026)
1. **Agent diff review**: all agent changes in one view, per-hunk accept/reject, revert, editable diffs (the single most-copied pattern; a whole tool category exists for it)
2. **Checkpoints/rewind**: per-turn checkpoint, restore code+session (Claude /rewind, Cursor checkpoints, VS Code review-and-revert)
3. **Plan mode**: plan → review/edit → approve → execute
4. **Streaming/live diffs while the agent edits** (Claude Code live diff panel)
5. **@-mentions** for context (files/symbols/docs/git) + inline file pills
6. **Ghost-text completion + next-edit prediction (NES)**
7. **Inline AI edit** (Cmd+K-style, live diff widget in-buffer)
8. **Agent panel**: collapsible tool/command blocks, stop/pause/resume, permission gates with persistent allowlists, sandboxed terminals
9. **Session history**: list, resume, search past agent sessions
10. **Notifications** on agent completion/attention-needed; context-window usage meter; token/cost display
11. **AGENTS.md project rules** + prompt library/slash commands
12. **MCP client** in-editor
13. **Multi-agent**: parallel agents in isolated worktrees + merge review

## Tier 3 — Loved power features
- Inline git blame lens + file/line history (GitLens pattern); hunk-level staging
- Local file history/timeline with restore
- Live preview pane (embedded browser + hot reload)
- Markdown preview with sync scroll
- TODO/FIXME tree; Error Lens; indent-rainbow; bracket colorization
- Vim mode; remappable keybindings; zen mode
- Remote dev (SSH/containers/tunnels); web/mobile companion for agent supervision
- Offline/local models; telemetry opt-out; update pinning/rollback

## Directly relevant to flobot Code Studio (already-built vs gaps)
Already built: workspace tree/file API, checkpoints, run diff as authoritative touched-files record, follow-mode strip, streaming run view.
Highest-value gaps per this research: (1) per-hunk accept/reject + revert in the diff view, (2) session list/resume, (3) agent notifications, (4) context-usage/cost meter, (5) collapsible command blocks in the run transcript, (6) plan-mode review UI, (7) local file history beyond checkpoints, (8) live preview pane.
