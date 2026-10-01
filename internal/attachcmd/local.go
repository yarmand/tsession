package attachcmd

import (
	"errors"
	"strconv"

	"github.com/yarma/tsession/internal/shellutil"
)

// LocalTerminalID is the reserved session ID for the web UI's dedicated
// local terminal. It is not a real agent session: no session store ever
// produces this ID, so it can never collide with a Copilot or pi session.
const LocalTerminalID = "__local-terminal__"

// LocalSessionName is the tmux session that hosts the dedicated local
// terminal's shell. It deliberately does NOT use tmux.WebSessionPrefix:
// webterm.ReapOrphanedLocal kills every prefixed session at server startup,
// and this one must outlive `tsession serve` / the GUI so reopening either
// returns to the same shell and scrollback.
const LocalSessionName = "tsession-local"

// LocalWebSessionName returns the ephemeral grouped tmux session the browser
// attaches through. Unlike LocalSessionName, this one is disposable: it is
// reaped at startup and torn down on explicit close.
func LocalWebSessionName() string {
	return WebSessionName("", LocalTerminalID)
}

// BuildLocal returns the command that attaches a browser terminal to the
// dedicated local shell. It first ensures the persistent tmux session exists
// (created in home, running the user's default shell), then attaches through
// a grouped session so the browser's window size never resizes another
// client attached to the same shell.
//
// The `has-session || new-session` pair is not atomic, but a lost race is
// harmless: the duplicate `new-session` fails, the shell continues past it,
// and the grouped attach that follows finds the session the winner created.
func BuildLocal(home string) (string, []string, error) {
	if home == "" {
		return "", nil, errors.New("attachcmd: empty home directory for the local terminal")
	}

	nameQ := shellutil.Quote(LocalSessionName)
	ensure := "tmux has-session -t " + nameQ + " 2>/dev/null || " +
		"tmux new-session -d -s " + nameQ + " -c " + shellutil.Quote(home) + "; "

	grouped, err := groupedAttachScript(LocalWebSessionName(), LocalSessionName)
	if err != nil {
		return "", nil, err
	}
	return "sh", []string{"-c", ensure + grouped}, nil
}

// BuildLocalKill returns the command that tears down only the *grouped*
// session the browser attached through. The persistent LocalSessionName is
// intentionally left running — that is what makes the local terminal
// survive a server restart.
func BuildLocalKill() (string, []string) {
	script := "tmux kill-session -t " + shellutil.Quote(LocalWebSessionName())
	return "sh", []string{"-c", script}
}

// BuildLocalRestore returns a command that finds the tmux client running in
// the local terminal's persistent PTY and switches it back to the grouped
// local session. Commands such as `tsession new` may switch that client to a
// newly-created session; the PTY itself remains alive, so reselecting the
// local terminal must explicitly restore its original target.
func BuildLocalRestore(clientPID int) (string, []string, error) {
	if clientPID <= 0 {
		return "", nil, errors.New("attachcmd: invalid local terminal client PID")
	}

	pid := strconv.Itoa(clientPID)
	target := shellutil.Quote(LocalWebSessionName())
	script := "client=$(tmux list-clients -F '#{client_pid} #{client_tty}' | " +
		"while read -r pid tty; do " +
		"if [ \"$pid\" = " + pid + " ]; then printf '%s\\n' \"$tty\"; break; fi; " +
		"done); " +
		"[ -n \"$client\" ] && exec tmux switch-client -c \"$client\" -t " + target
	return "sh", []string{"-c", script}, nil
}
