// Package attachcmd builds the shell command used to attach a browser
// terminal (see internal/webterm) to a session's tmux state.
//
// The core idea: local sessions attach through a *grouped* tmux session
// (`tmux new-session -t <original>`) so the browser gets its own size without
// resizing any native terminal already attached to the real session. Remote
// sessions run the identical script, wrapped in the session's configured SSH
// / Codespaces / devcontainer transport — the same script text works
// unmodified in both cases, since tmux itself (local or remote) is what
// interprets it.
//
// This intentionally does not touch cmd/remote_bridge.go's local tmux
// bridges, which remain the mechanism for the CLI's fzf picker.
package attachcmd

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/yarma/tsession/internal/config"
	"github.com/yarma/tsession/internal/sessions"
	"github.com/yarma/tsession/internal/shellutil"
	"github.com/yarma/tsession/internal/tmux"
)

// WebSessionName returns the deterministic grouped (or, when no tmux target
// exists, standalone) tmux session name used to attach a browser terminal to
// the session identified by (origin, sessionID). origin is "" for local
// sessions. The name is stable across calls so re-selecting the same session
// reuses the same tmux session rather than creating a new one each time.
func WebSessionName(origin, sessionID string) string {
	sum := sha256.Sum256([]byte(origin + "\x00" + sessionID))
	return fmt.Sprintf("%s%x", tmux.WebSessionPrefix, sum[:6])
}

// Build returns the binary and arguments to execute (via os/exec) in order to
// attach a browser terminal to s's tmux state. For a local session
// (s.Origin == "") the returned command runs directly on this host and r is
// ignored. For a remote session, r must be the resolved config.Remote for
// s.Origin; the returned command wraps the same attach script in r's
// interactive transport (ssh -t, gh codespace ssh -t, or docker exec -it).
func Build(s sessions.Session, r config.Remote) (string, []string, error) {
	script, err := buildScript(s)
	if err != nil {
		return "", nil, err
	}
	return wrapInTransport(s, r, script)
}

// BuildKill returns the binary and arguments to execute in order to tear
// down the tmux session Build attached to for s, wrapped in the same
// transport. ok is false when Build's script for s never created a tmux
// session in the first place (the remote-has-no-tmux-at-all case), meaning
// there is nothing to kill — callers should treat that as a no-op rather
// than an error.
func BuildKill(s sessions.Session, r config.Remote) (bin string, args []string, ok bool, err error) {
	if s.Origin != "" && !s.RemoteTmuxAvailable {
		return "", nil, false, nil
	}
	web := WebSessionName(s.Origin, s.ID)
	script := "tmux kill-session -t " + shellutil.Quote(web)
	bin, args, err = wrapInTransport(s, r, script)
	return bin, args, true, err
}

// wrapInTransport wraps script for execution: run directly for a local
// session, or through r's interactive transport for a remote one. See
// Build's doc comment for the ssh/codespace-vs-devcontainer quoting
// distinction this preserves.
func wrapInTransport(s sessions.Session, r config.Remote, script string) (string, []string, error) {
	if s.Origin == "" {
		return "sh", []string{"-c", script}, nil
	}

	switch r.Type {
	case "", "ssh", "codespace":
		bin, args := r.ResumeCommand()
		return bin, append(args, "bash -lc "+shellutil.Quote(script)), nil
	case "devcontainer":
		bin, args := r.ResumeCommand()
		return bin, append(args, "bash", "-lc", script), nil
	default:
		return "", nil, fmt.Errorf("attachcmd: unsupported remote type %q", r.Type)
	}
}

// buildScript builds the tmux script (or, when no tmux is reachable at all,
// the bare resume command) for s, without any remote transport wrapping.
func buildScript(s sessions.Session) (string, error) {
	web := WebSessionName(s.Origin, s.ID)

	if s.Origin == "" {
		if s.TmuxTarget != "" {
			return groupedAttachScript(web, s.TmuxTarget)
		}
		if s.TmuxName != "" {
			return groupedAttachScript(web, s.TmuxName)
		}
		return newSessionAndAttachScript(web, localResumeCommand(s)), nil
	}

	if s.RemoteTmuxAvailable {
		if s.RemoteTmuxTarget != "" {
			return groupedAttachScript(web, s.RemoteTmuxTarget)
		}
		return remoteNoTargetScript(web, s), nil
	}

	// No tmux reachable on the remote host at all: grouping is impossible,
	// so run the resume command directly with no tmux wrapper.
	return remoteDirectResumeCommand(s), nil
}

// groupedAttachScript builds the script that ensures a grouped tmux session
// named web exists (sharing target's windows/panes) and switches it to the
// specific window and pane identified by target when it is in
// "session:window.pane" form (as produced by tmux.Pane.Target()). A
// session-only target is also accepted when discovery found the tmux session
// by working directory but could not resolve the owning process to a pane.
//
// tmux new-session -A is deliberately not used here: it *attaches* when the
// session already exists (ignoring -d), so it cannot serve as an "ensure a
// detached session exists" primitive. has-session/new-session is the
// deterministic form.
func groupedAttachScript(web, target string) (string, error) {
	if target == "" {
		return "", fmt.Errorf("attachcmd: empty tmux target")
	}

	webQ := shellutil.Quote(web)
	if !strings.Contains(target, ":") {
		origQ := shellutil.Quote(target)
		return "tmux has-session -t " + webQ + " 2>/dev/null || tmux new-session -d -s " + webQ + " -t " + origQ + "; " +
			"exec tmux attach-session -t " + webQ, nil
	}

	origSession, windowPane, ok := splitTarget(target)
	if !ok {
		return "", fmt.Errorf("attachcmd: malformed tmux target %q", target)
	}
	window, pane, ok := splitWindowPane(windowPane)
	if !ok {
		return "", fmt.Errorf("attachcmd: malformed tmux target %q", target)
	}

	origQ := shellutil.Quote(origSession)
	winTargetQ := shellutil.Quote(web + ":" + window)
	paneTargetQ := shellutil.Quote(web + ":" + window + "." + pane)

	return "tmux has-session -t " + webQ + " 2>/dev/null || tmux new-session -d -s " + webQ + " -t " + origQ + "; " +
		"tmux select-window -t " + winTargetQ + "; " +
		"tmux select-pane -t " + paneTargetQ + "; " +
		"exec tmux attach-session -t " + webQ, nil
}

// newSessionAndAttachScript builds the script for a session with no existing
// tmux target to group onto: it creates a standalone (ungrouped) tmux
// session running resumeCmd if one doesn't already exist, then attaches.
// This is used for local sessions without a live tmux pane, and mirrors the
// remote fallback below for consistency.
func newSessionAndAttachScript(web, resumeCmd string) string {
	webQ := shellutil.Quote(web)
	resumeQ := shellutil.Quote(resumeCmd)
	return "tmux has-session -t " + webQ + " 2>/dev/null || tmux new-session -d -s " + webQ + " " + resumeQ + "; " +
		"exec tmux attach-session -t " + webQ
}

// localResumeCommand returns the resume command for a local session with no
// live tmux pane, dispatching on Source the same way cmd/resume.go does.
func localResumeCommand(s sessions.Session) string {
	if s.Source == "pi" {
		return "exec pi --session " + shellutil.Quote(s.ID)
	}
	return "exec copilot --resume=" + shellutil.Quote(s.ID)
}

// remoteNoTargetScript builds the script for a remote session where tmux is
// available but the session has no existing target: it resolves the remote
// copilot binary via the user's own login shell (respecting PATH
// customizations, mirroring cmd/remote_bridge.go's remote bridge behavior),
// creates a standalone remote tmux session running it if one doesn't already
// exist, then attaches. Unlike the local no-target case, remote sessions are
// assumed to always be Copilot sessions — the existing CLI remote bridge path
// has no pi-over-SSH handling either.
func remoteNoTargetScript(web string, s sessions.Session) string {
	webQ := shellutil.Quote(web)
	resolver := shellutil.CopilotResolverCommand()
	resumeQ := shellutil.Quote(`exec "$copilot_bin" --resume=` + shellutil.Quote(s.ID))

	return "if ! tmux has-session -t " + webQ + " 2>/dev/null; then " + resolver +
		"tmux new-session -d -s " + webQ +
		` -e TSESSION_COPILOT_BIN="$copilot_bin" ` + resumeQ +
		" || { tmux has-session -t " + webQ + " 2>/dev/null || exit $?; }" +
		"; fi; exec tmux attach-session -t " + webQ
}

// remoteDirectResumeCommand returns the resume command for a remote host
// with no tmux at all: no grouping is possible, so the resume command runs
// directly.
func remoteDirectResumeCommand(s sessions.Session) string {
	return shellutil.CopilotResolverCommand() + `exec "$copilot_bin" --resume=` + shellutil.Quote(s.ID)
}

// splitTarget splits a tmux target of the form "session:window.pane" (as
// produced by tmux.Pane.Target()) into the session name and the
// "window.pane" suffix.
func splitTarget(target string) (session, windowPane string, ok bool) {
	parts := strings.SplitN(target, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// splitWindowPane splits a "window.pane" suffix into its window and pane
// components.
func splitWindowPane(windowPane string) (window, pane string, ok bool) {
	idx := strings.LastIndexByte(windowPane, '.')
	if idx < 0 || idx == 0 || idx == len(windowPane)-1 {
		return "", "", false
	}
	return windowPane[:idx], windowPane[idx+1:], true
}
