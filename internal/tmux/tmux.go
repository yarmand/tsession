package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	Title       string // terminal title (set by running app, e.g. copilot session summary)
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
	return SwitchClientTarget(name, "")
}

// SwitchClientTarget switches the specified tmux client to the given session.
// If clientTarget is empty, it switches the current client (default behavior).
// clientTarget is resolved via ResolveTarget before use.
func SwitchClientTarget(name, clientTarget string) error {
	resolved, err := ResolveTarget(clientTarget)
	if err != nil {
		return err
	}

	if !InTmux() {
		cmd := exec.Command("tmux", "attach-session", "-t", name)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	}

	args := []string{"switch-client", "-t", name}
	if resolved != "" {
		args = append(args, "-c", resolved)
	}
	return exec.Command("tmux", args...).Run()
}

// ResolveTarget resolves a --target value into a tmux client path.
// Returns empty string if target is empty (meaning "current client").
// If target is a "/dev/..." path, it's used directly.
// Any other value triggers an interactive picker from tmux list-clients.
func ResolveTarget(target string) (string, error) {
	if target == "" {
		return "", nil
	}
	if strings.HasPrefix(target, "/dev/") {
		return target, nil
	}
	return pickTargetClient()
}

func splitNonEmpty(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func InTmux() bool { return os.Getenv("TMUX") != "" }

// NewSession creates a detached tmux session named name, with working directory
// path, running command (interpreted by the shell). Use SwitchClientTarget to
// focus it afterward.
func NewSession(name, path, command string) error {
	return exec.Command("tmux", "new-session", "-d", "-s", name, "-c", path, command).Run()
}

// ResolveSessionName decides which tmux session name to use for a new session
// rooted at path, given the current session list. If any existing session is
// already rooted at the same path, it returns that session's name and true,
// signalling the caller should resume it instead of creating a new one. This
// makes re-running `new` on the same worktree reattach rather than spawn
// duplicates, even when the session previously took a suffixed name. Otherwise,
// if the desired name is free it is returned; if the desired name is taken by a
// session at a different path, a unique suffixed name (desired-2, desired-3,
// ...) is returned with false.
func ResolveSessionName(desired, path string, existing []Session) (string, bool) {
	cleanTarget := filepath.Clean(path)
	taken := make(map[string]bool, len(existing))
	for _, s := range existing {
		taken[s.Name] = true
		if filepath.Clean(s.Path) == cleanTarget {
			return s.Name, true
		}
	}
	if !taken[desired] {
		return desired, false
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", desired, i)
		if !taken[candidate] {
			return candidate, false
		}
	}
}

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

// ListPanesWithTitle returns panes including the pane title.
// The title is set by the running application (e.g. copilot sets it to the
// session summary), which propagates through SSH to the local terminal.
func ListPanesWithTitle() ([]Pane, error) {
	cmd := exec.Command("tmux", "list-panes", "-a", "-F", "#{session_name}|#{window_index}|#{pane_index}|#{pane_pid}|#{pane_title}")
	out, err := cmd.Output()
	if err != nil {
		return nil, nil
	}
	return parseListPanesWithTitle(string(out)), nil
}

func parseListPanesWithTitle(s string) []Pane {
	var out []Pane
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 5)
		if len(parts) < 4 {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(parts[3]))
		if err != nil {
			continue
		}
		p := Pane{
			SessionName: parts[0],
			WindowIndex: parts[1],
			PaneIndex:   parts[2],
			PID:         pid,
		}
		if len(parts) == 5 {
			p.Title = parts[4]
		}
		out = append(out, p)
	}
	return out
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
