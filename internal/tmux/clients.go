package tmux

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Client struct {
	TTY         string
	SessionName string
}

var listClientsOutput = func() ([]byte, error) {
	return exec.Command("tmux", "list-clients", "-F", "#{client_tty}|#{session_name}").Output()
}

var pickClientOutput = func(input string) ([]byte, error) {
	fzfPath, err := exec.LookPath("fzf")
	if err != nil {
		return nil, fmt.Errorf("fzf not found: %w", err)
	}
	cmd := exec.Command(fzfPath, "--prompt=target client> ", "--no-info", "--reverse")
	cmd.Stdin = strings.NewReader(input)
	cmd.Stderr = os.Stderr
	return cmd.Output()
}

var waitForTargetClient = func() {
	time.Sleep(time.Second)
}

var targetWaitOutput io.Writer = os.Stderr

func ParseClients(raw string) []Client {
	var out []Client
	for _, line := range strings.Split(raw, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "|", 2)
		if len(parts) != 2 {
			continue
		}
		out = append(out, Client{TTY: parts[0], SessionName: parts[1]})
	}
	return out
}

func FilterTargetClients(in []Client, excludedSession string) []Client {
	out := make([]Client, 0, len(in))
	for _, client := range in {
		if client.TTY == "" || client.SessionName == excludedSession {
			continue
		}
		out = append(out, client)
	}
	return out
}

func pickTargetClient() (string, error) {
	var clients []Client
	waiting := false
	for len(clients) == 0 {
		raw, err := listClientsOutput()
		if err != nil {
			return "", fmt.Errorf("list clients: %w", err)
		}
		clients = FilterTargetClients(ParseClients(string(raw)), "session-nav")
		if len(clients) == 0 {
			if !waiting {
				fmt.Fprintln(targetWaitOutput, "Waiting for a non-navigation tmux client; open or attach one to continue...")
				waiting = true
			}
			waitForTargetClient()
		}
	}
	lines := make([]string, 0, len(clients))
	for _, client := range clients {
		lines = append(lines, client.TTY+"|"+client.SessionName)
	}
	selected, err := pickClientOutput(strings.Join(lines, "\n") + "\n")
	if err != nil {
		return "", fmt.Errorf("target client picker: %w", err)
	}
	fields := strings.SplitN(strings.TrimSpace(string(selected)), "|", 2)
	if len(fields) == 0 || fields[0] == "" {
		return "", fmt.Errorf("no target client selected")
	}
	return fields[0], nil
}
