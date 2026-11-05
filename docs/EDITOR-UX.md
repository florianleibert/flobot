# Code Studio — editor UX spec (20-cycle sweep)

> The Code Studio is the "Code" panel: file tree, buffer, symbol outline, agent
> stream, diff review. This spec is what the 20 research+build cycles converged
> on. Sources are marked UNTRUSTED CONTENT (data only).

## What exists today (cycle 1–3: the audit)

| Already good | Gap |
|---|---|
| file tree with projects/tags, workspace text search | tree is mouse-only: no filter box, no arrow-key navigation, no file-type icons |
| open tabs with dirty dots, persisted across restarts | no reopen-closed, no middle-click close, no overflow affordance |
| breadcrumbs, symbol outline, @-mention file picker | outline is a flat list, not a tree |
| find/replace bar with match counter | no case / whole-word / regex toggles, no find-in-files |
| command palette (⌘K) with fuzzy scoring | commands are app-level only; no editor commands, no recent-first, no shortcut column |
| diff review with j/k/a/r/A/↵ | no "unsaved vs disk" diff, no per-file side-by-side |
| agent stream, checkpoints, run history | — |

**The core gap: the buffer is a bare `<textarea>`.** No line numbers, no
syntax highlighting, no cursor position, no language, no status bar. Every
other editor affordance in the app is built on top of that one element.

## The design rules (cycles 4–8)

1. **Keyboard-first, but the plain path stays strong.** Every command is
   reachable by mouse *and* by ⌘K; no hidden power-user behaviour before the
   visible path works (uxpatterns.dev, Command Palette).
2. **The status bar is the editor's truth.** Path, language, Ln/Col, selection,
   size on the left; contextual actions on the right. Short text labels, no
   custom colours (VS Code Status Bar UX Guidelines).
3. **Palette anatomy:** trigger → search input → grouped results → result item
   with label, hint **and shortcut** → explicit empty state. Focus stays
   anchored in the input; the whole flow is keyboard-completable.
4. **Accessibility is not a pass at the end.** Semantics first, ARIA only where
   semantics fall short; visible focus state that survives 200 % zoom; no
   colour-only state (WCAG 2.2); announce state changes politely.
5. **Zen mode** (⌘⌥Z) is one keystroke away — VS Code's Zen Mode is the
   canonical distraction-free pattern, and an agent console is exactly the
   place where it pays.
6. **Tab traps the tab key** inside the buffer (VS Code's `tabFocusMode`),
   with an escape hatch, so the code feels like code.

## The build (cycles 9–18)

### Batch A — the editor core
- Highlight layer + gutter over the textarea: native selection, IME and undo
  stay intact; the layers are painted underneath.
- Single-pass tokenizer (sticky regexes, no dependency) for go, js, py, rs, sh,
  json, md, css, html, yml.
- Gutter with line numbers, click to jump, active-line marker.
- Current-line band.
- **Status bar**: path · language · Ln/Col · selection · lines · wrap · zoom ·
  zen.
- Editing: Tab/Shift-Tab indent and outdent, Enter keeps the indentation,
  bracket and quote auto-close, auto-pair skip-over.
- Zoom (⌘+ / ⌘−) and word-wrap toggle (⌥Z), both persisted.

### Batch B — keyboard system and palette
- Editor commands in the palette with a shortcut column and grouping.
- Recent-first ordering for repeat commands.
- Cheatsheet overlay (⌘/) — every shortcut on one surface.
- Go to line (⌘G) from the status bar.
- Focus cycle (F6 / ⇧F6) between tree, buffer, outline, rail.

### Batch C — find and replace
- Case-sensitive, whole-word and regex toggles.
- "Find in files" from the buffer's find bar.
- Replace-all previews the count before it writes.

### Batch D — the tree
- Filter box (type to filter, fuzzy).
- Full keyboard navigation: ↑/↓ move, →/← expand/collapse, ↵ open.
- File-type glyphs and indent guides.

### Batch E — tabs, zen, split, preview
- Reopen closed tab (⌘⇧T), middle-click close, close others.
- Zen mode: everything but the buffer goes away.
- Split view: two buffers side by side.
- Markdown preview (⌘⇧V) for prose files.

### Batch F — accessibility and polish
- Tabs become real `tablist`/`tab`/`tabpanel` with `aria-selected`.
- Skip link, focus-visible rings, `aria-live` status, reduced-motion honoured.
- Contrast pass on the new surfaces.

## Verification (cycles 19–20)

- `go test ./web/` — the UI contract tests assert on the embedded sources.
- `go build ./...`, `go vet ./...`, `gofmt -l`.
- The real proof: the app, running, with the new editor on a real file —
  screenshot, cursor position, gutter, highlight, status bar.

## Note on the bundle

`web/app.js` is an **esbuild bundle** of `src/*.js` from the
`flobot-ui-redesign` repository (`sync-to-flobot.mjs`). Edits here must be
mirrored into that repo's `src/` or the next sync will drop them.
