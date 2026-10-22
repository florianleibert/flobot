# flobot desktop (Electron)

Wraps the flobot web UI in an Electron desktop app. The Electron main process
spawns the `flobot` binary (`flobot serve --addr 127.0.0.1:4120`), waits for
`/health`, then loads the UI in a `BrowserWindow`.

## Run (dev)

```bash
# from repo root: build the server binary first
go build -o flobot ./cmd/flobot

cd desktop
npm install
npm start
```

The main process looks for the binary in this order:

1. `Resources/flobot` (packaged app)
2. `../flobot` (repo root, dev)
3. `flobot` on `PATH`

## Package

```bash
go build -o flobot ./cmd/flobot
cd desktop
npm run dist
```

`electron-builder` bundles the binary as an extra resource; output lands in
`desktop/dist`.
