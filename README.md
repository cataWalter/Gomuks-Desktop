# Fyne Matrix client skeleton

This desktop client embeds gomuks' `pkg/hicli` engine directly and uses Fyne v2
for the native interface. It stores the gomuks SQLite database and a private
Olm pickle key under the current user's configuration directory.

The local SQLite cache contains message data, including decrypted event
contents. The application restricts its data directory to the current user;
use operating-system disk encryption for protection at rest. gomuks is
AGPL-3.0-or-later, so review its license obligations before distributing an
application that embeds it.

The current gomuks release requires Go 1.26 or newer and its SQLite FTS5 build
tag. For the desktop build, Fyne needs native OpenGL and window-system
development libraries on Linux. The `goolm` tag selects mautrix-go's pure-Go
Olm implementation if libolm is not installed.

Run with:

```sh
go run -tags "sqlite_fts5 goolm" .
```

In a Linux VNC desktop, make sure its X server is running and launch the app
with that display selected. This workspace's VNC setup uses display `:1`:

```sh
export DISPLAY=:1
go run -tags "sqlite_fts5 goolm" .
```

If both `DISPLAY` and `WAYLAND_DISPLAY` are unset, the app exits with a
diagnostic instead of attempting to initialize Fyne's desktop driver.

Sync events are handled off the GUI thread and all resulting widget updates are
scheduled with `fyne.Do`.

Room and member profile images are downloaded from their Matrix content URIs,
cached in memory for the current app session, and resized for display. Missing
or unavailable images use a generated initials icon instead.

The sidebar includes a persisted **Needs reply** view. Marking a room answered
stores the latest event ID in the local SQLite database; a later incoming event
can put the room back in that view. `E` marks the selected room answered, and
`Ctrl+Z` / `⌘Z` restores the most recently dismissed room. Triage shortcuts are
ignored while the message composer is focused.

Open **Settings** from the sidebar or with `Ctrl+,` / `⌘,` to configure theme,
timeline avatars and clock format, notification privacy, and the Enter-to-send
behavior. The Storage tab reports the SQLite database/WAL size, can clear the
in-memory avatar cache, and can run SQLite `VACUUM`. This client currently does
not keep a disk media cache.

The composer also has an emoji picker and accepts files dropped onto the window.
Where the native desktop driver exposes a system tray, its menu can reopen the
window, mark rooms read, or quit; an unread badge is shown when supported.

Pushing a `v*` tag runs the standalone release workflow. It builds Windows
amd64, Linux amd64/arm64, and a universal macOS app bundle, then attaches
archives and a SHA-256 checksum file to the GitHub Release. The Windows arm64
artifact is not currently produced.

To compile and run tests without a native desktop display driver:

```sh
go test -tags "ci sqlite_fts5 goolm" ./...
```

Tagged versions (`v*`) trigger the GitHub Actions release workflow. It builds
portable Windows and Linux amd64/arm64 archives plus a universal macOS app
archive, then publishes them with a `checksums.txt` SHA-256 manifest. Linux
releases are built against Debian Bullseye's glibc baseline. Release builds
embed the version, commit, and build timestamp; builds outside the release
workflow report `dev` as their version.
