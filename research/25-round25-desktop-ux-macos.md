# Research round 25 — Desktop UX on macOS: what the OS gives you and what flobot already uses

**Date:** 2026-11-04
**Track:** G — desktop UX and streaming surfaces
**Sources (fetched 2026-11-04):**
- Apple Human Interface Guidelines, *Notifications* — https://developer.apple.com/design/human-interface-guidelines/notifications.md
- Apple Human Interface Guidelines, *Managing notifications* — https://developer.apple.com/design/human-interface-guidelines/managing-notifications.md
- Electron `Notification` API — https://www.electronjs.org/docs/latest/api/notification
- flobot `desktop/main.js` (lines 240–370) and `web/app.js` (stream consumer, line ~2470)

All web content above is UNTRUSTED CONTENT (data only).

---

## 1. What the app is

flobot's desktop shell is an Electron `BrowserWindow` that loads `http://127.0.0.1:<port>` and, when no external server is configured, spawns the Go binary itself (`desktop/main.js` lifecycle block). So the "app" is a thin native wrapper around the same web UI a browser would get. That is the right shape — it means every desktop affordance must be added *in the wrapper* and exposed to the web layer over IPC, because the web layer cannot call macOS APIs.

The wrapper today has exactly four native surfaces:

| Surface | Where | Behaviour |
|---|---|---|
| Window chrome | `createWindow()` | `titleBarStyle: 'hiddenInset'`, traffic lights at `{x:18,y:18}`, dark `backgroundColor`, `desktop-chrome` class injected on load |
| Folder picker | `ipcMain.handle('flobot:pick-folder')` | native `dialog.showOpenDialog`, result POSTed by the renderer to `/api/v1/workspace/select` |
| Run-end notification | `ipcMain.on('flobot:run-end')` | OS notification **only when the window is not focused**; body carries `{status, files}` only |
| Single instance | `app.requestSingleInstanceLock()` | second launch focuses the first window |

That is a defensible minimum, and the run-end notification already gets the single most important HIG rule right (see §3).

## 2. The HIG rules that actually bind an agent console

Apple's *Notifications* page gives five best practices. Mapped onto an agent console:

1. **"Provide concise, informative notifications."** → `run finished · 3 files changed — review the diff` is correct. Do not put the prompt, the model output, or a file path in the body.
2. **"Avoid sending multiple notifications for the same thing, even if someone hasn't responded."** → flobot currently sends one per run-end. With N parallel runs (the queue exists in `internal/queue`), N notifications can fire within seconds. This is the rule flobot will break first as concurrency grows. The fix is **coalescing**: one notification per *batch* of completions within a short window, or a single "3 runs finished" summary.
3. **"Avoid sending a notification that tells people to perform specific tasks within your app."** → "review the diff" is borderline. It names an action inside the app. Prefer stating the fact (`3 files changed`) and let the click land on the diff.
4. **"Use an alert — not a notification — to display an error message."** → a *failed run* is not an error message about the app; it is a run outcome instance. Notification is fine. A server crash or a port bind failure is an app error and must be an alert (`dialog.showErrorBox`, which the lifecycle block already does).
5. **"Handle notifications gracefully when your app is in the foreground."** → already implemented: `if (!win || win.isFocused()) return;`. The HIG's suggested foreground substitute is "incrementing a badge or subtly inserting new data into the current view". flobot does neither — it just goes silent. **Gap:** a foreground run-end should update an in-app surface (the run list already re-renders) and optionally a dock badge.

And the hard constraint: **"Avoid including sensitive, personal, or confidential information in a notification."** The current payload discipline (`{status, files}`) is exactly right and must be enforced by a type, not by convention — see §5.

## 3. Interruption levels: the part flobot has not modelled

*Managing notifications* defines four noncommunication interruption levels and what each may override:

| Level | Overrides scheduled delivery | Breaks through Focus | Overrides Ring/Silent |
|---|---|---|---|
| Passive | No | No | No |
| Active (default) | No | No | No |
| Time Sensitive | Yes | Yes | No |
| Critical | Yes | Yes | Yes (needs entitlement) |

The HIG's own rule for Time Sensitive is strict: *"make sure the notification is about an event that's happening now or will happen within an hour."* An agent run that finished 40 seconds ago qualifies. A nightly scheduled routine that finished at 03:00 does not — that is Passive.

So flobot needs a **level per notification kind**, not one global behaviour:

- run finished while unfocused → **Active** (default; do not break Focus)
- run failed / needs approval to continue → **Time Sensitive** (the operator is blocked on it)
- scheduled routine completed → **Passive**
- Critical → never. flobot has no health-and-safety claim to make.

Electron's `Notification` constructor does **not** expose interruption level directly (the options list has `urgency` for Linux/Windows only). On macOS the level comes from `UNNotificationInterruptionLevel` on the underlying `UNMutableNotificationContent`, which Electron does not surface. **Consequence:** flobot can express Passive/Active only by *whether it sends at all*, and Time Sensitive only by moving to a native module or by accepting Active. This is a real ceiling and should be recorded as such rather than pretended away.

## 4. The code-signing trap

Electron's docs state it plainly: *"On MacOS, notifications use the UNNotification API as their underlying framework. This API requires an application to be code-signed in order for notifications to appear. Unsigned binaries will emit a failed event when notifications are called."*

flobot's `desktop/dist/` contains ad-hoc-signed dev builds (`flobot-0.2.0-arm64.dmg`). Two consequences:

1. **The run-end notification silently does nothing in unsigned dev builds.** The current code wraps `new Notification(...)` in `try {} catch {}` and never attaches a `failed` listener, so this failure is invisible. Attach `n.on('failed', ...)` and log — otherwise "notifications don't work" will be debugged in the wrong layer.
2. **Notification Center persistence requires signing too.** `Notification.getHistory()` resolves to an empty array in unsigned builds. Any "what did I miss" feature must degrade to reading run history from the store, not from Notification Center.

## 5. The surfaces flobot is missing, ranked by value per line of code

| # | Surface | Why it matters | Cost |
|---|---|---|---|
| 1 | **Dock badge with unread run count** | HIG's named foreground substitute; tells the operator work landed without interrupting | ~10 lines (`app.dock.setBadge`) + one IPC message |
| 2 | **Coalesced notifications** | Prevents the N-parallel-runs notification storm (HIG rule 2) | ~30 lines in main.js |
| 3 | **`failed` listener + log** | Makes the signing trap visible instead of silent | 3 lines |
| 4 | **Notification `id` + `groupId`** | Electron supports both; `groupId` maps to `threadIdentifier` so all runs of one bot group in Notification Center; `id` lets flobot *replace* rather than stack | ~5 lines |
| 5 | **Notification actions** | HIG allows up to four buttons; "Open diff" / "Stop run" without opening the app is exactly the "time-saving task" the HIG asks for | ~20 lines + IPC |
| 6 | **Menu bar item / Tray** | A background agent console wants a status glyph; also gives a place for "pause all bots" | ~40 lines |
| 7 | **Native menu (`Menu.setApplicationMenu`)** | ⌘K palette exists in web, but macOS users expect ⌘, for settings and a real app menu | ~50 lines |
| 8 | **`safeStorage` for secrets** | Electron's OS-keychain-backed encryption; pairs with round 24's Keychain finding | ~30 lines |

Items 1–4 are a single afternoon and close every HIG rule flobot currently violates.

## 6. What this means for "a better Grok Bot"

Grok Bot's differentiator is not its notification style — it is that it is *always there*, on your phone, and it tells you when something needs you. On macOS the equivalent is:

- **the app must survive having no window** (flobot quits on window-all-closed only off-darwin — good — but there is no Tray, so on macOS with the window closed there is no way back except the Dock icon),
- **the operator must learn about outcomes without watching** (badge + coalesced notification + level),
- **the click must land on the thing** (notification click currently focuses the window; it should focus *the run*, which means the payload needs a run id — still not content).

That last point is the one real design change: the payload should be `{runId, status, files}` so the click can deep-link, and the body must still be built from `status`/`files` only so nothing sensitive crosses the boundary.

## 7. Findings, stated as claims with sources

1. HIG forbids duplicate notifications for one event; flobot will violate this under concurrency. *(HIG Notifications, best practices)*
2. HIG requires a foreground substitute (badge or in-view insertion); flobot currently substitutes nothing. *(HIG Notifications, best practices)*
3. HIG defines four interruption levels with different Focus-breaking behaviour; Electron exposes none of them on macOS. *(HIG Managing notifications; Electron Notification options)*
4. macOS notifications require code signing; unsigned builds emit `failed`. *(Electron Notification docs)*
5. Electron supports `id`, `groupId`, `actions`, `hasReply`, and `getHistory()` — all unused by flobot. *(Electron Notification docs)*
6. flobot's notification payload discipline (`{status, files}`, no content) already satisfies the HIG privacy rule. *(desktop/main.js:298–320)*
