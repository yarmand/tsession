package cmd

import (
	"flag"
	"time"
)

func Popup(args []string) error {
	fs := flag.NewFlagSet("popup", flag.ExitOnError)
	maxAge := fs.Duration("max-age", 14*24*time.Hour, "ignore sessions older than this")
	active := fs.Bool("active", false, "only show sessions attached to tmux with a known, non-exited state")
	short := fs.Bool("short", false, "compact output: state, age, repo basename, summary truncated to 30 chars")
	lshort := fs.Int("lshort", 0, "like --short, but also truncate each output line to N characters")
	localOnly := fs.Bool("local-only", false, "only show local sessions")
	_ = fs.Parse(args)

	id, err := runFzf(*maxAge, "", true, *active, *short, *lshort, *localOnly, "", false)
	if err != nil {
		return err
	}
	if id == "" {
		return nil
	}
	return Resume([]string{id})
}
