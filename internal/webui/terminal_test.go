package webui

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/yarma/tsession/internal/config"
	"github.com/yarma/tsession/internal/sessions"
	"github.com/yarma/tsession/internal/webterm"
)

// dialTerminal connects a real WebSocket client to srv's /api/terminal
// route, served over httptest.NewServer (httptest.NewRecorder cannot
// perform an HTTP Upgrade, so unlike the rest of this package's handlers,
// the terminal handler needs a live listener).
func dialTerminal(t *testing.T, srv *Server, path string) *websocket.Conn {
	t.Helper()
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	url := "ws" + strings.TrimPrefix(ts.URL, "http") + path
	conn, _, err := websocket.Dial(context.Background(), url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn
}

func TestHandleTerminal_NotConfiguredReturns501(t *testing.T) {
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/terminal/local/s1"
	_, resp, err := websocket.Dial(context.Background(), url, nil)
	if err == nil {
		t.Fatal("expected dial to fail when terminal support is not configured")
	}
	if resp == nil || resp.StatusCode != 501 {
		t.Fatalf("expected 501, got resp=%+v", resp)
	}
}

func TestHandleTerminal_UnknownSessionReturns404(t *testing.T) {
	registry := webterm.NewRegistry()
	t.Cleanup(func() { _ = registry.Shutdown() })

	srv := NewServer(
		func() ([]sessions.Session, error) { return nil, nil },
		WithTerminal(registry),
	)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/terminal/local/missing"
	_, resp, err := websocket.Dial(context.Background(), url, nil)
	if err == nil {
		t.Fatal("expected dial to fail for an unknown session")
	}
	if resp == nil || resp.StatusCode != 404 {
		t.Fatalf("expected 404, got resp=%+v", resp)
	}
}

func TestHandleTerminal_LocalEchoesPTYOutput(t *testing.T) {
	registry := webterm.NewRegistry()
	t.Cleanup(func() { _ = registry.Shutdown() })

	all := []sessions.Session{{ID: "s1"}}
	srv := NewServer(
		func() ([]sessions.Session, error) { return all, nil },
		WithTerminal(registry),
	)

	// The real attach command would run tmux; substitute a harmless command
	// by registering a warm Terminal for this key directly, bypassing
	// attachcmd.Build. That means this test only exercises the WebSocket
	// bridge, not command construction (covered separately by attachcmd's
	// own tests).
	key := webterm.Key{Origin: "", ID: "s1"}
	if _, err := registry.Attach(key, webterm.Spec{Bin: "sh", Args: []string{"-c", "cat"}}); err != nil {
		t.Fatalf("pre-attach: %v", err)
	}

	conn := dialTerminal(t, srv, "/api/terminal/local/s1")

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

func TestHandleTerminal_RemoteWithoutResolverReturns400(t *testing.T) {
	registry := webterm.NewRegistry()
	t.Cleanup(func() { _ = registry.Shutdown() })

	all := []sessions.Session{{ID: "s1", Origin: "host1"}}
	srv := NewServer(
		func() ([]sessions.Session, error) { return all, nil },
		WithTerminal(registry),
	)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/terminal/host1/s1"
	_, resp, err := websocket.Dial(context.Background(), url, nil)
	if err == nil {
		t.Fatal("expected dial to fail without a configured RemoteResolver")
	}
	if resp == nil || resp.StatusCode != 400 {
		t.Fatalf("expected 400, got resp=%+v", resp)
	}
}

func TestHandleTerminal_RemoteResolverErrorReturns400(t *testing.T) {
	registry := webterm.NewRegistry()
	t.Cleanup(func() { _ = registry.Shutdown() })

	all := []sessions.Session{{ID: "s1", Origin: "host1"}}
	srv := NewServer(
		func() ([]sessions.Session, error) { return all, nil },
		WithTerminal(registry),
		WithRemotes(func(origin string) (config.Remote, bool, error) {
			return config.Remote{}, false, errors.New("boom")
		}),
	)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/terminal/host1/s1"
	_, resp, err := websocket.Dial(context.Background(), url, nil)
	if err == nil {
		t.Fatal("expected dial to fail when the resolver errors")
	}
	if resp == nil || resp.StatusCode != 400 {
		t.Fatalf("expected 400, got resp=%+v", resp)
	}
}
