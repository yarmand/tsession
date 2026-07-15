package remote

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/yarma/tsession/internal/pisessions"
	"github.com/yarma/tsession/internal/sessions"
	"github.com/yarma/tsession/internal/tmux"
)

// defaultSnapshotMaxAge bounds how far back the daemon looks for sessions
// when assembling a snapshot (mirrors the local `list` default).
const defaultSnapshotMaxAge = 14 * 24 * time.Hour

var (
	tmuxAvailable       = tmux.Available
	listTmuxSessions    = tmux.ListSessions
	listTmuxPanes       = tmux.ListPanes
	loadLocalSessionsFn = loadLocalSessions
)

// Serve reads newline-delimited RPCRequest JSON objects from in, dispatches
// them, and writes newline-delimited RPCResponse JSON objects to out. It
// returns nil on a clean EOF from in.
func Serve(in io.Reader, out io.Writer) error {
	dec := json.NewDecoder(in)
	enc := json.NewEncoder(out)
	for {
		var req RPCRequest
		if err := dec.Decode(&req); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		resp := handleRequest(req)
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
}

// ServeOneShot handles a single RPC method invocation without an
// interactive stdio loop, writing exactly one JSON RPCResponse line to out.
// It backs `tsession remote rpc <method>`, used by the local client to
// request a snapshot (or health check) over a fresh SSH-style exec.
func ServeOneShot(method string, out io.Writer) error {
	resp := handleRequest(RPCRequest{Method: method})
	enc := json.NewEncoder(out)
	if !resp.OK {
		if err := enc.Encode(resp); err != nil {
			return err
		}
		return fmt.Errorf("remote rpc %s: %s", method, resp.Error)
	}
	return enc.Encode(resp)
}

func handleRequest(req RPCRequest) RPCResponse {
	switch req.Method {
	case "health":
		return RPCResponse{ID: req.ID, ProtocolVersion: ProtocolVersion, OK: true}
	case "snapshot":
		payload, err := BuildActiveSnapshot(time.Now().UTC())
		if err != nil {
			return RPCResponse{ID: req.ID, ProtocolVersion: ProtocolVersion, OK: false, Error: err.Error()}
		}
		return RPCResponse{ID: req.ID, ProtocolVersion: ProtocolVersion, OK: true, Payload: payload}
	default:
		return RPCResponse{
			ID:              req.ID,
			ProtocolVersion: ProtocolVersion,
			OK:              false,
			Error:           fmt.Sprintf("unknown method: %s", req.Method),
		}
	}
}

// BuildActiveSnapshot loads the daemon host's local sessions (Copilot + pi)
// and returns only the active ones, per the daemon's active-only filtering
// contract: state != exited && state != unknown && state != inactive-idle.
func BuildActiveSnapshot(now time.Time) (SnapshotPayload, error) {
	all, available, err := loadLocalSessionsFn(defaultSnapshotMaxAge)
	if err != nil {
		return SnapshotPayload{}, err
	}

	payload := SnapshotPayload{
		TmuxAvailable: available,
		Sessions:      make([]SessionPayload, 0, len(all)),
	}
	for _, s := range all {
		if s.State == sessions.StateExited || s.State == sessions.StateUnknown || s.State == sessions.StateInactiveIdle {
			continue
		}
		payload.Sessions = append(payload.Sessions, sessionToPayload(s))
	}
	return payload, nil
}

// loadLocalSessions mirrors cmd.loadAllLive: it loads the daemon host's own
// Copilot + pi sessions. It lives here (rather than being shared with cmd)
// to avoid an import cycle, since cmd imports internal/remote.
func loadLocalSessions(maxAge time.Duration) ([]sessions.Session, bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, false, err
	}
	dbPath := filepath.Join(home, ".copilot", "session-store.db")
	stateRoot := filepath.Join(home, ".copilot", "session-state")

	store, err := sessions.LoadRecent(dbPath, maxAge)
	if err != nil {
		return nil, false, fmt.Errorf("load session store: %w", err)
	}

	knownIDs := make(map[string]bool, len(store))
	for _, s := range store {
		knownIDs[s.ID] = true
	}
	live := sessions.DiscoverLiveSessions(stateRoot, knownIDs)
	store = append(store, live...)

	ids := make([]string, len(store))
	for i, s := range store {
		ids[i] = s.ID
	}
	sd, err := sessions.LoadStateDirsForIDs(stateRoot, ids)
	if err != nil {
		return nil, false, fmt.Errorf("load state dirs: %w", err)
	}
	available := tmuxAvailable()
	var tx []tmux.Session
	var panes []tmux.Pane
	if available {
		tx, err = listTmuxSessions()
		if err != nil {
			return nil, false, fmt.Errorf("list tmux: %w", err)
		}
		panes, err = listTmuxPanes()
		if err != nil {
			return nil, false, fmt.Errorf("list tmux panes: %w", err)
		}
	}
	merged := sessions.Merge(store, sd, tx)
	merged = sessions.ResolveTmuxByPID(merged, sd, panes)

	for i := range merged {
		if merged[i].Source == "" {
			merged[i].Source = "copilot"
		}
	}

	piSessions, piErr := pisessions.LoadAll()
	if piErr == nil && len(piSessions) > 0 {
		cutoff := time.Now().Add(-maxAge)
		var piFiltered []sessions.Session
		for _, ps := range piSessions {
			if !ps.UpdatedAt.IsZero() && ps.UpdatedAt.Before(cutoff) {
				continue
			}
			piFiltered = append(piFiltered, ps)
		}
		piSD := pisessions.StateDirInfos(piFiltered)
		piFiltered = sessions.ResolveTmuxByPID(piFiltered, piSD, panes)
		merged = append(merged, piFiltered...)
	}

	return merged, available, nil
}
