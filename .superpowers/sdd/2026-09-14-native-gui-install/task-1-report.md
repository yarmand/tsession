# Task 1 Report — Native GUI Installation

**Status:** DONE

**Commit:** `0b79e0b1bcd1d12330f601d2010630f6f283f98b`

## Files changed

- `Makefile`
- `README.md`
- `gui/README.md`

## Red validation

Command:

```bash
tmp="$(mktemp -d)"
make install PREFIX="$tmp"
test -x "$tmp/tsession"
test -d "$tmp/TSession.app"
```

Outcome: failed as expected before the fix; the final `test -d` exited non-zero because `make install` did not install the macOS GUI bundle.

## Green validation

Command:

```bash
tmp="$(mktemp -d)"
make install PREFIX="$tmp"
test -x "$tmp/tsession"
case "$(go env GOOS)" in
  darwin) test -x "$tmp/TSession.app/Contents/MacOS/TSession" ;;
  windows) test -x "$tmp/TSession.exe" ;;
  *) test -x "$tmp/TSession" ;;
esac
```

Outcome: passed. The temporary prefix contained the CLI and the platform-specific GUI artifact.

## Repository validation

Commands:

```bash
go test ./...
(cd gui && go test ./...)
git diff --check
```

Outcome:

- `go test ./...` passed
- `(cd gui && go test ./...)` passed
- `git diff --check` passed

## Full validation summary

The install recipe now installs `tsession` plus the native GUI artifact beside it under `PREFIX`, with platform-specific handling for macOS app bundles, Windows executables, and Linux binaries. The repository test suites and diff hygiene checks are clean, and the implementation was committed as requested.

## Self-review concerns

- Wails emits a non-blocking version mismatch warning during GUI builds (`go.mod` references Wails 2.15.0 while the installed CLI is 2.16.0).
- `make install` intentionally rebuilds the GUI prerequisite before installing, which increases install time but matches the brief and preserves the existing build flow.

## Fix round 1/5

**Files changed:** `README.md`

**Exact commands:**

```bash
git status --short
go test ./... && (cd gui && go test ./...) && git diff --check
```

**Outcomes:**

- `git status --short` showed only pre-existing untracked Wails-generated directories: `gui/build/darwin/` and `gui/frontend/dist/wailsjs/`
- repository validation passed

**Commit SHA:** `079e694e0f95733f90ada27a449bdd6ecd881d89`

**Self-review:** The root install instructions now call out the Wails CLI and platform-native webview runtime up front, while still pointing readers to `gui/README.md` for the full prerequisite list.
