package pisessions

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/yarma/tsession/internal/sessions"
)

type stateFile struct {
	ID          string `json:"id"`
	State       string `json:"state"`
	CWD         string `json:"cwd"`
	Summary     string `json:"summary"`
	UpdatedAt   string `json:"updatedAt"`
	PID         int    `json:"pid"`
	SessionFile string `json:"sessionFile"`
}

// LoadAll reads all pi state files from ~/.tsession/pi-state/.
func LoadAll() ([]sessions.Session, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".tsession", "pi-state")
	return loadFromDir(dir)
}

func loadFromDir(dir string) ([]sessions.Session, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var out []sessions.Session
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var sf stateFile
		if err := json.Unmarshal(data, &sf); err != nil {
			continue
		}
		s := sessions.Session{
			ID:      sf.ID,
			CWD:     sf.CWD,
			Summary: sf.Summary,
			Source:  "pi",
		}
		s.UpdatedAt = parseTime(sf.UpdatedAt)
		s.LastEventAt = s.UpdatedAt
		s.State = mapState(sf.State)

		// Stale PID detection: if not exited, check if process alive
		if s.State != sessions.StateExited && sf.PID > 0 {
			if !isProcessAlive(sf.PID) {
				s.State = sessions.StateExited
			}
		}

		out = append(out, s)
	}
	return out, nil
}

// StateDirInfos builds StateDirInfo entries from pi sessions for tmux PID matching.
func StateDirInfos(sess []sessions.Session) []sessions.StateDirInfo {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dir := filepath.Join(home, ".tsession", "pi-state")
	return stateDirInfosFromDir(dir, sess)
}

func stateDirInfosFromDir(dir string, sess []sessions.Session) []sessions.StateDirInfo {
	var out []sessions.StateDirInfo
	for _, s := range sess {
		data, err := os.ReadFile(filepath.Join(dir, s.ID+".json"))
		if err != nil {
			continue
		}
		var sf stateFile
		if err := json.Unmarshal(data, &sf); err != nil {
			continue
		}
		out = append(out, sessions.StateDirInfo{
			ID:  s.ID,
			PID: sf.PID,
		})
	}
	return out
}

func mapState(s string) sessions.State {
	switch s {
	case "working":
		return sessions.StateWorking
	case "question":
		return sessions.StateWaiting
	case "done":
		return sessions.StateDone
	case "idle":
		return sessions.StateActiveIdle
	case "exited":
		return sessions.StateExited
	default:
		return sessions.StateUnknown
	}
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t.UTC()
}

func isProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}
