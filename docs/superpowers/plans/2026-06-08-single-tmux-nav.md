# Single-tmux Navigation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the multi-client `--target` model with a single self-contained tmux experience where one navigator pane docks beside whichever agent session is selected.

**Architecture:** A `sessions-nav` tmux session laid out as `[nav | main]` is auto-created by `tsession browse`. The `nav` pane (fzf picker) is the only traveler: on selection it `join-pane`s itself to the left of the agent's existing window (preserving its width) and `switch-client`s there; agents are never relocated. Remote sessions hop to their resolved local pane the same way. A lightweight `Repo`/`Worktree` attribution helper is added (no renderer wiring yet).

**Tech Stack:** Go 1.25, tmux, fzf. Build with `go build .`, test with `go test ./...` from the repo root.

---

## File Structure

| Path | Responsibility |
|------|----------------|
| `internal/tmux/tmux.go` | tmux command builders + `NavHop`, `PaneWidth`, simplified `SwitchClient`; multi-client helpers removed. |
| `internal/tmux/tmux_test.go` | Unit tests for the pure arg-builders; obsolete `--target` tests removed. |
| `cmd/resume.go` | Nav-hop selection logic; `--target` removed; remote path hops beside the resolved pane. |
| `cmd/browse.go` | `[nav | main]` `sessions-nav` bootstrap; `--target` removed; `enterBinding` without target; home-hop on exit. |
| `cmd/popup.go` | Updated `runFzf` call (no target arg). |
| `internal/sessions/session.go` | New `Repo` / `Worktree` fields. |
| `internal/sessions/workspace.go` | Git-derivation of repo/worktree labels (pure parser + thin git wrapper). |
| `internal/sessions/workspace_test.go` | Tests for the pure label parser. |
| `main.go`, `README.md`, `AGENTS.md` | Docs/usage updated to the single-tmux flow. |

---

## Task 1: tmux pane primitives (NavHop + arg builders)

**Files:**
- Modify: `internal/tmux/tmux.go`
- Test: `internal/tmux/tmux_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/tmux/tmux_test.go` (add `"reflect"` to the import block — change `import "testing"` to a grouped import):

```go
import (
	"reflect"
	"testing"
)

func TestPaneWidthArgs(t *testing.T) {
	got := paneWidthArgs("%3")
	want := []string{"display-message", "-p", "-t", "%3", "#{pane_width}"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paneWidthArgs = %v, want %v", got, want)
	}
}

func TestJoinPaneLeftArgs_WithSize(t *testing.T) {
	got := joinPaneLeftArgs("%3", "sess:1.2", "82")
	want := []string{"join-pane", "-h", "-b", "-l", "82", "-s", "%3", "-t", "sess:1.2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("joinPaneLeftArgs = %v, want %v", got, want)
	}
}

func TestJoinPaneLeftArgs_NoSize(t *testing.T) {
	got := joinPaneLeftArgs("%3", "sess:1.2", "")
	want := []string{"join-pane", "-h", "-b", "-s", "%3", "-t", "sess:1.2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("joinPaneLeftArgs = %v, want %v", got, want)
	}
}

func TestSwitchClientArgs(t *testing.T) {
	got := switchClientArgs("sess:1.2")
	want := []string{"switch-client", "-t", "sess:1.2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("switchClientArgs = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tmux/ -run 'PaneWidthArgs|JoinPaneLeftArgs|SwitchClientArgs' -v`
Expected: FAIL — `undefined: paneWidthArgs` (and the other builders).

- [ ] **Step 3: Add the builders + NavHop**

In `internal/tmux/tmux.go`, add after the imports/`Pane` definitions (keep `strconv` and `strings` imports, which already exist):

```go
// NavSession is the holding session that owns the navigator pane.
const NavSession = "sessions-nav"

func paneWidthArgs(pane string) []string {
	return []string{"display-message", "-p", "-t", pane, "#{pane_width}"}
}

func joinPaneLeftArgs(src, target, size string) []string {
	args := []string{"join-pane", "-h", "-b"}
	if size != "" {
		args = append(args, "-l", size)
	}
	return append(args, "-s", src, "-t", target)
}

func switchClientArgs(target string) []string {
	return []string{"switch-client", "-t", target}
}

// PaneWidth returns the column width of the given pane.
func PaneWidth(pane string) (int, error) {
	out, err := exec.Command("tmux", paneWidthArgs(pane)...).Output()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

// NavHop moves the navigator pane (navPane, e.g. from $TMUX_PANE) to the left
// of targetWindow, preserving the navigator's current width, then focuses that
// window. targetWindow may be a window or pane target
// (e.g. "sessions-nav:0" or "sess:1.2"). If the navigator is already in the
// target window, join-pane fails harmlessly and the switch-client still runs.
func NavHop(navPane, targetWindow string) error {
	size := "30%"
	if w, err := PaneWidth(navPane); err == nil && w > 0 {
		size = strconv.Itoa(w)
	}
	_ = exec.Command("tmux", joinPaneLeftArgs(navPane, targetWindow, size)...).Run()
	return exec.Command("tmux", switchClientArgs(targetWindow)...).Run()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tmux/ -run 'PaneWidthArgs|JoinPaneLeftArgs|SwitchClientArgs' -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Build**

Run: `go build .`
Expected: success (no output).

- [ ] **Step 6: Commit**

```bash
git add internal/tmux/tmux.go internal/tmux/tmux_test.go
git commit -m "feat(tmux): add NavHop and pane command builders"
```

---

## Task 2: Resume uses nav-hop instead of client switching

**Files:**
- Modify: `cmd/resume.go`
- Test: `cmd/resume_navhop_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `cmd/resume_navhop_test.go`:

```go
package cmd

import (
	"testing"
	"time"

	"github.com/yarma/tsession/internal/cache"
	"github.com/yarma/tsession/internal/sessions"
)

func TestResume_LocalTmuxSessionHopsToTarget(t *testing.T) {
	writeConfigFile(t, "") // sets HOME to a temp dir, no remotes
	if err := cache.Write(cache.File{
		UpdatedAt: time.Now().UTC(),
		Interval:  10 * time.Second,
		Sessions: []sessions.Session{{
			ID:         "local-id",
			TmuxTarget: "work:2.1",
			UpdatedAt:  time.Now().UTC(),
		}},
	}); err != nil {
		t.Fatal(err)
	}

	oldLoadAllLive := loadAllLiveFn
	defer func() { loadAllLiveFn = oldLoadAllLive }()
	loadAllLiveFn = func(maxAge time.Duration) ([]sessions.Session, error) {
		return []sessions.Session{{ID: "local-id", TmuxTarget: "work:2.1", UpdatedAt: time.Now().UTC()}}, nil
	}

	oldHop := switchToPane
	defer func() { switchToPane = oldHop }()
	var gotNav, gotTarget string
	switchToPane = func(navPane, target string) error {
		gotNav, gotTarget = navPane, target
		return nil
	}

	t.Setenv("TMUX_PANE", "%9")
	if err := Resume([]string{"local-id"}); err != nil {
		t.Fatal(err)
	}
	if gotNav != "%9" || gotTarget != "work:2.1" {
		t.Fatalf("switchToPane(%q, %q), want (%q, %q)", gotNav, gotTarget, "%9", "work:2.1")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/ -run TestResume_LocalTmuxSessionHopsToTarget -v`
Expected: FAIL — `undefined: switchToPane`.

- [ ] **Step 3: Edit `cmd/resume.go`**

Add `"github.com/yarma/tsession/internal/tmux"` is already imported. Add this package-level seam after `var execCommand = exec.Command` (line 16):

```go
// switchToPane focuses the agent at target, docking the navigator pane beside
// it when running inside the navigator (navPane non-empty). Swappable in tests.
var switchToPane = func(navPane, target string) error {
	if navPane == "" {
		return tmux.SwitchClient(target)
	}
	return tmux.NavHop(navPane, target)
}
```

Remove the `--target` flag. Change the flag block (lines 19-23) to:

```go
	fs := flag.NewFlagSet("resume", flag.ExitOnError)
	summary := fs.String("summary", "", "session summary (tiebreaker for multi-pane remotes)")
	origin := fs.String("origin", "", "remote origin name (for instant local pane lookup)")
	_ = fs.Parse(args)
```

Update the usage string (line 26) to:

```go
		return fmt.Errorf("usage: tsession resume <session-id>")
```

Immediately after `id := fs.Arg(0)`, add:

```go
	navPane := os.Getenv("TMUX_PANE")
```

Replace the fast-path block (lines 32-40) with:

```go
	if *origin != "" {
		if paneTarget := findLocalPaneForRemote(*origin, *summary); paneTarget != "" {
			if err := switchToPane(navPane, paneTarget); err != nil {
				return err
			}
			_ = donestate.Clear(id)
			return nil
		}
	}
```

Replace the remote local-pane switch (lines 72-82) — the `if match.TmuxTarget != "" || match.TmuxName != ""` block — with:

```go
		if match.TmuxTarget != "" || match.TmuxName != "" {
			tmuxTarget := match.TmuxTarget
			if tmuxTarget == "" {
				tmuxTarget = match.TmuxName
			}
			if err := switchToPane(navPane, tmuxTarget); err != nil {
				return err
			}
			_ = donestate.Clear(id)
			return nil
		}
```

Replace the local tmux switch block (lines 94-104) with:

```go
	if match != nil && (match.TmuxTarget != "" || match.TmuxName != "") {
		tmuxTarget := match.TmuxTarget
		if tmuxTarget == "" {
			tmuxTarget = match.TmuxName
		}
		if err := switchToPane(navPane, tmuxTarget); err != nil {
			return err
		}
		_ = donestate.Clear(id)
		return nil
	}
```

Update the `findLocalPaneForRemote` signature (line 173) to drop the unused target arg:

```go
func findLocalPaneForRemote(origin, summary string) string {
```

> Note: the remote *no-local-pane* fallback (the `remoteResumeArgs` SSH/codespace/docker spawn, lines ~84-91) is left unchanged. It already does not use `--target`, so it stays functional. The design's "open SSH as a new window, then hop" enhancement is deferred — discover-and-hop covers the common case where a local pane already hosts the connection.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/ -run TestResume_LocalTmuxSessionHopsToTarget -v`
Expected: PASS.

- [ ] **Step 5: Run the resume regression tests**

Run: `go test ./cmd/ -run 'Resume|RemoteResumeArgs' -v`
Expected: PASS (existing `TestResume_RemoteSessionUsesSSHFromCache`, `TestRemoteResumeArgs` still green).

- [ ] **Step 6: Commit**

```bash
git add cmd/resume.go cmd/resume_navhop_test.go
git commit -m "feat(resume): dock navigator beside selected agent, drop --target"
```

---

## Task 3: Browse builds the [nav | main] layout and drops --target

**Files:**
- Modify: `cmd/browse.go`
- Modify: `cmd/popup.go`

- [ ] **Step 1: Edit `launchInTmux` in `cmd/browse.go`**

Replace the body of `launchInTmux` (lines 69-91) with a detached bootstrap that creates `[nav | main]` then attaches:

```go
func launchInTmux(browseArgs []string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}

	tmuxCmd := shellQuote(self) + " browse"
	for _, a := range browseArgs {
		tmuxCmd += " " + shellQuote(a)
	}

	home := os.Getenv("HOME")
	if home == "" {
		home = "/"
	}

	// Create the holding session once, laid out as [nav | main]. If it already
	// exists we just attach. new-session must be detached so we can split in the
	// main pane before attaching.
	if exec.Command("tmux", "has-session", "-t", tmux.NavSession).Run() != nil {
		create := exec.Command("tmux", "new-session", "-d", "-s", tmux.NavSession, "-c", home, tmuxCmd)
		create.Stdin, create.Stdout, create.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := create.Run(); err != nil {
			return err
		}
		// main: a plain shell to the right of nav.
		_ = exec.Command("tmux", "split-window", "-h", "-t", tmux.NavSession+":0.0", "-c", home).Run()
		_ = exec.Command("tmux", "select-pane", "-t", tmux.NavSession+":0.0").Run()
	}

	attach := exec.Command("tmux", "attach-session", "-t", tmux.NavSession)
	attach.Stdin, attach.Stdout, attach.Stderr = os.Stdin, os.Stdout, os.Stderr
	return attach.Run()
}
```

- [ ] **Step 2: Drop the `--target` flag and ResolveTarget**

In `Browse` (lines 23-42), remove the `target` flag declaration (line 30) and the ResolveTarget block (lines 38-42). The flag section becomes:

```go
	fs := flag.NewFlagSet("browse", flag.ExitOnError)
	maxAge := fs.Duration("max-age", 14*24*time.Hour, "ignore sessions older than this")
	active := fs.Bool("active", false, "only show sessions attached to tmux with a known, non-exited state")
	short := fs.Bool("short", false, "compact output: state, age, repo basename, summary truncated to 30 chars")
	lshort := fs.Int("lshort", 0, "like --short, but also truncate each output line to N characters")
	localOnly := fs.Bool("local-only", false, "only show local sessions")
	watch := fs.Bool("watch", false, "auto-refresh the list every 5s and keep browsing after each selection")
	_ = fs.Parse(args)
	query := strings.Join(fs.Args(), " ")

	if _, err := exec.LookPath("fzf"); err != nil {
		return fmt.Errorf("fzf not found in PATH (brew install fzf)")
	}
```

Replace the non-watch path (lines 44-53) with:

```go
	if !*watch {
		id, err := runFzf(*maxAge, query, false, *active, *short, *lshort, *localOnly)
		if err != nil {
			return err
		}
		if id == "" {
			navHome()
			return nil
		}
		return Resume([]string{id})
	}

	for {
		id, err := runFzfOpts(*maxAge, query, false, *active, *short, *lshort, *localOnly, true)
		if err != nil {
			return err
		}
		if id == "" {
			navHome()
			return nil
		}
	}
```

- [ ] **Step 3: Add `navHome` and update `runFzf`/`runFzfOpts`/`enterBinding` signatures**

Add `navHome` near `launchInTmux`:

```go
// navHome returns the navigator pane to sessions-nav:0 (beside main) when the
// picker exits, so a docked navigator does not get orphaned in an agent window.
func navHome() {
	if navPane := os.Getenv("TMUX_PANE"); navPane != "" {
		_ = tmux.NavHop(navPane, tmux.NavSession+":0")
	}
}
```

Change `runFzf` (lines 93-95):

```go
func runFzf(maxAge time.Duration, query string, popup, active, short bool, lshort int, localOnly bool) (string, error) {
	return runFzfOpts(maxAge, query, popup, active, short, lshort, localOnly, false)
}
```

Change `runFzfOpts` signature (line 97) to drop the trailing `target string`:

```go
func runFzfOpts(maxAge time.Duration, query string, popup, active, short bool, lshort int, localOnly bool, autoReload bool) (string, error) {
```

Inside `runFzfOpts`, change the `enterBinding` call (line 164) to `enterBinding(self),`.

Change `enterBinding` (lines 281-293) to drop the target parameter:

```go
func enterBinding(self string) string {
	if tmux.InTmux() {
		resumeCmd := shellQuote(self) + " resume"
		// {10} is session origin (remote name), {8} is summary.
		// fzf shell-quotes field replacements automatically.
		resumeCmd += " --origin {10} --summary {8} {2}"
		return "--bind=enter:execute-silent(" + resumeCmd + ")+accept"
	}
	return "--bind=enter:accept"
}
```

- [ ] **Step 4: Update `cmd/popup.go`**

Change the `runFzf` call (line 17) to drop the trailing `""`:

```go
	id, err := runFzf(*maxAge, "", true, *active, *short, *lshort, *localOnly)
```

- [ ] **Step 5: Build**

Run: `go build .`
Expected: success. (`tmux.ResolveTarget` is still defined; it is removed in Task 4.)

- [ ] **Step 6: Run cmd tests**

Run: `go test ./cmd/ -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/browse.go cmd/popup.go
git commit -m "feat(browse): bootstrap sessions-nav as [nav | main], drop --target"
```

---

## Task 4: Remove multi-client plumbing from the tmux package

**Files:**
- Modify: `internal/tmux/tmux.go`
- Modify: `internal/tmux/tmux_test.go`

- [ ] **Step 1: Simplify `SwitchClient` and delete the multi-client helpers**

In `internal/tmux/tmux.go`, replace `SwitchClient` + `SwitchClientTarget` (lines 55-79) with a single function:

```go
// SwitchClient switches the current tmux client to the named target, or
// attaches if invoked from outside tmux.
func SwitchClient(name string) error {
	if !InTmux() {
		cmd := exec.Command("tmux", "attach-session", "-t", name)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	}
	return exec.Command("tmux", switchClientArgs(name)...).Run()
}
```

Delete `ResolveTarget` (lines 81-94), `pickClient` (lines 96-131), and `splitNonEmpty` (lines 133-142) entirely.

Remove the now-unused `"fmt"` import from the import block.

- [ ] **Step 2: Remove obsolete tests**

In `internal/tmux/tmux_test.go`, delete `TestResolveTarget_Empty`, `TestResolveTarget_DevPath`, and `TestSplitNonEmpty`.

- [ ] **Step 3: Build and test the package**

Run: `go build ./... && go test ./internal/tmux/ -v`
Expected: build succeeds; tests PASS (parse tests + the three new arg-builder tests from Task 1).

- [ ] **Step 4: Full test suite**

Run: `go test ./...`
Expected: PASS across all packages.

- [ ] **Step 5: Commit**

```bash
git add internal/tmux/tmux.go internal/tmux/tmux_test.go
git commit -m "refactor(tmux): remove --target multi-client plumbing"
```

---

## Task 5: Workspace attribution (Repo/Worktree)

**Files:**
- Modify: `internal/sessions/session.go`
- Create: `internal/sessions/workspace.go`
- Test: `internal/sessions/workspace_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/sessions/workspace_test.go`:

```go
package sessions

import "testing"

func TestWorkspaceLabels_MainWorktree(t *testing.T) {
	repo, wt := workspaceLabels("/home/u/myrepo", "/home/u/myrepo/.git")
	if repo != "myrepo" || wt != "myrepo" {
		t.Fatalf("got (%q, %q), want (myrepo, myrepo)", repo, wt)
	}
}

func TestWorkspaceLabels_LinkedWorktree(t *testing.T) {
	// A linked worktree's git-common-dir points back at the main repo's .git.
	repo, wt := workspaceLabels("/home/u/myrepo-feature", "/home/u/myrepo/.git")
	if repo != "myrepo" || wt != "myrepo-feature" {
		t.Fatalf("got (%q, %q), want (myrepo, myrepo-feature)", repo, wt)
	}
}

func TestWorkspaceLabels_TrailingSlash(t *testing.T) {
	repo, wt := workspaceLabels("/home/u/myrepo", "/home/u/myrepo/.git/")
	if repo != "myrepo" || wt != "myrepo" {
		t.Fatalf("got (%q, %q), want (myrepo, myrepo)", repo, wt)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/sessions/ -run TestWorkspaceLabels -v`
Expected: FAIL — `undefined: workspaceLabels`.

- [ ] **Step 3: Create `internal/sessions/workspace.go`**

```go
package sessions

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// gitRevParse runs `git -C cwd rev-parse <arg>`; swappable in tests.
var gitRevParse = func(cwd, arg string) (string, error) {
	out, err := exec.Command("git", "-C", cwd, "rev-parse", arg).Output()
	return strings.TrimSpace(string(out)), err
}

// workspaceLabels derives (repo, worktree) display labels from a git toplevel
// path and the absolute git-common-dir. The worktree label is the basename of
// the toplevel; the repo label is the basename of the directory containing the
// common .git dir (which is shared by all worktrees of a repo).
func workspaceLabels(toplevel, commonDir string) (repo, worktree string) {
	worktree = filepath.Base(toplevel)
	cd := strings.TrimRight(commonDir, "/")
	repo = filepath.Base(filepath.Dir(cd))
	if repo == "" || repo == "." || repo == string(filepath.Separator) {
		repo = worktree
	}
	return repo, worktree
}

// DeriveWorkspace resolves the repo and worktree labels for a working dir.
// Falls back to the cwd basename when the dir is not inside a git repo.
func DeriveWorkspace(cwd string) (repo, worktree string) {
	top, err := gitRevParse(cwd, "--show-toplevel")
	if err != nil || top == "" {
		base := filepath.Base(cwd)
		return base, base
	}
	common, err := gitRevParse(cwd, "--git-common-dir")
	if err != nil || common == "" {
		base := filepath.Base(top)
		return base, base
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(top, common)
	}
	return workspaceLabels(top, common)
}
```

- [ ] **Step 4: Add fields to `Session`**

In `internal/sessions/session.go`, add to the `Session` struct (after the `Origin` field):

```go
	Repo        string // repo grouping label (derived from CWD; empty until enriched)
	Worktree    string // worktree label (derived from CWD; empty until enriched)
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/sessions/ -run TestWorkspaceLabels -v`
Expected: PASS (3 tests).

- [ ] **Step 6: Build and full test suite**

Run: `go build . && go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/sessions/session.go internal/sessions/workspace.go internal/sessions/workspace_test.go
git commit -m "feat(sessions): add repo/worktree workspace attribution"
```

---

## Task 6: Update docs and usage

**Files:**
- Modify: `main.go`
- Modify: `README.md`
- Modify: `AGENTS.md`

- [ ] **Step 1: Update `main.go` usage strings**

In `main.go`, change the resume usage line (line 57) from:

```
  tsession resume [--target=..] <session-id>  Switch tmux to session
```

to:

```
  tsession resume <session-id>                Dock navigator beside session
```

- [ ] **Step 2: Update `README.md` browse section**

Replace the "Browse" section body (lines 13-29) with:

```markdown
## Browse — single-tmux navigation

The primary workflow: run `tsession browse` outside tmux. It creates a tmux
session named `sessions-nav` laid out as two panes — `nav` on the left (the
fzf picker) and `main` on the right (a placeholder shell):

![browse](browse.png)

```bash
tsession browse --watch --active --short
```

Press `enter` to switch to a session: the `nav` pane docks itself to the left
of that agent's own window (preserving its width), and the client follows.
Agents are never moved — their scrollback and layout stay intact. Press `esc`
to send the navigator home beside `main` and exit.

The `--watch` flag keeps the picker open and refreshes every 5 seconds — a
persistent session dashboard.
```

- [ ] **Step 3: Update `AGENTS.md`**

In `AGENTS.md`, update the resume command line in the Commands reference:

Change `tsession resume [--target=..] <session-id>   # switch tmux pane (or fall back)` to:

```
tsession resume <session-id>                 # dock navigator beside session (or fall back)
```

Remove the `--target <value>` row from the flags table.

Add a short subsection after the "tmux" data-source section:

```markdown
## Navigator layout

`tsession browse` (run outside tmux) creates a `sessions-nav` session laid out
as `[nav | main]`. The `nav` pane runs the picker; `main` is a placeholder
shell. On selection, the `nav` pane uses `join-pane` to dock to the left of the
selected agent's existing window (preserving its width) and `switch-client`s
there. There is no multi-client `--target`; one client follows the navigator.
```

- [ ] **Step 4: Verify build still green (docs-only, sanity check)**

Run: `go build . && go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add main.go README.md AGENTS.md
git commit -m "docs: document single-tmux navigator flow"
```

---

## Final verification

- [ ] **Step 1: Full build + test**

Run: `go build . && go test ./...`
Expected: build succeeds; all tests PASS.

- [ ] **Step 2: Grep for leftover obsolete references**

Run: `grep -rn -- "--target\|ResolveTarget\|pickClient\|SwitchClientTarget" --include='*.go' . ; grep -rn "session-nav" --include='*.go' .`
Expected: no matches. (Note: the new constant is `sessions-nav` with a trailing `s` on `sessions`, which the `session-nav` pattern does not match.)
