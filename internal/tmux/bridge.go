package tmux

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type BridgeSpec struct {
	Name      string
	Path      string
	Command   string
	Origin    string
	SessionID string
}

type BridgeCollisionError struct {
	Name string
}

func (e *BridgeCollisionError) Error() string {
	return "bridge name belongs to another session: " + e.Name
}

var errTmuxSessionMissing = errors.New("tmux session missing")

var runTmux = func(args ...string) ([]byte, error) {
	out, err := exec.Command("tmux", args...).CombinedOutput()
	if err != nil && len(args) > 0 && args[0] == "has-session" {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && tmuxSessionMissing(out) {
			return out, errTmuxSessionMissing
		}
	}
	return out, err
}

func tmuxSessionMissing(out []byte) bool {
	message := strings.ToLower(string(out))
	return strings.Contains(message, "can't find session") ||
		strings.Contains(message, "no server running") ||
		strings.Contains(message, "failed to connect to server") ||
		(strings.Contains(message, "error connecting to") &&
			strings.Contains(message, "no such file or directory"))
}

func EnsureBridge(spec BridgeSpec) (string, error) {
	_, err := runTmux("has-session", "-t", spec.Name)
	switch {
	case errors.Is(err, errTmuxSessionMissing):
		return createBridge(spec)
	case err != nil:
		return "", fmt.Errorf("inspect bridge %s: %w", spec.Name, err)
	}

	origin, err := bridgeOption(spec.Name, "@tsession-origin")
	if err != nil {
		return "", err
	}
	sessionID, err := bridgeOption(spec.Name, "@tsession-session-id")
	if err != nil {
		return "", err
	}
	if origin != spec.Origin || sessionID != spec.SessionID {
		return "", &BridgeCollisionError{Name: spec.Name}
	}

	out, err := runTmux("display-message", "-p", "-t", spec.Name, "#{pane_dead}")
	if err != nil {
		return "", tmuxBridgeError("inspect bridge pane "+spec.Name, out, err)
	}
	switch strings.TrimSpace(string(out)) {
	case "0":
		return spec.Name, nil
	case "1":
		if out, err := runTmux("respawn-pane", "-k", "-c", spec.Path, "-t", spec.Name, spec.Command); err != nil {
			return "", tmuxBridgeError("reconnect bridge "+spec.Name, out, err)
		}
		return spec.Name, nil
	default:
		return "", fmt.Errorf("inspect bridge pane %s: unexpected pane state %q", spec.Name, strings.TrimSpace(string(out)))
	}
}

func createBridge(spec BridgeSpec) (string, error) {
	if out, err := runTmux("new-session", "-d", "-s", spec.Name, "-c", spec.Path); err != nil {
		return "", tmuxBridgeError("create bridge "+spec.Name, out, err)
	}

	configure := [][]string{
		{"set-option", "-w", "-t", spec.Name, "remain-on-exit", "on"},
		{"set-option", "-t", spec.Name, "@tsession-origin", spec.Origin},
		{"set-option", "-t", spec.Name, "@tsession-session-id", spec.SessionID},
	}
	for _, args := range configure {
		if out, err := runTmux(args...); err != nil {
			_, _ = runTmux("kill-session", "-t", spec.Name)
			return "", tmuxBridgeError("configure bridge "+spec.Name, out, err)
		}
	}
	if out, err := runTmux("respawn-pane", "-k", "-c", spec.Path, "-t", spec.Name, spec.Command); err != nil {
		return "", tmuxBridgeError("start bridge "+spec.Name, out, err)
	}
	return spec.Name, nil
}

func bridgeOption(name, option string) (string, error) {
	out, err := runTmux("show-option", "-qv", "-t", name, option)
	if err != nil {
		return "", tmuxBridgeError("read bridge option "+option, out, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func tmuxBridgeError(action string, out []byte, err error) error {
	message := strings.TrimSpace(string(out))
	if message == "" {
		return fmt.Errorf("%s: %w", action, err)
	}
	return fmt.Errorf("%s: %w: %s", action, err, message)
}
