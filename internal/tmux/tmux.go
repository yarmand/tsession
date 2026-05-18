package tmux

import (
	"os"
	"os/exec"
	"strings"
)

type Session struct {
	Name string
	Path string
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
