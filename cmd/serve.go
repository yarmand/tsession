package cmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/yarma/tsession/internal/config"
	"github.com/yarma/tsession/internal/reponames"
	"github.com/yarma/tsession/internal/sessions"
	"github.com/yarma/tsession/internal/webterm"
	"github.com/yarma/tsession/internal/webui"
)

// defaultServeAddr binds loopback-only on a fixed, memorable port so
// repeated `tsession serve` invocations reuse the same URL.
const defaultServeAddr = "127.0.0.1:4270"

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

	if err := webterm.ReapOrphanedLocal(); err != nil {
		fmt.Fprintln(os.Stderr, "warning: failed to reap orphaned web sessions:", err)
	}

	registry := webterm.NewRegistry()

	srv := webui.NewServer(
		func() ([]sessions.Session, error) { return mergedSessionsForServe(*maxAge) },
		webui.WithAliases(reponames.Load),
		webui.WithRemotes(remoteResolverFromConfig),
		webui.WithTerminal(registry),
	)

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", *addr, err)
	}

	url := "http://" + listener.Addr().String() + "/"
	fmt.Println("tsession serve listening on", url)

	httpServer := &http.Server{Handler: srv.Handler()}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- httpServer.Serve(listener)
	}()

	if *open {
		if err := openBrowser(url); err != nil {
			fmt.Fprintln(os.Stderr, "warning: failed to open browser:", err)
		}
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-stop:
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdownErr := httpServer.Shutdown(ctx)
	teardownErr := registry.Shutdown()

	if shutdownErr != nil {
		return shutdownErr
	}
	return teardownErr
}

// requireLoopback rejects any --addr that is not explicitly loopback. PTYs
// served over this listener must never be reachable off-host, so this
// check runs before any listener is created.
func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid --addr %q: %w", addr, err)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("--addr %q is not loopback; tsession serve refuses to bind a non-loopback address", addr)
	}
	return nil
}

// mergedSessionsForServe returns the full local+remote session list, with
// Session.Repository best-effort filled in from git for local sessions
// (enrichOrigins) so repository labels and aliasing work the same way
// --short rendering does.
func mergedSessionsForServe(maxAge time.Duration) ([]sessions.Session, error) {
	local, remoteMap, remoteNames, warnings, err := loadAllWithRemotes(maxAge, false, false)
	if err != nil {
		return nil, err
	}
	for _, warning := range warnings {
		fmt.Fprintln(os.Stderr, "warning:", warning)
	}

	all := append([]sessions.Session(nil), local...)
	for _, name := range remoteNames {
		all = append(all, remoteMap[name]...)
	}
	enrichOrigins(all)
	return all, nil
}

// remoteResolverFromConfig implements webui.RemoteResolver by loading
// ~/.config/tsession/config.yaml on every call. Config is small and rarely
// changes at runtime, so re-reading it per terminal-connect request keeps
// this simple rather than caching it.
func remoteResolverFromConfig(origin string) (config.Remote, bool, error) {
	cfg, err := loadConfig()
	if err != nil {
		return config.Remote{}, false, err
	}
	for _, r := range cfg.Remotes {
		if r.Name == origin {
			return r, true, nil
		}
	}
	return config.Remote{}, false, nil
}

// openBrowser launches the platform's default browser pointed at url.
// Best-effort: the caller logs but does not fail startup on error.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
