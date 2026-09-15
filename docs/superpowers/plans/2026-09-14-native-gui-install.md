# Native GUI Installation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `make install` install both the `tsession` CLI and the Wails GUI artifact adjacent to it under `$(PREFIX)`.

**Architecture:** Preserve the existing `install: build gui` dependency and add a platform-selecting install recipe based on `go env GOOS`. macOS copies the complete `.app` bundle; Linux and Windows install the generated executable. Documentation describes the resulting paths and the launcher's adjacent lookup behavior.

**Tech Stack:** GNU/BSD Make, POSIX shell, Go toolchain, Wails v2, Markdown.

## Global Constraints

- The default destination remains `PREFIX ?= $(HOME)/.local/bin`.
- `make install` must not require `sudo` or write to system application directories.
- `make gui` remains build-only.
- The GUI artifact must be installed adjacent to the `tsession` CLI.
- A missing platform-specific Wails artifact must fail installation explicitly.
- Existing untracked Wails-generated directories must not be committed.

---

### Task 1: Install the native GUI beside the CLI

**Files:**
- Modify: `Makefile`
- Modify: `README.md`
- Modify: `gui/README.md`

**Interfaces:**
- Consumes: Wails output under `gui/build/bin/` and `go env GOOS`.
- Produces: `$(PREFIX)/tsession` plus `$(PREFIX)/TSession.app`, `$(PREFIX)/TSession`, or `$(PREFIX)/TSession.exe`, depending on the host platform.

- [ ] **Step 1: Verify the current install target omits the GUI artifact**

Run on macOS:

```bash
tmp="$(mktemp -d)"
make install PREFIX="$tmp"
test -x "$tmp/tsession"
test -d "$tmp/TSession.app"
```

Expected: the final `test -d` command fails because the current recipe installs only `tsession`.

- [ ] **Step 2: Add platform-specific GUI installation**

Update the `install` recipe in `Makefile` to:

```make
install: build gui
	mkdir -p $(PREFIX)
	install -m 0755 $(BIN) $(PREFIX)/$(BIN)
	@case "$$(go env GOOS)" in \
		darwin) \
			test -d gui/build/bin/TSession.app || { echo "error: GUI artifact gui/build/bin/TSession.app was not produced" >&2; exit 1; }; \
			rm -rf "$(PREFIX)/TSession.app"; \
			cp -R gui/build/bin/TSession.app "$(PREFIX)/TSession.app"; \
			;; \
		windows) \
			test -f gui/build/bin/TSession.exe || { echo "error: GUI artifact gui/build/bin/TSession.exe was not produced" >&2; exit 1; }; \
			install -m 0755 gui/build/bin/TSession.exe "$(PREFIX)/TSession.exe"; \
			;; \
		*) \
			test -f gui/build/bin/TSession || { echo "error: GUI artifact gui/build/bin/TSession was not produced" >&2; exit 1; }; \
			install -m 0755 gui/build/bin/TSession "$(PREFIX)/TSession"; \
			;; \
	esac
```

The targeted `rm -rf` is limited to the fully resolved installed application
bundle path and is needed to prevent stale files from a previous macOS bundle.

- [ ] **Step 3: Verify installation into a temporary prefix**

Run:

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

Expected: all commands exit successfully.

- [ ] **Step 4: Document the combined installation**

Change the root `README.md` install example to:

```markdown
make install    # installs the CLI and native GUI under ~/.local/bin
make gui        # builds the native Wails GUI app without installing it
```

In `gui/README.md`, replace the manual placement guidance with:

```markdown
`make install` from the repository root builds and installs both artifacts
under `PREFIX` (default `~/.local/bin`):

- macOS: `tsession` and `TSession.app`
- Linux: `tsession` and `TSession`
- Windows: `tsession` and `TSession.exe`

The GUI is installed adjacent to the CLI, which is the first location checked
by `tsession gui`. Use `make gui` when only a build is wanted.
```

- [ ] **Step 5: Run repository validation**

Run:

```bash
go test ./...
(cd gui && go test ./...)
git diff --check
```

Expected: both test suites pass and `git diff --check` reports no errors.

- [ ] **Step 6: Commit the implementation**

```bash
git add Makefile README.md gui/README.md
git commit -m "build: install native GUI with CLI" \
  -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```
