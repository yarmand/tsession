package webui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

// fakeFailingCodeScript writes a shell script standing in for a `code`
// binary that is present but broken: it exits immediately with a non-zero
// status and never prints a "Web UI available at" line, so codeserver's
// waitLoop marks the instance StatusFailed shortly after launch.
func fakeFailingCodeScript(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-failing-code.sh")
	script := "#!/bin/sh\necho 'boom: missing dependency' >&2\nexit 1\n"
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

// newTestCodeServerWithBin is like newTestCodeServer but lets the caller
// supply an arbitrary `code` binary path (e.g. one that fails to start),
// and captures every debug log line the server emits.
func newTestCodeServerWithBin(t *testing.T, all []sessions.Session, bin string) (*Server, *bytes.Buffer) {
	t.Helper()
	registry := codeserver.NewRegistry()
	t.Cleanup(func() { _ = registry.Shutdown() })

	cfg := &config.Config{CodeCommand: bin}
	srv := NewServer(
		func() ([]sessions.Session, error) { return all, nil },
		WithCodeServer(registry, func() (*config.Config, error) { return cfg, nil }),
	)
	srv.SetCodeDataDir(t.TempDir())

	var logs bytes.Buffer
	srv.SetDebugLog(func(format string, args ...any) {
		_, _ = fmt.Fprintf(&logs, format+"\n", args...)
	})
	return srv, &logs
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

// TestHandleCodeServerStart_LogsSSHCommandForRemoteSession covers the
// "any ssh command sent to the remote" debug requirement: starting a code
// view for a remote session must log the exact ssh command used to launch
// it, before attempting to run it, so a misconfigured code_command or
// unreachable host is diagnosable from the ssh command that was actually
// invoked.
func TestHandleCodeServerStart_LogsSSHCommandForRemoteSession(t *testing.T) {
	all := []sessions.Session{{ID: "s1", Origin: "devbox", CWD: "/home/me/proj"}}
	registry := codeserver.NewRegistry()
	t.Cleanup(func() { _ = registry.Shutdown() })

	// SSHCommand deliberately doesn't exist: the ssh command is logged
	// before it's ever run, so registry.Start failing afterward doesn't
	// prevent the log line from appearing.
	remote := config.Remote{Name: "devbox", Type: "ssh", Host: "devbox.example.com", CodeCommand: "/usr/local/bin/vscode-cli", SSHCommand: "/does/not/exist"}
	cfg := &config.Config{}
	srv := NewServer(
		func() ([]sessions.Session, error) { return all, nil },
		WithCodeServer(registry, func() (*config.Config, error) { return cfg, nil }),
		WithRemotes(func(origin string) (config.Remote, bool, error) {
			if origin == "devbox" {
				return remote, true, nil
			}
			return config.Remote{}, false, nil
		}),
	)
	srv.SetCodeDataDir(t.TempDir())

	var logs bytes.Buffer
	srv.SetDebugLog(func(format string, args ...any) {
		_, _ = fmt.Fprintf(&logs, format+"\n", args...)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/codeserver/devbox/s1", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	got := logs.String()
	for _, want := range []string{
		"action=code-view-ssh-command",
		"session=s1",
		"origin=devbox",
		"/does/not/exist",
		"devbox.example.com",
		"/usr/local/bin/vscode-cli",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("debug log missing %q: %s", want, got)
		}
	}
}

// TestHandleCodeServerStart_LogsFailureWhenBinaryMissing covers the
// synchronous failure path: the configured `code` binary doesn't exist at
// all, so codeserver.Registry.Start fails immediately (before any instance
// is created) and the handler must still surface a debug log line, since
// this is exactly the "code is failing to start" case operators need to
// diagnose.
func TestHandleCodeServerStart_LogsFailureWhenBinaryMissing(t *testing.T) {
	all := []sessions.Session{{ID: "s1", CWD: t.TempDir()}}
	srv, logs := newTestCodeServerWithBin(t, all, filepath.Join(t.TempDir(), "no-such-code-binary"))

	req := httptest.NewRequest(http.MethodPost, "/api/codeserver/local/s1", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500, body=%s", rec.Code, rec.Body.String())
	}
	got := logs.String()
	for _, want := range []string{"level=error", "action=code-view-start-failed", "session=s1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("debug log missing %q: %s", want, got)
		}
	}
}

// TestHandleCodeServerStatus_LogsFailureWhenInstanceExitsImmediately covers
// the asynchronous failure path: the `code` binary exists and launches, but
// exits immediately (e.g. a broken install), so the instance transitions
// from starting to StatusFailed on its own. The first status poll to
// observe that transition must log it.
func TestHandleCodeServerStatus_LogsFailureWhenInstanceExitsImmediately(t *testing.T) {
	all := []sessions.Session{{ID: "s1", CWD: t.TempDir()}}
	srv, logs := newTestCodeServerWithBin(t, all, fakeFailingCodeScript(t))

	req := httptest.NewRequest(http.MethodPost, "/api/codeserver/local/s1", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("start status = %d, body=%s", rec.Code, rec.Body.String())
	}

	waitForCodeStatus(t, srv, "/api/codeserver/local/s1", string(codeserver.StatusFailed))

	got := logs.String()
	for _, want := range []string{"level=error", "action=code-view-instance-failed", "session=s1", "boom: missing dependency"} {
		if !strings.Contains(got, want) {
			t.Fatalf("debug log missing %q: %s", want, got)
		}
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
