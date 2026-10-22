'use strict';

const { contextBridge } = require('electron');

// The web UI talks to the flobot HTTP API directly on 127.0.0.1:4120,
// so no privileged bridge is needed. Expose a minimal marker for debugging.
contextBridge.exposeInMainWorld('flobotDesktop', {
  desktop: true,
  version: process.env.npm_package_version || '0.1.0',
});
