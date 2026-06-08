package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/yarma/tsession/internal/names"
	"github.com/yarma/tsession/internal/sessions"
	"github.com/yarma/tsession/internal/tmux"
)

func Rename(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: tsession rename <session-id> [name]")
	}
	id := args[0]

	var name string
	if len(args) >= 2 {
		name = strings.Join(args[1:], " ")
	} else {
		// Show session context so the user knows what they're renaming
		displaySessionContext(id)

		current := names.Get(id)
		if current != "" {
			fmt.Printf("Current name: %s\n", current)
		}
		fmt.Println("New name (empty to clear):")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			name = strings.TrimSpace(scanner.Text())
		}
	}

	if err := names.Set(id, name); err != nil {
		return err
	}

	// Rename the corresponding tmux session if one exists
	if name != "" {
		if match := findSession(id); match != nil && match.TmuxName != "" {
			_ = tmux.RenameSession(match.TmuxName, name)
		}
	}

	if name == "" {
		fmt.Println("Name cleared.")
	} else {
		fmt.Printf("Renamed to: %s\n", name)
	}
	return nil
}

// displaySessionContext prints repo/cwd and summary for the session being renamed.
func displaySessionContext(id string) {
	match := findSession(id)
	if match == nil {
		return
	}
	loc := match.Repository
	if loc == "" {
		loc = match.CWD
	}
	if loc != "" {
		fmt.Printf("Session: %s\n", loc)
	}
	if match.Summary != "" {
		summary := match.Summary
		if len(summary) > 60 {
			summary = summary[:59] + "…"
		}
		fmt.Printf("Summary: %s\n", summary)
	}
}

func findSession(id string) *sessions.Session {
	merged, err := loadAll(14*24*time.Hour, false)
	if err != nil {
		return nil
	}
	for i := range merged {
		if merged[i].ID == id {
			return &merged[i]
		}
	}
	return nil
}
