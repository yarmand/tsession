package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/yarma/tsession/cmd"
	"github.com/yarma/tsession/internal/webterm"
	"github.com/yarma/tsession/internal/webui"
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
	listener net.Listener
	registry *webterm.Registry
	server   *webui.Server
	http     *http.Server
}

// newApp starts listening and constructs the embedded web UI server, but
// does not start serving yet — that happens in start(), called from
// OnStartup once Wails has a context to report fatal errors against.
func newApp() (*App, error) {
	listener, err := listenWithFallback(defaultGUIAddr)
	if err != nil {
		return nil, fmt.Errorf("start embedded server listener: %w", err)
	}

	srv, registry, err := cmd.BuildEmbeddedServer(14 * 24 * time.Hour)
	if err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("build embedded web UI server: %w", err)
	}

	return &App{
		listener: listener,
		registry: registry,
		server:   srv,
		http:     &http.Server{Handler: srv.Handler()},
	}, nil
}

// port reports the concrete TCP port the embedded server bound, for
// portMiddleware to publish at /tsession-port.
func (a *App) port() int {
	return a.listener.Addr().(*net.TCPAddr).Port
}

// startup is Wails' OnStartup hook: it starts serving the embedded server
// in the background and reports a fatal error dialog (then quits) if it
// exits unexpectedly — e.g. the listener is torn down externally.
func (a *App) startup(ctx context.Context) {
	go func() {
		err := a.http.Serve(a.listener)
		if err != nil && err != http.ErrServerClosed {
			runtime.LogFatal(ctx, fmt.Sprintf("embedded server stopped unexpectedly: %v", err))
		}
	}()
}

// shutdown is Wails' OnShutdown hook: it stops the HTTP server and tears
// down every warm PTY (registry.Shutdown runs each terminal's teardown
// hook, e.g. `tmux kill-session` for grouped web sessions), mirroring
// `tsession serve`'s Ctrl-C/SIGTERM behavior in cmd/serve.go.
func (a *App) shutdown(ctx context.Context) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = a.http.Shutdown(shutdownCtx)
	_ = a.registry.Shutdown()
	_ = ctx
}
