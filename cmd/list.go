package cmd

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yarma/tsession/internal/render"
	"github.com/yarma/tsession/internal/sessions"
	"github.com/yarma/tsession/internal/tmux"
)

func List(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	maxAge := fs.Duration("max-age", 14*24*time.Hour, "ignore sessions older than this")
	noColor := fs.Bool("no-color", false, "disable ANSI colors")
	fzfMode := fs.Bool("fzf", false, "emit tab-delimited lines for fzf consumption")
	_ = fs.Parse(args)

	merged, err := loadAll(*maxAge)
	if err != nil {
		return err
	}

	color := !*noColor && !*fzfMode
	now := time.Now()

	if !*fzfMode {
		if color {
			fmt.Fprintln(os.Stdout, "\x1b[1;34m"+render.Header+"\x1b[0m")
		} else {
			fmt.Fprintln(os.Stdout, render.Header)
		}
	}
	for _, s := range merged {
		fmt.Fprintln(os.Stdout, render.FormatLine(s, now, color))
	}
	return nil
}

func loadAll(maxAge time.Duration) ([]sessions.Session, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(home, ".copilot", "session-store.db")
	stateRoot := filepath.Join(home, ".copilot", "session-state")

	store, err := sessions.LoadRecent(dbPath, maxAge)
	if err != nil {
		return nil, fmt.Errorf("load session store: %w", err)
	}
	sd, err := sessions.LoadAllStateDirs(stateRoot)
	if err != nil {
		return nil, fmt.Errorf("load state dirs: %w", err)
	}
	tx, err := tmux.ListSessions()
	if err != nil {
		return nil, fmt.Errorf("list tmux: %w", err)
	}
	return sessions.Merge(store, sd, tx), nil
}
