// This file implements the HTTP surface for the optional VS Code "code
// view": control endpoints to start/inspect/stop a per-session code server
// (backed by internal/codeserver), and a same-origin reverse proxy that
// lets the browser reach it without any cross-origin storage partitioning
// or iframe security friction (see the design doc's rationale for
// --server-base-path plus an unmodified-path proxy).
package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"

	"github.com/yarma/tsession/internal/codecmd"
	"github.com/yarma/tsession/internal/codeserver"
	"github.com/yarma/tsession/internal/config"
	"github.com/yarma/tsession/internal/sessions"
)

// CodeConfigProvider returns the current config.Config, used to resolve the
// `code` binary override (top-level or per-remote code_command) when
// building a launch command. Re-read on every call, matching
// RemoteResolver's own "config is small and rarely changes" rationale.
type CodeConfigProvider func() (*config.Config, error)

// defaultCodeDataDir returns ~/.tsession/codeserver, the base directory
// under which each code server key gets its own --server-data-dir
// subdirectory (VS Code persists its own extensions/settings state there
// across restarts of the same session's code view).
func defaultCodeDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".tsession", "codeserver")
}

// WithCodeServer enables the /api/codeserver and /api/code routes, backed
// by registry and cfgFn. Without it, those routes respond 501 Not
// Implemented, matching how WithTerminal degrades today.
func WithCodeServer(registry *codeserver.Registry, cfgFn CodeConfigProvider) Option {
	return func(s *Server) {
		s.codeRegistry = registry
		s.codeCfgFn = cfgFn
	}
}

// SetCodeDataDir overrides the base directory code server instances persist
// their state under. Tests use this to avoid touching the real
// ~/.tsession directory; production callers can leave the default.
func (s *Server) SetCodeDataDir(dir string) { s.codeDataDir = dir }

// codeServerResponse is the JSON shape returned by the start and status
// endpoints.
type codeServerResponse struct {
	Key    string `json:"key"`
	Path   string `json:"path,omitempty"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	Log    string `json:"log,omitempty"`
}

// resolveCodeTarget maps the {origin}/{id} path segments to the session it
// names plus its config.Remote (zero value for local sessions), returning
// resolveCodeTarget maps the {origin}/{id} path segments to the session it
// names plus its config.Remote (zero value for local sessions), returning
// an *http.Error-worthy message and status code on failure ("" origin
// means local, mirroring handleTerminal's localOriginSegment convention).
func (s *Server) resolveCodeTarget(originParam, id string) (origin string, target *sessions.Session, remote config.Remote, err error, status int) {
	origin = originParam
	if origin == localOriginSegment {
		origin = ""
	}

	target, err = s.findSession(origin, id)
	if err != nil {
		return origin, nil, config.Remote{}, err, http.StatusInternalServerError
	}
	if target == nil {
		return origin, nil, config.Remote{}, fmt.Errorf("session not found"), http.StatusNotFound
	}

	if origin != "" {
		remote, err = s.resolveRemote(origin)
		if err != nil {
			return origin, nil, config.Remote{}, err, http.StatusBadRequest
		}
	}
	return origin, target, remote, nil, 0
}

// handleCodeServerStart serves POST /api/codeserver/{origin}/{id}: it
// starts a code serve-web instance for the named session's CWD, or returns
// the already-running one unchanged (see codeserver.Registry.Start) — this
// is what makes reactivating a session with an existing code view reuse it
// instead of relaunching VS Code.
func (s *Server) handleCodeServerStart(w http.ResponseWriter, r *http.Request) {
	if s.codeRegistry == nil {
		http.Error(w, "code view not configured", http.StatusNotImplemented)
		return
	}

	originParam := r.PathValue("origin")
	id := r.PathValue("id")

	origin, target, remote, err, status := s.resolveCodeTarget(originParam, id)
	if err != nil {
		http.Error(w, err.Error(), status)
		return
	}

	cfg, err := s.codeConfig()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	key := codecmd.Key(origin, id)
	basePath := "/api/code/" + key
	dataDir := filepath.Join(s.codeDataDir, key)

	bin, args, err := codecmd.Build(*target, remote, cfg, basePath, dataDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	regKey := codeserver.Key{Origin: origin, ID: id}
	inst, err := s.codeRegistry.Start(regKey, codeserver.Spec{
		Bin:       bin,
		Args:      args,
		PortReady: codeserver.PortReadyFor(origin, remote),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.registerCodeKey(key, regKey)

	status_, _, logTail, instErr := inst.Status()
	writeCodeServerResponse(w, key, basePath+"/", status_, logTail, instErr)
}

// handleCodeServerStatus serves GET /api/codeserver/{origin}/{id}: it
// reports the current lifecycle status of the named session's code server
// instance without starting one, so the browser can poll while VS Code is
// still starting up (which can take minutes on first run). A session with
// no instance at all is reported as "stopped" rather than 404, since that
// is the steady state before the user has ever activated its code view.
func (s *Server) handleCodeServerStatus(w http.ResponseWriter, r *http.Request) {
	if s.codeRegistry == nil {
		http.Error(w, "code view not configured", http.StatusNotImplemented)
		return
	}

	originParam := r.PathValue("origin")
	id := r.PathValue("id")
	origin := originParam
	if origin == localOriginSegment {
		origin = ""
	}

	key := codecmd.Key(origin, id)
	regKey := codeserver.Key{Origin: origin, ID: id}
	inst, ok := s.codeRegistry.Get(regKey)
	if !ok {
		writeCodeServerResponse(w, key, "", codeserver.StatusStopped, nil, nil)
		return
	}

	s.registerCodeKey(key, regKey)
	status_, _, logTail, instErr := inst.Status()
	writeCodeServerResponse(w, key, "/api/code/"+key+"/", status_, logTail, instErr)
}

// handleCodeServerStop serves DELETE /api/codeserver/{origin}/{id}: the
// only thing that stops VS Code, per the chosen lifecycle (hide-on-switch
// otherwise leaves it running).
func (s *Server) handleCodeServerStop(w http.ResponseWriter, r *http.Request) {
	if s.codeRegistry == nil {
		http.Error(w, "code view not configured", http.StatusNotImplemented)
		return
	}

	originParam := r.PathValue("origin")
	id := r.PathValue("id")
	origin := originParam
	if origin == localOriginSegment {
		origin = ""
	}

	regKey := codeserver.Key{Origin: origin, ID: id}
	if err := s.codeRegistry.Stop(regKey); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	key := codecmd.Key(origin, id)
	s.unregisterCodeKey(key)

	writeCodeServerResponse(w, key, "", codeserver.StatusStopped, nil, nil)
}

func writeCodeServerResponse(w http.ResponseWriter, key, path string, status codeserver.Status, logTail []byte, instErr error) {
	resp := codeServerResponse{Key: key, Path: path, Status: string(status), Log: string(logTail)}
	if instErr != nil {
		resp.Error = instErr.Error()
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

// codeConfig returns the current config.Config via s.codeCfgFn, or an
// empty config if none was injected (WithCodeServer not used, though that
// case is already caught earlier by the s.codeRegistry nil check).
func (s *Server) codeConfig() (*config.Config, error) {
	if s.codeCfgFn == nil {
		return &config.Config{}, nil
	}
	return s.codeCfgFn()
}

// registerCodeKey and unregisterCodeKey maintain the mapping from a code
// server's opaque URL key back to the (origin, id) Registry key, needed by
// handleCodeProxy, which only sees the key (from /api/code/{key}/...) and
// has no other way to recover which session it belongs to. Also drops any
// cached proxy Transport for key so a restarted instance gets a fresh
// connection pool rather than reusing one dialing a now-dead process.
func (s *Server) registerCodeKey(key string, target codeserver.Key) {
	s.codeKeysMu.Lock()
	defer s.codeKeysMu.Unlock()
	s.codeKeys[key] = target
}

func (s *Server) unregisterCodeKey(key string) {
	s.codeKeysMu.Lock()
	defer s.codeKeysMu.Unlock()
	delete(s.codeKeys, key)
	if t, ok := s.codeTransports[key]; ok {
		t.CloseIdleConnections()
		delete(s.codeTransports, key)
	}
}

// handleCodeProxy serves /api/code/{key}/... : a same-origin reverse proxy
// to the code server instance registered under key, dialing through
// whatever internal/codeserver.Instance.Dial resolves to (a direct
// loopback dial for local instances, an SSH/Codespaces tunnel's local end,
// or a fresh docker-exec stdio relay per connection for devcontainers) —
// the proxy itself is fully transport-agnostic. The request path is
// forwarded unmodified, matching the code server's own
// --server-base-path, and http.Transport's built-in Upgrade handling
// passes WebSocket connections through untouched.
func (s *Server) handleCodeProxy(w http.ResponseWriter, r *http.Request) {
	if s.codeRegistry == nil {
		http.Error(w, "code view not configured", http.StatusNotImplemented)
		return
	}

	key := r.PathValue("key")

	s.codeKeysMu.Lock()
	target, ok := s.codeKeys[key]
	s.codeKeysMu.Unlock()
	if !ok {
		http.Error(w, "unknown code server key", http.StatusNotFound)
		return
	}

	inst, ok := s.codeRegistry.Get(target)
	if !ok {
		http.Error(w, "code server not running", http.StatusNotFound)
		return
	}

	transport := s.codeTransportFor(key, inst)
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = "http"
			req.URL.Host = "codeserver"
		},
		Transport: transport,
	}
	proxy.ServeHTTP(w, r)
}

// codeTransportFor returns a cached *http.Transport for key, creating one
// on first use. Its DialContext always dials through inst regardless of
// the requested network/address, since inst.Dial is the only thing that
// knows how to actually reach this particular instance; caching it (rather
// than building a fresh Transport per request) preserves HTTP keep-alive
// connection reuse, which VS Code's web client relies on heavily.
func (s *Server) codeTransportFor(key string, inst *codeserver.Instance) *http.Transport {
	s.codeKeysMu.Lock()
	defer s.codeKeysMu.Unlock()
	if t, ok := s.codeTransports[key]; ok {
		return t
	}
	t := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return inst.Dial()
		},
	}
	s.codeTransports[key] = t
	return t
}
