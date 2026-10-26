# Code Studio — the top 40 ways to interact with the code editor

**Status:** Implemented and audited · 2026-10-25
**Scope:** the Code Studio panel of the macOS app (`/Applications/flobot.app`), served at
`http://127.0.0.1:4120` (loopback only).
**Audit:** `interact40` (Playwright, real API — no mocks) plus `verify_new_ui` for the
features that need a second surface (artifact-dir toggle, routine pause, Hubs panel).

Every row below is a **user-visible interaction**, not an internal API call, and every row
is exercised by the audit. The `Audit ID` column is the tag the audit prints next to the
PASS/FAIL line, so any row can be re-verified in isolation.

---

## A. Getting into the editor (1–8)

| # | Interaction | Surface | Audit ID |
|---|---|---|---|
| 1 | Open the Code Studio panel from the nav rail | `nav button[data-panel="editor"]` | `1` |
| 2 | Jump between panels with keyboard shortcuts (`g`→`e`, `g`→`r`) | keyboard | `2` |
| 3 | Open the command palette | `⌘K` | `3` |
| 4 | Choose the teammate a code run is dispatched to | `#edRunBot` | `4`, `4b` |
| 5 | Write the prompt and start a run | `#edPrompt`, `#edRunBtn` | `7` |
| 6 | Watch the run stream arrive as NDJSON events | `#edStream .ev` | `11` |
| 7 | Land on the stream pane automatically when a run starts | tab rail | `12` |
| 8 | Stop a run that is in flight | `#edStopBtn` | `8` |

## B. Workspace and file tree (9–17)

| # | Interaction | Surface | Audit ID |
|---|---|---|---|
| 9 | See which workspace folder is open | `#edRoot` | `16` |
| 10 | Open the folder chooser | `#edFolder` | `17` |
| 11 | Switch to a typed absolute path | `#edFolderPath`, `#edFolderGo` | `20/21` |
| 12 | Close the folder bar with `Escape` | keyboard | `20b` |
| 13 | Be refused an invalid folder (`/`) with a friendly toast, not a stack trace | guard | `21b` |
| 14 | Keep the chosen folder across an app reload | persisted selection | `21c` |
| 15 | Refresh the file tree | `#edRefresh` | `18` |
| 16 | Expand a directory in the tree | tree row → `tree?dir=` | `22` |
| 17 | See the workspace files listed in the tree | tree | `23a` |

**Build-artifact dirs are hidden by default.** `node_modules`, `dist`, `build`, `out`,
`coverage`, `venv`, `.next`, `__pycache__` and friends are filtered **server-side**
(`GET /api/v1/workspace/tree` omits them and reports `hidden`), so the tree and the API can
never disagree about what exists on disk. The 👁 toggle in the editor header is what asks
for them explicitly (`?noise=1`); the choice persists in `localStorage`.

## C. Opening and editing files (18–24)

| # | Interaction | Surface | Audit ID |
|---|---|---|---|
| 18 | Open a file by clicking its tree row | tree row → buffer | `23` |
| 19 | Open a file by typing its path | `#edPath`, `#edOpen` | `26` |
| 20 | Open a missing file → confirm → the file is created | confirm dialog | `27` |
| 21 | Open a bogus path → a toast, never a dialog loop | guard | `26b` |
| 22 | Type into the buffer and see the unsaved marker | `#edContent`, `#edDirty` | `29/30` |
| 23 | Save with `⌘S` and watch the dirty marker clear | keyboard | `39` |
| 24 | See the save actually land on disk | API round trip | `28` |

## D. Quick open (25–27)

| # | Interaction | Surface | Audit ID |
|---|---|---|---|
| 25 | Open quick open | `⌘P` | `32/40` |
| 26 | Filter the list as you type | `#edQuickInput` | `33` |
| 27 | Pick the first hit with `Enter`; the overlay closes | keyboard | `34` |

## E. Seeing what the agent did (28–31)

| # | Interaction | Surface | Audit ID |
|---|---|---|---|
| 28 | Reveal build/artifact directories with the 👁 toggle | `#edShowFiles` → `tree?noise=1` | `verify_new_ui` |
| 29 | Switch the rail to the diff pane | `.ed-tab[data-tab="diff"]` | `36` |
| 30 | Switch the rail to the checkpoints pane | `.ed-tab[data-tab="check"]` | `36b` |
| 31 | Switch the rail to the run history pane | `.ed-tab[data-tab="runs"]` | `36c/53` |

## F. Reviewing the diff (32–38)

| # | Interaction | Surface | Audit ID |
|---|---|---|---|
| 32 | See per-hunk accept/reject buttons | `#edDiff [data-act="a"]` | `48` |
| 33 | Toggle a single hunk's acceptance | hunk button | `48a` |
| 34 | Move the hunk cursor with `j` / `k` | keyboard | `41` |
| 35 | Accept the focused hunk with `a` | keyboard | `42` |
| 36 | Read the pending-hunk badge | `#edDiffBadge` | `37` |
| 37 | Accept everything with `A` and watch the badge clear | keyboard | `44`, `37b` |
| 38 | Apply the selection with `Enter` → `POST diff/apply` | keyboard | `45/49` |

## G. History, checkpoints, undo (39–40)

| # | Interaction | Surface | Audit ID |
|---|---|---|---|
| 39 | Open a past run from the runs rail and load its diff | `.ed-run-item` | `54` |
| 40 | See the checkpoint list and restore a checkpoint in one click | `#edCheck .ed-check-item` | `55`, `56` |

---

## Beyond the 40 (verified by `verify_new_ui`)

- **Cron scheduling for routines.** The routine form's trigger field takes a cron
  expression (`cron 0 9 * * *`), an interval (`every 5m`) or `manual`, with quick-pick
  chips for the common ones. The server parses the expression before it stores the
  routine and refuses a cron or interval that does not parse (`400 invalid cron schedule
  …`) instead of arming a fire that can never happen.
- **Routine pause / resume** — a scheduled routine can be paused from the Routines panel
  and resumed; the card shows the paused state and the next fire.
- **Hubs panel** — the mesh Hubs panel lists the peers flobot can talk to.
- **Workspace defaults** — the workspace folder is chosen on first launch and remembered;
  it can be re-pointed at any time from the editor header.

## Known gaps (not yet in the UI)

- **No next-fire preview.** A valid schedule can still not be the one you meant
  (`0 9 * * *` in a timezone you did not expect); the UI shows no "next fire at …" line.
- **A routine's trigger cannot be edited after it is created** — there is no PATCH for
  the trigger, so the routine has to be deleted and recreated. Pause/resume is available.
- **Diff pane for a run that is still in flight** — the live diff lands only after the run
  finishes; there is no streaming hunk view.
