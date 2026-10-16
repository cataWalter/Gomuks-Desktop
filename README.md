# Gomuks Desktop

This desktop client embeds gomuks' `pkg/hicli` engine in a Wails v3 host with a
React and TypeScript interface. It stores the gomuks SQLite database and a
private Olm pickle key under the current user's configuration directory. On
first launch, existing Fyne data, preferences, and encryption keys are migrated
to the current app data directory; the legacy database is copied with SQLite's
online backup API.

The local SQLite cache contains message data, including decrypted event
contents. The application restricts its data directory to the current user;
use operating-system disk encryption for protection at rest. gomuks is
AGPL-3.0-or-later, so review its license obligations before distributing an
application that embeds it.

## Run locally

The desktop app targets macOS 12 or newer and requires Go 1.26 or newer, Node.js
22 or newer with npm, and the Xcode Command Line Tools. The `sqlite_fts5` build
tag is required. The `goolm` tag selects mautrix-go's pure-Go Olm implementation,
so libolm does not need to be installed.

From the repository root, install the frontend dependencies, build the frontend,
and launch the desktop app:

```sh
npm --prefix frontend ci
npm --prefix frontend run build
go run -tags "sqlite_fts5 goolm" .
```

The Go command stays attached to the terminal while the app is running. Quit
with `Cmd+Q` or stop it with `Ctrl+C`. On later launches, run only the Go
command; rebuild the frontend first whenever its source or dependencies change.

For live development with the Wails frontend and Go hot-reload workflow, install
the Wails CLI version matching `go.mod` if needed, then run:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.30
wails3 task dev
```

This development command starts the Vite server and desktop app together; use
it instead of the direct `go run` command while developing.

Matrix sync events are handled off the UI thread and sent to the React interface
through Wails runtime events.

Room and member profile images are downloaded from their Matrix content URIs,
cached in memory for the current app session, and resized for display. Missing
or unavailable images use a generated initials icon instead.

The sidebar includes a persisted **Needs reply** view. Marking a room answered
stores the latest event ID in the local SQLite database; a later incoming event
can put the room back in that view. `E` marks the selected room answered, and
`Ctrl+Z` / `⌘Z` restores the most recently dismissed room. Triage shortcuts are
ignored while the message composer is focused. The chat header can hide or
restore the room sidebar, and its **Details** control opens an optional panel
with the selected room's topic and members.

Messages keep **Reply** and quick reaction controls visible; the **More** menu
contains context-specific actions such as edit, delete, retry, preview, and
copy.

Open **Settings** from the sidebar or with `Ctrl+,` / `⌘,` to configure theme,
timeline avatars and clock format, notification privacy, and the Enter-to-send
behavior. The Storage tab reports the SQLite database/WAL size, can clear the
in-memory avatar cache, and can run SQLite `VACUUM`. This client currently does
not keep a disk media cache.

The composer also has an emoji picker and accepts pasted images and files
dropped onto the window. The system tray can reopen the window, mark rooms read,
or quit; its icon reflects unread activity. Closing the window can minimize the
app to the tray, and the startup preference registers the app with the host OS.
Clicking a notification focuses the app and opens the relevant room.

Pushing a `v*` tag runs the standalone release workflow. It builds a universal
macOS app bundle, then attaches the archive and a SHA-256 checksum file to the
GitHub Release.

Build the frontend before compiling the Go app. The focused backend checks do not
launch a desktop window:

```sh
npm --prefix frontend ci
npm --prefix frontend run build
go test -tags "sqlite_fts5 goolm" ./desktop ./matrix ./migration
go vet -tags "sqlite_fts5 goolm" ./desktop ./matrix ./migration
go build -tags "sqlite_fts5 goolm" .
```

Tagged versions (`v*`) trigger the GitHub Actions release workflow. It builds
a universal macOS app archive on `macos-latest`, then publishes it with a
`checksums.txt` SHA-256 manifest. Release builds
embed the version, commit, and build timestamp; builds outside the release
workflow report `dev` as their version.
