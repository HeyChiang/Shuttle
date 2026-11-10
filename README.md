# Shuttle

A desktop SSH manager for working with many servers at once — built with [Wails v3](https://v3.wails.io/), Go and Vue 3.

English | [简体中文](README.zh-CN.md) | [Español](README.es.md)

## Features

- **Saved hosts** — keep connection profiles (host, port, user, password / private key, terminal type, encoding) and group them.
- **Multi-session terminals** — open as many SSH sessions as you need, switch with tabs, split them into a grid layout, or send input to several sessions at once (broadcast mode).
- **SFTP workspace** — a two-pane file manager where each side can independently browse the local disk or a remote server, with drag-and-drop upload, download and server-to-server transfer.
- **Snippets** — store frequently used command scripts and run or paste them into the focused terminal from a side drawer.
- **Encrypted import / export** — back up and restore your host list as a passphrase-protected file.
- **Themes and typography** — light / dark appearance and adjustable terminal font size.
- **Trilingual UI** — English, Simplified Chinese and Spanish, switchable at runtime from Settings → Language.

## Requirements

- Go 1.26+
- Node.js 18+ and npm
- The [Wails v3 CLI](https://v3.wails.io/getting-started/installation/) and [Task](https://taskfile.dev/)

## Development

```bash
# install frontend dependencies
cd frontend && npm install && cd ..

# run in development mode with hot reload
wails3 dev
```

## Build

```bash
wails3 build
```

The executable is written to `bin/`.

## Project layout

```
.
├── main.go              # application entry point and window setup
├── sshservice.go        # Wails service exposed to the frontend
├── internal/sshcore/    # SSH sessions, SFTP, host storage, crypto
├── frontend/src/        # Vue 3 app
│   ├── App.vue          # main window, tabs, host list, settings
│   ├── components/      # terminal pane, SFTP panes, dialogs
│   └── locales/         # en / zh-CN / es message catalogs
└── build/               # platform build assets and Taskfiles
```

## Internationalization

All UI strings live in `frontend/src/locales`. The active language is detected from the browser locale on first launch, then persisted in `localStorage` under `shuttle.locale`. Adding a language means:

1. Create `frontend/src/locales/<locale>.ts` by copying `en.ts`.
2. Register it in `frontend/src/i18n.ts` (`messages` and `localeOptions`).

## Contributing

Issues and pull requests are welcome. Please keep new UI text in the locale catalogs rather than hard-coding it in components.
