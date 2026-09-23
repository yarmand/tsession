package webui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yarma/tsession/internal/codeserver"
	"github.com/yarma/tsession/internal/config"
	"github.com/yarma/tsession/internal/sessions"
)

// fakeCodeScript writes a shell script standing in for `code`: it ignores
// all `serve-web` flags it's invoked with, starts a tiny Python HTTP
// server on an OS-assigned loopback port that echoes the request path back
// in its response body (so proxy tests can assert the path reached it
// unmodified), and prints VS Code's real "Web UI available at" startup
// line so codeserver's port-scanning picks it up.
func fakeCodeScript(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-code.sh")
	script := `#!/bin/sh
exec python3 -c "
import http.server, socketserver

class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header('Content-Type', 'text/plain')
        self.end_headers()
        self.wfile.write(('hello from ' + self.path).encode())
    def log_message(self, *a):
        pass

httpd = socketserver.TCPServer(('127.0.0.1', 0), Handler)
port = httpd.server_address[1]
print('Web UI available at http://127.0.0.1:%d/' % port, flush=True)
httpd.serve_forever()
"
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func newTestCodeServer(t *testing.T, all []sessions.Session) (*Server, *codeserver.Registry) {
	t.Helper()
	registry := codeserver.NewRegistry()
	t.Cleanup(func() { _ = registry.Shutdown() })

	cfg := &config.Config{CodeCommand: fakeCodeScript(t)}
	srv := NewServer(
		func() ([]sessions.Session, error) { return all, nil },
		WithCodeServer(registry, func() (*config.Config, error) { return cfg, nil }),
	)
	srv.SetCodeDataDir(t.TempDir())
	return srv, registry
}

func decodeCodeServerResponse(t *testing.T, rec *httptest.ResponseRecorder) codeServerResponse {
	t.Helper()
	var resp codeServerResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return resp
}

func waitForCodeStatus(t *testing.T, srv *Server, path string, want string) codeServerResponse {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var last codeServerResponse
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		last = decodeCodeServerResponse(t, rec)
		if last.Status == want {
			return last
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("status for %s never reached %q, last=%+v", path, want, last)
	return last
}

func TestHandleCodeServerStart_NotConfiguredReturns501(t *testing.T) {
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })

	req := httptest.NewRequest(http.MethodPost, "/api/codeserver/local/s1", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rec.Code)
	}
}

func TestHandleCodeServerStart_UnknownSessionReturns404(t *testing.T) {
	srv, _ := newTestCodeServer(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/codeserver/local/missing", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandleCodeServerStart_LaunchesAndReusesInstance(t *testing.T) {
	all := []sessions.Session{{ID: "s1", CWD: t.TempDir()}}
	srv, _ := newTestCodeServer(t, all)

	req := httptest.NewRequest(http.MethodPost, "/api/codeserver/local/s1", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("start status = %d, body=%s", rec.Code, rec.Body.String())
	}
	first := decodeCodeServerResponse(t, rec)
	if first.Key == "" || first.Path == "" {
		t.Fatalf("expected key and path, got %+v", first)
	}

	running := waitForCodeStatus(t, srv, "/api/codeserver/local/s1", string(codeserver.StatusRunning))
	if running.Key != first.Key || running.Path != first.Path {
		t.Fatalf("status response mismatch: %+v vs %+v", running, first)
	}

	// Re-POSTing must reuse the already-running instance (same key), per
	// the "don't relaunch VS Code when reactivating" requirement.
	req2 := httptest.NewRequest(http.MethodPost, "/api/codeserver/local/s1", nil)
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, req2)
	second := decodeCodeServerResponse(t, rec2)
	if second.Key != first.Key {
		t.Fatalf("expected reuse of key %q, got %q", first.Key, second.Key)
	}
	if second.Status != string(codeserver.StatusRunning) {
		t.Fatalf("expected reused instance to already be running, got %q", second.Status)
	}
}

func TestHandleCodeServerStatus_UnstartedReportsStopped(t *testing.T) {
	all := []sessions.Session{{ID: "s1"}}
	srv, _ := newTestCodeServer(t, all)

	req := httptest.NewRequest(http.MethodGet, "/api/codeserver/local/s1", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	resp := decodeCodeServerResponse(t, rec)
	if resp.Status != string(codeserver.StatusStopped) {
		t.Fatalf("expected stopped, got %+v", resp)
	}
}

func TestHandleCodeServerStop_TearsDownInstance(t *testing.T) {
	all := []sessions.Session{{ID: "s1", CWD: t.TempDir()}}
	srv, registry := newTestCodeServer(t, all)

	req := httptest.NewRequest(http.MethodPost, "/api/codeserver/local/s1", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	waitForCodeStatus(t, srv, "/api/codeserver/local/s1", string(codeserver.StatusRunning))

	delReq := httptest.NewRequest(http.MethodDelete, "/api/codeserver/local/s1", nil)
	delRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("stop status = %d, body=%s", delRec.Code, delRec.Body.String())
	}

	if _, ok := registry.Get(codeserver.Key{Origin: "", ID: "s1"}); ok {
		t.Fatal("expected instance to be removed from the registry after stop")
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/api/codeserver/local/s1", nil)
	statusRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(statusRec, statusReq)
	resp := decodeCodeServerResponse(t, statusRec)
	if resp.Status != string(codeserver.StatusStopped) {
		t.Fatalf("expected stopped after DELETE, got %+v", resp)
	}
}

func TestHandleCodeProxy_UnknownKeyReturns404(t *testing.T) {
	srv, _ := newTestCodeServer(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/code/deadbeef1234/", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandleCodeProxy_RoundTripsRequestsWithUnmodifiedPath(t *testing.T) {
	all := []sessions.Session{{ID: "s1", CWD: t.TempDir()}}
	srv, _ := newTestCodeServer(t, all)

	startReq := httptest.NewRequest(http.MethodPost, "/api/codeserver/local/s1", nil)
	startRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(startRec, startReq)
	started := decodeCodeServerResponse(t, startRec)
	waitForCodeStatus(t, srv, "/api/codeserver/local/s1", string(codeserver.StatusRunning))

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + started.Path + "some/deep/path")
	if err != nil {
		t.Fatalf("GET through proxy: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body=%s", resp.StatusCode, body)
	}
	want := "hello from " + started.Path + "some/deep/path"
	if string(body) != want {
		t.Fatalf("body = %q, want %q (path must reach the instance unmodified)", body, want)
	}
}
