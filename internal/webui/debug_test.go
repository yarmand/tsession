package webui

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yarma/tsession/internal/sessions"
)

func TestDebugEventLogsUserInteraction(t *testing.T) {
	var logs bytes.Buffer
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })
	srv.SetDebugLog(func(format string, args ...any) {
		_, _ = fmt.Fprintf(&logs, format+"\n", args...)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/debug", strings.NewReader(`{
		"action": "select-session",
		"sessionId": "abc123",
		"origin": "remote",
		"sessionName": "Investigate thing"
	}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	got := logs.String()
	for _, want := range []string{
		"user interaction",
		"action=select-session",
		"session=abc123",
		"origin=remote",
		"name=\"Investigate thing\"",
		"level=info",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log missing %q: %s", want, got)
		}
	}
}

func TestDebugEventLogsFailureWithErrorLevel(t *testing.T) {
	var logs bytes.Buffer
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })
	srv.SetDebugLog(func(format string, args ...any) {
		_, _ = fmt.Fprintf(&logs, format+"\n", args...)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/debug", strings.NewReader(`{
		"action": "code-view-failed",
		"sessionId": "abc123",
		"level": "error",
		"detail": "VS Code failed to start: exit status 1"
	}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	got := logs.String()
	for _, want := range []string{
		"action=code-view-failed",
		"level=error",
		"detail=\"VS Code failed to start: exit status 1\"",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log missing %q: %s", want, got)
		}
	}
}
