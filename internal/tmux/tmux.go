package tmux

import (
	"fmt"
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

// PaneWidth returns the column width of the given pane.
func PaneWidth(pane string) (int, error) {
	out, err := exec.Command("tmux", paneWidthArgs(pane)...).Output()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

// NavHop moves the navigator pane (navPane, e.g. from $TMUX_PANE) to the left
// of targetWindow, preserving the navigator's current width, then focuses that
// window. targetWindow may be a window or pane target
// (e.g. "sessions-nav:0" or "sess:1.2"). If the navigator is already in the
// target window, join-pane fails harmlessly and the switch-client still runs.
func NavHop(navPane, targetWindow string) error {
	size := "30%"
	if w, err := PaneWidth(navPane); err == nil && w > 0 {
		size = strconv.Itoa(w)
	}
	_ = exec.Command("tmux", joinPaneLeftArgs(navPane, targetWindow, size)...).Run()
	return exec.Command("tmux", switchClientArgs(targetWindow)...).Run()
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
	// Any other value (e.g. "pick", "?") triggers interactive selection.
	return pickClient()
}

// pickClient shows tmux clients and lets the user choose one via fzf.
func pickClient() (string, error) {
	out, err := exec.Command("tmux", "list-clients", "-F", "#{client_tty} #{session_name}").Output()
	if err != nil {
		return "", fmt.Errorf("list-clients failed: %w", err)
	}
	lines := splitNonEmpty(string(out))
	if len(lines) == 0 {
		return "", fmt.Errorf("no tmux clients found")
	}
	if len(lines) == 1 {
		// Only one client — use it directly.
		return strings.Fields(lines[0])[0], nil
	}

	// Use fzf if available, otherwise just pick the first non-current client.
	fzfPath, fzfErr := exec.LookPath("fzf")
	if fzfErr != nil {
		// No fzf — return first client.
		return strings.Fields(lines[0])[0], nil
	}

	input := strings.Join(lines, "\n")
	cmd := exec.Command(fzfPath, "--prompt=target client> ", "--no-info", "--reverse")
	cmd.Stdin = strings.NewReader(input)
	cmd.Stderr = os.Stderr
	selected, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("fzf cancelled")
	}
	fields := strings.Fields(strings.TrimSpace(string(selected)))
	if len(fields) == 0 {
		return "", fmt.Errorf("no client selected")
	}
	return fields[0], nil
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
