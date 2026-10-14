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

Sync events are handled off the GUI thread and all resulting widget updates are
scheduled with `fyne.Do`.

To compile and run tests without a native desktop display driver:

```sh
go test -tags "ci sqlite_fts5 goolm" ./...
```
