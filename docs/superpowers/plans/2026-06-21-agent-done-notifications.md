# Agent done / asking-question notifications Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fire a macOS desktop notification (with sound) the moment a tracked session enters the `done` or `question` state, opt-in via a `--notify` flag on `watch`, `list`, and `browse`.

**Architecture:** A new `internal/notify` package diffs the current session list against a persisted snapshot (`~/.tsession/notify.json`) under a cross-process file lock, firing a notification once per transition into `done`/`question`. The macOS notifier shells out to `osascript`; non-macOS builds are a no-op. The `--notify` flag wires the diff into the watch daemon's refresh loop and into `list` (which `browse --watch` re-runs every 5s).

**Tech Stack:** Go 1.25, standard library only (`encoding/json`, `os/exec`, `syscall.Flock`), build-tagged platform files.

Spec: `docs/superpowers/specs/2026-06-21-agent-done-notifications-design.md`

---

## File Structure

- **Create** `internal/notify/notify.go` — snapshot load/save, file lock, state mapping, `displayLabel`, `escapeAppleScript`, `Process`.
- **Create** `internal/notify/notify_darwin.go` — `fire()` via `osascript` (`//go:build darwin`).
- **Create** `internal/notify/notify_other.go` — `fire()` no-op (`//go:build !darwin`).
- **Create** `internal/notify/notify_test.go` — table-driven tests (white-box, `package notify`).
- **Modify** `cmd/list.go` — add `--notify` flag; call `notify.Process` on the full session union.
- **Modify** `cmd/watch.go` — add `--notify` flag; propagate through `spawnDaemon`; call `notify.Process` in `refresh`.
- **Modify** `cmd/browse.go` — add `--notify` flag; append `--notify` to the fzf reload command; thread through `runFzf`/`runFzfOpts`.
- **Modify** `cmd/popup.go` — update the `runFzf` call site for the new signature.
- **Modify** `README.md`, `AGENTS.md` — document the flag and snapshot file.

---

## Task 1: notify package — state mapping, message, displayLabel

**Files:**
- Create: `internal/notify/notify.go`
- Test: `internal/notify/notify_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/notify/notify_test.go`:

```go
package notify

import (
	"testing"

	"github.com/yarma/tsession/internal/sessions"
)

func TestNotifiableState(t *testing.T) {
	cases := []struct {
		state sessions.State
		want  string
	}{
		{sessions.StateDone, "done"},
		{sessions.StateWaiting, "question"},
		{sessions.StateWorking, ""},
		{sessions.StateActiveIdle, ""},
		{sessions.StateInactiveIdle, ""},
		{sessions.StateExited, ""},
		{sessions.StateUnknown, ""},
	}
	for _, c := range cases {
		if got := notifiableState(c.state); got != c.want {
			t.Errorf("notifiableState(%v) = %q, want %q", c.state, got, c.want)
		}
	}
}

func TestMessageFor(t *testing.T) {
	m, ok := messageFor("done", "myproj")
	if !ok || m.text != "[myproj] done!" || m.sound != "Tink" {
		t.Errorf("done message = %+v ok=%v", m, ok)
	}
	m, ok = messageFor("question", "myproj")
	if !ok || m.text != "[myproj] needs your input" || m.sound != "Funk" {
		t.Errorf("question message = %+v ok=%v", m, ok)
	}
	if _, ok := messageFor("", "myproj"); ok {
		t.Errorf("empty state should not produce a message")
	}
}

func TestDisplayLabel(t *testing.T) {
	if got := displayLabel(sessions.Session{Name: "n", Summary: "s", CWD: "/a/b"}); got != "n" {
		t.Errorf("Name priority: got %q", got)
	}
	if got := displayLabel(sessions.Session{Summary: "s", CWD: "/a/b"}); got != "s" {
		t.Errorf("Summary fallback: got %q", got)
	}
	if got := displayLabel(sessions.Session{CWD: "/a/b"}); got != "b" {
		t.Errorf("basename fallback: got %q", got)
	}
	if got := displayLabel(sessions.Session{Summary: "line1\nline2"}); got != "line1 line2" {
		t.Errorf("summary newline flatten: got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/notify/`
Expected: FAIL — build error, `notifiableState`/`messageFor`/`displayLabel` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/notify/notify.go`:

```go
// Package notify fires desktop notifications when a tracked session enters a
// "done" or "question" state. State is diffed against a persisted snapshot
// (~/.tsession/notify.json) under a cross-process file lock so each transition
// fires exactly once across the separate processes that observe sessions over
// time (the watch daemon and the repeated `list` reloads driven by
// `browse --watch`).
package notify

import (
	"path/filepath"
	"strings"

	"github.com/yarma/tsession/internal/sessions"
)

type message struct {
	text  string
	sound string
}

// notifiableState maps a session state to "done", "question", or "" (a state
// that should never produce a notification).
func notifiableState(s sessions.State) string {
	switch s {
	case sessions.StateDone:
		return "done"
	case sessions.StateWaiting:
		return "question"
	default:
		return ""
	}
}

// messageFor returns the notification text and sound for a notifiable state.
func messageFor(state, label string) (message, bool) {
	switch state {
	case "done":
		return message{text: "[" + label + "] done!", sound: "Tink"}, true
	case "question":
		return message{text: "[" + label + "] needs your input", sound: "Funk"}, true
	default:
		return message{}, false
	}
}

// displayLabel resolves the human-facing session label using the same priority
// as the UI: user-defined Name, then Summary, then basename(CWD), then ID.
func displayLabel(s sessions.Session) string {
	if s.Name != "" {
		return s.Name
	}
	if s.Summary != "" {
		return strings.ReplaceAll(strings.ReplaceAll(s.Summary, "\n", " "), "\r", " ")
	}
	if s.CWD != "" {
		return filepath.Base(s.CWD)
	}
	return s.ID
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/notify/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/notify/notify.go internal/notify/notify_test.go
git commit -m "feat(notify): add state mapping, message, and label helpers"
```

---

## Task 2: notify package — AppleScript escaping

**Files:**
- Modify: `internal/notify/notify.go`
- Test: `internal/notify/notify_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/notify/notify_test.go`:

```go
func TestEscapeAppleScript(t *testing.T) {
	cases := map[string]string{
		`plain`:        `plain`,
		`say "hi"`:     `say \"hi\"`,
		`back\slash`:   `back\\slash`,
		`both "\"`:     `both \"\\\"`,
	}
	for in, want := range cases {
		if got := escapeAppleScript(in); got != want {
			t.Errorf("escapeAppleScript(%q) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/notify/ -run TestEscapeAppleScript`
Expected: FAIL — `escapeAppleScript` undefined.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/notify/notify.go`:

```go
// escapeAppleScript escapes a string for use inside an AppleScript double-
// quoted literal. Backslash must be escaped first, then the double-quote.
func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/notify/ -run TestEscapeAppleScript`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/notify/notify.go internal/notify/notify_test.go
git commit -m "feat(notify): add AppleScript string escaping"
```

---

## Task 3: notify package — snapshot persistence

**Files:**
- Modify: `internal/notify/notify.go`
- Test: `internal/notify/notify_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/notify/notify_test.go`:

```go
import (
	"path/filepath"  // add alongside existing imports if not present
)

func TestSnapshotRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notify.json")

	// Missing file -> empty, non-nil map.
	s := loadSnapshot(path)
	if s.Entries == nil {
		t.Fatal("loadSnapshot of missing file returned nil Entries")
	}
	if len(s.Entries) != 0 {
		t.Fatalf("expected empty entries, got %v", s.Entries)
	}

	s.Entries["abc"] = "done"
	if err := saveSnapshot(path, s); err != nil {
		t.Fatalf("saveSnapshot: %v", err)
	}

	got := loadSnapshot(path)
	if got.Entries["abc"] != "done" {
		t.Fatalf("round trip lost data: %v", got.Entries)
	}
}

func TestLoadSnapshotCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notify.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := loadSnapshot(path)
	if s.Entries == nil || len(s.Entries) != 0 {
		t.Fatalf("corrupt file should yield empty map, got %v", s.Entries)
	}
}
```

Add `"os"` to the test file's import block if not already present.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/notify/ -run TestSnapshot`
Expected: FAIL — `loadSnapshot`/`saveSnapshot`/`snapshot` undefined.

- [ ] **Step 3: Write minimal implementation**

Add to the import block of `internal/notify/notify.go`:

```go
	"encoding/json"
	"os"
```

Append to `internal/notify/notify.go`:

```go
// snapshot is the on-disk last-notified-state map: session ID -> "done" |
// "question" | "".
type snapshot struct {
	Entries map[string]string `json:"entries"`
}

// loadSnapshot reads the snapshot file. A missing or corrupt file yields an
// empty (non-nil) map so callers can treat every session as a first sighting.
func loadSnapshot(path string) snapshot {
	data, err := os.ReadFile(path)
	if err != nil {
		return snapshot{Entries: map[string]string{}}
	}
	var s snapshot
	if err := json.Unmarshal(data, &s); err != nil || s.Entries == nil {
		return snapshot{Entries: map[string]string{}}
	}
	return s
}

// saveSnapshot atomically writes the snapshot via a temp file + rename.
func saveSnapshot(path string, s snapshot) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".notify.*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/notify/ -run TestSnapshot`
Expected: PASS. Also run `go test ./internal/notify/` to confirm earlier tests still pass.

- [ ] **Step 5: Commit**

```bash
git add internal/notify/notify.go internal/notify/notify_test.go
git commit -m "feat(notify): add atomic snapshot persistence"
```

---

## Task 4: notify package — directory + cross-process lock

**Files:**
- Modify: `internal/notify/notify.go`

- [ ] **Step 1: Write the implementation (no new behavior to unit-test in isolation; covered by Task 5's Process tests via a temp HOME)**

Add to the import block of `internal/notify/notify.go`:

```go
	"syscall"
```

Append to `internal/notify/notify.go`:

```go
const (
	dirName      = ".tsession"
	snapshotFile = "notify.json"
	lockFile     = "notify.lock"
)

// dir returns ~/.tsession, creating it if missing.
func dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(home, dirName)
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	return d, nil
}

// lock acquires an exclusive advisory lock on path and returns a release func.
// It serializes the read-modify-write of the snapshot across the watch daemon
// and any concurrent `list --notify` processes so each transition fires once.
func lock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
```

- [ ] **Step 2: Verify it builds**

Run: `go build ./internal/notify/`
Expected: builds with no error.

- [ ] **Step 3: Commit**

```bash
git add internal/notify/notify.go
git commit -m "feat(notify): add ~/.tsession dir helper and file lock"
```

---

## Task 5: notify package — Process diff logic

**Files:**
- Modify: `internal/notify/notify.go`
- Test: `internal/notify/notify_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/notify/notify_test.go`:

```go
type fired struct {
	title string
	sound string
}

// withCaptureFire swaps fireFunc for the duration of the test and restores it.
func withCaptureFire(t *testing.T) *[]fired {
	t.Helper()
	var calls []fired
	prev := fireFunc
	fireFunc = func(title, sound string) {
		calls = append(calls, fired{title, sound})
	}
	t.Cleanup(func() { fireFunc = prev })
	return &calls
}

func sess(id string, st sessions.State) sessions.Session {
	return sessions.Session{ID: id, Name: id, State: st}
}

func TestProcessFirstSightingDoesNotFire(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	calls := withCaptureFire(t)

	// Session is already "done" the first time we see it -> record, no fire.
	if err := Process([]sessions.Session{sess("a", sessions.StateDone)}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 {
		t.Fatalf("first sighting should not fire, got %v", *calls)
	}
}

func TestProcessFiresOnTransition(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	calls := withCaptureFire(t)

	// First sighting: working (seeded silently).
	if err := Process([]sessions.Session{sess("a", sessions.StateWorking)}); err != nil {
		t.Fatal(err)
	}
	// Transition to done -> one fire with Tink.
	if err := Process([]sessions.Session{sess("a", sessions.StateDone)}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].title != "[a] done!" || (*calls)[0].sound != "Tink" {
		t.Fatalf("expected one Tink done fire, got %v", *calls)
	}
}

func TestProcessFiresOnQuestion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	calls := withCaptureFire(t)

	_ = Process([]sessions.Session{sess("a", sessions.StateWorking)})
	_ = Process([]sessions.Session{sess("a", sessions.StateWaiting)})
	if len(*calls) != 1 || (*calls)[0].title != "[a] needs your input" || (*calls)[0].sound != "Funk" {
		t.Fatalf("expected one Funk question fire, got %v", *calls)
	}
}

func TestProcessNoRefireWhileDone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	calls := withCaptureFire(t)

	_ = Process([]sessions.Session{sess("a", sessions.StateWorking)})
	_ = Process([]sessions.Session{sess("a", sessions.StateDone)})
	_ = Process([]sessions.Session{sess("a", sessions.StateDone)})
	if len(*calls) != 1 {
		t.Fatalf("done should fire once, got %d: %v", len(*calls), *calls)
	}
}

func TestProcessRefiresAfterLeavingDone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	calls := withCaptureFire(t)

	_ = Process([]sessions.Session{sess("a", sessions.StateWorking)})
	_ = Process([]sessions.Session{sess("a", sessions.StateDone)})       // fire 1
	_ = Process([]sessions.Session{sess("a", sessions.StateActiveIdle)}) // leave done
	_ = Process([]sessions.Session{sess("a", sessions.StateDone)})       // fire 2
	if len(*calls) != 2 {
		t.Fatalf("expected refire after leaving done, got %d: %v", len(*calls), *calls)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/notify/ -run TestProcess`
Expected: FAIL — `Process` and `fireFunc` undefined.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/notify/notify.go`:

```go
// fireFunc is the platform notification sender. It is a package variable so
// tests can capture invocations.
var fireFunc = fire

// Process diffs ss against the persisted snapshot, firing a notification for
// each session that has just entered the done/question state, then persists the
// updated snapshot. The first time a session ID is seen its state is recorded
// silently (no notification) to avoid a flood for sessions already idle when
// observation begins. Sessions absent from ss are pruned from the snapshot.
//
// Errors are returned but are intended to be non-fatal to callers: a failure to
// notify must never break list/browse/watch.
func Process(ss []sessions.Session) error {
	d, err := dir()
	if err != nil {
		return err
	}
	snapPath := filepath.Join(d, snapshotFile)

	unlock, err := lock(filepath.Join(d, lockFile))
	if err != nil {
		return err
	}
	defer unlock()

	snap := loadSnapshot(snapPath)
	seen := make(map[string]bool, len(ss))

	for _, s := range ss {
		seen[s.ID] = true
		cur := notifiableState(s.State)
		prev, known := snap.Entries[s.ID]
		if !known {
			snap.Entries[s.ID] = cur
			continue
		}
		if cur == prev {
			continue
		}
		snap.Entries[s.ID] = cur
		if msg, ok := messageFor(cur, displayLabel(s)); ok {
			fireFunc(msg.text, msg.sound)
		}
	}

	for id := range snap.Entries {
		if !seen[id] {
			delete(snap.Entries, id)
		}
	}

	return saveSnapshot(snapPath, snap)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/notify/`
Expected: PASS (all notify tests).

- [ ] **Step 5: Commit**

```bash
git add internal/notify/notify.go internal/notify/notify_test.go
git commit -m "feat(notify): add Process transition-diff with silent first sighting"
```

---

## Task 6: notify package — platform fire implementations

**Files:**
- Create: `internal/notify/notify_darwin.go`
- Create: `internal/notify/notify_other.go`

- [ ] **Step 1: Write the darwin implementation**

Create `internal/notify/notify_darwin.go`:

```go
//go:build darwin

package notify

import "os/exec"

// fire displays a macOS notification with sound via osascript. Failures (e.g.
// running headless) are ignored.
func fire(title, sound string) {
	script := `display notification "` + escapeAppleScript(title) +
		`" sound name "` + escapeAppleScript(sound) + `"`
	_ = exec.Command("osascript", "-e", script).Run()
}
```

- [ ] **Step 2: Write the non-darwin implementation**

Create `internal/notify/notify_other.go`:

```go
//go:build !darwin

package notify

// fire is a no-op on non-macOS platforms.
func fire(title, sound string) {}
```

- [ ] **Step 3: Verify build and tests on the host platform**

Run: `go build ./internal/notify/ && go test ./internal/notify/`
Expected: builds and all tests PASS. (On macOS the darwin file compiles; tests still use the captured `fireFunc`, so no real notification is shown.)

- [ ] **Step 4: Commit**

```bash
git add internal/notify/notify_darwin.go internal/notify/notify_other.go
git commit -m "feat(notify): add macOS osascript fire and no-op fallback"
```

---

## Task 7: Wire `--notify` into `list`

**Files:**
- Modify: `cmd/list.go`

- [ ] **Step 1: Add the import**

In `cmd/list.go`, add to the import block (after the existing `internal/cache` import):

```go
	"github.com/yarma/tsession/internal/notify"
```

- [ ] **Step 2: Add the flag**

In `cmd/list.go`, in `List`, after the `lshort` flag declaration (currently line 36) add:

```go
	notifyFlag := fs.Bool("notify", false, "fire desktop notifications when sessions become done or ask a question (macOS only)")
```

- [ ] **Step 3: Call Process on the full session union**

In `cmd/list.go`, immediately after the `for _, warning := range warnings { ... }` loop (currently ends line 45) and before the `if *active {` block, insert:

```go
	if *notifyFlag {
		full := append([]sessions.Session(nil), local...)
		for _, name := range remoteNames {
			full = append(full, remoteMap[name]...)
		}
		if err := notify.Process(full); err != nil {
			fmt.Fprintln(os.Stderr, "warning: notify failed:", err)
		}
	}
```

(The union is built before the `--active` filter so notification tracking sees every session and never loses transitions when `--active` is also passed.)

- [ ] **Step 4: Verify build and a manual smoke run**

Run: `go build . && ./tsession list --notify >/dev/null`
Expected: builds; command runs without error. On the first run it silently seeds `~/.tsession/notify.json` (no notifications). Confirm the file exists:

Run: `test -f ~/.tsession/notify.json && echo OK`
Expected: `OK`.

- [ ] **Step 5: Commit**

```bash
git add cmd/list.go
git commit -m "feat(list): add --notify flag firing session notifications"
```

---

## Task 8: Wire `--notify` into `watch` (loop + daemon)

**Files:**
- Modify: `cmd/watch.go`

- [ ] **Step 1: Add the import**

In `cmd/watch.go`, add to the import block (after the existing `internal/cache` import):

```go
	"github.com/yarma/tsession/internal/notify"
```

- [ ] **Step 2: Add the flag and thread it through Watch**

In `cmd/watch.go`, in `Watch`, after the `daemon := fs.Bool(...)` line (currently line 34) add:

```go
	notifyFlag := fs.Bool("notify", false, "fire desktop notifications when sessions become done or ask a question (macOS only)")
```

Change the daemon spawn call (currently line 38):

```go
	if *daemon && os.Getenv(daemonEnvFlag) == "" {
		return spawnDaemon(*interval, *maxAge, *notifyFlag)
	}
```

Change both `refresh` calls in `Watch` (currently lines 52 and 60) to pass the flag:

```go
	if err := refresh(*interval, *maxAge, *notifyFlag); err != nil {
		fmt.Fprintln(os.Stderr, "warning: initial refresh failed:", err)
	}
```

and inside the `for` loop:

```go
		case <-tick.C:
			if err := refresh(*interval, *maxAge, *notifyFlag); err != nil {
				fmt.Fprintln(os.Stderr, "warning: refresh failed:", err)
			}
```

- [ ] **Step 3: Update `refresh` to accept and act on the flag**

In `cmd/watch.go`, change the `refresh` signature (currently line 103) and add the notify call before the final `return cache.Write(...)`:

```go
func refresh(interval, maxAge time.Duration, notifyEnabled bool) error {
```

Then, just before `return cache.Write(cache.File{` (currently line 130), insert:

```go
	if notifyEnabled {
		if err := notify.Process(allSessions); err != nil {
			fmt.Fprintln(os.Stderr, "warning: notify failed:", err)
		}
	}

```

- [ ] **Step 4: Update `spawnDaemon` to accept and forward the flag**

In `cmd/watch.go`, change the `spawnDaemon` signature (currently line 258):

```go
func spawnDaemon(interval, maxAge time.Duration, notifyEnabled bool) error {
```

In the `args` slice built inside `spawnDaemon` (currently lines 280-285), append `--notify` when enabled. Replace the `args := []string{...}` block with:

```go
	args := []string{
		self,
		"watch",
		"--interval=" + interval.String(),
		"--max-age=" + maxAge.String(),
	}
	if notifyEnabled {
		args = append(args, "--notify")
	}
```

- [ ] **Step 5: Verify build and daemon round-trip**

Run: `go build .`
Expected: builds.

Run: `./tsession watch --daemon --notify && sleep 1 && cat ~/.tsession/watch.log | tail -n 2 && ./tsession stop-watch`
Expected: daemon starts (prints pid), no fatal errors in the log, then stops cleanly.

- [ ] **Step 6: Commit**

```bash
git add cmd/watch.go
git commit -m "feat(watch): add --notify flag, propagated to the detached daemon"
```

---

## Task 9: Wire `--notify` into `browse` and fix `popup` call site

**Files:**
- Modify: `cmd/browse.go`
- Modify: `cmd/popup.go`

- [ ] **Step 1: Add the flag in Browse**

In `cmd/browse.go`, in `Browse`, after the `target := fs.String(...)` line (currently line 30) add:

```go
	notifyFlag := fs.Bool("notify", false, "fire desktop notifications when sessions become done or ask a question (macOS only)")
```

- [ ] **Step 2: Thread `notify` through the call sites in Browse**

In `cmd/browse.go`, update the non-watch call (currently line 45):

```go
		id, err := runFzf(*maxAge, query, false, *active, *short, *lshort, *localOnly, resolvedTarget, *notifyFlag)
```

And the watch-loop call (currently line 56):

```go
		id, err := runFzfOpts(*maxAge, query, false, *active, *short, *lshort, *localOnly, true, resolvedTarget, *notifyFlag)
```

- [ ] **Step 3: Update `runFzf` and `runFzfOpts` signatures**

In `cmd/browse.go`, change `runFzf` (currently lines 93-95):

```go
func runFzf(maxAge time.Duration, query string, popup, active, short bool, lshort int, localOnly bool, target string, notify bool) (string, error) {
	return runFzfOpts(maxAge, query, popup, active, short, lshort, localOnly, false, target, notify)
}
```

Change the `runFzfOpts` signature (currently line 97):

```go
func runFzfOpts(maxAge time.Duration, query string, popup, active, short bool, lshort int, localOnly bool, autoReload bool, target string, notify bool) (string, error) {
```

- [ ] **Step 4: Append `--notify` to the reload command**

In `cmd/browse.go`, in `runFzfOpts`, after the `if localOnly { reloadCmd += " --local-only" }` block (currently ends line 114) add:

```go
	if notify {
		reloadCmd += " --notify"
	}
```

- [ ] **Step 5: Fix the popup call site**

In `cmd/popup.go`, update the `runFzf` call (currently line 17) to pass `false` for the new `notify` parameter:

```go
	id, err := runFzf(*maxAge, "", true, *active, *short, *lshort, *localOnly, "", false)
```

- [ ] **Step 6: Verify build**

Run: `go build .`
Expected: builds with no error.

- [ ] **Step 7: Verify the reload command carries the flag**

Run: `grep -n 'reloadCmd += " --notify"' cmd/browse.go`
Expected: one match. This confirms `browse --watch --notify` propagates `--notify` into each `tsession list --fzf` reload.

- [ ] **Step 8: Commit**

```bash
git add cmd/browse.go cmd/popup.go
git commit -m "feat(browse): add --notify flag, propagated into fzf reload command"
```

---

## Task 10: Full build, test, and documentation

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`

- [ ] **Step 1: Run the full build and test suite**

Run: `go build . && go test ./...`
Expected: build succeeds; all tests PASS.

- [ ] **Step 2: Document the flag in README.md**

In `README.md`, find the flags table for `list`/`browse`/`popup` and add a row:

```markdown
| `--notify` | Fire a macOS desktop notification (with sound) when a session enters `done` or `question`. Off by default. Requires a long-running observer: `watch --daemon --notify` or `browse --watch --notify`. |
```

Also add a short paragraph near the `watch` section:

```markdown
### Notifications

Pass `--notify` to `watch`, `list`, or `browse` to get a macOS notification the
moment an agent finishes (`done`, sound "Tink") or asks a question (`question`,
sound "Funk"). The most common setups are `tsession watch --daemon --notify`
(background) or `tsession browse --watch --notify` (while browsing). State is
tracked in `~/.tsession/notify.json`; the first observation of each session is
recorded silently so you are not flooded on startup. macOS only — a no-op on
other platforms.
```

- [ ] **Step 3: Document in AGENTS.md**

In `AGENTS.md`, under the "Commands (full reference)" flags section, add a `--notify` row mirroring the README, and add a line to the `~/.tsession` data-source notes:

```markdown
| `~/.tsession/notify.json` | Last-notified state per session for `--notify` de-duplication |
```

- [ ] **Step 4: Verify docs build is unaffected**

Run: `go build .`
Expected: builds (docs changes don't affect the build; this is a sanity check).

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: document --notify flag and notify.json snapshot"
```

---

## Self-Review Notes

- **Spec coverage:** new package (Tasks 1-6), persisted snapshot + lock (Tasks 3-4), exactly-once via lock + silent first sighting (Task 5), macOS osascript + no-op (Task 6), `--notify` on `list`/`watch`/`browse` (Tasks 7-9), all-sessions/local+remote scope (Task 7 union; Task 8 `allSessions`), docs (Task 10). All spec sections map to a task.
- **Type consistency:** `Process([]sessions.Session) error`, `fireFunc`/`fire(title, sound string)`, `notifiableState`, `messageFor(state, label)`, `displayLabel`, `loadSnapshot`/`saveSnapshot`/`snapshot`, `escapeAppleScript`, `dir`/`lock` — names used identically across tasks and call sites.
- **No placeholders:** every code and command step is concrete.
