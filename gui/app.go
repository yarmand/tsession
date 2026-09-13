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
