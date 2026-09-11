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
	"net/http"
	"time"

	"github.com/yarma/tsession/internal/render"
	"github.com/yarma/tsession/internal/sessions"
)

// SessionsProvider returns the current merged (local + remote) session
// list, unfiltered — the handler applies the same "active" filter the CLI's
// `--active` flag uses (sessions.FilterActive) before rendering it as JSON.
type SessionsProvider func() ([]sessions.Session, error)

// AliasesProvider returns the repository alias map (see internal/reponames)
// used to label sessions the same way `--short` rendering does.
type AliasesProvider func() (map[string]string, error)

// Server holds the dependencies for the web UI's HTTP handlers.
type Server struct {
	sessionsFn SessionsProvider
	aliasesFn  AliasesProvider
	now        func() time.Time
}

// NewServer builds a Server. aliasesFn may be nil, in which case repository
// aliasing is disabled (labels fall back to the plain origin short name).
func NewServer(sessionsFn SessionsProvider, aliasesFn AliasesProvider) *Server {
	if aliasesFn == nil {
		aliasesFn = func() (map[string]string, error) { return nil, nil }
	}
	return &Server{sessionsFn: sessionsFn, aliasesFn: aliasesFn, now: time.Now}
}

// Handler returns an http.Handler serving this Server's API routes, mounted
// at their final paths (e.g. "/api/sessions"). Callers combine it with
// static asset handlers as needed.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions", s.handleSessions)
	return mux
}

// SessionView is the JSON shape of one session in the /api/sessions
// response. Fields mirror what --short rendering shows, plus the raw
// timestamps so the browser can compute and continuously update its own
// "age" display rather than freezing the age at fetch time.
type SessionView struct {
	ID          string    `json:"id"`
	Origin      string    `json:"origin"`
	Source      string    `json:"source"`
	State       string    `json:"state"`
	Name        string    `json:"name"`
	Repository  string    `json:"repository"`
	CWD         string    `json:"cwd"`
	Summary     string    `json:"summary"`
	UpdatedAt   time.Time `json:"updatedAt"`
	LastEventAt time.Time `json:"lastEventAt"`
	HasTmux     bool      `json:"hasTmux"`
}

// SessionsResponse is the top-level JSON payload of GET /api/sessions.
type SessionsResponse struct {
	Sessions []SessionView `json:"sessions"`
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	all, err := s.sessionsFn()
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
	repo := ctx.RepositoryLabel(s)
	if repo == "" {
		repo = render.WorktreeName(s)
	}

	hasTmux := s.Origin == "" && s.TmuxTarget != "" || s.Origin != "" && s.RemoteTmuxAvailable

	return SessionView{
		ID:          s.ID,
		Origin:      s.Origin,
		Source:      s.Source,
		State:       s.State.String(),
		Name:        s.Name,
		Repository:  repo,
		CWD:         s.CWD,
		Summary:     s.Summary,
		UpdatedAt:   s.UpdatedAt,
		LastEventAt: s.LastEventAt,
		HasTmux:     hasTmux,
	}
}
