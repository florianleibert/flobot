'use strict';

const { contextBridge, ipcRenderer } = require('electron');

// The web UI talks to the flobot HTTP API directly on 127.0.0.1:4120,
// so no privileged bridge is needed for data. Expose a minimal marker
// for debugging plus one IPC channel: run-completion notifications
// (Code Studio AC-2e). The renderer sends only a status string and an
// optional file count — never content, never secrets.
contextBridge.exposeInMainWorld('flobotDesktop', {
  desktop: true,
  version: process.env.npm_package_version || '0.1.0',
  // notifyRunEnd asks the main process to show an OS notification when
  // the window is not focused. Payload is {status, files} — status is
  // one of done|error|stopped, files is an integer diffstat (or 0).
  notifyRunEnd: (payload) => {
    try {
      ipcRenderer.send('flobot:run-end', {
        status: String(payload && payload.status || 'done'),
        files: Number(payload && payload.files) || 0,
      });
    } catch {}
  },
});
