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

func TestHandleLocalTerminal_ExitedWarmPTYIsDiscardedBeforeRestart(t *testing.T) {
	registry := webterm.NewRegistry()
	t.Cleanup(func() { _ = registry.Shutdown() })

	srv := NewServer(
		func() ([]sessions.Session, error) { return nil, nil },
		WithTerminal(registry),
	)

	key := webterm.Key{Origin: "", ID: attachcmd.LocalTerminalID}
	stale, err := registry.Attach(key, webterm.Spec{Bin: "sh", Args: []string{"-c", "exit 0"}})
	if err != nil {
		t.Fatalf("pre-attach: %v", err)
	}

	select {
	case <-stale.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for stale terminal to exit")
	}

	exited, _ := stale.Exited()
	if !exited {
		t.Fatal("expected the pre-attached terminal to have exited")
	}

	srv.SetLocalTerminalHome(func() (string, error) { return "", errors.New("fresh startup failed") })

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/localterm"
	conn, resp, err := websocket.Dial(context.Background(), url, nil)
	if err == nil {
		conn.CloseNow()
		t.Fatal("expected dial to fail after stale local terminal cleanup")
	}
	if resp == nil || resp.StatusCode != 500 {
		t.Fatalf("expected 500, got resp=%+v", resp)
	}
	if got, ok := registry.Get(key); ok {
		if got == stale {
			t.Fatal("expected exited terminal to be removed from the registry")
		}
		t.Fatalf("expected no terminal to remain registered after failed restart, got %+v", got)
	}
}
