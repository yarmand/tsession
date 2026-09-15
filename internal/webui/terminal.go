package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"

	"github.com/coder/websocket"

	"github.com/yarma/tsession/internal/attachcmd"
	"github.com/yarma/tsession/internal/config"
	"github.com/yarma/tsession/internal/sessions"
	"github.com/yarma/tsession/internal/webterm"
)

// localOriginSegment is the {origin} path segment used for local sessions,
// since an empty path segment is not a valid, unambiguous route match.
const localOriginSegment = "local"

// terminalControlMessage is a JSON control frame sent by the client over a
// WebSocket text message. Client keystrokes are sent as binary messages
// instead (see handleTerminal), so this type only ever carries out-of-band
// instructions like resize.
type terminalControlMessage struct {
	Type string `json:"type"`
	Rows uint16 `json:"rows"`
	Cols uint16 `json:"cols"`
}

// handleTerminal serves GET /api/terminal/{origin}/{id}: it resolves the
// session, builds (or reuses) its persistent PTY via webterm, and streams
// PTY output to the client as binary WebSocket messages while accepting
// client keystrokes (binary messages) and resize instructions (JSON text
// messages) in return. The PTY itself outlives this single WebSocket
// connection — closing the browser tab merely unsubscribes it.
func (s *Server) handleTerminal(w http.ResponseWriter, r *http.Request) {
	if s.registry == nil {
		http.Error(w, "terminal support not configured", http.StatusNotImplemented)
		return
	}

	originParam := r.PathValue("origin")
	id := r.PathValue("id")
	origin := originParam
	if origin == localOriginSegment {
		origin = ""
	}

	target, err := s.findSession(origin, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if target == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	var remote config.Remote
	if origin != "" {
		remote, err = s.resolveRemote(origin)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	bin, args, err := attachcmd.Build(*target, remote)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sess := *target
	rem := remote
	teardown := func() error {
		killBin, killArgs, ok, err := attachcmd.BuildKill(sess, rem)
		if err != nil || !ok {
			return nil
		}
		// Best-effort: the grouped/standalone web tmux session may already
		// be gone (e.g. the user killed it directly), so a non-zero exit
		// here is expected and not reported as an error.
		_ = exec.Command(killBin, killArgs...).Run()
		return nil
	}

	key := webterm.Key{Origin: origin, ID: id}
	term, err := s.registry.Attach(key, webterm.Spec{Bin: bin, Args: args, Rows: 24, Cols: 80, Teardown: teardown})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		// Accept has already written an error response.
		return
	}
	defer conn.CloseNow()

	ctx := context.Background()
	unsubscribe, err := term.Subscribe(&wsBinaryWriter{ctx: ctx, conn: conn})
	if err != nil {
		return
	}
	defer unsubscribe()

	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		switch typ {
		case websocket.MessageBinary:
			_, _ = term.Write(data)
		case websocket.MessageText:
			var msg terminalControlMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			if msg.Type == "resize" && msg.Rows > 0 && msg.Cols > 0 {
				_ = term.Resize(msg.Rows, msg.Cols)
			}
		}
	}
}

// findSession returns the session matching (origin, id) in the current
// session list, or nil if none matches.
func (s *Server) findSession(origin, id string) (*sessions.Session, error) {
	all, err := s.sessionsFn()
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Origin == origin && all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, nil
}

// resolveRemote looks up origin's config.Remote via the injected
// RemoteResolver (see WithRemotes).
func (s *Server) resolveRemote(origin string) (config.Remote, error) {
	if s.remoteFn == nil {
		return config.Remote{}, errUnknownRemote(origin)
	}
	r, ok, err := s.remoteFn(origin)
	if err != nil {
		return config.Remote{}, err
	}
	if !ok {
		return config.Remote{}, errUnknownRemote(origin)
	}
	return r, nil
}

type errUnknownRemote string

func (e errUnknownRemote) Error() string { return "unknown remote: " + string(e) }

// wsBinaryWriter adapts a *websocket.Conn to the io.Writer Terminal.Subscribe
// expects, sending each Write as one binary WebSocket message.
type wsBinaryWriter struct {
	ctx  context.Context
	conn *websocket.Conn
}

func (w *wsBinaryWriter) Write(p []byte) (int, error) {
	if err := w.conn.Write(w.ctx, websocket.MessageBinary, p); err != nil {
		return 0, err
	}
	return len(p), nil
}
