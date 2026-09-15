package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestAppConnectsToExistingServerIfRunning(t *testing.T) {
	// Start a mock tsession serve server responding to GET /api/sessions.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/sessions" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"sessions":[]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	addr := ts.Listener.Addr().String()
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("failed to split host port: %v", err)
	}
	mockPort, _ := strconv.Atoi(portStr)

	app := newApp()
	ctx := context.Background()

	if err := app.startServerOnAddr(ctx, addr); err != nil {
		t.Fatalf("startServerOnAddr failed: %v", err)
	}

	if got := app.port(); got != mockPort {
		t.Fatalf("app.port() = %d, want existing server port %d", got, mockPort)
	}

	// Verify no embedded server listener was started
	if app.listener != nil {
		t.Fatalf("expected app.listener to be nil when connecting to existing server")
	}
}

func TestAppStartsEmbeddedServerIfNoExistingServer(t *testing.T) {
	// Reserve an ephemeral port then close it so it's free.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen failed: %v", err)
	}
	addr := l.Addr().String()
	l.Close()

	app := newApp()
	ctx := context.Background()
	defer app.shutdown(ctx)

	if err := app.startServerOnAddr(ctx, addr); err != nil {
		t.Fatalf("startServerOnAddr failed: %v", err)
	}

	if got := app.port(); got <= 0 {
		t.Fatalf("expected positive port, got %d", got)
	}

	if app.listener == nil {
		t.Fatalf("expected app.listener to be non-nil for embedded server")
	}
}
