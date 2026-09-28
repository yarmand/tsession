package webui

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yarma/tsession/internal/sessions"
)

func TestOpenExternal(t *testing.T) {
	var opened []string
	srv := NewServer(
		func() ([]sessions.Session, error) { return nil, nil },
		WithExternalOpener(func(url string) error {
			opened = append(opened, url)
			return nil
		}),
	)
	for _, tc := range []struct {
		name, origin, contentType, body string
		wantStatus                      int
	}{
		{"device login", "http://127.0.0.1:4270", "application/json", `{"url":"https://github.com/login/device"}`, 204},
		{"other origin", "https://evil.example", "application/json", `{"url":"https://github.com/login/device"}`, 403},
		{"missing origin", "", "application/json", `{"url":"https://github.com/login/device"}`, 403},
		{"form submission", "http://127.0.0.1:4270", "application/x-www-form-urlencoded", `url=https://github.com/login/device`, 415},
		{"unsafe scheme", "http://127.0.0.1:4270", "application/json", `{"url":"javascript:alert(1)"}`, 400},
		{"credentials", "http://127.0.0.1:4270", "application/json", `{"url":"https://user:pass@github.com/"}`, 400},
		{"external plaintext", "http://127.0.0.1:4270", "application/json", `{"url":"http://github.com/login/device"}`, 400},
		{"loopback callback", "http://127.0.0.1:4270", "application/json", `{"url":"http://127.0.0.1:4270/callback"}`, 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:4270/api/open-external", strings.NewReader(tc.body))
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			req.Header.Set("Content-Type", tc.contentType)
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
	if len(opened) != 2 || opened[0] != "https://github.com/login/device" || opened[1] != "http://127.0.0.1:4270/callback" {
		t.Fatalf("opened = %q", opened)
	}
}

func TestOpenExternal_ReportsLaunchFailure(t *testing.T) {
	srv := NewServer(
		func() ([]sessions.Session, error) { return nil, nil },
		WithExternalOpener(func(string) error { return errors.New("browser unavailable") }),
	)
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:4270/api/open-external", strings.NewReader(`{"url":"https://github.com/login/device"}`))
	req.Header.Set("Origin", "http://127.0.0.1:4270")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "browser unavailable") {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestOpenExternal_DoesNotLogAuthorizationTokens(t *testing.T) {
	var logLines strings.Builder
	srv := NewServer(
		func() ([]sessions.Session, error) { return nil, nil },
		WithExternalOpener(func(string) error { return nil }),
	)
	srv.SetDebugLog(func(format string, args ...any) {
		fmt.Fprintf(&logLines, format, args...)
	})
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:4270/api/open-external",
		strings.NewReader(`{"url":"https://github.com/login/oauth/authorize?state=secret-oauth-state"}`))
	req.Header.Set("Origin", "http://127.0.0.1:4270")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
	if strings.Contains(logLines.String(), "secret-oauth-state") {
		t.Fatalf("debug log exposed authorization token: %s", logLines.String())
	}
}

func TestOpenExternal_RejectsDNSRebindingHost(t *testing.T) {
	called := false
	srv := NewServer(
		func() ([]sessions.Session, error) { return nil, nil },
		WithExternalOpener(func(string) error { called = true; return nil }),
	)
	req := httptest.NewRequest(http.MethodPost, "http://attacker.example:4270/api/open-external",
		strings.NewReader(`{"url":"https://github.com/login/device"}`))
	req.Header.Set("Origin", "http://attacker.example:4270")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || called {
		t.Fatalf("status=%d called=%v", rec.Code, called)
	}
}
