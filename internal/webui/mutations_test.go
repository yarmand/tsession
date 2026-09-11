package webui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yarma/tsession/internal/names"
	"github.com/yarma/tsession/internal/reponames"
	"github.com/yarma/tsession/internal/sessions"
)

func stubTmuxRenameSession(fn func(oldName, newName string) error) (restore func()) {
	prev := tmuxRenameSessionFn
	tmuxRenameSessionFn = fn
	return func() { tmuxRenameSessionFn = prev }
}

func TestHandleRenameSession_SetsAndClearsName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/sess1/name", strings.NewReader(`{"name":"my session"}`))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	if got := names.Get("sess1"); got != "my session" {
		t.Fatalf("names.Get(sess1) = %q, want %q", got, "my session")
	}

	// Clearing: empty name.
	req = httptest.NewRequest(http.MethodPost, "/api/sessions/sess1/name", strings.NewReader(`{"name":""}`))
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if got := names.Get("sess1"); got != "" {
		t.Fatalf("expected name cleared, got %q", got)
	}
}

func TestHandleRenameSession_RenamesLiveTmuxSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	all := []sessions.Session{{ID: "sess2", TmuxName: "orig-tmux-name"}}
	srv := NewServer(func() ([]sessions.Session, error) { return all, nil })

	var renamedOld, renamedNew string
	restore := stubTmuxRenameSession(func(oldName, newName string) error {
		renamedOld, renamedNew = oldName, newName
		return nil
	})
	defer restore()

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/sess2/name", strings.NewReader(`{"name":"new-name"}`))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	if renamedOld != "orig-tmux-name" || renamedNew != "new-name" {
		t.Fatalf("tmux.RenameSession called with (%q, %q), want (orig-tmux-name, new-name)", renamedOld, renamedNew)
	}
}

func TestHandleRenameSession_InvalidJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/sess1/name", strings.NewReader(`not json`))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleRepoAlias_SetsAndClearsAlias(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })

	body, _ := json.Marshal(repoAliasRequest{Repository: "git@github.com:org/repo.git", Alias: "shortname"})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/alias", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	got, err := reponames.Get("git@github.com:org/repo.git")
	if err != nil {
		t.Fatalf("reponames.Get: %v", err)
	}
	if got != "shortname" {
		t.Fatalf("reponames.Get() = %q, want %q", got, "shortname")
	}
}

func TestHandleRepoAlias_RequiresRepository(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })

	body, _ := json.Marshal(repoAliasRequest{Alias: "shortname"})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/alias", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleEvents_StreamsNotifyTransition(t *testing.T) {
	dir := t.TempDir()

	var mu sync.Mutex
	state := sessions.StateWorking
	srv := NewServer(func() ([]sessions.Session, error) {
		mu.Lock()
		defer mu.Unlock()
		return []sessions.Session{{ID: "s1", Name: "s1", State: state}}, nil
	})
	srv.SetNotifyStorePath(dir + "/notify-web.json")
	srv.SetPollInterval(5 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		srv.Handler().ServeHTTP(rec, req)
		close(done)
	}()

	// Let the first poll record the initial (non-notifiable) sighting, then
	// flip to a notifiable state and let a later poll observe the
	// transition.
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	state = sessions.StateDone
	mu.Unlock()
	time.Sleep(40 * time.Millisecond)
	cancel()
	<-done

	body := rec.Body.String()
	if !strings.Contains(body, "event: notify") {
		t.Fatalf("expected a notify event in SSE stream, got: %q", body)
	}
	if !strings.Contains(body, `"sessionId":"s1"`) || !strings.Contains(body, `"kind":"done"`) {
		t.Fatalf("expected s1/done event payload, got: %q", body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
}

func TestHandleEvents_NoTransitionsProducesNoFrames(t *testing.T) {
	dir := t.TempDir()
	srv := NewServer(func() ([]sessions.Session, error) {
		return []sessions.Session{{ID: "s1", State: sessions.StateWorking}}, nil
	})
	srv.SetNotifyStorePath(dir + "/notify-web.json")
	srv.SetPollInterval(5 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		srv.Handler().ServeHTTP(rec, req)
		close(done)
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done

	if strings.Contains(rec.Body.String(), "event: notify") {
		t.Fatalf("expected no notify frames for a session that never transitions, got: %q", rec.Body.String())
	}
}
