package cmd

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"unicode"

	"github.com/yarma/tsession/internal/config"
	"github.com/yarma/tsession/internal/sessions"
)

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

func remoteFallbackTmuxName(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return fmt.Sprintf("tsession-%x", sum[:6])
}

func remoteResumeCommand(sessionID string) string {
	return "exec copilot --resume=" + shellQuote(sessionID)
}

func remoteSessionShellCommand(s sessions.Session) string {
	if !s.RemoteTmuxAvailable {
		return remoteResumeCommand(s.ID)
	}

	fallback := shellQuote(remoteFallbackTmuxName(s.ID))
	resume := shellQuote(remoteResumeCommand(s.ID))
	createAndAttach := "tmux has-session -t " + fallback +
		" 2>/dev/null || tmux new-session -d -s " + fallback + " " + resume +
		"; exec tmux attach-session -t " + fallback
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
