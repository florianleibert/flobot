'use strict';

const { app, BrowserWindow, shell, ipcMain, Notification, dialog } = require('electron');
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
  // otherwise default to ~/flobot-workspace so a GUI launch does not fall
  // back to the spawn cwd (literally "/" for a double-clicked app).
  const env = (process.env.FLOBOT_WORKSPACE || '').trim();
  const dir = env || path.join(os.homedir(), 'flobot-workspace');
  try { fs.mkdirSync(dir, { recursive: true }); } catch {}
  return dir;
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
    width: 1280,
    height: 860,
    title: 'flobot',
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
    },
  });

  // Open external links in the default browser, not the app window.
  win.webContents.setWindowOpenHandler(({ url: target }) => {
    if (target.startsWith(baseUrl)) return { action: 'allow' };
    shell.openExternal(target);
    return { action: 'deny' };
  });

  win.loadURL(baseUrl);
  win.on('closed', () => (win = null));
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

// notifyRunEnd shows an OS notification only when the window is not
// focused (flow-protection: no interruption while the user is watching
// the run). Payload carries only {status, files} — never content, never
// secrets. Clicking the notification focuses the window.
ipcMain.on('flobot:run-end', (_evt, payload) => {
  if (!win || win.isFocused()) return; // silent while focused
  const status = payload && payload.status || 'done';
  const files = Number(payload && payload.files) || 0;
  const label = status === 'error' ? 'failed'
    : status === 'stopped' ? 'stopped' : 'finished';
  const body = files > 0
    ? `run ${label} · ${files} file${files === 1 ? '' : 's'} changed — review the diff`
    : `run ${label}`;
  try {
    const n = new Notification({ title: 'flobot', body, silent: false });
    n.on('click', () => {
      if (win) { if (win.isMinimized()) win.restore(); win.focus(); }
    });
    n.show();
  } catch {}
});

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
