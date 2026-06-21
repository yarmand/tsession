# `tsession new` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tsession new` subcommand that creates (or reuses) a git worktree, opens a tmux session in it, and launches Copilot CLI there, with the worktree-creation commands configurable via an auto-generated shell script.

**Architecture:** A new `internal/worktree` package owns the configurable creation script (auto-written with defaults, executed with the branch name, returns the worktree path from the last stdout line). New tmux helpers handle detached session creation and name-collision/resume resolution. `cmd/new.go` orchestrates: split `--` copilot args, validate branch-vs-`--path`, resolve the worktree path, pick a session name, create the session, then switch/attach.

**Tech Stack:** Go 1.25, standard library only (`os/exec`, `flag`), tmux CLI. Build with `go build .`, test with `go test ./...` from the repo root.

---

### Task 1: `internal/worktree` — script path & default-script generation

**Files:**
- Create: `internal/worktree/worktree.go`
- Test: `internal/worktree/worktree_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/worktree/worktree_test.go`:

```go
package worktree

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureScriptWritesDefaultWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	configHome = func() (string, error) { return dir, nil }

	if err := EnsureScript(); err != nil {
		t.Fatalf("EnsureScript: %v", err)
	}

	path := filepath.Join(dir, "new-worktree.sh")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v, want 0755", info.Mode().Perm())
	}
	data, _ := os.ReadFile(path)
	if string(data) != defaultScript {
		t.Fatalf("content does not match defaultScript")
	}
}

func TestEnsureScriptPreservesExisting(t *testing.T) {
	dir := t.TempDir()
	configHome = func() (string, error) { return dir, nil }
	path := filepath.Join(dir, "new-worktree.sh")
	if err := os.WriteFile(path, []byte("custom\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := EnsureScript(); err != nil {
		t.Fatalf("EnsureScript: %v", err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "custom\n" {
		t.Fatalf("existing script overwritten: %q", string(data))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/worktree/`
Expected: FAIL (compile error: `configHome`, `EnsureScript`, `defaultScript` undefined).

- [ ] **Step 3: Write minimal implementation**

Create `internal/worktree/worktree.go`:

```go
// Package worktree creates git worktrees via a user-configurable script and
// reports the resulting worktree path.
package worktree

import (
	"os"
	"path/filepath"
)

const defaultScript = `#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(git rev-parse --git-common-dir)/.." && pwd)"
wt_folder="${repo_root}.worktrees"
mkdir -p "$wt_folder"
wt_path="$(realpath "$wt_folder")/$1"
git worktree add -b "$USER/$1" "$wt_path"
echo "$wt_path"
`

// configHome returns the tsession config directory. Overridable in tests.
var configHome = func() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "tsession"), nil
}

// ScriptPath returns the path to the worktree-creation script.
func ScriptPath() (string, error) {
	dir, err := configHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "new-worktree.sh"), nil
}

// EnsureScript writes the default script (mode 0755) if it does not already
// exist. An existing script is never overwritten.
func EnsureScript() error {
	path, err := ScriptPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(defaultScript), 0o755)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/worktree/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/worktree/worktree.go internal/worktree/worktree_test.go
git commit -m "feat(worktree): add configurable creation script scaffolding"
```

---

### Task 2: `internal/worktree` — run script and capture worktree path

**Files:**
- Modify: `internal/worktree/worktree.go`
- Test: `internal/worktree/worktree_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/worktree/worktree_test.go`:

```go
func TestCreateReturnsLastStdoutLine(t *testing.T) {
	dir := t.TempDir()
	configHome = func() (string, error) { return dir, nil }
	path := filepath.Join(dir, "new-worktree.sh")
	stub := "#!/usr/bin/env bash\n" +
		"echo \"progress\" >&2\n" +
		"echo \"\"\n" +
		"echo \"/tmp/fake/$1\"\n"
	if err := os.WriteFile(path, []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := Create("mybranch")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got != "/tmp/fake/mybranch" {
		t.Fatalf("got %q, want /tmp/fake/mybranch", got)
	}
}

func TestCreateErrorsWhenNoPathPrinted(t *testing.T) {
	dir := t.TempDir()
	configHome = func() (string, error) { return dir, nil }
	path := filepath.Join(dir, "new-worktree.sh")
	stub := "#!/usr/bin/env bash\necho \"only stderr\" >&2\n"
	if err := os.WriteFile(path, []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Create("b"); err == nil {
		t.Fatal("expected error when script prints no path")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/worktree/ -run TestCreate`
Expected: FAIL (compile error: `Create` undefined).

- [ ] **Step 3: Write minimal implementation**

Add to `internal/worktree/worktree.go` (imports become `bytes`, `fmt`, `os`, `os/exec`, `path/filepath`, `strings`):

```go
// Create ensures the script exists, runs it with the branch name, and returns
// the worktree path printed as the last non-empty line of stdout. The script's
// stderr is streamed to the user. The script runs in the current working
// directory.
func Create(branch string) (string, error) {
	if err := EnsureScript(); err != nil {
		return "", err
	}
	path, err := ScriptPath()
	if err != nil {
		return "", err
	}

	var stdout bytes.Buffer
	cmd := exec.Command("bash", path, branch)
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("worktree script failed: %w", err)
	}

	line := lastNonEmptyLine(stdout.String())
	if line == "" {
		return "", fmt.Errorf("worktree script printed no path on stdout")
	}
	return line, nil
}

func lastNonEmptyLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return ""
}
```

Update the import block at the top of the file to:

```go
import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/worktree/`
Expected: PASS (all worktree tests).

- [ ] **Step 5: Commit**

```bash
git add internal/worktree/worktree.go internal/worktree/worktree_test.go
git commit -m "feat(worktree): run script and capture worktree path"
```

---

### Task 3: `internal/tmux` — session-name resolution (collision/resume)

**Files:**
- Modify: `internal/tmux/tmux.go`
- Test: `internal/tmux/tmux_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/tmux/tmux_test.go`:

```go
func TestResolveSessionName(t *testing.T) {
	cases := []struct {
		name     string
		desired  string
		path     string
		existing []Session
		wantName string
		wantResume bool
	}{
		{
			name:     "no existing",
			desired:  "foo",
			path:     "/a",
			existing: nil,
			wantName: "foo", wantResume: false,
		},
		{
			name:     "same name same path resumes",
			desired:  "foo",
			path:     "/a",
			existing: []Session{{Name: "foo", Path: "/a"}},
			wantName: "foo", wantResume: true,
		},
		{
			name:     "same name different path suffixes",
			desired:  "foo",
			path:     "/a",
			existing: []Session{{Name: "foo", Path: "/b"}},
			wantName: "foo-2", wantResume: false,
		},
		{
			name:    "skips taken suffixes",
			desired: "foo",
			path:    "/a",
			existing: []Session{
				{Name: "foo", Path: "/b"},
				{Name: "foo-2", Path: "/c"},
			},
			wantName: "foo-3", wantResume: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotName, gotResume := ResolveSessionName(tc.desired, tc.path, tc.existing)
			if gotName != tc.wantName || gotResume != tc.wantResume {
				t.Fatalf("got (%q,%v), want (%q,%v)", gotName, gotResume, tc.wantName, tc.wantResume)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tmux/ -run TestResolveSessionName`
Expected: FAIL (compile error: `ResolveSessionName` undefined).

- [ ] **Step 3: Write minimal implementation**

Add to `internal/tmux/tmux.go` (the `path/filepath` import must be added to the existing import block):

```go
// ResolveSessionName decides which tmux session name to use for a new session
// rooted at path, given the current session list. If a session with the desired
// name already exists at the same path, it returns (desired, true) signalling
// the caller should resume it instead of creating a new one. If the name is
// taken by a session at a different path, it returns a unique suffixed name
// (desired-2, desired-3, ...) and false.
func ResolveSessionName(desired, path string, existing []Session) (string, bool) {
	byName := make(map[string]string, len(existing))
	for _, s := range existing {
		byName[s.Name] = s.Path
	}
	existingPath, taken := byName[desired]
	if !taken {
		return desired, false
	}
	if filepath.Clean(existingPath) == filepath.Clean(path) {
		return desired, true
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", desired, i)
		if _, ok := byName[candidate]; !ok {
			return candidate, false
		}
	}
}
```

Add `"path/filepath"` to the import block in `internal/tmux/tmux.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tmux/ -run TestResolveSessionName`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tmux/tmux.go internal/tmux/tmux_test.go
git commit -m "feat(tmux): add ResolveSessionName for new-session collision handling"
```

---

### Task 4: `internal/tmux` — create detached session

**Files:**
- Modify: `internal/tmux/tmux.go`

- [ ] **Step 1: Add the implementation**

Add to `internal/tmux/tmux.go`:

```go
// NewSession creates a detached tmux session named name, with working directory
// path, running command (interpreted by the shell). Use SwitchClientTarget to
// focus it afterward.
func NewSession(name, path, command string) error {
	return exec.Command("tmux", "new-session", "-d", "-s", name, "-c", path, command).Run()
}
```

- [ ] **Step 2: Verify it builds**

Run: `go build ./...`
Expected: success, no output.

- [ ] **Step 3: Commit**

```bash
git add internal/tmux/tmux.go
git commit -m "feat(tmux): add NewSession to create a detached session"
```

---

### Task 5: `cmd/new` — pure arg helpers (`--` split, validation)

**Files:**
- Create: `cmd/new.go`
- Test: `cmd/new_test.go`

- [ ] **Step 1: Write the failing test**

Create `cmd/new_test.go`:

```go
package cmd

import (
	"reflect"
	"testing"
)

func TestSplitDashDash(t *testing.T) {
	cases := []struct {
		name         string
		args         []string
		wantBefore   []string
		wantAfter    []string
	}{
		{"no dashdash", []string{"branch"}, []string{"branch"}, nil},
		{"with dashdash", []string{"branch", "--", "--resume"}, []string{"branch"}, []string{"--resume"}},
		{"dashdash only", []string{"--"}, []string{}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before, after := splitDashDash(tc.args)
			if !reflect.DeepEqual(before, tc.wantBefore) || !reflect.DeepEqual(after, tc.wantAfter) {
				t.Fatalf("got (%v,%v), want (%v,%v)", before, after, tc.wantBefore, tc.wantAfter)
			}
		})
	}
}

func TestValidateNewArgs(t *testing.T) {
	if err := validateNewArgs("", ""); err == nil {
		t.Error("expected error when neither branch nor path given")
	}
	if err := validateNewArgs("b", "/p"); err == nil {
		t.Error("expected error when both branch and path given")
	}
	if err := validateNewArgs("b", ""); err != nil {
		t.Errorf("branch only: unexpected error %v", err)
	}
	if err := validateNewArgs("", "/p"); err != nil {
		t.Errorf("path only: unexpected error %v", err)
	}
}

func TestBuildCopilotCommand(t *testing.T) {
	if got := buildCopilotCommand(nil); got != "copilot" {
		t.Errorf("got %q, want copilot", got)
	}
	if got := buildCopilotCommand([]string{"--resume", "x y"}); got != "copilot --resume 'x y'" {
		t.Errorf("got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run 'TestSplitDashDash|TestValidateNewArgs|TestBuildCopilotCommand'`
Expected: FAIL (compile error: `splitDashDash`, `validateNewArgs`, `buildCopilotCommand` undefined).

- [ ] **Step 3: Write minimal implementation**

Create `cmd/new.go` (only `fmt` is imported now; `New`/`resolveWorktreePath` and their imports are added in Task 6):

```go
package cmd

import (
	"fmt"
)

// splitDashDash splits args at the first literal "--". Everything before is the
// command's own args; everything after is forwarded to copilot. When there is
// no "--", after is nil.
func splitDashDash(args []string) (before, after []string) {
	for i, a := range args {
		if a == "--" {
			return args[:i], args[i+1:]
		}
	}
	return args, nil
}

// validateNewArgs enforces that exactly one of branch / path is provided.
func validateNewArgs(branch, path string) error {
	switch {
	case branch == "" && path == "":
		return fmt.Errorf("usage: tsession new <branch> | --path <dir>")
	case branch != "" && path != "":
		return fmt.Errorf("provide either a branch or --path, not both")
	default:
		return nil
	}
}

// buildCopilotCommand builds the shell command run inside the tmux session.
func buildCopilotCommand(extra []string) string {
	cmd := "copilot"
	for _, a := range extra {
		cmd += " " + shellQuote(a)
	}
	return cmd
}
```

(`buildCopilotCommand` uses `shellQuote`, which already exists in `cmd/browse.go`.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/ -run 'TestSplitDashDash|TestValidateNewArgs|TestBuildCopilotCommand'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/new.go cmd/new_test.go
git commit -m "feat(new): add pure arg helpers for the new command"
```

---

### Task 6: `cmd/new` — orchestration (`New` entrypoint)

**Files:**
- Modify: `cmd/new.go`

- [ ] **Step 1: Add `New` and `resolveWorktreePath` to `cmd/new.go`**

Update the import block at the top of `cmd/new.go` from `import ( "fmt" )` to:

```go
import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yarma/tsession/internal/tmux"
	"github.com/yarma/tsession/internal/worktree"
)
```

Then add these two functions to the file (keep the existing `splitDashDash`,
`validateNewArgs`, and `buildCopilotCommand` from Task 5):

```go
// New implements `tsession new`: create (or reuse) a git worktree, open a tmux
// session in it, and start copilot there.
//
//	tsession new <branch> [-- <copilot-args>...]
//	tsession new --path <dir> [-- <copilot-args>...]
func New(args []string) error {
	before, copilotArgs := splitDashDash(args)

	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	path := fs.String("path", "", "use an existing worktree at this directory instead of creating one")
	if err := fs.Parse(before); err != nil {
		return err
	}
	branch := fs.Arg(0)

	if err := validateNewArgs(branch, *path); err != nil {
		return err
	}

	wtPath, err := resolveWorktreePath(branch, *path)
	if err != nil {
		return err
	}

	name := filepath.Base(wtPath)
	sess, _ := tmux.ListSessions()
	resolved, resume := tmux.ResolveSessionName(name, wtPath, sess)

	if !resume {
		if err := tmux.NewSession(resolved, wtPath, buildCopilotCommand(copilotArgs)); err != nil {
			return fmt.Errorf("create tmux session: %w", err)
		}
	}

	return tmux.SwitchClientTarget(resolved, "")
}

// resolveWorktreePath returns the worktree directory: either the validated
// existing --path, or a freshly created worktree for branch.
func resolveWorktreePath(branch, path string) (string, error) {
	if path != "" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(abs)
		if err != nil {
			return "", fmt.Errorf("--path %q: %w", path, err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("--path %q is not a directory", path)
		}
		return abs, nil
	}
	return worktree.Create(branch)
}
```

The final `cmd/new.go` contains five functions: `New`, `resolveWorktreePath`,
`splitDashDash`, `validateNewArgs`, `buildCopilotCommand`.

- [ ] **Step 2: Verify it builds and helper tests still pass**

Run: `go build ./... && go test ./cmd/ -run 'TestSplitDashDash|TestValidateNewArgs|TestBuildCopilotCommand'`
Expected: build succeeds; tests PASS.

- [ ] **Step 3: Commit**

```bash
git add cmd/new.go
git commit -m "feat(new): orchestrate worktree creation, tmux session, copilot launch"
```

---

### Task 7: Wire `new` into the CLI dispatch and usage

**Files:**
- Modify: `main.go`

- [ ] **Step 1: Add the dispatch case**

In `main.go`, inside the `switch sub {` block, add a case after the `case "list":` block (before `case "browse":` is fine — placement is cosmetic):

```go
	case "new":
		err = cmd.New(args)
```

- [ ] **Step 2: Add usage text**

In `main.go`, in the `usage()` function's printed block, add this line after the `tsession list ...` line:

```
  tsession new <branch> [-- copilot-args]      Create a worktree + tmux session and start copilot
  tsession new --path <dir> [-- copilot-args]  Start a session on an existing worktree
```

- [ ] **Step 3: Verify it builds**

Run: `go build .`
Expected: success, no output.

- [ ] **Step 4: Manual smoke test (informational)**

Run: `go run . -h`
Expected: help text now lists the `new` command.

- [ ] **Step 5: Commit**

```bash
git add main.go
git commit -m "feat(cli): wire up the new subcommand and usage"
```

---

### Task 8: Documentation (README + AGENTS.md)

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`

- [ ] **Step 1: Update AGENTS.md command reference**

In `AGENTS.md`, in the "Commands (full reference)" code block, add this line near the top of the command list (after the `tsession list ...` line):

```
tsession new <branch> [-- copilot-args]      # create worktree + tmux session, start copilot
tsession new --path <dir> [-- copilot-args]  # start a session on an existing worktree
```

Then add a new section after the "Commands (full reference)" section:

```markdown
## New Sessions (`new`)

`tsession new <branch>` creates a git worktree, opens a tmux session named
`basename(worktree-path)`, and starts copilot in it. `tsession new --path <dir>`
does the same on an existing worktree. Anything after `--` is forwarded to
copilot.

The worktree-creation commands are configurable via `~/.config/tsession/new-worktree.sh`,
auto-created with defaults on first run. The script receives the branch name as
`$1` and must print the final worktree path as the last line of stdout. The
default creates `<repo>.worktrees/<branch>` with a `$USER/<branch>` branch.

If a tmux session with the target name already exists at the same path, `new`
resumes it; if it exists at a different path, `new` uses a unique suffixed name.
```

- [ ] **Step 2: Update README.md**

In `README.md`, add a "Creating Sessions" section (place it before the "Remote Sessions" section near line 95). Use this content:

```markdown
## Creating Sessions

Create a fresh worktree and start a Copilot session in it:

```bash
tsession new my-feature                 # creates a worktree for branch my-feature
tsession new --path ~/src/repo.wt/foo   # use an existing worktree
tsession new my-feature -- --resume     # forward args after -- to copilot
```

`new` creates (or reuses) a git worktree, opens a tmux session named after the
worktree directory, and launches `copilot` inside it, then switches/attaches you
to that session.

### Configuring worktree creation

The commands used to create the worktree live in
`~/.config/tsession/new-worktree.sh`, auto-created with defaults on first run.
The script receives the branch name as `$1` and must print the resulting
worktree path as the **last line of stdout**. Edit it freely to match your
workflow. The default:

```sh
#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(git rev-parse --git-common-dir)/.." && pwd)"
wt_folder="${repo_root}.worktrees"
mkdir -p "$wt_folder"
wt_path="$(realpath "$wt_folder")/$1"
git worktree add -b "$USER/$1" "$wt_path"
echo "$wt_path"
```
```

- [ ] **Step 3: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: document the new command and worktree-creation config"
```

---

### Task 9: Full verification

**Files:** none (verification only)

- [ ] **Step 1: Build**

Run: `go build .`
Expected: success, no output.

- [ ] **Step 2: Full test suite**

Run: `go test ./...`
Expected: all packages PASS (including new `internal/worktree`, `internal/tmux`, `cmd` tests).

- [ ] **Step 3: Manual end-to-end (informational, run from inside a git repo with tmux)**

Run: `go run . new try-new-cmd`
Expected: a worktree `<repo>.worktrees/try-new-cmd` is created, a tmux session `try-new-cmd` starts with copilot, and you are switched/attached to it. Clean up afterward with `git worktree remove` and `tmux kill-session -t try-new-cmd`.

- [ ] **Step 4: Final commit (if any uncommitted verification fixes)**

```bash
git add -A
git commit -m "chore: finalize tsession new command" || true
```
