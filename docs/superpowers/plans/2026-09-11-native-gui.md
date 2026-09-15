# Native GUI (`tsession gui`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tsession gui` launcher subcommand plus a separate `gui/` Wails v2 module that embeds the existing `internal/webui` server and opens it in a native OS window.

**Architecture:** Extract a small, reusable "build the embedded web UI server" constructor out of `cmd/serve.go` so both `tsession serve` and the new Wails app use the exact same session-loading, PTY-registry, and HTTP-handler wiring. The Wails app (`gui/` — its own Go module) starts that server on a loopback listener, then opens a native window whose embedded page redirects to that server's URL. `cmd/gui.go` is a thin launcher in the main CLI binary: it finds the already-built native app on disk and starts it — it does not build or embed Wails itself.

**Tech Stack:** Go 1.25, `github.com/wailsapp/wails/v2` (new dependency, isolated in the `gui/` module only), existing `internal/webui` / `internal/webterm` packages, no npm/Vite/Node — the embedded frontend is a few lines of hand-written HTML/JS.

## Global Constraints

- The root module (`go.mod` at the repo root) must **not** gain Wails or any CGO dependency — `go build ./...` from the repo root stays exactly as fast/portable as it is today. (Per design: "keeps CGO-free and cross-compilable exactly as it is today.")
- `gui/` is a **separate Go module** with its own `go.mod` and a `replace github.com/yarma/tsession => ../` directive.
- The embedded frontend must not require npm/Vite/webpack — plain static HTML/JS only, matching the existing `internal/webui/static` philosophy of "vendored/hand-written, no Node toolchain."
- `tsession gui` (the CLI launcher) never builds the native app itself — build is `wails build` in `gui/`, run manually by a developer/release pipeline. The launcher only searches for and starts an already-built artifact.
- `--addr`/loopback-only behavior of the embedded server must match `tsession serve`'s existing rule: PTYs must never be reachable off-host.
- Every new Go file needs tests using the same table-driven style as existing `cmd/*_test.go` files (see `cmd/serve_test.go`, `cmd/remote_bridge_test.go`).
- This sandbox has **no network access to the Go module proxy** (confirmed: `go run github.com/wailsapp/wails/v2/cmd/wails@latest version` fails with `403 Forbidden` against `localhost:5000`). Any step that runs `go get`/`go mod tidy` inside `gui/` to fetch Wails must be run in a normal developer environment with network access — do not treat a proxy failure in a sandboxed CI/agent environment as a plan defect.
- Reuse the existing PWA icon art (`internal/webui/static/icons/icon-512.png`) as the native app icon source — do not regenerate new artwork.

---

### Task 1: Extract a reusable embedded-server constructor from `cmd/serve.go`

**Files:**
- Modify: `cmd/serve.go`
- Test: `cmd/serve_test.go`

**Interfaces:**
- Produces: `func BuildEmbeddedServer(maxAge time.Duration) (*webui.Server, *webterm.Registry, error)` — exported from package `cmd`, callable by the future `gui` module (which will import `github.com/yarma/tsession/cmd` directly, since `cmd` is not an `internal` package).

This task changes zero runtime behavior of `tsession serve` — it only pulls the existing server-construction code (registry creation, `webui.NewServer(...)` with its three options) out of `Serve()` into its own exported function, so a later task (Task 5) can call the identical wiring from the Wails app without duplicating it.

- [ ] **Step 1: Write the failing test**

Add to `cmd/serve_test.go`:

```go
func TestBuildEmbeddedServerReturnsWorkingHandlerAndRegistry(t *testing.T) {
	srv, registry, err := BuildEmbeddedServer(14 * 24 * time.Hour)
	if err != nil {
		t.Fatalf("BuildEmbeddedServer: %v", err)
	}
	if srv == nil {
		t.Fatal("expected non-nil *webui.Server")
	}
	if registry == nil {
		t.Fatal("expected non-nil *webterm.Registry")
	}
	defer registry.Shutdown()

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/sessions")
	if err != nil {
		t.Fatalf("GET /api/sessions: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/sessions status = %d, want 200", resp.StatusCode)
	}
}
```

Add `"net/http"`, `"net/http/httptest"` to the existing `import` block in `cmd/serve_test.go` (keep the existing `"errors"`, `"testing"`, `"github.com/yarma/tsession/internal/config"` imports).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/... -run TestBuildEmbeddedServerReturnsWorkingHandlerAndRegistry -v`
Expected: FAIL with `undefined: BuildEmbeddedServer`

- [ ] **Step 3: Extract `BuildEmbeddedServer` in `cmd/serve.go`**

Replace the body of `Serve` from the `webterm.ReapOrphanedLocal()` call through the `webui.NewServer(...)` call with a call to the new function. The full updated file section:

```go
// BuildEmbeddedServer wires up the same session-loading, alias-loading,
// remote-resolving, and PTY-registry configuration used by `tsession
// serve`. It is exported so the native GUI app (see gui/) can embed the
// identical web UI server without duplicating this wiring. Callers own the
// returned registry and must call registry.Shutdown() when done (this also
// tears down every warm PTY).
func BuildEmbeddedServer(maxAge time.Duration) (*webui.Server, *webterm.Registry, error) {
	if err := webterm.ReapOrphanedLocal(); err != nil {
		fmt.Fprintln(os.Stderr, "warning: failed to reap orphaned web sessions:", err)
	}

	registry := webterm.NewRegistry()

	srv := webui.NewServer(
		func() ([]sessions.Session, error) { return mergedSessionsForServe(maxAge) },
		webui.WithAliases(reponames.Load),
		webui.WithRemotes(remoteResolverFromConfig),
		webui.WithTerminal(registry),
	)

	return srv, registry, nil
}

// Serve runs `tsession serve`: a loopback-only web server exposing the
// session list and a browser terminal (see internal/webui). It runs in the
// foreground until interrupted (Ctrl-C) or sent SIGTERM, at which point it
// shuts down the HTTP server and every warm PTY (running each terminal's
// teardown hook, e.g. `tmux kill-session` for grouped web sessions).
func Serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", defaultServeAddr, "address to bind (must be loopback: 127.0.0.1, ::1, or localhost)")
	maxAge := fs.Duration("max-age", 14*24*time.Hour, "ignore sessions older than this")
	open := fs.Bool("open", false, "open the UI in the default browser once the server is listening")
	_ = fs.Parse(args)

	if err := requireLoopback(*addr); err != nil {
		return err
	}

	srv, registry, err := BuildEmbeddedServer(*maxAge)
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", *addr, err)
	}
	...
```

(The rest of `Serve` — from `url := "http://" + ...` through the end of the function — is unchanged; only the block that built `registry`/`srv` inline is replaced by the call to `BuildEmbeddedServer`.)

`BuildEmbeddedServer`'s error return is currently always `nil` (nothing in the extracted block can fail) — it is kept in the signature because the Wails app in Task 5 needs a single call it can `if err != nil { return err }` on without the plan having to change this signature later if reap/registry construction ever start returning real errors.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/... -run TestBuildEmbeddedServerReturnsWorkingHandlerAndRegistry -v`
Expected: PASS

- [ ] **Step 5: Run the full existing `cmd` test suite to confirm no regression**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all packages PASS, identical to the baseline before this task.

- [ ] **Step 6: Commit**

```bash
git add cmd/serve.go cmd/serve_test.go
git commit -m "cmd: extract BuildEmbeddedServer so gui/ can reuse the web UI server wiring

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

### Task 2: Scaffold the `gui/` Go module and its embedded loader page

**Files:**
- Create: `gui/go.mod`
- Create: `gui/wails.json`
- Create: `gui/frontend/dist/index.html`
- Create: `gui/.gitignore`

**Interfaces:**
- Produces: a buildable (once `go mod tidy` succeeds against a real network) empty module at `github.com/yarma/tsession/gui`, ready for Task 3/4/5 to add Go source files into.

This task only creates the module skeleton and the static loader page — no Go code yet, so there is nothing to unit-test here. Verification is "the module files are syntactically valid" (JSON/HTML), checked by hand, not by `go test`.

- [ ] **Step 1: Create the nested module's `go.mod`**

```
module github.com/yarma/tsession/gui

go 1.25

require (
	github.com/wailsapp/wails/v2 v2.10.1
	github.com/yarma/tsession v0.0.0-00010101000000-000000000000
)

replace github.com/yarma/tsession => ../
```

(`v2.10.1` is the latest Wails v2 release at the time of writing; if a newer patch/minor v2 release exists when this task is actually executed, use that version instead — check `https://github.com/wailsapp/wails/releases` — but stay on the v2 major line, not v3, since v3 is still in alpha and has a different API.)

- [ ] **Step 2: Create `gui/wails.json`**

```json
{
  "$schema": "https://wails.io/schemas/config.v2.json",
  "name": "TSession",
  "outputfilename": "TSession",
  "frontend:install": "",
  "frontend:build": "",
  "frontend:dev:watcher": "",
  "frontend:dev:serverUrl": "auto",
  "wailsjsdir": "./frontend/dist"
}
```

Setting `frontend:install` and `frontend:build` to empty strings tells the Wails CLI there is no npm/Vite step to run — `gui/frontend/dist/index.html` is checked into the repo directly and embedded as-is. This is what keeps the whole feature Node-toolchain-free.

- [ ] **Step 3: Create the loader page `gui/frontend/dist/index.html`**

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <title>tsession</title>
  <style>
    html, body {
      margin: 0;
      height: 100%;
      background: #1e1e1e;
      color: #d0d0d0;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      display: flex;
      align-items: center;
      justify-content: center;
    }
    #status { font-size: 14px; opacity: 0.8; }
  </style>
</head>
<body>
  <div id="status">Starting tsession&hellip;</div>
  <script>
    // The real UI (session list + xterm.js terminal) is served by the
    // embedded internal/webui server started in gui/main.go before this
    // window is shown. That server publishes its own address at
    // "/tsession-port" via a small HTTP middleware (see gui/main.go) so
    // this loader never needs a Wails Go<->JS runtime binding to learn
    // where to navigate.
    (function poll(attempt) {
      fetch("/tsession-port", { cache: "no-store" })
        .then(function (resp) {
          if (!resp.ok) throw new Error("not ready");
          return resp.json();
        })
        .then(function (data) {
          window.location.replace("http://127.0.0.1:" + data.port + "/");
        })
        .catch(function () {
          if (attempt >= 20) {
            document.getElementById("status").textContent =
              "tsession failed to start. Check the app's logs and try again.";
            return;
          }
          setTimeout(function () { poll(attempt + 1); }, 100);
        });
    })(0);
  </script>
</body>
</html>
```

The `catch` gives up after 20 attempts (~2s at 100ms apart), matching the design spec's "bounded retry with backoff before giving up."

- [ ] **Step 4: Create `gui/.gitignore`**

```
/build/bin/
```

(Wails' `wails build` writes compiled app bundles under `build/bin/` by default — those are build artifacts, not source, and must not be committed.)

- [ ] **Step 5: Commit**

```bash
git add gui/go.mod gui/wails.json gui/frontend/dist/index.html gui/.gitignore
git commit -m "gui: scaffold the Wails module skeleton and static loader page

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

### Task 3: Port-fallback listener logic (pure, unit-testable)

**Files:**
- Create: `gui/listen.go`
- Test: `gui/listen_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `func listenWithFallback(preferred string) (net.Listener, error)` — used by Task 5's `main.go`.

This is deliberately split into its own tiny file so the "try the default port, fall back to an OS-assigned one" logic (the one piece of real decision-making in the whole `gui/main.go`) has a fast, non-Wails-dependent unit test. Everything else in `gui/` either needs a live OS window (untestable in CI) or is pure wiring.

- [ ] **Step 1: Write the failing test**

Create `gui/listen_test.go`:

```go
package main

import (
	"net"
	"testing"
)

func TestListenWithFallbackUsesPreferredPortWhenFree(t *testing.T) {
	l, err := listenWithFallback("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listenWithFallback: %v", err)
	}
	defer l.Close()
	if l.Addr().(*net.TCPAddr).Port == 0 {
		t.Fatal("expected a concrete bound port, got 0")
	}
}

func TestListenWithFallbackFallsBackWhenPreferredIsTaken(t *testing.T) {
	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve a port for the test: %v", err)
	}
	defer blocker.Close()
	taken := blocker.Addr().String()

	l, err := listenWithFallback(taken)
	if err != nil {
		t.Fatalf("listenWithFallback: %v", err)
	}
	defer l.Close()

	got := l.Addr().(*net.TCPAddr).Port
	want := blocker.Addr().(*net.TCPAddr).Port
	if got == want {
		t.Fatalf("expected a different port than the taken one %d, got the same", want)
	}
}

func TestListenWithFallbackDoesNotFallbackForNonAddrInUseErrors(t *testing.T) {
	_, err := listenWithFallback("127.0.0.1:-1")
	if err == nil {
		t.Fatal("expected invalid preferred port to return an error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd gui && go vet ./... 2>&1 | head -5`
Expected: FAIL / build error — `undefined: listenWithFallback` (note: this and later `gui/` test runs require `go mod tidy` to have succeeded against a real network first; if run in a sandbox without registry access, this step is expected to fail at the module-resolution stage instead — see Global Constraints).

- [ ] **Step 3: Write `gui/listen.go`**

```go
package main

import (
	"errors"
	"fmt"
	"net"
	"syscall"
)

// listenWithFallback tries to bind preferred (typically the same address
// tsession serve defaults to, so the native app and a manually-run
// `tsession serve` land on the same familiar URL when only one is
// running). If preferred is already in use — e.g. `tsession serve` is
// separately running, or a second copy of the native app is starting up —
// it falls back to an OS-assigned ephemeral port on the same host so the
// app still starts rather than failing outright. Other listen errors are
// returned as-is so configuration or permission problems are not hidden by
// an unexpected fallback port.
func listenWithFallback(preferred string) (net.Listener, error) {
	l, err := net.Listen("tcp", preferred)
	if err == nil {
		return l, nil
	}
	if !errors.Is(err, syscall.EADDRINUSE) {
		return nil, fmt.Errorf("listen on %s: %w", preferred, err)
	}

	host, _, splitErr := net.SplitHostPort(preferred)
	if splitErr != nil {
		return nil, fmt.Errorf("listen on %s: %w", preferred, err)
	}

	fallback, fallbackErr := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if fallbackErr != nil {
		return nil, fmt.Errorf("listen on %s failed (%v), and fallback ephemeral port also failed: %w", preferred, err, fallbackErr)
	}
	return fallback, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd gui && go test ./... -run TestListenWithFallback -v`
Expected: PASS (both subtests)

- [ ] **Step 5: Commit**

```bash
git add gui/listen.go gui/listen_test.go
git commit -m "gui: add loopback listener with ephemeral-port fallback

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

### Task 4: `/tsession-port` asset-server middleware (pure, unit-testable)

**Files:**
- Create: `gui/portmiddleware.go`
- Test: `gui/portmiddleware_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks (only `net/http`).
- Produces: `func portMiddleware(port func() int, next http.Handler) http.Handler` — used by Task 5's `main.go` as the Wails `AssetServer.Middleware`.

This is the piece the loader page's `fetch("/tsession-port")` (Task 2) talks to. Splitting it out lets it be tested with a plain `httptest.NewRecorder()`, with no Wails runtime involved at all.

- [ ] **Step 1: Write the failing test**

Create `gui/portmiddleware_test.go`:

```go
package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPortMiddlewareServesPortAsJSON(t *testing.T) {
	inner := http.NewServeMux()
	handler := portMiddleware(func() int { return 4321 }, inner)

	req := httptest.NewRequest(http.MethodGet, "/tsession-port", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	want := `{"port":4321}`
	if got := rec.Body.String(); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestPortMiddlewarePassesOtherPathsThrough(t *testing.T) {
	inner := http.NewServeMux()
	inner.HandleFunc("/index.html", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("loader"))
	})
	handler := portMiddleware(func() int { return 4321 }, inner)

	req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Body.String() != "loader" {
		t.Fatalf("expected pass-through to inner handler, got %q", rec.Body.String())
	}
}

func TestPortMiddlewarePassesNonGetPortRequestsThrough(t *testing.T) {
	inner := http.NewServeMux()
	inner.HandleFunc("/tsession-port", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte("inner"))
	})
	handler := portMiddleware(func() int { return 4321 }, inner)

	req := httptest.NewRequest(http.MethodPost, "/tsession-port", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	if rec.Body.String() != "inner" {
		t.Fatalf("expected pass-through to inner handler, got %q", rec.Body.String())
	}
}

func TestPortMiddlewareReportsNotReadyWhenPortUnavailable(t *testing.T) {
	inner := http.NewServeMux()
	handler := portMiddleware(func() int { return 0 }, inner)

	req := httptest.NewRequest(http.MethodGet, "/tsession-port", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd gui && go test ./... -run TestPortMiddleware -v`
Expected: FAIL — `undefined: portMiddleware`

- [ ] **Step 3: Write `gui/portmiddleware.go`**

```go
package main

import (
	"fmt"
	"net/http"
)

// portMiddleware intercepts GET /tsession-port and reports the port the
// embedded internal/webui server is actually listening on, as JSON. Every
// other request is passed straight through to next (the Wails asset
// server, which serves gui/frontend/dist). This is how the static loader
// page (frontend/dist/index.html) learns where to navigate without any
// Wails Go<->JS runtime binding.
func portMiddleware(port func() int, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/tsession-port" {
			p := port()
			if p <= 0 {
				http.Error(w, "tsession embedded server is not ready", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"port":%d}`, p)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd gui && go test ./... -run TestPortMiddleware -v`
Expected: PASS (both subtests)

- [ ] **Step 5: Commit**

```bash
git add gui/portmiddleware.go gui/portmiddleware_test.go
git commit -m "gui: add /tsession-port middleware for the loader page

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

### Task 5: Wails app entrypoint wiring `main.go`/`app.go`

**Files:**
- Create: `gui/app.go`
- Create: `gui/main.go`

**Interfaces:**
- Consumes: `cmd.BuildEmbeddedServer` (Task 1), `listenWithFallback` (Task 3), `portMiddleware` (Task 4).
- Produces: the `main()` entrypoint Wails' build tooling compiles into the native app.

This task has no automated test: it requires a live OS window (Wails' `wails.Run` blocks and owns the OS main thread) and cannot run under `go test`. Verification here is `go build`/`go vet` succeeding, plus a manual smoke test note for whoever runs `wails build`/`wails dev` next. This matches the design spec's explicit call-out: *"the `gui/` module: kept intentionally small; ... verification of the actual native window is manual, per platform, before each release."*

- [ ] **Step 1: Write `gui/app.go`**

```go
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/yarma/tsession/cmd"
	"github.com/yarma/tsession/internal/webterm"
)

// defaultGUIAddr matches cmd.defaultServeAddr so the native app and a
// manually-run `tsession serve` land on the same familiar port when only
// one of them is running at a time.
const defaultGUIAddr = "127.0.0.1:4270"

// App owns the embedded internal/webui server's lifecycle for the
// lifetime of the native window. It is bound to Wails as OnStartup /
// OnShutdown, not as a JS-callable binding — the frontend never calls
// into Go directly (see gui/frontend/dist/index.html).
type App struct {
	mu       sync.RWMutex
	listener net.Listener
	registry *webterm.Registry
	http     *http.Server
}

func newApp() *App {
	return &App{}
}

// startEmbeddedServer starts the embedded web UI server and only publishes
// its port after the HTTP endpoint has answered a readiness probe. Until
// then, /tsession-port returns 503 and the loader page keeps polling.
func (a *App) startEmbeddedServer(ctx context.Context) error {
	listener, err := listenWithFallback(defaultGUIAddr)
	if err != nil {
		return fmt.Errorf("start embedded server listener: %w", err)
	}

	srv, registry, err := cmd.BuildEmbeddedServer(14 * 24 * time.Hour)
	if err != nil {
		_ = listener.Close()
		return fmt.Errorf("build embedded web UI server: %w", err)
	}

	httpServer := &http.Server{Handler: srv.Handler()}
	go func() {
		err := httpServer.Serve(listener)
		if err != nil && err != http.ErrServerClosed {
			a.showErrorAndQuit(ctx, fmt.Sprintf("The embedded tsession server stopped unexpectedly:\n\n%v", err))
		}
	}()

	if err := waitForServerReady(listener.Addr().(*net.TCPAddr).Port); err != nil {
		_ = httpServer.Close()
		_ = registry.Shutdown()
		return err
	}

	a.mu.Lock()
	a.listener = listener
	a.registry = registry
	a.http = httpServer
	a.mu.Unlock()
	return nil
}

// port reports the concrete TCP port the embedded server bound, for
// portMiddleware to publish at /tsession-port.
func (a *App) port() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.listener == nil {
		return 0
	}
	return a.listener.Addr().(*net.TCPAddr).Port
}

// startup is Wails' OnStartup hook: it starts the embedded server after the
// runtime context exists, so startup failures can be shown in a native
// dialog instead of disappearing into stderr when launched from a GUI.
func (a *App) startup(ctx context.Context) {
	if err := a.startEmbeddedServer(ctx); err != nil {
		a.showErrorAndQuit(ctx, fmt.Sprintf("Failed to start the embedded tsession server:\n\n%v", err))
	}
}

// shutdown is Wails' OnShutdown hook: it stops the HTTP server and tears
// down every warm PTY (registry.Shutdown runs each terminal's teardown
// hook, e.g. `tmux kill-session` for grouped web sessions), mirroring
// `tsession serve`'s Ctrl-C/SIGTERM behavior in cmd/serve.go.
func (a *App) shutdown(ctx context.Context) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.mu.RLock()
	httpServer := a.http
	registry := a.registry
	a.mu.RUnlock()
	if httpServer != nil {
		_ = httpServer.Shutdown(shutdownCtx)
	}
	if registry != nil {
		_ = registry.Shutdown()
	}
	_ = ctx
}

func (a *App) showErrorAndQuit(ctx context.Context, message string) {
	_, _ = runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
		Type:    runtime.ErrorDialog,
		Title:   "TSession",
		Message: message,
	})
	runtime.Quit(ctx)
}

func waitForServerReady(port int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()

	var lastErr error
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 500 {
				return nil
			}
			lastErr = fmt.Errorf("GET %s returned status %d", url, resp.StatusCode)
		} else {
			lastErr = err
		}

		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("wait for embedded server readiness: %w", lastErr)
			}
			return fmt.Errorf("wait for embedded server readiness: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
```

- [ ] **Step 2: Write `gui/main.go`**

```go
package main

import (
	"embed"
	"fmt"
	"net/http"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed frontend/dist
var assets embed.FS

func main() {
	app := newApp()

	err := wails.Run(&options.App{
		Title:  "TSession",
		Width:  1280,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
			Middleware: func(next http.Handler) http.Handler {
				return portMiddleware(app.port, next)
			},
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "tsession gui:", err)
		os.Exit(1)
	}
}
```

Add `"net/http"` to the import block (needed for the `http.Handler` type in the `Middleware` closure).

- [ ] **Step 3: Build check**

Run: `cd gui && go build ./... && go vet ./...`
Expected: succeeds once `go mod tidy` has resolved the Wails dependency against a real network (see Global Constraints — this cannot be verified inside this sandbox). Fix any unused-import issue flagged by `go vet` per the note at the end of Step 1.

- [ ] **Step 4: Manual smoke test (record as a note, not an automated step)**

From a machine with network access and the Wails CLI installed (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`), run `cd gui && wails dev`. Confirm: a native window opens, briefly shows "Starting tsession…", then shows the same session-list-plus-terminal UI `tsession serve --open` shows in a browser. Confirm closing the window doesn't leave a stray process (check `ps aux | grep tsession`).

- [ ] **Step 5: Commit**

```bash
git add gui/app.go gui/main.go
git commit -m "gui: wire the Wails app entrypoint to the embedded web UI server

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

### Task 6: App icon

**Files:**
- Create: `gui/build/appicon.png`

**Interfaces:** none — a static asset `wails build` picks up by convention (Wails looks for `build/appicon.png` relative to `wails.json` and generates all platform-specific icon formats — `.icns`, `.ico`, etc. — from it at build time).

- [ ] **Step 1: Copy the existing PWA icon art as the source**

```bash
mkdir -p gui/build
cp internal/webui/static/icons/icon-512.png gui/build/appicon.png
```

- [ ] **Step 2: Verify it's a valid, reasonably sized PNG**

Run: `file gui/build/appicon.png`
Expected: reports a PNG image, 512x512 (matches `file internal/webui/static/icons/icon-512.png`, since it's a direct copy).

- [ ] **Step 3: Commit**

```bash
git add gui/build/appicon.png
git commit -m "gui: reuse the PWA icon art as the native app icon

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

### Task 7: `tsession gui` launcher subcommand

**Files:**
- Create: `cmd/gui.go`
- Create: `cmd/gui_detach_unix.go`
- Create: `cmd/gui_detach_windows.go`
- Test: `cmd/gui_test.go`
- Modify: `main.go`

**Interfaces:**
- Produces: `func Gui(args []string) error` — dispatched from `main.go` exactly like every other subcommand (`cmd.List`, `cmd.Serve`, etc).
- Produces (internal to `cmd/gui.go`, exported for testability): `func locateGUIApp(goos string, exeDir string, homeDir string) (string, error)` — pure path-search logic, no OS calls, so it's fully unit-testable with temp directories.

This is the piece that ships in the main `tsession` binary — no Wails dependency here at all, just filesystem search and an `exec`/`open` call.

- [ ] **Step 1: Write the failing tests**

Create `cmd/gui_test.go`:

```go
package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLocateGUIAppFindsBundleNextToExecutableOnDarwin(t *testing.T) {
	exeDir := t.TempDir()
	appPath := filepath.Join(exeDir, "TSession.app")
	if err := os.MkdirAll(appPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := locateGUIApp("darwin", exeDir, t.TempDir())
	if err != nil {
		t.Fatalf("locateGUIApp: %v", err)
	}
	if got != appPath {
		t.Fatalf("locateGUIApp = %q, want %q", got, appPath)
	}
}

func TestLocateGUIAppFindsBundleInApplicationsOnDarwin(t *testing.T) {
	home := t.TempDir()
	appsDir := filepath.Join(home, "Applications")
	appPath := filepath.Join(appsDir, "TSession.app")
	if err := os.MkdirAll(appPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := locateGUIApp("darwin", t.TempDir(), home)
	if err != nil {
		t.Fatalf("locateGUIApp: %v", err)
	}
	if got != appPath {
		t.Fatalf("locateGUIApp = %q, want %q", got, appPath)
	}
}

func TestLocateGUIAppPrefersExecutableAdjacentOverHomeApplications(t *testing.T) {
	exeDir := t.TempDir()
	home := t.TempDir()
	adjacent := filepath.Join(exeDir, "TSession.app")
	inHome := filepath.Join(home, "Applications", "TSession.app")
	for _, p := range []string{adjacent, inHome} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}

	got, err := locateGUIApp("darwin", exeDir, home)
	if err != nil {
		t.Fatalf("locateGUIApp: %v", err)
	}
	if got != adjacent {
		t.Fatalf("locateGUIApp = %q, want the executable-adjacent path %q", got, adjacent)
	}
}

func TestLocateGUIAppErrorsWithSearchedPathsWhenNotFound(t *testing.T) {
	exeDir := t.TempDir()
	home := t.TempDir()

	_, err := locateGUIApp("darwin", exeDir, home)
	if err == nil {
		t.Fatal("expected an error when no app bundle exists anywhere searched")
	}
	msg := err.Error()
	for _, want := range []string{
		filepath.Join(exeDir, "TSession.app"),
		filepath.Join(home, "Applications", "TSession.app"),
		filepath.Join("/Applications", "TSession.app"),
	} {
		if !contains(msg, want) {
			t.Fatalf("error message %q does not mention searched path %q", msg, want)
		}
	}
}

func TestLocateGUIAppFindsWailsOutputNameNextToExecutableOnLinux(t *testing.T) {
	exeDir := t.TempDir()
	appPath := filepath.Join(exeDir, "TSession")
	if err := os.WriteFile(appPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write app: %v", err)
	}

	got, err := locateGUIApp("linux", exeDir, t.TempDir())
	if err != nil {
		t.Fatalf("locateGUIApp: %v", err)
	}
	if got != appPath {
		t.Fatalf("locateGUIApp = %q, want %q", got, appPath)
	}
}

func TestLocateGUIAppFindsWailsOutputNameOnLinuxPath(t *testing.T) {
	exeDir := t.TempDir()
	pathDir := t.TempDir()
	appPath := filepath.Join(pathDir, "TSession")
	if err := os.WriteFile(appPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write app: %v", err)
	}
	t.Setenv("PATH", pathDir)

	got, err := locateGUIApp("linux", exeDir, t.TempDir())
	if err != nil {
		t.Fatalf("locateGUIApp: %v", err)
	}
	if got != appPath {
		t.Fatalf("locateGUIApp = %q, want %q", got, appPath)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

func TestGUILaunchCommandUsesOpenForDarwin(t *testing.T) {
	cmd := guiLaunchCommand("darwin", "/Applications/TSession.app")
	if got, want := filepath.Base(cmd.Path), "open"; got != want {
		t.Fatalf("command path basename = %q, want %q", got, want)
	}
	if len(cmd.Args) != 2 || cmd.Args[1] != "/Applications/TSession.app" {
		t.Fatalf("command args = %#v, want open /Applications/TSession.app", cmd.Args)
	}
}

func TestGUILaunchCommandDetachesNonDarwinProcess(t *testing.T) {
	cmd := guiLaunchCommand("linux", "/tmp/tsession-gui")
	if cmd.Path != "/tmp/tsession-gui" {
		t.Fatalf("command path = %q, want /tmp/tsession-gui", cmd.Path)
	}
	assertDetachedGUICommand(t, cmd)
}

func assertDetachedGUICommand(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if cmd.SysProcAttr == nil {
		t.Fatal("expected non-darwin GUI command to have SysProcAttr for detached launch")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/... -run TestLocateGUIApp -v`
Expected: FAIL — `undefined: locateGUIApp`

- [ ] **Step 3: Write `cmd/gui.go`**

```go
package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Gui runs `tsession gui`: it locates the separately-built native GUI
// application (see gui/, built via `wails build`) and launches it. This
// subcommand never builds or embeds Wails itself — see
// docs/superpowers/specs/2026-09-11-native-gui-design.md.
func Gui(args []string) error {
	fs := flag.NewFlagSet("gui", flag.ExitOnError)
	_ = fs.Parse(args)

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate current executable: %w", err)
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("locate home directory: %w", err)
	}

	appPath, err := locateGUIApp(goos(), filepath.Dir(exePath), homeDir)
	if err != nil {
		return err
	}

	return launchGUIApp(goos(), appPath)
}

// goos is a seam over runtime.GOOS so tests can be added later without
// needing to cross-compile; production code always calls it with no
// override.
func goos() string { return runtimeGOOS }

// locateGUIApp searches, in priority order, for the installed native app:
// 1. next to the currently running tsession executable (covers side-by-side
//    dev/test installs and simple manual deployments)
// 2. the platform's conventional install location(s)
// It returns the first match, or an error listing every path it checked.
func locateGUIApp(goos string, exeDir string, homeDir string) (string, error) {
	var candidates []string

	switch goos {
	case "darwin":
		candidates = []string{
			filepath.Join(exeDir, "TSession.app"),
			filepath.Join(homeDir, "Applications", "TSession.app"),
			filepath.Join("/Applications", "TSession.app"),
		}
	case "windows":
		candidates = []string{
			filepath.Join(exeDir, "TSession.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "TSession", "TSession.exe"),
		}
	default: // linux and other unix-likes
		candidates = []string{
			filepath.Join(exeDir, "TSession"),
			filepath.Join(exeDir, "tsession-gui"),
		}
		if pathEnv := os.Getenv("PATH"); pathEnv != "" {
			for _, dir := range filepath.SplitList(pathEnv) {
				candidates = append(candidates, filepath.Join(dir, "TSession"))
				candidates = append(candidates, filepath.Join(dir, "tsession-gui"))
			}
		}
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}

	msg := "could not find the tsession native GUI app; searched:\n"
	for _, c := range candidates {
		msg += "  - " + c + "\n"
	}
	msg += "build it with `wails build` in gui/, or install TSession"
	if goos == "darwin" {
		msg += ".app in /Applications"
	}
	return "", fmt.Errorf("%s", msg)
}

// launchGUIApp starts the located app, detached from this process so
// closing the terminal that ran `tsession gui` does not kill it.
func launchGUIApp(goos string, appPath string) error {
	cmd := guiLaunchCommand(goos, appPath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch %s: %w", appPath, err)
	}
	return nil
}

func guiLaunchCommand(goos string, appPath string) *exec.Cmd {
	switch goos {
	case "darwin":
		return exec.Command("open", appPath)
	default:
		cmd := exec.Command(appPath)
		detachGUICommand(cmd)
		return cmd
	}
}
```

- [ ] **Step 4: Add platform-specific detachment helpers**

Create `cmd/gui_detach_unix.go`:

```go
//go:build !windows

package cmd

import (
	"os/exec"
	"syscall"
)

func detachGUICommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
```

Create `cmd/gui_detach_windows.go`:

```go
//go:build windows

package cmd

import (
	"os/exec"
	"syscall"
)

const windowsDetachedProcess = 0x00000008

func detachGUICommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | windowsDetachedProcess,
	}
}
```

- [ ] **Step 5: Add the `runtimeGOOS` seam**

At the top of `cmd/gui.go`, add the import and package-level var so `goos()` has something to return without importing `runtime` directly into test-covered logic (this keeps `locateGUIApp` itself 100% pure/testable while production code still reflects the real OS):

```go
import "runtime"

var runtimeGOOS = runtime.GOOS
```

(Fold this into the same file rather than a separate one — it is a two-line seam, not worth its own file.)

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./cmd/... -run 'TestLocateGUIApp|TestGUILaunchCommand' -v`
Expected: PASS (all locator and launch-command-construction subtests)

- [ ] **Step 7: Wire `gui` into `main.go`**

In `main.go`, add a new case to the subcommand switch, right after the existing `"serve"` case:

```go
	case "serve":
		err = cmd.Serve(args)
	case "gui":
		err = cmd.Gui(args)
```

And add a line to the `usage()` function's help text, after the `tsession serve` line:

```go
  tsession serve [--addr] [--open]  Start the loopback web UI (browser terminal)
  tsession gui                  Launch the installed native GUI app (see gui/)
```

- [ ] **Step 8: Run the full suite**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all packages PASS, including the four new `TestLocateGUIApp*` subtests.

- [ ] **Step 9: Compile-check platform detachment helpers**

Run:

```bash
GOOS=windows GOARCH=amd64 go test -c cmd/gui_detach_windows.go
rm -f gui_detach_windows.test
GOOS=linux GOARCH=amd64 go test -c cmd/gui_detach_unix.go
rm -f gui_detach_unix.test
```

Expected: both compile-only helper checks succeed. (Do not use `GOOS=linux go test ./cmd` from macOS because it builds a Linux test binary and then tries to execute it locally.)

- [ ] **Step 10: Commit**

```bash
git add cmd/gui.go cmd/gui_test.go cmd/gui_detach_unix.go cmd/gui_detach_windows.go main.go
git commit -m "cmd: add tsession gui launcher subcommand

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

### Task 8: Documentation — build/install instructions for the native app

**Files:**
- Modify: `README.md`
- Create: `gui/README.md`

The root `README.md` already documents the "three ways to use it" table and a "Native GUI (`tsession gui`)" subsection marked as design-stage (see the `2026-09-11-native-gui-design.md` spec work). This task removes the "not yet implemented" caveat now that it's built, and adds a `gui/README.md` with concrete build steps.

- [ ] **Step 1: Update the "Native GUI" subsection in `README.md`**

Replace:

```markdown
#### Native GUI (`tsession gui`)

> Design stage — see
> `docs/superpowers/specs/2026-09-11-native-gui-design.md`; not yet
> implemented.

`tsession gui` will launch a separately built native application (Wails,
covering macOS/Windows/Linux) that embeds the same `tsession serve` server
internally and opens a plain OS window pointed at it — no browser required
at all, and no separate `tsession serve` process to manage. The `tsession`
CLI's `gui` subcommand only locates and launches the installed app; the app
itself is built via a separate `wails build` pipeline documented in the
design spec.
```

with:

```markdown
#### Native GUI (`tsession gui`)

`tsession gui` launches a separately built native application (Wails,
covering macOS/Windows/Linux) that embeds the same web UI server internally
and opens a plain OS window pointed at it — no browser required at all, and
no separate `tsession serve` process to manage. The `tsession` CLI's `gui`
subcommand only locates and launches the installed app; build the app
itself from `gui/` — see `gui/README.md`. Design background:
`docs/superpowers/specs/2026-09-11-native-gui-design.md`.
```

- [ ] **Step 2: Write `gui/README.md`**

```markdown
# tsession native GUI

A [Wails v2](https://wails.io) desktop wrapper around the same web UI
server `tsession serve` runs (`internal/webui`). See
`../docs/superpowers/specs/2026-09-11-native-gui-design.md` for the design.

This is a separate Go module from the rest of the repo (own `go.mod`) so
Wails' CGO/native-webview dependencies never affect `go build` of the main
`tsession` CLI binary.

## Prerequisites

- Go 1.25+
- The Wails v2 CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- Platform native webview runtime:
  - **macOS**: none — uses system WebKit.
  - **Windows**: the [WebView2 runtime](https://developer.microsoft.com/microsoft-edge/webview2/) (preinstalled on Windows 11).
  - **Linux**: `webkit2gtk` (e.g. `apt install libwebkit2gtk-4.1-dev` on Debian/Ubuntu).

No Node.js/npm is required — the embedded frontend
(`frontend/dist/index.html`) is a small hand-written loader page, not a
built JS app; `wails.json` sets `frontend:install`/`frontend:build` to
no-ops.

## Building

```bash
cd gui
wails build
```

The resulting app bundle is written to `gui/build/bin/` (e.g.
`TSession.app` on macOS). Install it wherever `tsession gui` looks for it:
next to the `tsession` binary, or the platform's conventional Applications
directory (see `cmd/gui.go`'s `locateGUIApp` for the exact search order).

## Developing

```bash
cd gui
wails dev
```

Opens the native window with the embedded server running, same as a real
build, useful for quick iteration without a full `wails build`.
```

- [ ] **Step 3: Commit**

```bash
git add README.md gui/README.md
git commit -m "docs: document gui/ build steps and drop the design-stage caveat

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Self-Review Notes

- **Spec coverage:** module split (Task 2), embedded-server reuse with zero duplication (Task 1 + Task 5), loader page with no Wails bindings (Task 2 + Task 4 + Task 5), launcher-only CLI role with explicit "searched paths" error (Task 7), icon reuse (Task 6), cross-platform search paths for macOS/Windows/Linux (Task 7), docs for all three usage modes (Task 8, plus the already-committed README table). Out-of-scope items from the spec (CI/signing/installers/auto-update/tray) are intentionally not tasked here.
- **Placeholder scan:** no TBD/TODO; every step has literal code or exact commands.
- **Type consistency:** `cmd.BuildEmbeddedServer(time.Duration) (*webui.Server, *webterm.Registry, error)` (Task 1) is consumed in Task 5's `startEmbeddedServer()`. `listenWithFallback(string) (net.Listener, error)` (Task 3) and `portMiddleware(func() int, http.Handler) http.Handler` (Task 4) are consumed with matching types in Task 5. `locateGUIApp(goos, exeDir, homeDir string) (string, error)` (Task 7) matches its test calls exactly.
