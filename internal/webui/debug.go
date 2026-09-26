package webui

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/yarma/tsession/internal/shellutil"
)

type debugEvent struct {
	Action      string `json:"action"`
	SessionID   string `json:"sessionId"`
	Origin      string `json:"origin"`
	SessionName string `json:"sessionName"`
	Detail      string `json:"detail"`
	Level       string `json:"level"`
}

// handleDebugEvent serves POST /api/debug: the browser reports interesting
// user interactions and client-side failures here (session switches, code
// view toggles, failed VS Code launches, ...) so they land in the same
// `serve` process log as server-originated events (see logInteraction),
// giving one place to look when diagnosing "why didn't X work".
func (s *Server) handleDebugEvent(w http.ResponseWriter, r *http.Request) {
	var ev debugEvent
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&ev); err != nil {
		http.Error(w, "invalid debug event", http.StatusBadRequest)
		return
	}
	ev.Action = strings.TrimSpace(ev.Action)
	if ev.Action == "" {
		http.Error(w, "missing debug action", http.StatusBadRequest)
		return
	}
	s.logInteraction(ev.Action, ev.SessionID, ev.Origin, ev.SessionName, ev.Detail, ev.Level)
	w.WriteHeader(http.StatusNoContent)
}

// logInteraction is the single place that formats and emits a debug log
// line, used both for browser-reported events (handleDebugEvent) and for
// failures the server itself observes (e.g. a code serve-web instance
// failing to start — see code.go). level defaults to "info"; failures pass
// "error" so `grep level=error` finds every failure regardless of which
// side detected it.
func (s *Server) logInteraction(action, sessionID, origin, name, detail, level string) {
	if s.debugLogf == nil {
		return
	}
	level = defaultDebugValue(level, "info")
	s.debugLogf(
		"serve user interaction: action=%s session=%s origin=%s level=%s name=%q detail=%q",
		action,
		sessionID,
		defaultDebugValue(origin, "local"),
		level,
		name,
		detail,
	)
}

func defaultDebugValue(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// formatCommand renders bin/args as a single shell-quoted command line,
// suitable for a debug log's detail field, so a failing remote launch can
// be copy-pasted and re-run by hand to diagnose it.
func formatCommand(bin string, args []string) string {
	return shellutil.Quote(bin) + " " + shellutil.Join(args)
}
