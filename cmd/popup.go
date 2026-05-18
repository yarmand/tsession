package cmd

import (
	"flag"
	"fmt"
	"os"
	"time"
)

func Popup(args []string) error {
	fs := flag.NewFlagSet("popup", flag.ExitOnError)
	maxAge := fs.Duration("max-age", 14*24*time.Hour, "ignore sessions older than this")
	_ = fs.Parse(args)

	if err := EnsureWatcherRunning(true); err != nil {
		fmt.Fprintln(os.Stderr, "warning: auto-start watcher failed:", err)
	}

	id, err := runFzf(*maxAge, "", true)
	if err != nil {
		return err
	}
	if id == "" {
		return nil
	}
	return Resume([]string{id})
}
