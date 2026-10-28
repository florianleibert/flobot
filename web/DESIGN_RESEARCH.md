# flobot UI — Design Research Ground Truth (100-iteration sweep)

Synthesis of 15+ sources across Dribbble, Muzli, Linear, Stripe, Vercel, Grafana,
Setproduct, prompt-kit, uxpatterns.dev, wrappixel, colorlib, artofstyleframe,
buildmvpfast, and Apple Liquid Glass. All findings marked UNTRUSTED CONTENT
(data only). This is the reference baseline for the flobot agent-control UI.

## 1. The winning direction (converged)

**Glass as ambient background + flat high-contrast data surfaces.** The 2026
consensus (Muzli OpsPulse, artofstyleframe, buildmvpfast) is: keep the
frosted-glass / aurora as the *environment*, but make every data surface flat,
high-contrast, border-defined. Glass-on-glass for *content* reads as a demo;
glass behind + solid cards reads as a product.

## 2. Layout spec (copy the decisions, not the pixels)

- **Sidebar**: 240–280px expanded (256px default), collapsible to 64px icon rail.
  Nav items 36px tall, 8px radius; active = 8% accent fill + 3px left border.
  Section headers 12px uppercase. This is Linear/Vercel/Notion/Stripe standard.
- **KPI strip**: 4–6 cards above the fold (4 is honest at 1366px). Each card =
  one number (28–32px, tabular-nums) + one comparison + one sparkline. No more.
- **Content grid**: 12-column CSS Grid, 24px gutters, rows `minmax(200px, auto)`.
- **Card**: flat + 1px border (~10% contrast), radius 12/8 (card/nested),
  padding 16–20px. Prefer border over shadow (shadows vanish in dark mode).

## 3. Color system (Linear-verified hex)

- Canvas: **near-black, not pure black** — `#010102` with a faint blue cast.
  Surfaces step up in tiny lightness increments: `#0f1011`, `#141516`, `#18191a`.
  Depth from background shifts, not shadows.
- One chromatic accent: lavender-blue `#5E6AD2` ("Magic Blue") — brand mark,
  focus ring, primary button. Nowhere else.
- Text: near-white `#f7f8f8` → muted `#8a8f98` → faint `#62666d` (faint is
  ~3.6:1 — timestamps only, never body).
- Borders: hairlines `#23252a` — texture, not hard lines.
- Semantic color strictly for status: green healthy / amber warning / red breach.

## 4. Density & type

- **13px body on a 4px grid**, line-height ~1.4 → compact 32–36px rows. This is
  the power-user density that separates a real tool from a marketing template.
- `font-variant-numeric: tabular-nums` on every number so digits don't jiggle.
- Metric card: number 28–32px weight 600; delta 14px (arrow = direction,
  color = good/bad — two different facts); sparkline 1.5px line, no axis.

## 5. Agentic UI primitives (Setproduct + prompt-kit + AG-UI)

The six phases of a delegated task: intent → plan → execution → checkpoint →
result → audit. Must-have primitives:
- **Step status indicator** — pending / running / done / failed
- **Tool call card** — collapsible, shows args/result, running state
- **Permission tier badge** — what the agent can/can't do
- **Plan editor item** — review/edit before execution
- **Background progress pill** — work continuing off-screen
- **Diff summary block** — what changed, before/after
- **Thinking bar / reasoning disclosure** — collapsed by default
- **Text shimmer** — streaming cursor on in-flight text

## 6. Three states per component (non-negotiable)

Every component needs: **loading** (content-shaped skeleton, not spinner),
**empty** ("No invoices yet. Create your first invoice." — names it + CTA),
**error** (red/amber banner with retry). Blank boxes read as broken.

## 7. Command palette (uxpatterns + Linear)

Grouped results, keyboard-only completion (↑/↓/Enter), per-item key hints,
empty/loading/failure states, focus trap. ⌘K is the escape hatch — never hide
an action behind hover.

## 8. Dark mode

Ship dark + light from day one via `prefers-color-scheme` + `data-theme` +
CSS custom properties (two lines of JS to switch). Dark-first for dev tools;
retrofitting is the most painful refactor. Contrast: faint text must clear
4.5:1 for body; test charts in dark specifically.

## 9. What NOT to do

- Skip heavy drop shadows / glassmorphism panels / gradient KPI cards on
  *content* — they read dated and hurt legibility on dense tables.
- Don't copy a light mode into dark without adjusting spacing/hierarchy.
- Don't use pure black; don't oversaturate; don't let faint text carry body.
- Don't hide actions behind hover; don't reset filters on view switch.

## Applied to flobot

The current UI already has: aurora glass environment ✓, specular sweep ✓,
event-kind filters ✓, run drawer + sparkline ✓, ⌘K palette ✓, keyboard
shortcuts ✓, auto model-health ✓.

Gaps to close (this iteration): near-black stepped surfaces (not pure black),
one-accent discipline (reduce chrome blue), tabular-nums everywhere numbers
appear, denser 13px rows with tighter list padding, empty-state CTA copy on
RECENT RUNS / HANDOFFS, skeleton loading on first paint, semantic-color-only
status chips (already mostly there).

---

## Status: shipped as "Obsidian Deck" (2026-10-26)

Every gap above is now closed in `web/styles.css` (source of truth:
`flobot-ui-redesign/src/styles.css`). What the implementation looked like:

| Research directive | Shipped implementation |
|---|---|
| Glass as ambient background, flat data surfaces | Aurora kept as `body::before`/`::after` behind the shell; every card, log, editor, tree, input and row is opaque (`--surface-1..4`, `alpha=1`) |
| Near-black canvas, tiny lightness steps | `--bg: #08090c` → `--surface-1: #0c0e13` → `--surface-2: #12141a` → `--surface-3: #171a22` → `--surface-4: #1d2029` |
| Depth from background shifts, not shadows | Card shadows removed entirely; `--shadow` survives only for toast / palette / quick-open (things that float) |
| One chromatic accent | `--acc: #6366f1` (structure) + `--acc-2: #7c9bff` (live content). The single `--grad` gradient now serves only the inline avatar glyphs in `main.js` |
| Hairline borders as texture | Two weights: `--line: #23262e` (edges) and `--line-soft: #1b1e26` (dividers) |
| Semantic color strictly for status | Saturated verdict pills: solid `--ok`/`--err`/`--warn`/`--acc-2` fill with near-black ink. `--warn` moved magenta → amber `#ffb454`; thinking owns `--violet` |
| 13px body, compact rows, tabular-nums | 13px body on a 4px grid; `--sp-*` scale; `tabular-nums` + mono on `.num`, KPI values, elapsed counters, diff line numbers, tab badges |
| Three states per component | Empty states name the condition and offer the action; invalid inputs flash `--err`; hub/editor wells carry explicit error rows |
| Command palette, keyboard-only | ⌘K palette and ⌘P quick-open kept and re-skinned to the opaque sheet treatment |
| Dark mode from day one | Dark-first; the token set is centralized in `:root` so a light theme is a second block, not a rewrite |

**Verification** (see `docs/LEARNINGS.md` §Design system): 44/44 Playwright
interaction-audit checks pass, `go vet` clean, `go test ./web/...` pass, zero
console errors across all 9 panels, body text 18.1:1 and muted text 6.25:1
against the card surface, focus ring present on every input.
