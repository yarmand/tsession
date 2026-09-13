package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPortMiddlewareServesPortAsJSON(t *testing.T) {
	inner := http.NewServeMux()
	handler := portMiddleware(4321, inner)

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
	handler := portMiddleware(4321, inner)

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
	handler := portMiddleware(4321, inner)

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
