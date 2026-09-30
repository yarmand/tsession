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
// working directory are all fixed by the server.
func (s *Server) handleLocalTerminal(w http.ResponseWriter, r *http.Request) {
	if s.registry == nil {
		http.Error(w, "terminal support not configured", http.StatusNotImplemented)
		return
	}

	key := webterm.Key{Origin: "", ID: attachcmd.LocalTerminalID}

	term, ok := s.registry.Live(key)
	if !ok {
		home, err := s.homeDirFn()
		if err != nil {
			s.failLocalTerminal(w, "cannot determine the home directory", err)
			return
		}
		bin, args, err := attachcmd.BuildLocal(home)
		if err != nil {
			s.failLocalTerminal(w, "cannot build the local terminal command", err)
			return
		}

		teardown := func() error {
			killBin, killArgs := attachcmd.BuildLocalKill()
			// Best-effort: the grouped session may already be gone. The
			// persistent shell session is deliberately left running.
			_ = exec.Command(killBin, killArgs...).Run()
			return nil
		}

		term, err = s.registry.Attach(key, webterm.Spec{
			Bin: bin, Args: args, Rows: 24, Cols: 80, Teardown: teardown,
		})
		if err != nil {
			s.failLocalTerminal(w, "cannot start the local terminal", err)
			return
		}
	}

	s.bridgeTerminal(w, r, term)
}

// failLocalTerminal reports a startup failure to the browser (which shows it
// in the terminal pane's banner) and to the serve log, so `grep level=error`
// finds it alongside every other failure.
func (s *Server) failLocalTerminal(w http.ResponseWriter, msg string, err error) {
	s.logInteraction("local-terminal-failed", attachcmd.LocalTerminalID, "", "Local terminal", msg+": "+err.Error(), "error")
	http.Error(w, msg+": "+err.Error(), http.StatusInternalServerError)
}
