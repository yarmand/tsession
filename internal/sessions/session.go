package sessions

import (
	"strings"
	"time"
)

type State int

const (
	StateUnknown State = iota
	StateExited
	StateInactiveIdle
	StateActiveIdle
	StateDone
	StateWorking
	StateWaiting
)

func (s State) String() string {
	switch s {
	case StateExited:
		return "exited"
	case StateInactiveIdle:
		return "idle"
	case StateActiveIdle:
		return "active"
	case StateDone:
		return "done"
	case StateWorking:
		return "working"
	case StateWaiting:
		return "question"
	default:
		return "unknown"
	}
}

type Session struct {
	ID                  string
	CWD                 string
	Repository          string
	Summary             string
	UpdatedAt           time.Time
	LastEventAt         time.Time
	State               State
	TmuxName            string
	TmuxTarget          string
	RemoteTmuxTarget    string
	RemoteTmuxAvailable bool
	Name                string // user-defined display name (from ~/.tsession/names.json)
	Source              string // "copilot" or "pi"
	Origin              string // "" = local, otherwise remote name from config
	RemoteHost          string // configured SSH/codespace/container endpoint
}

// TmuxSessionName returns the discovered tmux session name, stripping any
// exact window/pane suffix from the target.
func (s Session) TmuxSessionName() string {
	target := s.TmuxTarget
	if s.Origin != "" {
		target = s.RemoteTmuxTarget
	}
	if target == "" {
		if s.Origin == "" {
			return s.TmuxName
		}
		return ""
	}
	if name, _, ok := strings.Cut(target, ":"); ok {
		return name
	}
	return target
}
