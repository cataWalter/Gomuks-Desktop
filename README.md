# Pinger

[![CI](https://github.com/cataWalter/pinger/actions/workflows/ci.yml/badge.svg)](https://github.com/cataWalter/pinger/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/cataWalter/pinger)](https://github.com/cataWalter/pinger/releases)
[![Go 1.26+](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Wails v3](https://img.shields.io/badge/Wails-v3.0.0_beta.30-ff5e14)](https://wails.io)
[![License: AGPL-3.0+](https://img.shields.io/badge/license-AGPL--3.0%2B-blue.svg)](#license)
[![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Windows-lightgrey)](#run-locally)

Desktop client for [gomuks](https://github.com/gomuks) — a Matrix client with SQLite caching, media storage, and a terminal interface. Pinger embeds gomuks' `pkg/hicli` engine in a [Wails v3](https://wails.io) (Go) host with a React + TypeScript interface, and also ships the gomuks terminal client.

> **License note:** This application embeds `gomuks`' `pkg/hicli` engine. Review the AGPL-3.0-or-later license obligations before distributing.

## Contents

- [Features](#features)
- [Keyboard shortcuts](#keyboard-shortcuts)
- [Data storage & privacy](#data-storage--privacy)
- [Run locally](#run-locally)
- [Project structure](#project-structure)
- [Development & checks](#development--checks)
- [Releases](#releases)
- [License](#license)

## Features

**Triage & workflow**

- **Needs reply** sidebar view, persisted in SQLite. Mark a room answered with `E` (configurable in Settings → Keyboard); `Ctrl+Z` / `⌘Z` restores the most recently dismissed room. Triage shortcuts are ignored while the composer is focused.
- **Quick switcher** (`Ctrl+K` / `⌘K`) jumps to any room by name; **message search** (`Ctrl+F` / `⌘F`) searches local history across rooms; `/` focuses room search.
- **Room filters & navigation**: `Ctrl+Shift+U` shows only unanswered rooms, `Ctrl+Shift+A` shows all; `Ctrl+1`–`Ctrl+9` jump to a room by list position; `Alt+↑`/`Alt+↓` cycle rooms; `Ctrl+Shift+L` jumps to the latest message.

**Messaging**

- Reply and quick-reaction controls stay visible; replying opens a threaded quote composer; reactions show as chips. The **More** menu offers Set reminder, edit, delete, retry, preview, and copy.
- The composer accepts pasted images and files dropped onto the window.

**Room details & media**

- The header toggles the sidebar; **Details** opens a panel with the room topic and members.
- Room/member avatars and attachments are downloaded from Matrix content URIs and stored in the on-disk media cache; avatars are also cached in memory for the session and resized for display. Missing images use a generated initials icon.

**System tray & notifications**

- Reopen the window, mark rooms read, or quit; the tray icon reflects unread activity. Closing the window can minimize to tray; the startup preference registers the app with the OS. Clicking a notification focuses the app and opens the relevant room.

**Settings** (`Ctrl+,` / `⌘,`)

- **Account** (profile, logout) · **Appearance** (theme, outgoing message color, font size, avatars, compact layout, clock format) · **Notifications** (permission, alerts only when unfocused, sound/volume, privacy) · **Keyboard** (Ctrl+Enter to send, mark-answered shortcut, full shortcut list) · **History** (messages loaded when a room opens) · **Devices** (list/revoke) · **Storage** (DB/WAL/media sizes, clear media cache, VACUUM) · **Security** (verification status, room-key export/import with passphrase) · **About** (version, build date).

## Keyboard shortcuts

Press `?` in the app (or open Settings → Keyboard) for the authoritative list.

| Shortcut | Action |
| --- | --- |
| `E` *(configurable)* | Mark the selected room answered |
| `Ctrl+Z` / `⌘Z` | Undo — restore the last answered room |
| `?` | Open Settings → Keyboard (full shortcut list) |
| `/` or `Ctrl+Shift+S` / `⌘⇧S` | Focus room search |
| `Ctrl+K` / `⌘K` | Open the quick switcher |
| `Ctrl+F` / `⌘F` | Search local message history across rooms |
| `Ctrl+,` / `⌘,` | Open Settings |
| `Alt+↑` / `Alt+↓` | Previous / next room |
| `Ctrl+Shift+↑` / `Ctrl+Shift+↓` | Previous / next unanswered room |
| `Ctrl+Shift+U` | Filter to unanswered rooms only |
| `Ctrl+Shift+A` | Show all rooms (clear filters) |
| `Ctrl+Shift+L` | Jump to the latest message |
| `Ctrl+1` … `Ctrl+9` | Jump to room by list position |
| `Ctrl+Enter` / `⌘↩` | Send message *(toggleable in Settings → Keyboard)* |

## Data storage & privacy

| | macOS | Windows | Linux |
| --- | --- | --- | --- |
| **Data directory** | `~/Library/Application Support/gomuks-desktop` | `%AppData%\gomuks-desktop` | `~/.config/gomuks-desktop` |
| **Media cache** | `~/Library/Caches/gomuks-desktop/media` | `%LocalAppData%\gomuks-desktop\media` | `~/.cache/gomuks-desktop/media` |

- The data directory holds the SQLite database (`matrix.db`), app preferences (`preferences.json`), and the Olm pickle key (`crypto.key`).
- The local SQLite cache stores message data, including decrypted event contents. Downloaded media is cached under the user's cache directory; encrypted payloads are decrypted before caching, and cache files are owner-only. Clearing the media cache (Settings → Storage) removes only downloaded media — use OS disk encryption for protection at rest.
- On first launch, data from the legacy Fyne build (`fyne-matrix` data directory and `fi.mau.fyne-matrix` preferences) is migrated into the current data directory; the legacy database is copied with SQLite's online backup API.

## Run locally

Targets macOS 12+ and Windows x64 with the WebView2 Runtime.

### Prerequisites

- [Go 1.26+](https://go.dev/dl/)
- [Node.js 22+](https://nodejs.org/) with npm
- Wails CLI **v3.0.0-beta.30** (must match `go.mod`):
  `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.30`
- macOS: Xcode Command Line Tools (`xcode-select --install`)
- Windows: [WebView2 Runtime](https://developer.microsoft.com/microsoft-edge/webview2/)

Two Go build tags are used throughout: `sqlite_fts5` is required (SQLite full-text search), and `goolm` selects mautrix-go's pure-Go Olm implementation (no libolm needed).

### Build and run

From the repository root:

```sh
wails3 generate bindings -f '-tags sqlite_fts5,goolm' -clean=true -ts -i
npm --prefix frontend ci
npm --prefix frontend run build
go run -tags "sqlite_fts5 goolm" .
```

- The Go command stays attached to the terminal — quit with `Cmd+Q` or `Ctrl+C`. On later launches, run only the Go command (rebuild the frontend first if it changed).
- Rebuild the frontend when its source or dependencies change — assets stay embedded in the binary. Regenerate bindings after Go service method changes; `wails3 task dev` does both.
- Matrix sync events are handled off the UI thread and delivered to the React interface through Wails runtime events.

### Terminal interface

Without the desktop frontend:

```sh
go run -tags "sqlite_fts5 goolm" ./cmd/gomuks-tui
```

The desktop binary also embeds the terminal client behind a `tui` subcommand:

```sh
go run -tags "sqlite_fts5 goolm" . tui
```

### Live development

```sh
wails3 task dev
```

Hot-reloads the frontend via Vite and rebuilds the Go app on change.

## Project structure

| Path | Purpose |
| --- | --- |
| `main.go` | Entry point — embeds the frontend, migrates legacy data, starts the desktop app or TUI |
| `desktop/` | Wails application: services, preferences, system tray, scheduled reminders, attachments |
| `frontend/` | React + TypeScript UI (Vite build, vitest tests) |
| `matrix/` | Matrix client wrapper around gomuks' `hicli` engine |
| `migration/` | First-run migration from the legacy Fyne app |
| `tui/` | Terminal interface (bubbletea) |
| `cmd/gomuks-tui/` | Standalone TUI binary |
| `build/` | Wails build configuration and platform assets |
| `assets/` | Application icons |
| `.github/workflows/` | CI and release pipelines |

## Development & checks

Build the frontend before compiling Go. The same checks run in CI on every push to `main` and pull request; backend checks don't launch a desktop window:

```sh
npm --prefix frontend ci
npm --prefix frontend test
npm --prefix frontend run build
go test -tags "sqlite_fts5 goolm" ./desktop ./matrix ./migration ./tui
go vet -tags "sqlite_fts5 goolm" ./desktop ./matrix ./migration ./tui
go build -tags "sqlite_fts5 goolm" .
go build -tags "sqlite_fts5 goolm" ./cmd/gomuks-tui
```

Press `?` in the app for the full keyboard shortcuts list.

## Releases

Pushing a `v*` tag runs the standalone release workflow. Tagged versions (`v*`) trigger GitHub Actions, which builds universal (arm64 + amd64) macOS binaries on `macos-latest` for the desktop app and standalone TUI, and x64 Windows binaries on `windows-latest`, and x64 Linux binaries on `ubuntu-latest`; the desktop executable embeds its icon and manifest. Artifacts: `Pinger-<version>-macos-universal.dmg`, `Pinger-TUI-<version>-macos-universal.dmg`, `Pinger-<version>-windows-amd64.zip`, `Pinger-TUI-<version>-windows-amd64.zip`, `Pinger-<version>-linux-amd64.deb`, `Pinger-<version>-linux-amd64.AppImage`. The macOS DMG contains the signed app plus an `/Applications` symlink; the TUI DMG contains the `gomuks-tui` binary plus a `bin` symlink to `/usr/local/bin`. Releases include a `checksums.txt` SHA-256 manifest. Desktop release builds embed version, commit, and build timestamp; other builds report `dev`. Manually dispatching the workflow uploads DMGs/ZIPs as artifacts; only a version-tag run publishes a GitHub Release.
