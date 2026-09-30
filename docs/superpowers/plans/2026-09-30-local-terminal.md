# Local Terminal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a persistent, always-present "Local terminal" entry to the `tsession serve` / GUI session list, reachable with `Alt+T`, backed by a dedicated tmux session that survives server restarts.

**Architecture:** A reserved tmux session (`tsession-local`) is created lazily in `$HOME` on first attach. The browser attaches to it through the existing *grouped* tmux mechanism (`tsession-web-<hash>`), so browser sizing never disturbs another client and startup reaping only removes the ephemeral grouped session — never the persistent one. A dedicated `GET /api/localterm` WebSocket route reuses `internal/webterm`'s PTY registry and the same WS bridge loop as agent sessions. The frontend renders a fixed pseudo-session row after the real sessions and binds `Alt+T`.

**Tech Stack:** Go 1.x (stdlib `net/http` with method+wildcard patterns), `github.com/coder/websocket`, `github.com/creack/pty`, vanilla browser JS (no build step, `go:embed`ed assets), tmux.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-09-30-local-terminal-design.md` (committed as `e5dabcf`).
- Dedicated tmux session name: `tsession-local` — **must not** start with `tsession-web-`, or `webterm.ReapOrphanedLocal` would kill it at every server start.
- The persistent tmux session is **never** killed by tsession: no teardown path, no shutdown path, no route may run `tmux kill-session -t tsession-local`.
- Starting directory is the user's home directory (`os.UserHomeDir()`); its default interactive shell is used (no explicit command argument to `tmux new-session`).
- The route accepts **no** client-supplied shell command, tmux target, or path. It takes no parameters at all.
- The local terminal is not an agent session: no rename, no repository alias, no code view, no notifications, no age, no agent state glyph.
- Reserved client-side/server-side ID: `__local-terminal__` (one constant, `attachcmd.LocalTerminalID`, referenced everywhere).
- Any change under `internal/webui/static/` requires bumping `CACHE_NAME` in `internal/webui/static/sw.js` (currently `tsession-shell-v15`).
- Follow existing package style: doc comments explain *why*; tests are table/behavior driven and never require a live tmux, SSH host, or `code` binary.

## File Structure

| File | Responsibility |
|---|---|
| `internal/attachcmd/local.go` (create) | Pure command building for the local terminal: reserved names + `BuildLocal` / `BuildLocalKill`. No process I/O. |
| `internal/attachcmd/local_test.go` (create) | Script-construction tests (names, ensure-then-group ordering, kill scope). |
| `internal/webui/terminal.go` (modify) | Extract the WebSocket bridge loop into a reusable `bridgeTerminal` method. |
| `internal/webui/localterm.go` (create) | `GET /api/localterm` handler: lazy attach via registry, home-dir resolution, error logging. |
| `internal/webui/localterm_test.go` (create) | Route behavior: 501 unconfigured, warm-PTY reuse, home-dir failure reporting. |
| `internal/webui/webui.go` (modify) | Register the route; add the `homeDirFn` field, default, and test seam. |
| `internal/webui/static/app.js` (modify) | Fixed local-terminal row, unified row model for keyboard nav, `Alt+T`, local WS URL. |
| `internal/webui/static/app.css` (modify) | Styling for the pinned row. |
| `internal/webui/static/index.html` (modify) | Hint text mentioning `Alt+T`. |
| `internal/webui/static/sw.js` (modify) | Cache version bump. |
| `internal/webui/static_assets_test.go` (modify) | Asserts the shipped assets actually contain the new behavior. |
| `AGENTS.md` (modify) | Document the feature, shortcut, and lifecycle guarantee. |

---

### Task 1: Local terminal command builder

**Files:**
- Create: `internal/attachcmd/local.go`
- Create: `internal/attachcmd/local_test.go`

**Interfaces:**
- Consumes: existing unexported `groupedAttachScript(web, target string) (string, error)` and exported `WebSessionName(origin, sessionID string) string` from `internal/attachcmd/attachcmd.go`; `shellutil.Quote(string) string`.
- Produces:
  - `const attachcmd.LocalTerminalID = "__local-terminal__"`
  - `const attachcmd.LocalSessionName = "tsession-local"`
  - `func attachcmd.LocalWebSessionName() string`
  - `func attachcmd.BuildLocal(home string) (bin string, args []string, err error)`
  - `func attachcmd.BuildLocalKill() (bin string, args []string)`

- [ ] **Step 1: Write the failing tests**

Create `internal/attachcmd/local_test.go`. (`shQ` already exists as a helper in `attachcmd_test.go` in this same package — reuse it, do not redefine it.)

```go
package attachcmd

import (
	"strings"
	"testing"

	"github.com/yarma/tsession/internal/tmux"
)

func TestLocalSessionName_IsNotReapedAsWebSession(t *testing.T) {
	// ReapOrphanedLocal kills every tmux session with the web prefix at
	// server startup. The persistent local terminal must survive that, so
	// its name must never carry the prefix.
	if strings.HasPrefix(LocalSessionName, tmux.WebSessionPrefix) {
		t.Fatalf("LocalSessionName %q must not start with %q", LocalSessionName, tmux.WebSessionPrefix)
	}
}

func TestBuildLocal_EnsuresPersistentSessionThenGroupedAttach(t *testing.T) {
	bin, args, err := BuildLocal("/Users/someone")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bin != "sh" || len(args) != 2 || args[0] != "-c" {
		t.Fatalf("unexpected command shape: bin=%q args=%v", bin, args)
	}
	script := args[1]
	web := LocalWebSessionName()

	// The persistent session must be created in the home directory, with no
	// trailing command argument: `tmux new-session -d -s X -c DIR` with
	// nothing after DIR starts the user's own default shell, which is what
	// the spec calls for. The clause therefore ends immediately at "; ".
	ensure := "tmux has-session -t " + shQ(LocalSessionName) +
		" 2>/dev/null || tmux new-session -d -s " + shQ(LocalSessionName) +
		" -c " + shQ("/Users/someone") + "; "
	if !strings.HasPrefix(script, ensure) {
		t.Fatalf("script must start by ensuring the persistent session %q:\n%s", ensure, script)
	}

	// Everything after that clause is the ordinary grouped attach, so the
	// persistent session necessarily exists before it is mirrored.
	for _, want := range []string{
		"tmux has-session -t " + shQ(web),
		"tmux new-session -d -s " + shQ(web) + " -t " + shQ(LocalSessionName),
		"exec tmux attach-session -t " + shQ(web),
	} {
		if !strings.Contains(strings.TrimPrefix(script, ensure), want) {
			t.Errorf("grouped attach missing %q:\n%s", want, script)
		}
	}
}

func TestBuildLocal_EmptyHomeIsAnError(t *testing.T) {
	if _, _, err := BuildLocal(""); err == nil {
		t.Fatal("expected an error for an empty home directory")
	}
}

func TestBuildLocalKill_OnlyKillsTheGroupedSession(t *testing.T) {
	bin, args := BuildLocalKill()
	if bin != "sh" || len(args) != 2 || args[0] != "-c" {
		t.Fatalf("unexpected command shape: bin=%q args=%v", bin, args)
	}
	script := args[1]
	if !strings.Contains(script, "tmux kill-session -t "+shQ(LocalWebSessionName())) {
		t.Errorf("expected the grouped session to be killed:\n%s", script)
	}
	if strings.Contains(script, shQ(LocalSessionName)) {
		t.Fatalf("teardown must never kill the persistent session %q:\n%s", LocalSessionName, script)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/attachcmd/ -run 'Local' -v`
Expected: FAIL — compile errors, `undefined: LocalSessionName`, `undefined: BuildLocal`, `undefined: BuildLocalKill`, `undefined: LocalWebSessionName`.

- [ ] **Step 3: Write the implementation**

Create `internal/attachcmd/local.go`:

```go
package attachcmd

import (
	"errors"

	"github.com/yarma/tsession/internal/shellutil"
)

// LocalTerminalID is the reserved session ID for the web UI's dedicated
// local terminal. It is not a real agent session: no session store ever
// produces this ID, so it can never collide with a Copilot or pi session.
const LocalTerminalID = "__local-terminal__"

// LocalSessionName is the tmux session that hosts the dedicated local
// terminal's shell. It deliberately does NOT use tmux.WebSessionPrefix:
// webterm.ReapOrphanedLocal kills every prefixed session at server startup,
// and this one must outlive `tsession serve` / the GUI so reopening either
// returns to the same shell and scrollback.
const LocalSessionName = "tsession-local"

// LocalWebSessionName returns the ephemeral grouped tmux session the browser
// attaches through. Unlike LocalSessionName, this one is disposable: it is
// reaped at startup and torn down on explicit close.
func LocalWebSessionName() string {
	return WebSessionName("", LocalTerminalID)
}

// BuildLocal returns the command that attaches a browser terminal to the
// dedicated local shell. It first ensures the persistent tmux session exists
// (created in home, running the user's default shell), then attaches through
// a grouped session so the browser's window size never resizes another
// client attached to the same shell.
//
// The `has-session || new-session` pair is not atomic, but a lost race is
// harmless: the duplicate `new-session` fails, the shell continues past it,
// and the grouped attach that follows finds the session the winner created.
func BuildLocal(home string) (string, []string, error) {
	if home == "" {
		return "", nil, errors.New("attachcmd: empty home directory for the local terminal")
	}

	nameQ := shellutil.Quote(LocalSessionName)
	ensure := "tmux has-session -t " + nameQ + " 2>/dev/null || " +
		"tmux new-session -d -s " + nameQ + " -c " + shellutil.Quote(home) + "; "

	grouped, err := groupedAttachScript(LocalWebSessionName(), LocalSessionName)
	if err != nil {
		return "", nil, err
	}
	return "sh", []string{"-c", ensure + grouped}, nil
}

// BuildLocalKill returns the command that tears down only the *grouped*
// session the browser attached through. The persistent LocalSessionName is
// intentionally left running — that is what makes the local terminal
// survive a server restart.
func BuildLocalKill() (string, []string) {
	script := "tmux kill-session -t " + shellutil.Quote(LocalWebSessionName())
	return "sh", []string{"-c", script}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/attachcmd/ -v`
Expected: PASS, including the pre-existing tests in this package.

- [ ] **Step 5: Commit**

```bash
git add internal/attachcmd/local.go internal/attachcmd/local_test.go
git commit -m "feat(attachcmd): build persistent local terminal attach command"
```

---

### Task 2: `/api/localterm` route

**Files:**
- Modify: `internal/webui/terminal.go` (extract the WebSocket bridge loop from `handleTerminal`)
- Create: `internal/webui/localterm.go`
- Create: `internal/webui/localterm_test.go`
- Modify: `internal/webui/webui.go` (`Server` struct fields, `NewServer` defaults, `Handler` route table)

**Interfaces:**
- Consumes: `attachcmd.LocalTerminalID`, `attachcmd.BuildLocal(home string) (string, []string, error)`, `attachcmd.BuildLocalKill() (string, []string)` from Task 1; existing `webterm.Registry.Live/Attach`, `webterm.Spec`, `Server.logInteraction(action, sessionID, origin, name, detail, level string)`.
- Produces:
  - route `GET /api/localterm`
  - `func (s *Server) SetLocalTerminalHome(fn func() (string, error))` (test seam)
  - `func (s *Server) bridgeTerminal(w http.ResponseWriter, r *http.Request, term *webterm.Terminal)`

- [ ] **Step 1: Write the failing tests**

Create `internal/webui/localterm_test.go`. It reuses `dialTerminal` from `terminal_test.go` (same package).

```go
package webui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/yarma/tsession/internal/attachcmd"
	"github.com/yarma/tsession/internal/sessions"
	"github.com/yarma/tsession/internal/webterm"
)

func TestHandleLocalTerminal_NotConfiguredReturns501(t *testing.T) {
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/localterm"
	_, resp, err := websocket.Dial(context.Background(), url, nil)
	if err == nil {
		t.Fatal("expected dial to fail when terminal support is not configured")
	}
	if resp == nil || resp.StatusCode != 501 {
		t.Fatalf("expected 501, got resp=%+v", resp)
	}
}

// The local terminal must never depend on the agent session list: it is not
// a discovered session, so a completely empty (or failing) session provider
// must not stop it from attaching.
func TestHandleLocalTerminal_WarmPTYServesWithoutAnySessions(t *testing.T) {
	registry := webterm.NewRegistry()
	t.Cleanup(func() { _ = registry.Shutdown() })

	srv := NewServer(
		func() ([]sessions.Session, error) { return nil, errors.New("session load must not be consulted") },
		WithTerminal(registry),
	)

	// Substitute a harmless command for the real tmux attach, exactly as
	// terminal_test.go does: this test covers the route and the registry
	// key, not command construction (covered by attachcmd's own tests).
	key := webterm.Key{Origin: "", ID: attachcmd.LocalTerminalID}
	if _, err := registry.Attach(key, webterm.Spec{Bin: "sh", Args: []string{"-c", "cat"}}); err != nil {
		t.Fatalf("pre-attach: %v", err)
	}

	conn := dialTerminal(t, srv, "/api/localterm")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageBinary, []byte("hello\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	var got []byte
	for time.Now().Before(deadline) {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if typ != websocket.MessageBinary {
			continue
		}
		got = append(got, data...)
		if strings.Contains(string(got), "hello") {
			break
		}
	}
	if !strings.Contains(string(got), "hello") {
		t.Fatalf("expected PTY echo to contain %q, got %q", "hello", got)
	}
}

// Reconnecting must reuse the same PTY rather than spawning a second shell,
// which is what makes the terminal's scrollback survive a tab close.
func TestHandleLocalTerminal_ReconnectReusesSamePTY(t *testing.T) {
	registry := webterm.NewRegistry()
	t.Cleanup(func() { _ = registry.Shutdown() })

	srv := NewServer(
		func() ([]sessions.Session, error) { return nil, nil },
		WithTerminal(registry),
	)

	key := webterm.Key{Origin: "", ID: attachcmd.LocalTerminalID}
	want, err := registry.Attach(key, webterm.Spec{Bin: "sh", Args: []string{"-c", "cat"}})
	if err != nil {
		t.Fatalf("pre-attach: %v", err)
	}

	conn := dialTerminal(t, srv, "/api/localterm")
	conn.CloseNow()

	got, ok := registry.Live(key)
	if !ok {
		t.Fatal("expected the local terminal PTY to stay warm after the socket closed")
	}
	if got != want {
		t.Fatal("expected the same PTY instance to be reused")
	}
}

func TestHandleLocalTerminal_HomeDirFailureIsReportedAndLogged(t *testing.T) {
	registry := webterm.NewRegistry()
	t.Cleanup(func() { _ = registry.Shutdown() })

	srv := NewServer(
		func() ([]sessions.Session, error) { return nil, nil },
		WithTerminal(registry),
	)
	srv.SetLocalTerminalHome(func() (string, error) { return "", errors.New("no home for you") })

	var logs bytes.Buffer
	srv.SetDebugLog(func(format string, args ...any) {
		_, _ = fmt.Fprintf(&logs, format+"\n", args...)
	})

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/localterm"
	_, resp, err := websocket.Dial(context.Background(), url, nil)
	if err == nil {
		t.Fatal("expected dial to fail when the home directory cannot be resolved")
	}
	if resp == nil || resp.StatusCode != 500 {
		t.Fatalf("expected 500, got resp=%+v", resp)
	}
	got := logs.String()
	for _, want := range []string{"action=local-terminal-failed", "level=error", "no home for you"} {
		if !strings.Contains(got, want) {
			t.Fatalf("debug log missing %q: %s", want, got)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/webui/ -run 'LocalTerminal' -v`
Expected: FAIL — `srv.SetLocalTerminalHome` undefined; once that compiles, the route returns 404 instead of 501/500.

- [ ] **Step 3: Extract the WebSocket bridge from `handleTerminal`**

In `internal/webui/terminal.go`, replace everything in `handleTerminal` from `conn, err := websocket.Accept(w, r, nil)` to the end of the function body with a single call, and add the extracted method below it:

```go
	s.bridgeTerminal(w, r, term)
}

// bridgeTerminal upgrades the request to a WebSocket and pumps bytes
// between it and term for the life of the connection: PTY output is sent as
// binary messages, client keystrokes arrive as binary messages, and JSON
// text messages carry out-of-band control instructions (resize). Returning
// only ends this one connection — the PTY itself outlives it, so closing a
// browser tab merely unsubscribes.
func (s *Server) bridgeTerminal(w http.ResponseWriter, r *http.Request, term *webterm.Terminal) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		// Accept has already written an error response.
		return
	}
	defer conn.CloseNow()

	ctx := context.Background()
	unsubscribe, err := term.Subscribe(&wsBinaryWriter{ctx: ctx, conn: conn})
	if err != nil {
		return
	}
	defer unsubscribe()

	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		switch typ {
		case websocket.MessageBinary:
			_, _ = term.Write(data)
		case websocket.MessageText:
			var msg terminalControlMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			if msg.Type == "resize" && msg.Rows > 0 && msg.Cols > 0 {
				_ = term.Resize(msg.Rows, msg.Cols)
			}
		}
	}
}
```

- [ ] **Step 4: Run the existing terminal tests to confirm the refactor is behavior-preserving**

Run: `go test ./internal/webui/ -run 'HandleTerminal' -v`
Expected: PASS — all pre-existing `TestHandleTerminal_*` tests still pass.

- [ ] **Step 5: Add the handler**

Create `internal/webui/localterm.go`:

```go
package webui

import (
	"net/http"
	"os/exec"

	"github.com/yarma/tsession/internal/attachcmd"
	"github.com/yarma/tsession/internal/webterm"
)

// handleLocalTerminal serves GET /api/localterm: a dedicated shell that is
// not an agent session at all. It exists so the web UI and the GUI can run
// commands like `tsession new` without switching to another application.
//
// Two things distinguish it from handleTerminal. First, it never consults
// the session list — there is no session to look up — so it works even when
// no agent session exists or the remote-backed session load is failing.
// Second, its teardown removes only the ephemeral grouped tmux session; the
// shell itself lives in attachcmd.LocalSessionName, which tsession never
// kills, so reopening `serve` or the GUI returns to the same shell and
// scrollback.
//
// The request carries no parameters: the command, the tmux target, and the
// working directory are all fixed by the server.
func (s *Server) handleLocalTerminal(w http.ResponseWriter, r *http.Request) {
	if s.registry == nil {
		http.Error(w, "terminal support not configured", http.StatusNotImplemented)
		return
	}

	key := webterm.Key{Origin: "", ID: attachcmd.LocalTerminalID}

	term, ok := s.registry.Live(key)
	if !ok {
		home, err := s.homeDirFn()
		if err != nil {
			s.failLocalTerminal(w, "cannot determine the home directory", err)
			return
		}
		bin, args, err := attachcmd.BuildLocal(home)
		if err != nil {
			s.failLocalTerminal(w, "cannot build the local terminal command", err)
			return
		}

		teardown := func() error {
			killBin, killArgs := attachcmd.BuildLocalKill()
			// Best-effort: the grouped session may already be gone. The
			// persistent shell session is deliberately left running.
			_ = exec.Command(killBin, killArgs...).Run()
			return nil
		}

		term, err = s.registry.Attach(key, webterm.Spec{
			Bin: bin, Args: args, Rows: 24, Cols: 80, Teardown: teardown,
		})
		if err != nil {
			s.failLocalTerminal(w, "cannot start the local terminal", err)
			return
		}
	}

	s.bridgeTerminal(w, r, term)
}

// failLocalTerminal reports a startup failure to the browser (which shows it
// in the terminal pane's banner) and to the serve log, so `grep level=error`
// finds it alongside every other failure.
func (s *Server) failLocalTerminal(w http.ResponseWriter, msg string, err error) {
	s.logInteraction("local-terminal-failed", attachcmd.LocalTerminalID, "", "Local terminal", msg+": "+err.Error(), "error")
	http.Error(w, msg+": "+err.Error(), http.StatusInternalServerError)
}
```

- [ ] **Step 6: Wire the field, default, seam, and route**

In `internal/webui/webui.go`, add the import `"os"` if it is not already present (it is, for `defaultNotifyStorePath`), then add the field to `Server` immediately after `registry *webterm.Registry`:

```go
	homeDirFn  func() (string, error)
```

In `NewServer`'s struct literal, add after `now: time.Now,`:

```go
		homeDirFn:       os.UserHomeDir,
```

Add the test seam next to `SetDebugLog`:

```go
// SetLocalTerminalHome overrides how the dedicated local terminal resolves
// its starting directory. Tests use this to exercise the failure path
// without touching the real environment; production callers leave the
// os.UserHomeDir default.
func (s *Server) SetLocalTerminalHome(fn func() (string, error)) {
	s.homeDirFn = fn
}
```

Register the route in `Handler()`, directly after the `GET /api/terminal/{origin}/{id}` line:

```go
	mux.HandleFunc("GET /api/localterm", s.handleLocalTerminal)
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/webui/ -run 'Terminal' -v`
Expected: PASS — the four new `TestHandleLocalTerminal_*` tests plus every pre-existing `TestHandleTerminal_*`.

- [ ] **Step 8: Commit**

```bash
git add internal/webui/localterm.go internal/webui/localterm_test.go internal/webui/terminal.go internal/webui/webui.go
git commit -m "feat(webui): serve a persistent local terminal at /api/localterm"
```

---

### Task 3: Pinned list row and `Alt+T`

**Files:**
- Modify: `internal/webui/static/app.js`
- Modify: `internal/webui/static/app.css`
- Modify: `internal/webui/static/index.html`
- Modify: `internal/webui/static/sw.js`
- Modify: `internal/webui/static_assets_test.go`

**Interfaces:**
- Consumes: route `GET /api/localterm` from Task 2; existing `ensurePane(key)`, `showPane(key)`, `connectPane(pane, s)`, `selectSession(s)`, `sessionKey(s)`, `focusTerminal()`, `focusList()`, `moveListCursor(delta)`, `reportUserInteraction(action, s, detail, level)` in `app.js`.
- Produces: the `LOCAL_TERMINAL` pseudo-session object and `listRows()` in `app.js`; no cross-file interface.

Background for the implementer: `state.listIndex` is the keyboard cursor. Today it indexes `state.sessions` directly. The pinned row must be reachable with the same arrow keys, so every consumer of `listIndex` is switched to a single `listRows()` array — `state.sessions` followed by the pinned row — and `state.sessions` is never mutated to contain the pseudo row (the session-refresh poll overwrites it wholesale, and `notifyFor` searches it by real session ID).

- [ ] **Step 1: Write the failing tests**

Append to `internal/webui/static_assets_test.go`:

```go
func TestStaticAssets_LocalTerminalRowIsPinnedAfterSessions(t *testing.T) {
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })

	for path, wants := range map[string][]string{
		"/app.js": {
			`const LOCAL_TERMINAL = {`,
			`localTerminal: true`,
			"function listRows()",
			"return state.sessions.concat([LOCAL_TERMINAL]);",
			"function openLocalTerminal()",
			`ev.code === "KeyT"`,
			`"/api/localterm"`,
		},
		"/app.css": {".session-row.local-terminal"},
		"/":        {"Alt+T terminal"},
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d", path, rec.Code)
		}
		for _, want := range wants {
			if !strings.Contains(rec.Body.String(), want) {
				t.Fatalf("GET %s: body missing %q", path, want)
			}
		}
	}
}

// The pinned row is not an agent session, so the code view and the two
// rename paths must skip it rather than sending it to an API that would
// reject it.
func TestStaticAssets_LocalTerminalRowIsNotRenamableOrCodeViewable(t *testing.T) {
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{
		"if (!s || s.localTerminal) return;",
		"if (s.localTerminal) return;",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("app.js missing guard %q", want)
		}
	}
}

// Any static asset change without a cache bump leaves the service worker
// serving the previous app shell.
func TestStaticAssets_ServiceWorkerCacheVersionIsCurrent(t *testing.T) {
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })
	req := httptest.NewRequest(http.MethodGet, "/sw.js", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `const CACHE_NAME = "tsession-shell-v16";`) {
		t.Fatal("sw.js CACHE_NAME was not bumped for this change")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/webui/ -run 'StaticAssets_LocalTerminal|StaticAssets_ServiceWorker' -v`
Expected: FAIL with `body missing "const LOCAL_TERMINAL = {"` and the sw.js assertion.

- [ ] **Step 3: Add the pseudo-session and the unified row model in `app.js`**

Immediately after the `state` object literal (before `const appEl = ...`), add:

```js
  // The dedicated local terminal is rendered as a pinned row after the real
  // sessions so there is always somewhere to run commands like
  // `tsession new` without leaving the app. It is deliberately NOT pushed
  // into state.sessions: that array is replaced wholesale by every refresh
  // poll and is searched by real session ID for notifications.
  const LOCAL_TERMINAL = {
    id: "__local-terminal__",
    origin: "",
    localTerminal: true,
    name: "Local terminal",
    summary: "Shell in your home directory",
  };

  // listRows is the keyboard-navigable row model: every consumer of
  // state.listIndex indexes this, not state.sessions.
  function listRows() {
    return state.sessions.concat([LOCAL_TERMINAL]);
  }
```

- [ ] **Step 4: Render the pinned row**

In `renderSessions()`, replace the closing lines of the function — currently:

```js
      listEl.appendChild(li);
    });
    renderSessionInfo();
  }
```

with:

```js
      listEl.appendChild(li);
    });
    renderLocalTerminalRow(state.sessions.length);
    renderSessionInfo();
  }

  // renderLocalTerminalRow appends the pinned row. It shows none of an
  // agent session's decorations (state glyph, source, repository, age,
  // summary-from-the-agent) because it has no agent behind it.
  function renderLocalTerminalRow(index) {
    const key = sessionKey(LOCAL_TERMINAL);
    const li = document.createElement("li");
    let cls = "session-row local-terminal";
    if (key === state.selectedKey) cls += " selected";
    if (state.focusTarget === "list" && index === state.listIndex) cls += " cursor";
    li.className = cls;
    li.dataset.key = key;

    const line1 = document.createElement("div");
    line1.className = "line1";

    const glyph = document.createElement("span");
    glyph.className = "glyph";
    glyph.textContent = "\u276F"; // ❯
    line1.appendChild(glyph);

    const label = document.createElement("span");
    label.className = "worktree";
    label.textContent = LOCAL_TERMINAL.name;
    label.title = "Persistent shell in your home directory (Alt+T)";
    line1.appendChild(label);

    const shortcut = document.createElement("span");
    shortcut.className = "age";
    shortcut.textContent = "Alt+T";
    line1.appendChild(shortcut);

    li.appendChild(line1);

    const summary = document.createElement("div");
    summary.className = "summary";
    summary.textContent = LOCAL_TERMINAL.summary;
    li.appendChild(summary);

    li.addEventListener("click", () => {
      state.listIndex = index;
      selectSession(LOCAL_TERMINAL);
    });
    listEl.appendChild(li);
  }
```

- [ ] **Step 5: Point every `listIndex` consumer at `listRows()`**

Five edits in `app.js`.

In `renderSessionInfo()`, replace `const s = state.sessions[state.listIndex];` and the body that follows it down to the first `addInfoRow("ID", ...)` call with:

```js
    const s = listRows()[state.listIndex];
    if (!s) {
      const empty = document.createElement("div");
      empty.className = "info-empty";
      empty.textContent = "Select a session to inspect.";
      infoEl.appendChild(empty);
      return;
    }

    if (s.localTerminal) {
      addInfoRow("Name", s.name);
      addInfoRow("Kind", "local terminal");
      addInfoRow("CWD", "home directory");
      addInfoRow("Tmux", "tsession-local");
      addInfoRow("Summary", s.summary, "info-summary");
      return;
    }

    addInfoRow("ID", infoValue(s.id));
```

In `refreshSessions()`, replace the two `state.listIndex` clamp lines:

```js
      if (state.listIndex == null) state.listIndex = 0;
      if (state.listIndex >= state.sessions.length) {
        state.listIndex = Math.max(0, state.sessions.length - 1);
      }
```

with:

```js
      if (state.listIndex == null) state.listIndex = 0;
      const rowCount = listRows().length;
      if (state.listIndex >= rowCount) state.listIndex = rowCount - 1;
```

In `selectSession(s)`, replace:

```js
    const idx = state.sessions.findIndex((x) => sessionKey(x) === state.selectedKey);
```

with:

```js
    const idx = listRows().findIndex((x) => sessionKey(x) === state.selectedKey);
```

Replace `moveListCursor`, `attachHighlighted`, and `cursorSession` wholesale:

```js
  function moveListCursor(delta) {
    const rows = listRows();
    if (rows.length === 0) return;
    const cur = state.listIndex == null ? 0 : state.listIndex;
    state.listIndex = Math.min(rows.length - 1, Math.max(0, cur + delta));
    renderSessions();
    const row = listEl.children[state.listIndex];
    if (row) row.scrollIntoView({ block: "nearest" });
  }

  function attachHighlighted() {
    const s = cursorSession();
    if (s) selectSession(s);
  }

  function cursorSession() {
    if (state.listIndex == null) return null;
    return listRows()[state.listIndex] || null;
  }
```

- [ ] **Step 6: Guard the agent-only actions**

In `app.js`, replace `renameCursorSession` and `renameCursorRepo`:

```js
  // renameCursorSession/renameCursorRepo mirror the TUI picker's ctrl-n /
  // ctrl-a bindings, acting on the keyboard cursor row. The pinned local
  // terminal has no stored name and no repository, so both skip it.
  function renameCursorSession() {
    const s = cursorSession();
    if (!s || s.localTerminal) return;
    openRenameModal("session", s.id, s.name || "");
  }

  function renameCursorRepo() {
    const s = cursorSession();
    if (!s || s.localTerminal) return;
    if (!s.repositoryId) return;
    openRenameModal("repo", s.repositoryId, s.repository || "");
  }
```

In `toggleCodeView()`, replace:

```js
    if (state.listIndex == null) return;
    const s = state.sessions[state.listIndex];
    if (!s) return;
```

with:

```js
    const s = cursorSession();
    if (!s) return;
    // The pinned local terminal has no repository to open in VS Code.
    if (s.localTerminal) return;
```

- [ ] **Step 7: Route the pinned row's WebSocket to `/api/localterm`**

In `connectPane(pane, s)`, replace:

```js
    const originSegment = s.origin ? encodeURIComponent(s.origin) : "local";
    const url = wsScheme() + "//" + location.host + "/api/terminal/" + originSegment + "/" + encodeURIComponent(s.id);
```

with:

```js
    const originSegment = s.origin ? encodeURIComponent(s.origin) : "local";
    // The pinned local terminal is not a discovered session, so it has its
    // own parameterless route rather than a /{origin}/{id} pair.
    const path = s.localTerminal
      ? "/api/localterm"
      : "/api/terminal/" + originSegment + "/" + encodeURIComponent(s.id);
    const url = wsScheme() + "//" + location.host + path;
```

- [ ] **Step 8: Bind `Alt+T`**

In `app.js`, add `openLocalTerminal` immediately before the `const SUPPRESSABLE_KEYS` declaration:

```js
  // openLocalTerminal is Alt+T's handler. Unlike Alt+E it works from either
  // panel, including while the terminal has focus, so the shell is always
  // one chord away.
  function openLocalTerminal() {
    state.listIndex = listRows().length - 1;
    selectSession(LOCAL_TERMINAL);
    focusTerminal();
  }
```

Then, inside the capture-phase `document.addEventListener("keydown", ...)` handler, add this block directly after the existing `ev.code === "Slash"` block and before the `ev.code === "KeyE"` block:

```js
      // Alt+T jumps to the pinned local terminal from anywhere. It must be
      // handled here, in the capture phase, because xterm.js's key handler
      // would otherwise translate the chord into an ESC sequence and send
      // it to the PTY (see altEscapeSequence).
      if (ev.altKey && !ev.ctrlKey && !ev.metaKey && !ev.shiftKey && ev.code === "KeyT") {
        ev.preventDefault();
        ev.stopPropagation();
        openLocalTerminal();
        return;
      }
```

- [ ] **Step 9: Style the row and update the hints**

Append to `internal/webui/static/app.css`:

```css
.session-row.local-terminal {
  border-top: 1px solid var(--border, #2a2a2a);
  margin-top: 4px;
}

.session-row.local-terminal .glyph {
  color: var(--active);
}

.session-row.local-terminal .worktree {
  font-weight: 600;
}
```

In `internal/webui/static/index.html`, replace the first hint line:

```html
    <div id="sessions-hint">Alt+/ focus &middot; Alt+H hide &middot; Alt+E code view &middot; &uarr;&darr; move &middot; &crarr; attach</div>
```

with:

```html
    <div id="sessions-hint">Alt+/ focus &middot; Alt+H hide &middot; Alt+T terminal &middot; Alt+E code view &middot; &uarr;&darr; move &middot; &crarr; attach</div>
```

and add a hint item to the second hint block, directly after the `Alt+H hide` item:

```html
      <span class="hint-item">Alt+T terminal</span>
```

- [ ] **Step 10: Bump the service worker cache**

In `internal/webui/static/sw.js`, change:

```js
const CACHE_NAME = "tsession-shell-v15";
```

to:

```js
const CACHE_NAME = "tsession-shell-v16";
```

- [ ] **Step 11: Run the tests to verify they pass**

Run: `go test ./internal/webui/ -v`
Expected: PASS, including the three new `TestStaticAssets_*` tests and every pre-existing one.

- [ ] **Step 12: Verify by hand**

Run: `go run . serve --open`

Check, in order:
1. A "Local terminal" row appears at the bottom of the sidebar.
2. Pressing `Alt+T` attaches to it and focus lands in the terminal; `pwd` prints your home directory.
3. Run `echo marker-one`, switch to an agent session, then press `Alt+T` again — `marker-one` is still on screen.
4. From the local terminal, run `tmux ls`; both `tsession-local` and a `tsession-web-*` session are listed.
5. Stop the server with Ctrl-C, run `tmux ls` — `tsession-local` is still there and `tsession-web-*` is gone.
6. Restart `go run . serve`, press `Alt+T` — `marker-one` is still on screen.
7. With the cursor on the pinned row, press `ctrl-n`, `ctrl-a`, and `Alt+E` — nothing happens, and no error appears in the server log.
8. Arrow up and down through the whole list — the cursor reaches the pinned row and every agent row, and the info panel below tracks it.

- [ ] **Step 13: Commit**

```bash
git add internal/webui/static internal/webui/static_assets_test.go
git commit -m "feat(webui): pin a local terminal row and bind Alt+T"
```

---

### Task 4: Documentation and full verification

**Files:**
- Modify: `AGENTS.md`
- Copy: `docs/superpowers/plans/2026-09-30-local-terminal.md` (this plan)

**Interfaces:**
- Consumes: the finished behavior from Tasks 1-3.
- Produces: nothing code-facing.

- [ ] **Step 1: Document the feature**

In `AGENTS.md`, inside the "Web UI (`serve`)" section, add this subsection immediately before the "### Code view (`Alt+E`)" heading:

```markdown
### Local terminal (`Alt+T`)

A pinned **Local terminal** row sits after the session list and is always
present, even with no agent sessions. `Alt+T` attaches to it from either
panel (it is handled in the document's capture phase so xterm.js does not
translate the chord into an ESC sequence first).

It is not a discovered session: it has no agent state, rename, repository
alias, code view, or notifications, and `GET /api/localterm`
(`internal/webui/localterm.go`) takes no parameters at all — the command,
tmux target, and working directory are fixed by the server, and the session
list is never consulted.

Its shell lives in a tmux session named `attachcmd.LocalSessionName`
(`tsession-local`), created lazily in `$HOME` on first attach and running the
user's default shell. That name deliberately avoids `tmux.WebSessionPrefix`,
because `webterm.ReapOrphanedLocal` kills every prefixed session at startup.
The browser attaches through the usual ephemeral grouped session
(`attachcmd.LocalWebSessionName()`), and teardown — `attachcmd.BuildLocalKill`
— kills only that grouped session. **tsession never kills `tsession-local`**,
which is what makes the shell and its scrollback survive closing the browser,
`serve`, or the GUI.
```

Also add `Alt+T` to the picker-shortcut table in the same section, as a row above `ctrl-n`:

```markdown
| `Alt+T` | Attach to the pinned local terminal (works from either panel) |
```

- [ ] **Step 2: Store the plan alongside the spec**

```bash
cp "$SESSION_PLAN" docs/superpowers/plans/2026-09-30-local-terminal.md
```

Where `$SESSION_PLAN` is this file's path in the session folder. If the
plan was authored directly in the repository, skip this step.

- [ ] **Step 3: Run the full test suite**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS, with no new failures compared to the pre-change baseline.

- [ ] **Step 4: Commit**

```bash
git add AGENTS.md docs/superpowers/plans/2026-09-30-local-terminal.md
git commit -m "docs: describe the pinned local terminal and its lifecycle"
```

---

## Notes and considerations

- **Why a pseudo-session instead of a real one:** synthesizing a
  `sessions.Session` would put the shell through agent-state discovery,
  sorting, active-filtering, notification diffing, and alias resolution —
  all of which could hide it or reorder it away from the bottom. A pinned
  client-side row plus a parameterless route keeps it unconditionally
  present.
- **Why `tsession-local` is not `tsession-web-*`:** `ReapOrphanedLocal` runs
  on every `serve`/GUI start and kills every session carrying the web
  prefix. Naming the persistent session with that prefix would silently
  destroy the shell on every restart — precisely the behavior this feature
  exists to avoid. Task 1's first test locks this in.
- **Race on first attach:** `webterm.Registry.Attach` holds its mutex while
  creating a terminal for a key, so two simultaneous first attaches produce
  one PTY. The `has-session || new-session` pair inside the script is not
  atomic, but a lost race just fails the duplicate `new-session` and
  proceeds to attach to the winner's session.
- **GUI needs no separate work:** `gui/app.go` embeds the same
  `webui.Server` via `cmd.BuildEmbeddedServer`, so the route and the row
  appear there automatically. Step 12's manual check can optionally be
  repeated in the GUI.
- **Not in scope:** a "New session" form, creating worktrees from the
  browser, multiple local terminals, and choosing the starting directory.
  `tsession new` remains the way to create agent sessions — the point of
  this feature is that you can now run it without leaving the app.
