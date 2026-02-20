# README

## About

This is the official Wails Svelte-TS template.

## Live Development

To run in live development mode, run `wails dev` in the project directory. This will run a Vite development
server that will provide very fast hot reload of your frontend changes. If you want to develop in a browser
and have access to your Go methods, there is also a dev server that runs on http://localhost:34115. Connect
to this in your browser, and you can call your Go code from devtools.

### Install Native Dependencies

Before running `wails dev`, install native dependencies once:

- macOS / Linux:

```bash
./scripts/install-native-deps.sh
```

- Windows (PowerShell):

```powershell
.\scripts\install-native-deps.ps1
```

## Building

To build a redistributable, production mode package, use `wails build`.
