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
		{"/app_logic.js", "LOCAL_TERMINAL"},
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

func TestStaticAssets_TerminalResizeSendsUpdatedDimensions(t *testing.T) {
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /app.js: status = %d", rec.Code)
	}

	if !strings.Contains(rec.Body.String(), "term.onResize(() => {") {
		t.Fatal("app.js does not forward xterm resize events to the terminal WebSocket")
	}
	if !strings.Contains(rec.Body.String(), `JSON.stringify({ type: "resize", cols: pane.term.cols, rows: pane.term.rows })`) {
		t.Fatal("app.js does not send xterm's current rows and columns in the resize control frame")
	}
}

func TestStaticAssets_LocalTerminalReselectSendsActivate(t *testing.T) {
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{
		"function sendActivate(pane)",
		`pane.key !== sessionKey(LOCAL_TERMINAL)`,
		`JSON.stringify({ type: "activate" })`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("app.js missing %q", want)
		}
	}
}

func TestStaticAssets_SessionRowsShowLocalOrRemoteLocation(t *testing.T) {
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })

	for path, want := range map[string]string{
		"/app.js":  "function remoteColors()",
		"/app.css": ".session-row .location.remote",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("GET %s: body missing %q", path, want)
		}
	}
}

func TestStaticAssets_SessionListCanCollapseAndOverlay(t *testing.T) {
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })

	for path, wants := range map[string][]string{
		"/": {`id="sessions-toggle"`, `id="sessions-resize"`, "Alt+H hide"},
		"/app_logic.js": {
			`isPlainAltChord(ev, "KeyH")`,
		},
		"/app.js": {
			`appLogic.captureKeyAction(ev, state.focusTarget)`,
			"sidebarOverlay",
			"function toggleSidebar()",
			`sessionsResize.addEventListener("pointerdown"`,
			`localStorage.setItem(SIDEBAR_WIDTH_KEY`,
		},
		"/app.css": {
			"#app.sidebar-collapsed",
			"#app.sidebar-collapsed.sidebar-overlay #sessions",
			"flex: 1 1 auto",
			"#sessions-resize",
			"position: fixed",
		},
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d", path, rec.Code)
		}
		for _, want := range wants {
			if !strings.Contains(rec.Body.String(), want) {
				t.Fatalf("GET %s: body missing %q", path, want)
			}
		}
	}
}

func TestStaticAssets_ServiceWorkerPrefersFreshShellAssets(t *testing.T) {
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })
	req := httptest.NewRequest(http.MethodGet, "/sw.js", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /sw.js: status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "return network.catch(() => cached)") {
		t.Fatal("service worker is not network-first for shell assets")
	}
}

func TestStaticAssets_CodePaneLayoutPresent(t *testing.T) {
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })

	indexReq := httptest.NewRequest(http.MethodGet, "/", nil)
	indexRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(indexRec, indexReq)
	for _, want := range []string{
		`id="terminal-wrap"`,
		`id="code-resize"`,
		`id="code-pane"`,
		`id="code-pane-header"`,
		`id="code-pane-label"`,
		`id="code-pane-status"`,
		`id="code-pane-close"`,
		`id="code-pane-body"`,
	} {
		if !strings.Contains(indexRec.Body.String(), want) {
			t.Fatalf("index.html missing %q", want)
		}
	}

	cssReq := httptest.NewRequest(http.MethodGet, "/app.css", nil)
	cssRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(cssRec, cssReq)
	for _, want := range []string{"--code-width", "#code-resize", ".code-frame"} {
		if !strings.Contains(cssRec.Body.String(), want) {
			t.Fatalf("app.css missing %q", want)
		}
	}

	jsReq := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	jsRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(jsRec, jsReq)
	for _, want := range []string{"setCodeWidth", "restoreCodeWidth", "tsession-code-width"} {
		if !strings.Contains(jsRec.Body.String(), want) {
			t.Fatalf("app.js missing %q", want)
		}
	}
}

func TestStaticAssets_CodeViewAltEBehaviourPresent(t *testing.T) {
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })

	jsReq := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	jsRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(jsRec, jsReq)
	body := jsRec.Body.String()
	for _, want := range []string{
		`appLogic.captureKeyAction(ev, state.focusTarget)`,
		"toggleCodeView",
		"closeCodeView",
		"renderCodePane",
		"/api/codeserver/",
		`method: "DELETE"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("app.js missing %q", want)
		}
	}

	indexReq := httptest.NewRequest(http.MethodGet, "/", nil)
	indexRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(indexRec, indexReq)
	if !strings.Contains(indexRec.Body.String(), "Alt+E code view") {
		t.Fatal("session list hint does not mention Alt+E")
	}

	logicReq := httptest.NewRequest(http.MethodGet, "/app_logic.js", nil)
	logicRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(logicRec, logicReq)
	if !strings.Contains(logicRec.Body.String(), `isPlainAltChord(ev, "KeyE")`) {
		t.Fatal("app_logic.js does not define the Alt+E capture-phase chord")
	}
}

func TestStaticAssets_ActivePaneCanZoom(t *testing.T) {
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })

	body := func(path string) string {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d", path, rec.Code)
		}
		return rec.Body.String()
	}

	for _, want := range []string{`id="terminal-zoom"`, `id="code-pane-zoom"`, "Alt+Z zoom"} {
		if !strings.Contains(body("/"), want) {
			t.Errorf("index.html missing %q", want)
		}
	}
	for _, want := range []string{"function togglePaneZoom", `"pane-zoom-terminal"`, `"pane-zoom-code"`} {
		if !strings.Contains(body("/app.js"), want) {
			t.Errorf("app.js missing %q", want)
		}
	}
	for _, want := range []string{"#terminal-pane.pane-zoom-terminal", "#terminal-pane.pane-zoom-code"} {
		if !strings.Contains(body("/app.css"), want) {
			t.Errorf("app.css missing %q", want)
		}
	}
	if !strings.Contains(body("/app_logic.js"), `isPlainAltChord(ev, "KeyZ")`) {
		t.Error("app_logic.js does not define the Alt+Z chord")
	}
	if !strings.Contains(body("/codekeys.js"), `ev.code === "KeyZ"`) {
		t.Error("codekeys.js does not forward Alt+Z from the code iframe")
	}
	if !strings.Contains(body("/sw.js"), `tsession-shell-v19`) {
		t.Error("service worker cache was not bumped for changed static assets")
	}
}

func TestStaticAssets_UserInteractionsReportDebugEvents(t *testing.T) {
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })

	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{
		"function reportUserInteraction",
		`"/api/debug"`,
		`reportUserInteraction("select-session"`,
		`reportUserInteraction("code-view-show"`,
		`reportUserInteraction("code-view-hide"`,
		`reportUserInteraction("code-view-close"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("app.js missing %q", want)
		}
	}
}

func TestStaticAssets_FailuresReportDebugEventsWithErrorLevel(t *testing.T) {
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })

	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{
		`function reportFailure`,
		`"error"`,
		`reportFailure("code-view-failed"`,
		`reportFailure("code-view-start-failed"`,
		`reportFailure("code-view-poll-failed"`,
		`reportFailure("terminal-connect-failed"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("app.js missing %q", want)
		}
	}
}

// tmux signals a copy with OSC 52. xterm.js has no handler for it, so the
// web terminal must register one or a copy made inside a session (most
// visibly a remote one) never reaches this machine's clipboard.
func TestStaticAssets_TerminalHandlesOSC52ClipboardWrites(t *testing.T) {
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })

	body := func(path string) string {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d", path, rec.Code)
		}
		return rec.Body.String()
	}

	clip := body("/clipboard.js")
	for _, want := range []string{"registerOscHandler", "52", "systemWriter"} {
		if !strings.Contains(clip, want) {
			t.Errorf("clipboard.js missing %q", want)
		}
	}

	index := body("/")
	if !strings.Contains(index, "/clipboard.js") {
		t.Error("index.html does not load clipboard.js")
	}

	app := body("/app.js")
	if !strings.Contains(app, "tsessionClipboard.install") {
		t.Error("app.js does not install the OSC 52 clipboard handler on the terminal")
	}

	sw := body("/sw.js")
	if !strings.Contains(sw, "/clipboard.js") {
		t.Error("service worker does not cache clipboard.js")
	}
}

// Keydowns inside the VS Code iframe never reach the parent document, so
// Alt+/ would be dead while the code pane has focus unless the frame's own
// window is hooked.
func TestStaticAssets_CodePaneForwardsAltSlashToTerminal(t *testing.T) {
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })

	body := func(path string) string {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d", path, rec.Code)
		}
		return rec.Body.String()
	}

	if !strings.Contains(body("/"), "/codekeys.js") {
		t.Error("index.html does not load codekeys.js")
	}
	app := body("/app.js")
	if !strings.Contains(app, "tsessionCodeKeys.install(iframe") {
		t.Error("app.js does not install the code pane key handler on the VS Code iframe")
	}
	if !strings.Contains(body("/sw.js"), "/codekeys.js") {
		t.Error("service worker does not cache codekeys.js")
	}
}

// The browser terminal is always fed by a PTY, whose line discipline already
// turns "\n" into "\r\n". tmux, however, deliberately emits a bare LF (the
// xterm-256color cud1 capability) to move the cursor down *within the same
// column* when drawing non-active split panes. xterm.js's convertEol would
// turn that into CR+LF, snapping the cursor to column 0 and corrupting every
// pane but the one at the left edge.
func TestStaticAssets_TerminalDoesNotConvertEol(t *testing.T) {
	data, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "convertEol: true") {
		t.Fatal("app.js enables xterm.js convertEol, which breaks tmux split-pane rendering")
	}
}

func TestStaticAssets_LocalTerminalRowIsPinnedAfterSessions(t *testing.T) {
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })

	for path, wants := range map[string][]string{
		"/app_logic.js": {
			`const LOCAL_TERMINAL = Object.freeze({`,
			`localTerminal: true`,
			"function listRows(sessions)",
			`return sessions.concat([LOCAL_TERMINAL]);`,
			`function captureKeyAction(ev, focusTarget)`,
			`return s.localTerminal`,
			`? "/api/localterm"`,
		},
		"/app.js": {
			"function openLocalTerminal()",
			`appLogic.captureKeyAction(ev, state.focusTarget)`,
			`appLogic.terminalSocketPath(s)`,
		},
		"/app.css": {".session-row.local-terminal"},
		"/":        {"Alt+T terminal", "/app_logic.js"},
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d", path, rec.Code)
		}
		for _, want := range wants {
			if !strings.Contains(rec.Body.String(), want) {
				t.Fatalf("GET %s: body missing %q", path, want)
			}
		}
	}
}

// The pinned row is not an agent session, so the code view and the two
// rename paths must skip it rather than sending it to an API that would
// reject it.
func TestStaticAssets_LocalTerminalRowIsNotRenamableOrCodeViewable(t *testing.T) {
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{
		"if (!s || s.localTerminal) return;",
		"if (s.localTerminal) return;",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("app.js missing guard %q", want)
		}
	}
}

// Any static asset change without a cache bump leaves the service worker
// serving the previous app shell.
func TestStaticAssets_ServiceWorkerCacheVersionIsCurrent(t *testing.T) {
	srv := NewServer(func() ([]sessions.Session, error) { return nil, nil })
	req := httptest.NewRequest(http.MethodGet, "/sw.js", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, `const CACHE_NAME = "tsession-shell-v19";`) {
		t.Fatal("sw.js CACHE_NAME was not bumped for this change")
	}
	if !strings.Contains(body, `"/app_logic.js"`) {
		t.Fatal("sw.js does not cache app_logic.js")
	}
}
