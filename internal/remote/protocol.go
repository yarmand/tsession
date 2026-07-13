package remote

import (
	"time"

	"github.com/yarma/tsession/internal/sessions"
)

// ProtocolVersion identifies the wire format used by RPCRequest/RPCResponse.
const ProtocolVersion = 1

// RPCRequest is sent from the local client to the remote daemon, one JSON
// object per line (JSONL) over stdio.
type RPCRequest struct {
	ID     string `json:"id"`
	Method string `json:"method"`
}

// RPCResponse is the daemon's reply to an RPCRequest.
type RPCResponse struct {
	ID              string          `json:"id"`
	ProtocolVersion int             `json:"protocolVersion"`
	OK              bool            `json:"ok"`
	Error           string          `json:"error,omitempty"`
	Payload         SnapshotPayload `json:"payload,omitempty"`
}

// SessionPayload is the wire representation of a single remote session.
type SessionPayload struct {
	ID          string `json:"id"`
	CWD         string `json:"cwd"`
	Repository  string `json:"repository"`
	Summary     string `json:"summary"`
	State       string `json:"state"`
	UpdatedAt   string `json:"updated_at,omitempty"`
	LastEventAt string `json:"last_event_at,omitempty"`
	Source      string `json:"source,omitempty"`
}

// SnapshotPayload is the full-snapshot response payload for the "snapshot"
// RPC method.
type SnapshotPayload struct {
	Sessions []SessionPayload `json:"sessions"`
}

// ToSessions converts a SnapshotPayload into local sessions.Session values,
// tagging each with the given origin (remote name) and dropping any session
// older than maxAge (when maxAge > 0).
func (p SnapshotPayload) ToSessions(origin string, maxAge time.Duration) []sessions.Session {
	cutoff := time.Time{}
	if maxAge > 0 {
		cutoff = time.Now().Add(-maxAge)
	}

	out := make([]sessions.Session, 0, len(p.Sessions))
	for _, sp := range p.Sessions {
		s := sessions.Session{
			ID:          sp.ID,
			CWD:         sp.CWD,
			Repository:  sp.Repository,
			Summary:     sp.Summary,
			State:       parseState(sp.State),
			UpdatedAt:   parseTime(sp.UpdatedAt),
			LastEventAt: parseTime(sp.LastEventAt),
			Source:      sp.Source,
			Origin:      origin,
		}
		if !cutoff.IsZero() && !s.UpdatedAt.IsZero() && s.UpdatedAt.Before(cutoff) {
			continue
		}
		out = append(out, s)
	}
	return out
}

func parseState(s string) sessions.State {
	switch s {
	case "exited":
		return sessions.StateExited
	case "idle":
		return sessions.StateInactiveIdle
	case "active":
		return sessions.StateActiveIdle
	case "done":
		return sessions.StateDone
	case "working":
		return sessions.StateWorking
	case "question":
		return sessions.StateWaiting
	default:
		return sessions.StateUnknown
	}
}

func sessionToPayload(s sessions.Session) SessionPayload {
	sp := SessionPayload{
		ID:         s.ID,
		CWD:        s.CWD,
		Repository: s.Repository,
		Summary:    s.Summary,
		State:      s.State.String(),
		Source:     s.Source,
	}
	if !s.UpdatedAt.IsZero() {
		sp.UpdatedAt = s.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	if !s.LastEventAt.IsZero() {
		sp.LastEventAt = s.LastEventAt.UTC().Format(time.RFC3339Nano)
	}
	return sp
}
