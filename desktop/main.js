'use strict';

const { app, BrowserWindow, shell, ipcMain, Notification, dialog, Tray, Menu, nativeImage } = require('electron');
const { spawn } = require('child_process');
const path = require('path');
const os = require('os');
const http = require('http');
const fs = require('fs');
const net = require('net');

const DEFAULT_PORT = 4120;
const LOG_PATH = '/tmp/flobot-desktop.log';
const LOG_MAX_BYTES = 1 << 20; // cap the log at 1 MiB
const HEALTH_TIMEOUT_MS = 3000;
const START_TIMEOUT_MS = 15000;
const RESTART_BACKOFF_MS = [500, 1000, 2000, 5000, 10000]; // then stay at 10s

let serverProc = null;
let win = null;
let baseUrl = null;
let quitting = false;
let restartTimer = null;
let restartAttempt = 0;
let tray = null;

// unseenRuns is the "what happened while you were away" count. It is shown as
// the dock badge and as the tray title, and cleared when the window is focused
// — the point is that you learn about a finished run without having watched it.
let unseenRuns = 0;

// Runs that finish within this window are announced as one notification.
// Three routines firing at 09:00 must not produce three banners.
const NOTIFY_COALESCE_MS = 1500;
let pendingNotify = null;

function flobotBinary() {
  // Packaged: binary lives in Resources/flobot
  if (process.resourcesPath) {
    const packaged = path.join(process.resourcesPath, 'flobot');
    if (fs.existsSync(packaged)) return packaged;
  }
  // Dev: repo root binary or PATH
  const repoBin = path.join(__dirname, '..', 'flobot');
  if (fs.existsSync(repoBin)) return repoBin;
  return 'flobot';
}

function log(...args) {
  const line = args.map((a) => (typeof a === 'string' ? a : JSON.stringify(a))).join(' ') + '\n';
  try {
    // Truncate the log file when it grows past the cap so it can never fill /tmp.
    try {
      const st = fs.statSync(LOG_PATH);
      if (st.size > LOG_MAX_BYTES) fs.writeFileSync(LOG_PATH, '');
    } catch {}
    fs.appendFileSync(LOG_PATH, line);
  } catch {}
  process.stdout.write(line);
}

// --- server health / port helpers -----------------------------------------

function probe(port) {
  return new Promise((resolve) => {
    const url = `http://127.0.0.1:${port}/health`;
    const req = http.get(url, { timeout: HEALTH_TIMEOUT_MS }, (res) => {
      res.resume();
      resolve(res.statusCode === 200);
    });
    req.on('timeout', () => { req.destroy(); resolve(false); });
    req.on('error', () => resolve(false));
  });
}

function isHealthy(port) {
  return probe(port);
}

function freePort() {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.on('error', reject);
    srv.listen(0, '127.0.0.1', () => {
      const { port } = srv.address();
      srv.close(() => resolve(port));
    });
  });
}

// Decide which port to serve on: reuse a healthy existing server on the
// default port (e.g. a dev `flobot serve`), otherwise take the default port
// if free, otherwise a random free port.
async function choosePort() {
  if (await isHealthy(DEFAULT_PORT)) {
    log(`reusing healthy flobot server on 127.0.0.1:${DEFAULT_PORT}`);
    return { port: DEFAULT_PORT, external: true };
  }
  const inUse = await new Promise((resolve) => {
    const srv = net.createServer();
    srv.once('error', () => resolve(true));
    srv.once('listening', () => srv.close(() => resolve(false)));
    srv.listen(DEFAULT_PORT, '127.0.0.1');
  });
  if (!inUse) return { port: DEFAULT_PORT, external: false };
  const port = await freePort();
  log(`port ${DEFAULT_PORT} busy; using ${port}`);
  return { port, external: false };
}

// --- server lifecycle -------------------------------------------------------

function workspaceDir() {
  // The Code panel edits this directory. An explicit FLOBOT_WORKSPACE wins;
  // otherwise the last folder chosen in the Code panel wins, so a runtime
  // switch survives a relaunch; otherwise default to ~/flobot-workspace so a
  // GUI launch does not fall back to the spawn cwd (literally "/" for a
  // double-clicked app).
  const env = (process.env.FLOBOT_WORKSPACE || '').trim();
  if (env) {
    try { fs.mkdirSync(env, { recursive: true }); } catch {}
    return env;
  }
  const recent = latestWorkspace();
  if (recent) return recent;
  const dir = path.join(os.homedir(), 'flobot-workspace');
  try { fs.mkdirSync(dir, { recursive: true }); } catch {}
  return dir;
}

// latestWorkspace returns the most recently selected Code Studio folder from
// ~/.flobot/config.json, or null when unset or the folder no longer exists.
// The server persists this list on POST /api/v1/workspace/select; the desktop
// shell reads it so the picker choice survives an app restart.
function latestWorkspace() {
  try {
    const cfgPath = path.join(os.homedir(), '.flobot', 'config.json');
    const cfg = JSON.parse(fs.readFileSync(cfgPath, 'utf8'));
    const dir = (cfg.workspaces || [])[0];
    if (!dir || !fs.existsSync(dir) || !fs.statSync(dir).isDirectory()) return null;
    return dir;
  } catch {
    return null;
  }
}

// guiPath returns the login shell's PATH plus the usual user install dirs.
// A GUI launch inherits a PATH without ~/.local/bin, so flocode would not
// be found by the server's PATH lookup.
function guiPath() {
  const extra = [
    path.join(os.homedir(), '.local', 'bin'),
    '/opt/homebrew/bin',
    '/usr/local/bin',
  ];
  let shellPath = '';
  try {
    shellPath = require('child_process')
      .execFileSync(process.env.SHELL || '/bin/zsh', ['-lc', 'echo $PATH'], { encoding: 'utf8', timeout: 5000 })
      .trim();
  } catch {}
  const parts = [...new Set([...shellPath.split(':'), ...extra, ...(process.env.PATH || '').split(':')].filter(Boolean))];
  return parts.join(':');
}

function startServer(port) {
  return new Promise((resolve, reject) => {
    const bin = flobotBinary();
    const ws = workspaceDir();
    log('spawning', bin, 'serve', `127.0.0.1:${port}`, 'workspace', ws);
    serverProc = spawn(bin, ['serve', '--addr', `127.0.0.1:${port}`, '--workspace', ws], {
      stdio: ['ignore', 'pipe', 'pipe'],
      env: { ...process.env, FLOBOT_WORKSPACE: ws, PATH: guiPath() },
    });
    serverProc.on('error', (err) => {
      log('spawn error', String(err));
      serverProc = null;
      reject(err);
    });
    serverProc.stdout.on('data', (d) => log('[flobot:out]', String(d).trim()));
    serverProc.stderr.on('data', (d) => log('[flobot:err]', String(d).trim()));
    serverProc.on('exit', (code) => {
      log('flobot serve exited', code);
      serverProc = null;
      if (quitting) return;
      // Crash mid-session: restart with backoff, then reload the window.
      scheduleRestart();
    });
    resolve();
  });
}

function scheduleRestart() {
  const delay = RESTART_BACKOFF_MS[Math.min(restartAttempt, RESTART_BACKOFF_MS.length - 1)];
  restartAttempt += 1;
  log(`restarting flobot serve in ${delay}ms (attempt ${restartAttempt})`);
  if (restartTimer) clearTimeout(restartTimer);
  restartTimer = setTimeout(async () => {
    restartTimer = null;
    try {
      await startServer(portFromUrl());
      await waitForServer(portFromUrl());
      restartAttempt = 0;
      if (win) win.webContents.reload();
    } catch (err) {
      log('restart failed', String(err));
      scheduleRestart();
    }
  }, delay);
}

function portFromUrl() {
  return baseUrl ? Number(new URL(baseUrl).port) : DEFAULT_PORT;
}

function waitForServer(port, timeoutMs = START_TIMEOUT_MS) {
  const deadline = Date.now() + timeoutMs;
  return new Promise((resolve, reject) => {
    (function ping() {
      const req = http.get(`http://127.0.0.1:${port}/health`, { timeout: HEALTH_TIMEOUT_MS }, (res) => {
        res.resume();
        if (res.statusCode === 200) return resolve();
        retry();
      });
      req.on('timeout', () => { req.destroy(); retry(); });
      req.on('error', retry);
      function retry() {
        if (Date.now() > deadline) return reject(new Error('flobot server did not become healthy'));
        setTimeout(ping, 250);
      }
    })();
  });
}

async function stopServer() {
  if (restartTimer) { clearTimeout(restartTimer); restartTimer = null; }
  if (!serverProc) return;
  const proc = serverProc;
  serverProc = null;
  await new Promise((resolve) => {
    let settled = false;
    let killTimer = null;
    const done = () => { if (!settled) { settled = true; if (killTimer) clearTimeout(killTimer); resolve(); } };
    // If the proc already exited (exitCode set), 'exit' will never fire again.
    if (proc.exitCode !== null || proc.signalCode) return done();
    killTimer = setTimeout(() => { try { proc.kill('SIGKILL'); } catch {} done(); }, 3000);
    proc.once('exit', done);
    try { proc.kill('SIGTERM'); } catch { done(); }
  });
}

// --- window -----------------------------------------------------------------

function createWindow() {
  win = new BrowserWindow({
    width: 1440,
    height: 920,
    minWidth: 960,
    minHeight: 640,
    title: 'flobot',
    // The UI is dark-first ("Obsidian Deck"). Tell macOS so the window
    // chrome, title bar and traffic lights render in dark mode instead of
    // flashing a light title bar over a near-black deck.
    backgroundColor: '#08090c',
    titleBarStyle: 'hiddenInset',
    trafficLightPosition: { x: 18, y: 18 },
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
    },
  });

  // The hidden-inset title bar overlaps the top bar, so reserve room for the
  // traffic lights: pad the top bar's left edge and let the brand sit after
  // them. The web layer handles the rest via a CSS class on <body>.
  win.webContents.on('did-finish-load', () => {
    win.webContents.executeJavaScript(
      'document.body.classList.add("desktop-chrome")',
    ).catch(() => {});
  });

  // Open external links in the default browser, not the app window.
  win.webContents.setWindowOpenHandler(({ url: target }) => {
    if (target.startsWith(baseUrl)) return { action: 'allow' };
    shell.openExternal(target);
    return { action: 'deny' };
  });

  win.loadURL(baseUrl);
  win.on('closed', () => (win = null));
  // Looking at the window is what clears the badge: the count exists to tell
  // you about runs you did not watch, so it must not outlive your attention.
  win.on('focus', () => setUnseen(0));
  win.webContents.on('did-finish-load', () => refreshTrayMenu());
}

// --- workspace folder picker (Code Studio) ----------------------------------

// The Code panel asks for an absolute path; we show the native folder
// chooser and hand the result back. The renderer then POSTs it to
// /api/v1/workspace/select, which also persists it as the default for the
// next launch — so nothing here needs to touch the server.
ipcMain.handle('flobot:pick-folder', async () => {
  const res = await dialog.showOpenDialog(win, {
    title: 'Choose workspace folder',
    buttonLabel: 'Open here',
    properties: ['openDirectory', 'createDirectory'],
  });
  if (res.canceled || !res.filePaths.length) return null;
  return res.filePaths[0];
});

// --- run-completion notifications (Code Studio AC-2e) ----------------------

// setUnseen updates the dock badge and tray title. A count of zero clears both:
// the badge means "something happened that you have not looked at", so it must
// disappear the moment you look.
function setUnseen(n) {
  unseenRuns = Math.max(0, n);
  try {
    if (app.dock) app.dock.setBadge(unseenRuns > 0 ? String(unseenRuns) : '');
  } catch {}
  if (tray) {
    try { tray.setTitle(unseenRuns > 0 ? String(unseenRuns) : ''); } catch {}
  }
}

function focusWindow() {
  if (!win) return;
  if (win.isMinimized()) win.restore();
  win.show();
  win.focus();
}

// showRunNotification announces a batch of finished runs in one banner.
// Payload carries only counts and statuses — never content, never secrets.
function showRunNotification(batch) {
  const failed = batch.filter((r) => r.status === 'error').length;
  const stopped = batch.filter((r) => r.status === 'stopped').length;
  const done = batch.length - failed - stopped;
  const files = batch.reduce((n, r) => n + (Number(r.files) || 0), 0);

  let body;
  if (batch.length === 1) {
    const status = batch[0].status;
    const label = status === 'error' ? 'failed' : status === 'stopped' ? 'stopped' : 'finished';
    body = files > 0
      ? `run ${label} · ${files} file${files === 1 ? '' : 's'} changed — review the diff`
      : `run ${label}`;
  } else {
    const parts = [];
    if (done > 0) parts.push(`${done} finished`);
    if (failed > 0) parts.push(`${failed} failed`);
    if (stopped > 0) parts.push(`${stopped} stopped`);
    body = `${batch.length} runs: ${parts.join(', ')}`;
    if (files > 0) body += ` · ${files} files changed`;
  }

  try {
    const n = new Notification({ title: 'flobot', body, silent: false });
    n.on('click', () => {
      focusWindow();
      setUnseen(0);
    });
    n.on('failed', (_e, err) => {
      // A swallowed notification failure is how "it told me nothing" becomes a
      // mystery. Log it so the reason is recoverable.
      log('notification failed', String(err));
    });
    n.show();
  } catch (err) {
    log('notification threw', String(err));
  }
}

// notifyRunEnd shows an OS notification only when the window is not focused
// (flow-protection: no interruption while the user is watching the run).
// Payload carries only {status, files} — never content, never secrets.
// Clicking the notification focuses the window.
ipcMain.on('flobot:run-end', (_evt, payload) => {
  const status = (payload && payload.status) || 'done';
  const files = Number(payload && payload.files) || 0;
  // The badge counts runs that finished without you watching them. A focused
  // window does not mean you were watching: you may be on the Queue tab, or
  // the Code panel may be scrolled away. Only the banner is suppressed when
  // focused — dropping the event here would leave the badge at zero for a run
  // you never saw, which is the one thing the count exists to prevent.
  setUnseen(unseenRuns + 1);
  if (win && win.isFocused()) {
    // You are looking at the app; do not interrupt with a banner.
    return;
  }
  if (!pendingNotify) pendingNotify = [];
  pendingNotify.push({ status, files });
  if (pendingNotify.length === 1) {
    setTimeout(() => {
      const batch = pendingNotify || [];
      pendingNotify = null;
      if (batch.length) showRunNotification(batch);
    }, NOTIFY_COALESCE_MS);
  }
});

// --- tray -------------------------------------------------------------------

// The tray keeps the count visible when the dock is hidden or the app is in the
// background, and gives a one-click way back to the window.
function createTray() {
  if (tray) return;
  try {
    // A template image so macOS tints it for light and dark menu bars. An empty
    // image is a valid template: the title carries the count.
    const img = nativeImage.createEmpty();
    img.setTemplateImage(true);
    tray = new Tray(img);
    tray.setToolTip('flobot');
    tray.on('click', () => {
      focusWindow();
      setUnseen(0);
    });
    refreshTrayMenu();
  } catch (err) {
    log('tray unavailable', String(err));
    tray = null;
  }
}

function refreshTrayMenu() {
  if (!tray) return;
  try {
    tray.setContextMenu(Menu.buildFromTemplate([
      { label: unseenRuns > 0 ? `${unseenRuns} run${unseenRuns === 1 ? '' : 's'} since you looked` : 'No new runs', enabled: false },
      { type: 'separator' },
      { label: 'Open flobot', click: () => { focusWindow(); setUnseen(0); } },
      { label: 'Clear badge', click: () => setUnseen(0) },
      { type: 'separator' },
      { label: 'Quit flobot', click: () => app.quit() },
    ]));
  } catch {}
}

// --- app lifecycle ----------------------------------------------------------

// One app instance at a time: the second launch just focuses the first.
if (!app.requestSingleInstanceLock()) {
  app.quit();
} else {
  app.on('second-instance', () => {
    if (win) {
      if (win.isMinimized()) win.restore();
      win.focus();
    }
  });

  app.whenReady().then(async () => {
    try {
      const { port, external } = await choosePort();
      baseUrl = `http://127.0.0.1:${port}`;
      if (!external) {
        await startServer(port);
        await waitForServer(port);
      }
    } catch (err) {
      const { dialog } = require('electron');
      dialog.showErrorBox('flobot', String(err.message || err));
      app.quit();
      return;
    }
    createWindow();
    createTray();
    app.on('activate', () => {
      if (BrowserWindow.getAllWindows().length === 0) createWindow();
    });
  });
}

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') app.quit();
});

app.on('before-quit', async (e) => {
  if (quitting) return;
  quitting = true;
  // Always cancel a pending restart, even if no server proc is live right now —
  // otherwise it would spawn a fresh server after we decided to quit.
  if (restartTimer) { clearTimeout(restartTimer); restartTimer = null; }
  if (!serverProc) return;
  e.preventDefault();
  await stopServer();
  app.exit(0);
});
