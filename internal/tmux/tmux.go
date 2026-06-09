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
	Title       string // terminal title (set by running app, e.g. copilot session summary)
}

// Target returns the tmux target string for this pane, suitable for
// `tmux switch-client -t` / `tmux attach-session -t`.
func (p Pane) Target() string {
	return p.SessionName + ":" + p.WindowIndex + "." + p.PaneIndex
}

// NavSession is the holding session that owns the navigator pane.
const NavSession = "sessions-nav"

func paneWidthArgs(pane string) []string {
	return []string{"display-message", "-p", "-t", pane, "#{pane_width}"}
}

func joinPaneLeftArgs(src, target, size string) []string {
	args := []string{"join-pane", "-h", "-b"}
	if size != "" {
		args = append(args, "-l", size)
	}
	return append(args, "-s", src, "-t", target)
}

func switchClientArgs(target string) []string {
	return []string{"switch-client", "-t", target}
}

func paneIDArgs(target string) []string {
	return []string{"display-message", "-p", "-t", target, "#{pane_id}"}
}

func selectPaneArgs(target string) []string {
	return []string{"select-pane", "-t", target}
}

// PaneID resolves a tmux target (a pane index target like "work:1.0" or a
// window id like "@2") to a stable pane id (e.g. "%5").
func PaneID(target string) (string, error) {
	out, err := exec.Command("tmux", paneIDArgs(target)...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// PaneWidth returns the column width of the given pane.
func PaneWidth(pane string) (int, error) {
	out, err := exec.Command("tmux", paneWidthArgs(pane)...).Output()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

// NavHop moves the navigator pane (navPane, e.g. from $TMUX_PANE) to the left
// of target, preserving the navigator's current width, then focuses the target
// pane. target may be a pane target (e.g. "work:1.2") or a window id (e.g.
// "@2"); it is resolved to a stable pane id first because join-pane shifts pane
// indices — the navigator becomes index 0, so an index-based target would
// otherwise resolve to the navigator after the move. Focus lands on the target
// (the agent/main pane), not the navigator that join-pane inserts.
//
// A sized join-pane fails when the requested width exceeds the target window
// (e.g. hopping into a narrower window). In that case we retry the join without
// a fixed size so the navigator still docks rather than being left behind.
func NavHop(navPane, target string) error {
	dest := target
	if id, err := PaneID(target); err == nil && id != "" {
		dest = id
	}
	size := "30%"
	if w, err := PaneWidth(navPane); err == nil && w > 0 {
		size = strconv.Itoa(w)
	}
	if exec.Command("tmux", joinPaneLeftArgs(navPane, dest, size)...).Run() != nil {
		_ = exec.Command("tmux", joinPaneLeftArgs(navPane, dest, "")...).Run()
	}
	_ = exec.Command("tmux", selectPaneArgs(dest)...).Run()
	return exec.Command("tmux", switchClientArgs(dest)...).Run()
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

// SwitchClient switches the current tmux client to the named target, or
// attaches if invoked from outside tmux.
func SwitchClient(name string) error {
	if !InTmux() {
		cmd := exec.Command("tmux", "attach-session", "-t", name)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	}
	return exec.Command("tmux", switchClientArgs(name)...).Run()
}

func InTmux() bool { return os.Getenv("TMUX") != "" }

// HasSession reports whether a tmux session with the given name exists.
func HasSession(name string) bool {
	return exec.Command("tmux", "has-session", "-t", name).Run() == nil
}

// FirstPaneID returns the pane id (e.g. "%3") of the first pane in the named
// session, or "" if it can't be determined. Used to operate on panes by id
// instead of assuming a window/pane base index of 0.
func FirstPaneID(session string) string {
	out, err := exec.Command("tmux", "list-panes", "-t", session, "-F", "#{pane_id}").Output()
	if err != nil {
		return ""
	}
	return firstLine(string(out))
}

// FirstWindowID returns the window id (e.g. "@2") of the first window in the
// named session, or "" if it can't be determined. The navigator's home window
// is always the lowest-indexed window of sessions-nav (agents are separate
// windows/sessions), so this is base-index independent.
func FirstWindowID(session string) string {
	out, err := exec.Command("tmux", "list-windows", "-t", session, "-F", "#{window_id}").Output()
	if err != nil {
		return ""
	}
	return firstLine(string(out))
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			return l
		}
	}
	return ""
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
