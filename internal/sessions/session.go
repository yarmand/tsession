package sessions

import "time"

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
	ID          string
	CWD         string
	Repository  string
	Summary     string
	UpdatedAt   time.Time
	LastEventAt time.Time
	State       State
	TmuxName    string
	TmuxTarget  string
}
