package sessions

import (
	"path/filepath"
	"sort"
	"time"

	"github.com/yarma/tsession/internal/donestate"
	"github.com/yarma/tsession/internal/tmux"
)

func Merge(store []Session, stateDirs []StateDirInfo, tmuxs []tmux.Session) []Session {
	stateByID := map[string]StateDirInfo{}
	for _, s := range stateDirs {
		stateByID[s.ID] = s
	}
	tmuxByPath := map[string]string{}
	tmuxByBase := map[string]string{}
	for _, t := range tmuxs {
		if t.Path != "" {
			tmuxByPath[t.Path] = t.Name
		}
		tmuxByBase[t.Name] = t.Name
	}

	rt, _ := donestate.Load()
	if rt == nil {
		rt = &donestate.File{Entries: map[string]donestate.Entry{}}
	}
	now := time.Now()
	dirty := false

	out := make([]Session, 0, len(store))
	for _, s := range store {
		if sd, ok := stateByID[s.ID]; ok {
			s.State = sd.State
			s.LastEventAt = sd.LastEventAt
			if sd.CWD != "" {
				s.CWD = sd.CWD
			}
		}
		if name, ok := tmuxByPath[s.CWD]; ok {
			s.TmuxName = name
		} else if name, ok := tmuxByBase[filepath.Base(s.CWD)]; ok && s.CWD != "" {
			s.TmuxName = name
		}

		raw := s.State
		entry := rt.Entries[s.ID]
		prev := entry.LastState
		hadDone := !entry.DoneSince.IsZero()
		switch {
		case raw == StateActiveIdle || raw == StateInactiveIdle:
			if prev == StateWorking.String() {
				entry.DoneSince = now
				hadDone = true
			}
			if hadDone {
				s.State = StateDone
			}
		default:
			// Any other state (working, question, exited, unknown)
			// clears a pending "done" marker.
			if hadDone {
				entry.DoneSince = time.Time{}
				hadDone = false
			}
		}
		if entry.LastState != raw.String() || entry.DoneSince != rt.Entries[s.ID].DoneSince {
			entry.LastState = raw.String()
			rt.Entries[s.ID] = entry
			dirty = true
		}

		out = append(out, s)
	}

	if dirty {
		_ = donestate.Save(rt)
	}

	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if ba, bb := bucket(a), bucket(b); ba != bb {
			return ba < bb
		}
		if pa, pb := statePriority(a.State), statePriority(b.State); pa != pb {
			return pa > pb
		}
		return a.UpdatedAt.After(b.UpdatedAt)
	})
	return out
}

// bucket returns the primary sort group for a session (lower is earlier):
//   0 — has an attached tmux session
//   1 — is "active" (Waiting / Working / ActiveIdle), regardless of tmux
//   2 — inactive idle / unknown
//   3 — exited (always last)
func bucket(s Session) int {
	if s.State == StateExited {
		return 3
	}
	if s.TmuxName != "" {
		return 0
	}
	if isActive(s.State) {
		return 1
	}
	return 2
}

func isActive(s State) bool {
	return s == StateWaiting || s == StateWorking || s == StateActiveIdle || s == StateDone
}

func statePriority(s State) int {
	switch s {
	case StateWaiting:
		return 5
	case StateWorking:
		return 4
	case StateDone:
		return 3
	case StateActiveIdle:
		return 2
	case StateInactiveIdle:
		return 1
	default:
		return 0
	}
}
