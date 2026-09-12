package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yarma/tsession/internal/sessions"
)

func TestStaticAssets_ServesIndexAndVendoredFiles(t *testing.T) {
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })

	cases := []struct {
		path        string
		wantContent string
	}{
		{"/", "<div id=\"app\">"},
		{"/app.js", "tsession"},
		{"/app.css", "session-row"},
		{"/vendor/xterm.js", "Terminal"},
		{"/vendor/addon-fit.js", "FitAddon"},
		{"/manifest.json", "\"name\": \"tsession\""},
		{"/sw.js", "CACHE_NAME"},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s: status = %d", tc.path, rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tc.wantContent) {
				t.Fatalf("GET %s: body missing %q", tc.path, tc.wantContent)
			}
		})
	}
}

func TestStaticAssets_UnknownPathReturns404(t *testing.T) {
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })
	req := httptest.NewRequest(http.MethodGet, "/does-not-exist.js", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestStaticAssets_ServesPWAIcons(t *testing.T) {
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })

	for _, path := range []string{
		"/icons/icon-192.png",
		"/icons/icon-512.png",
		"/icons/icon-192-maskable.png",
		"/icons/icon-512-maskable.png",
		"/icons/apple-touch-icon.png",
	} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s: status = %d", path, rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
				t.Fatalf("GET %s: content-type = %q, want image/png", path, ct)
			}
		})
	}
}
