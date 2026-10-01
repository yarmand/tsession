package webui

import (
	"net/http"
	"os/exec"

	"github.com/yarma/tsession/internal/attachcmd"
	"github.com/yarma/tsession/internal/webterm"
)

// handleLocalTerminal serves GET /api/localterm: a dedicated shell that is
// not an agent session at all. It exists so the web UI and the GUI can run
// commands like `tsession new` without switching to another application.
//
// Two things distinguish it from handleTerminal. First, it never consults
// the session list — there is no session to look up — so it works even when
// no agent session exists or the remote-backed session load is failing.
// Second, its teardown removes only the ephemeral grouped tmux session; the
// shell itself lives in attachcmd.LocalSessionName, which tsession never
// kills, so reopening `serve` or the GUI returns to the same shell and
// scrollback.
//
// The request carries no parameters: the command, the tmux target, and the
// working directory are all fixed by the server. An activate control frame
// switches this PTY's exact tmux client back to its grouped local session in
// case a command such as `tsession new` moved it elsewhere.
func (s *Server) handleLocalTerminal(w http.ResponseWriter, r *http.Request) {
	if s.registry == nil {
		http.Error(w, "terminal support not configured", http.StatusNotImplemented)
		return
	}

	key := webterm.Key{Origin: "", ID: attachcmd.LocalTerminalID}
	term, err := s.ensureLocalTerminal(key)
	if err != nil {
		s.failLocalTerminal(w, err.msg, err.err)
		return
	}

	s.bridgeTerminal(w, r, term, func() {
		if err := s.localRestoreFn(term.ProcessPID()); err != nil {
			s.logInteraction("local-terminal-restore-failed", attachcmd.LocalTerminalID, "", "Local terminal", err.Error(), "error")
		}
	})
}

type localTerminalError struct {
	msg string
	err error
}

func restoreLocalTerminal(clientPID int) error {
	bin, args, err := attachcmd.BuildLocalRestore(clientPID)
	if err != nil {
		return err
	}
	return exec.Command(bin, args...).Run()
}

func (e *localTerminalError) Error() string { return e.msg + ": " + e.err.Error() }

func (s *Server) ensureLocalTerminal(key webterm.Key) (*webterm.Terminal, *localTerminalError) {
	s.localTermMu.Lock()
	defer s.localTermMu.Unlock()

	if term, ok := s.registry.Live(key); ok {
		exited, _ := term.Exited()
		if !exited {
			return term, nil
		}
		if err := s.registry.Close(key); err != nil {
			return nil, &localTerminalError{msg: "cannot reset the exited local terminal", err: err}
		}
	}

	home, err := s.homeDirFn()
	if err != nil {
		return nil, &localTerminalError{msg: "cannot determine the home directory", err: err}
	}
	bin, args, err := attachcmd.BuildLocal(home)
	if err != nil {
		return nil, &localTerminalError{msg: "cannot build the local terminal command", err: err}
	}

	teardown := func() error {
		killBin, killArgs := attachcmd.BuildLocalKill()
		// Best-effort: the grouped session may already be gone. The
		// persistent shell session is deliberately left running.
		_ = exec.Command(killBin, killArgs...).Run()
		return nil
	}

	term, err := s.registry.Attach(key, webterm.Spec{
		Bin: bin, Args: args, Rows: 24, Cols: 80, Teardown: teardown,
	})
	if err != nil {
		return nil, &localTerminalError{msg: "cannot start the local terminal", err: err}
	}
	return term, nil
}

// failLocalTerminal reports a startup failure to the browser (which shows it
// in the terminal pane's banner) and to the serve log, so `grep level=error`
// finds it alongside every other failure.
func (s *Server) failLocalTerminal(w http.ResponseWriter, msg string, err error) {
	s.logInteraction("local-terminal-failed", attachcmd.LocalTerminalID, "", "Local terminal", msg+": "+err.Error(), "error")
	http.Error(w, msg+": "+err.Error(), http.StatusInternalServerError)
}
