package webui

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yarma/tsession/internal/sessions"
)

func TestHandleSessions_FiltersAndShapesPayload(t *testing.T) {
	now := time.Now()
	all := []sessions.Session{
		{ID: "active-local", Repository: "git@github.com:org/repo.git", CWD: "/home/u/repo", TmuxName: "proj", TmuxTarget: "proj:0.0", State: sessions.StateWorking, Source: "copilot", UpdatedAt: now},
		{ID: "exited-local", Repository: "git@github.com:org/repo.git", TmuxName: "proj2", State: sessions.StateExited, UpdatedAt: now},
		{ID: "active-remote", Origin: "host1", Repository: "git@github.com:org/repo.git", State: sessions.StateWaiting, RemoteTmuxAvailable: true, Source: "pi", UpdatedAt: now},
	}

	srv := NewServer(
		func() ([]sessions.Session, error) { return all, nil },
		WithAliases(func() (map[string]string, error) { return map[string]string{"github.com/org/repo": "myalias"}, nil }),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}

	var resp SessionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rec.Body.String())
	}

	if len(resp.Sessions) != 2 {
		t.Fatalf("got %d sessions, want 2 (exited-local should be filtered): %+v", len(resp.Sessions), resp.Sessions)
	}

	byID := map[string]SessionView{}
	for _, v := range resp.Sessions {
		byID[v.ID] = v
	}

	local, ok := byID["active-local"]
	if !ok {
		t.Fatal("expected active-local in response")
	}
	if local.State != "working" {
		t.Errorf("active-local.State = %q, want working", local.State)
	}
	if local.Repository != "myalias" {
		t.Errorf("active-local.Repository = %q, want myalias (aliased)", local.Repository)
	}
	if !local.HasTmux {
		t.Error("active-local.HasTmux = false, want true (has TmuxTarget)")
	}

	remote, ok := byID["active-remote"]
	if !ok {
		t.Fatal("expected active-remote in response")
	}
	if remote.Origin != "host1" {
		t.Errorf("active-remote.Origin = %q, want host1", remote.Origin)
	}
	if remote.State != "question" {
		t.Errorf("active-remote.State = %q, want question", remote.State)
	}
	if !remote.HasTmux {
		t.Error("active-remote.HasTmux = false, want true (RemoteTmuxAvailable)")
	}

	if _, exited := byID["exited-local"]; exited {
		t.Error("exited-local should have been filtered out")
	}
}

func TestHandleSessions_ProviderErrorReturns500(t *testing.T) {
	srv := NewServer(
		func() ([]sessions.Session, error) { return nil, errors.New("boom") },
	)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestHandleSessions_AliasesProviderErrorReturns500(t *testing.T) {
	srv := NewServer(
		func() ([]sessions.Session, error) { return nil, nil },
		WithAliases(func() (map[string]string, error) { return nil, errors.New("boom") }),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestHandleSessions_NilAliasesProviderDefaultsToNoAliases(t *testing.T) {
	all := []sessions.Session{
		{ID: "s1", Repository: "git@github.com:org/repo.git", TmuxName: "p", TmuxTarget: "p:0.0", State: sessions.StateActiveIdle},
	}
	srv := NewServer(func() ([]sessions.Session, error) { return all, nil })

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp SessionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Sessions) != 1 || resp.Sessions[0].Repository != "repo" {
		t.Fatalf("unexpected payload: %+v", resp.Sessions)
	}
}

func TestHandleSessions_EmptyListReturnsEmptyArray(t *testing.T) {
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	var resp SessionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Sessions == nil {
		t.Error("expected non-nil (possibly empty) Sessions slice in JSON payload")
	}
	if len(resp.Sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(resp.Sessions))
	}
}
