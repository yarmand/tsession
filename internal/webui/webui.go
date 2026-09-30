// Package webui implements the HTTP surface for `tsession serve`: the
// session list JSON API, session/repository renaming, a server-sent-events
// notification stream, and (in a later increment) the terminal WebSocket.
// It intentionally holds no session-loading or tmux logic of its own —
// everything it needs is handed in by the caller (cmd/serve.go) as plain
// data or small provider functions, so this package can be exercised with
// httptest and fakes rather than a live tmux/git/SSH environment.
package webui

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/yarma/tsession/internal/codeserver"
	"github.com/yarma/tsession/internal/config"
	"github.com/yarma/tsession/internal/render"
	"github.com/yarma/tsession/internal/sessions"
	"github.com/yarma/tsession/internal/webterm"
)

// SessionsProvider returns the current merged (local + remote) session
// list, unfiltered — the handler applies the same "active" filter the CLI's
// `--active` flag uses (sessions.FilterActive) before rendering it as JSON.
type SessionsProvider func() ([]sessions.Session, error)

// AliasesProvider returns the repository alias map (see internal/reponames)
// used to label sessions the same way `--short` rendering does.
type AliasesProvider func() (map[string]string, error)

// RemoteResolver resolves a session's Origin to its configured
// config.Remote. ok is false when origin does not match any configured
// remote.
type RemoteResolver func(origin string) (r config.Remote, ok bool, err error)

// defaultNotifyStorePath returns ~/.tsession/notify-web.json, the web UI's
// own notification snapshot — kept separate from the desktop path's
// ~/.tsession/notify.json so the two observers never race over the same
// lock/snapshot (see internal/notify.DiffWithStore's doc comment).
func defaultNotifyStorePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".tsession", "notify-web.json")
}

// Server holds the dependencies for the web UI's HTTP handlers.
type Server struct {
	sessionsFn SessionsProvider
	aliasesFn  AliasesProvider
	remoteFn   RemoteResolver
	registry   *webterm.Registry
	homeDirFn  func() (string, error)
	now        func() time.Time

	sessionCache *sessionCache

	notifyStorePath string
	pollInterval    time.Duration

	codeRegistry *codeserver.Registry
	codeCfgFn    CodeConfigProvider
	codeDataDir  string

	localTermMu sync.Mutex

	codeKeysMu     sync.Mutex
	codeKeys       map[string]codeserver.Key
	codeTransports map[string]*http.Transport

	debugLogf    func(format string, args ...any)
	openExternal func(string) error
}

// Option configures optional Server dependencies not every caller needs
// (e.g. httptest-based unit tests of /api/sessions have no use for a
// terminal registry). See WithAliases, WithRemotes, and WithTerminal.
type Option func(*Server)

// WithAliases enables alias-aware repository labels (see internal/reponames)
// in /api/sessions. Without it, labels fall back to the plain origin short
// name.
func WithAliases(fn AliasesProvider) Option {
	return func(s *Server) { s.aliasesFn = fn }
}

// WithRemotes enables GET /api/terminal for remote sessions by letting the
// handler resolve a session's Origin to its config.Remote. Without it,
// terminal requests for remote sessions fail.
func WithRemotes(fn RemoteResolver) Option {
	return func(s *Server) { s.remoteFn = fn }
}

// WithTerminal enables GET /api/terminal/{origin}/{id}, backed by registry.
// Without it, the route responds 501 Not Implemented.
func WithTerminal(registry *webterm.Registry) Option {
	return func(s *Server) { s.registry = registry }
}

// WithExternalOpener lets the code view send blocked external links to the
// host's default browser. Without it, /api/open-external is unavailable.
func WithExternalOpener(open func(string) error) Option {
	return func(s *Server) { s.openExternal = open }
}

// NewServer builds a Server from a SessionsProvider and optional Options.
func NewServer(sessionsFn SessionsProvider, opts ...Option) *Server {
	s := &Server{
		sessionsFn:      sessionsFn,
		aliasesFn:       func() (map[string]string, error) { return nil, nil },
		now:             time.Now,
		homeDirFn:       os.UserHomeDir,
		notifyStorePath: defaultNotifyStorePath(),
		pollInterval:    3 * time.Second,
		codeDataDir:     defaultCodeDataDir(),
		codeKeys:        make(map[string]codeserver.Key),
		codeTransports:  make(map[string]*http.Transport),
		debugLogf:       log.Printf,
	}
	s.sessionCache = newSessionCache(sessionsFn, s.now)
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// SetNotifyStorePath overrides the path GET /api/events reads/writes its
// notification snapshot at. Tests use this to avoid touching the real
// ~/.tsession directory; production callers can leave the default.
func (s *Server) SetNotifyStorePath(path string) { s.notifyStorePath = path }

// SetPollInterval overrides how often GET /api/events re-checks the session
// list for done/question transitions. Tests use a short interval to avoid
// slow test runs; production callers can leave the 3s default.
func (s *Server) SetPollInterval(d time.Duration) { s.pollInterval = d }

// SetSessionTTL overrides how long a loaded session list is considered
// fresh before a read triggers a background refresh (see sessionCache).
// 0 disables caching entirely — every read loads synchronously. Tests that
// assert on session-list transitions within a few milliseconds (faster than
// a real SSH-backed load could ever complete) need this to keep observing
// every change; production callers can leave the 3s default.
func (s *Server) SetSessionTTL(d time.Duration) { s.sessionCache.SetTTL(d) }

// SetDebugLog overrides where browser user-interaction debug events are
// written. Production callers use the default log.Printf.
func (s *Server) SetDebugLog(fn func(format string, args ...any)) {
	if fn == nil {
		s.debugLogf = func(string, ...any) {}
		return
	}
	s.debugLogf = fn
}

// SetLocalTerminalHome overrides how the dedicated local terminal resolves
// its starting directory. Tests use this to exercise the failure path
// without touching the real environment; production callers leave the
// os.UserHomeDir default.
func (s *Server) SetLocalTerminalHome(fn func() (string, error)) {
	s.homeDirFn = fn
}

// Handler returns an http.Handler serving this Server's API routes, mounted
// at their final paths (e.g. "/api/sessions"). Callers combine it with
// static asset handlers as needed.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions", s.handleSessions)
	mux.HandleFunc("POST /api/sessions/{id}/name", s.handleRenameSession)
	mux.HandleFunc("POST /api/repos/alias", s.handleRepoAlias)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("POST /api/debug", s.handleDebugEvent)
	mux.HandleFunc("POST /api/open-external", s.handleOpenExternal)
	mux.HandleFunc("GET /api/terminal/{origin}/{id}", s.handleTerminal)
	mux.HandleFunc("GET /api/localterm", s.handleLocalTerminal)
	mux.HandleFunc("POST /api/codeserver/{origin}/{id}", s.handleCodeServerStart)
	mux.HandleFunc("GET /api/codeserver/{origin}/{id}", s.handleCodeServerStatus)
	mux.HandleFunc("DELETE /api/codeserver/{origin}/{id}", s.handleCodeServerStop)
	mux.HandleFunc("/api/code/{key}/", s.handleCodeProxy)
	mux.Handle("/", staticHandler())
	return mux
}

// SessionView is the JSON shape of one session in the /api/sessions
// response. Fields mirror what --short rendering shows, plus the raw
// timestamps so the browser can compute and continuously update its own
// "age" display rather than freezing the age at fetch time.
type SessionView struct {
	ID           string    `json:"id"`
	Origin       string    `json:"origin"`
	RemoteHost   string    `json:"remoteHost"`
	Source       string    `json:"source"`
	State        string    `json:"state"`
	Name         string    `json:"name"`
	Repository   string    `json:"repository"`
	RepositoryID string    `json:"repositoryId"`
	Worktree     string    `json:"worktree"`
	CWD          string    `json:"cwd"`
	Summary      string    `json:"summary"`
	UpdatedAt    time.Time `json:"updatedAt"`
	LastEventAt  time.Time `json:"lastEventAt"`
	HasTmux      bool      `json:"hasTmux"`
	TmuxSession  string    `json:"tmuxSession"`
	TmuxTarget   string    `json:"tmuxTarget"`
}

// SessionsResponse is the top-level JSON payload of GET /api/sessions.
type SessionsResponse struct {
	Sessions []SessionView `json:"sessions"`
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	all, err := s.sessionCache.Get()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	aliases, err := s.aliasesFn()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	active := sessions.FilterActive(all)
	ctx := render.BuildShortContextWithAliases(all, aliases)

	resp := SessionsResponse{Sessions: BuildSessionViews(active, ctx)}
	for i := range resp.Sessions {
		if resp.Sessions[i].Origin == "" {
			continue
		}
		if resp.Sessions[i].RemoteHost == "" {
			resp.Sessions[i].RemoteHost = resp.Sessions[i].Origin
		}
		if s.remoteFn == nil {
			continue
		}
		if remote, ok, resolveErr := s.remoteFn(resp.Sessions[i].Origin); resolveErr == nil && ok {
			resp.Sessions[i].RemoteHost = remote.Endpoint()
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// BuildSessionViews converts sessions into their JSON view, resolving
// repository labels through ctx the same way --short rendering does. ctx
// should be built from the full (pre-filter) session list so labels stay
// consistent regardless of which sessions later get filtered out.
func BuildSessionViews(active []sessions.Session, ctx render.ShortContext) []SessionView {
	views := make([]SessionView, 0, len(active))
	for _, s := range active {
		views = append(views, sessionView(s, ctx))
	}
	return views
}

func sessionView(s sessions.Session, ctx render.ShortContext) SessionView {
	worktree := render.WorktreeName(s)
	repo := ctx.RepositoryLabel(s)
	if repo == "" {
		repo = worktree
	}

	hasTmux := s.Origin == "" && (s.TmuxTarget != "" || s.TmuxName != "") || s.Origin != "" && s.RemoteTmuxAvailable
	tmuxTarget := s.TmuxTarget
	if s.Origin != "" {
		tmuxTarget = s.RemoteTmuxTarget
	}

	return SessionView{
		ID:           s.ID,
		Origin:       s.Origin,
		RemoteHost:   s.RemoteHost,
		Source:       s.Source,
		State:        s.State.String(),
		Name:         s.Name,
		Repository:   repo,
		RepositoryID: s.Repository,
		Worktree:     worktree,
		CWD:          s.CWD,
		Summary:      s.Summary,
		UpdatedAt:    s.UpdatedAt,
		LastEventAt:  s.LastEventAt,
		HasTmux:      hasTmux,
		TmuxSession:  s.TmuxSessionName(),
		TmuxTarget:   tmuxTarget,
	}
}
