package cmd

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/yarma/tsession/internal/config"
	"github.com/yarma/tsession/internal/sessions"
	"github.com/yarma/tsession/internal/tmux"
)

var ensureBridgeFn = tmux.EnsureBridge
var switchClientTargetFn = tmux.SwitchClientTarget

func remoteBridgeName(origin, sessionID string) string {
	var b strings.Builder
	lastHyphen := false
	for _, r := range origin {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			lastHyphen = false
		case b.Len() > 0 && !lastHyphen:
			b.WriteByte('-')
			lastHyphen = true
		}
	}
	clean := strings.Trim(b.String(), "-")
	if clean == "" {
		clean = "remote"
	}
	if len(clean) > 20 {
		clean = strings.TrimRight(clean[:20], "-")
	}
	sum := sha256.Sum256([]byte(origin + "\x00" + sessionID))
	return fmt.Sprintf("tsession-r-%s-%x", clean, sum[:6])
}

func remoteBridgeAlternateName(origin, sessionID string) string {
	sum := sha256.Sum256([]byte(origin + "\x00" + sessionID))
	return fmt.Sprintf("%s-%x", remoteBridgeName(origin, sessionID), sum[6:10])
}

func remoteFallbackTmuxName(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return fmt.Sprintf("tsession-%x", sum[:6])
}

func remoteCopilotResolverCommand() string {
	probe := "exec /bin/sh -c " + shellQuote("command -v copilot")
	return `remote_shell=${SHELL:-/bin/sh}; ` +
		`case "${remote_shell##*/}" in csh|tcsh) shell_flags=-ic ;; *) shell_flags=-lic ;; esac; ` +
		`copilot_bin=$("$remote_shell" "$shell_flags" ` + shellQuote(probe) + ` 2>/dev/null | tail -n 1); ` +
		`case "$copilot_bin" in /*) ;; *) echo 'copilot resolver did not return an absolute path' >&2; exit 127 ;; esac; ` +
		`if [ ! -x "$copilot_bin" ]; then echo 'copilot not found in remote interactive shell PATH' >&2; exit 127; fi; `
}

func remoteResumeCommand(sessionID string) string {
	return remoteCopilotResolverCommand() +
		`exec "$copilot_bin" --resume=` + shellQuote(sessionID)
}

func remoteTmuxResumeCommand(sessionID string) string {
	return `exec "$TSESSION_COPILOT_BIN" --resume=` + shellQuote(sessionID)
}

func remoteSessionShellCommand(s sessions.Session) string {
	if !s.RemoteTmuxAvailable {
		return remoteResumeCommand(s.ID)
	}

	fallback := shellQuote(remoteFallbackTmuxName(s.ID))
	resume := shellQuote(remoteTmuxResumeCommand(s.ID))
	createAndAttach := "if ! tmux has-session -t " + fallback +
		" 2>/dev/null; then " + remoteCopilotResolverCommand() +
		"tmux new-session -d -s " + fallback +
		` -e TSESSION_COPILOT_BIN="$copilot_bin" ` + resume +
		" || { tmux has-session -t " + fallback + " 2>/dev/null || exit $?; }" +
		"; fi; exec tmux attach-session -t " + fallback
	if s.RemoteTmuxTarget == "" {
		return createAndAttach
	}

	target := shellQuote(s.RemoteTmuxTarget)
	paneDead := `"$(tmux display-message -p -t ` + target + " " + shellQuote("#{pane_dead}") + ` 2>/dev/null)"`
	return "if [ " + paneDead + " = 0 ]; then tmux attach-session -t " + target + " && exit 0" +
		"; fi; " + createAndAttach
}

func remoteBridgeCommand(s sessions.Session, r config.Remote) (string, []string, error) {
	remoteCommand := remoteSessionShellCommand(s)
	switch r.Type {
	case "", "ssh", "codespace":
		bin, args := r.ResumeCommand()
		return bin, append(args, "bash -lc "+shellQuote(remoteCommand)), nil
	case "devcontainer":
		bin, args := r.ResumeCommand()
		return bin, append(args, "bash", "-lc", remoteCommand), nil
	default:
		return "", nil, fmt.Errorf("unsupported remote type %q", r.Type)
	}
}

func ensureRemoteBridge(s sessions.Session, r config.Remote) (string, error) {
	bin, args, err := remoteBridgeCommand(s, r)
	if err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve bridge home: %w", err)
	}
	spec := tmux.BridgeSpec{
		Name:      remoteBridgeName(s.Origin, s.ID),
		Path:      home,
		Command:   shellJoin(append([]string{bin}, args...)),
		Origin:    s.Origin,
		SessionID: s.ID,
	}
	bridge, err := ensureBridgeFn(spec)
	var collision *tmux.BridgeCollisionError
	if !errors.As(err, &collision) {
		return bridge, err
	}
	spec.Name = remoteBridgeAlternateName(s.Origin, s.ID)
	return ensureBridgeFn(spec)
}

func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellQuote(arg)
	}
	return strings.Join(quoted, " ")
}
