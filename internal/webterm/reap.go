package webterm

import "github.com/yarma/tsession/internal/tmux"

// ReapOrphanedLocal kills any local tsession-web-* tmux sessions left behind
// by a previous, uncleanly-terminated `tsession serve` process. It should be
// called once at server startup, before any Registry.Attach calls: at that
// point no grouped session can legitimately still be in use, since the
// current process's Registry starts out empty. Remote-host orphans are not
// handled here — they are harmless, since attachcmd's deterministic naming
// means the next attach for that (origin, session) simply reuses the
// existing tmux session instead of creating a duplicate.
func ReapOrphanedLocal() error {
	names, err := tmux.ListWebSessionNames()
	if err != nil {
		return err
	}
	var firstErr error
	for _, name := range names {
		if err := tmux.KillSession(name); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
