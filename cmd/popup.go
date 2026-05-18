package cmd

import (
	"flag"
	"time"
)

func Popup(args []string) error {
	fs := flag.NewFlagSet("popup", flag.ExitOnError)
	maxAge := fs.Duration("max-age", 14*24*time.Hour, "ignore sessions older than this")
	_ = fs.Parse(args)

	id, err := runFzf(*maxAge, "", true)
	if err != nil {
		return err
	}
	if id == "" {
		return nil
	}
	return Resume([]string{id})
}
