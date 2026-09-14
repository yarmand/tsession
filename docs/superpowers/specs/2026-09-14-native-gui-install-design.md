# Native GUI Installation Design

## Goal

Extend `make install` so it installs both the `tsession` CLI and the native
Wails GUI application built by its existing `gui` prerequisite.

## Installation Layout

Install the GUI adjacent to the CLI under `$(PREFIX)`. This matches the first
location checked by `tsession gui` and keeps installation user-local by default.

| Platform | Build artifact | Installed path |
|---|---|---|
| macOS | `gui/build/bin/TSession.app` | `$(PREFIX)/TSession.app` |
| Linux | `gui/build/bin/TSession` | `$(PREFIX)/TSession` |
| Windows-compatible make environment | `gui/build/bin/TSession.exe` | `$(PREFIX)/TSession.exe` |

The default `PREFIX` remains `$(HOME)/.local/bin`, so installation does not
require elevated permissions.

## Makefile Behavior

Keep `install: build gui`, preserving the user's change that builds both
artifacts before installation. The install recipe will:

1. Create `$(PREFIX)`.
2. Install the CLI as `$(PREFIX)/tsession`.
3. Detect the host platform with `go env GOOS`.
4. Copy the macOS application bundle recursively, or install the Linux/Windows
   executable with mode `0755`.
5. Fail with an explicit error if the expected Wails artifact is absent.

`make gui` remains build-only.

## Documentation

Update the root `README.md` installation example to state that `make install`
installs both the CLI and native GUI. Update `gui/README.md` to document the
adjacent paths and that `tsession gui` discovers the installed artifact there.

## Validation

Test installation into a temporary `PREFIX` using the already-built native
artifact, verify the CLI and platform-specific GUI paths exist, and run the
existing Go test suites. The install recipe must not modify system application
directories or require `sudo`.
