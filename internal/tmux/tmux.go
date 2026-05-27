package tmux

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type Session struct {
	Name string
	Path string
}

type Pane struct {
	SessionName string
	WindowIndex string
	PaneIndex   string
	PID         int
}

// Target returns the tmux target string for this pane, suitable for
// `tmux switch-client -t` / `tmux attach-session -t`.
func (p Pane) Target() string {
	return p.SessionName + ":" + p.WindowIndex + "." + p.PaneIndex
}

func ListSessions() ([]Session, error) {
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}|#{session_path}")
	out, err := cmd.Output()
	if err != nil {
		return nil, nil
	}
	return parseListSessions(string(out)), nil
}

func parseListSessions(s string) []Session {
	var out []Session
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		out = append(out, Session{Name: parts[0], Path: parts[1]})
	}
	return out
}

func SwitchClient(name string) error {
	if InTmux() {
		return exec.Command("tmux", "switch-client", "-t", name).Run()
	}
	cmd := exec.Command("tmux", "attach-session", "-t", name)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func InTmux() bool { return os.Getenv("TMUX") != "" }

// RenameSession renames a tmux session from oldName to newName.
func RenameSession(oldName, newName string) error {
	return exec.Command("tmux", "rename-session", "-t", oldName, newName).Run()
}

// ListPanes returns one entry per tmux pane across all sessions, with the
// pane's foreground/root PID. Used to map a Copilot CLI process to the tmux
// session that contains it (by walking the process tree up from the
// Copilot PID until an ancestor matches a pane PID).
func ListPanes() ([]Pane, error) {
	cmd := exec.Command("tmux", "list-panes", "-a", "-F", "#{session_name}|#{window_index}|#{pane_index}|#{pane_pid}")
	out, err := cmd.Output()
	if err != nil {
		return nil, nil
	}
	return parseListPanes(string(out)), nil
}

func parseListPanes(s string) []Pane {
	var out []Pane
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) != 4 {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(parts[3]))
		if err != nil {
			continue
		}
		out = append(out, Pane{
			SessionName: parts[0],
			WindowIndex: parts[1],
			PaneIndex:   parts[2],
			PID:         pid,
		})
	}
	return out
}
