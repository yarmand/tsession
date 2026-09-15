package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/yarma/tsession/internal/names"
	"github.com/yarma/tsession/internal/notify"
	"github.com/yarma/tsession/internal/reponames"
	"github.com/yarma/tsession/internal/tmux"
)

// tmuxRenameSessionFn is a seam for tests to stub out the real tmux
// invocation; production code always uses tmux.RenameSession.
var tmuxRenameSessionFn = tmux.RenameSession

// renameSessionRequest is the body of POST /api/sessions/{id}/name. An
// empty (or absent) Name clears the stored name, same as `tsession rename
// <id> ""`.
type renameSessionRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleRenameSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}

	var req renameSessionRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	if err := names.Set(id, req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Best-effort: also rename the live tmux session, mirroring
	// `tsession rename`'s behavior, so the native tmux client's status bar
	// picks up the new name too. A lookup miss (session has no live tmux
	// pane, or the sessions provider errors) is not fatal to the rename.
	if req.Name != "" {
		if all, err := s.sessionsFn(); err == nil {
			for _, sess := range all {
				if sess.ID == id && sess.TmuxName != "" {
					_ = tmuxRenameSessionFn(sess.TmuxName, req.Name)
					break
				}
			}
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// repoAliasRequest is the body of POST /api/repos/alias. Repository
// identifies the repo the same way `tsession rename-repo` does (a git
// remote URL or other repository identity string); an empty Alias clears
// the stored alias.
type repoAliasRequest struct {
	Repository string `json:"repository"`
	Alias      string `json:"alias"`
}

func (s *Server) handleRepoAlias(w http.ResponseWriter, r *http.Request) {
	var req repoAliasRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Repository == "" {
		http.Error(w, "repository is required", http.StatusBadRequest)
		return
	}

	if err := reponames.Set(req.Repository, req.Alias); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleEvents serves GET /api/events as a Server-Sent Events stream: every
// pollInterval it re-fetches the session list, diffs it against this
// server's own notify snapshot (see notify.DiffWithStore), and forwards any
// newly-entered done/question transitions to the client as "notify" events.
// It never invokes the desktop (osascript) notifier — the browser renders
// its own Notification from the event payload.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	if s.notifyStorePath == "" {
		http.Error(w, "notification store path unavailable", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	interval := s.pollInterval
	if interval <= 0 {
		interval = 3 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.emitEvents(w, flusher)
		}
	}
}

func (s *Server) emitEvents(w http.ResponseWriter, flusher http.Flusher) {
	all, err := s.sessionsFn()
	if err != nil {
		return
	}
	events, err := notify.DiffWithStore(all, s.notifyStorePath)
	if err != nil || len(events) == 0 {
		return
	}
	for _, e := range events {
		data, err := json.Marshal(e)
		if err != nil {
			continue
		}
		fmt.Fprintf(w, "event: notify\ndata: %s\n\n", data)
	}
	flusher.Flush()
}
