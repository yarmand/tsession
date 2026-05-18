// main.go
package main

import (
	"fmt"
	"os"

	"github.com/yarma/tsession/cmd"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	sub := os.Args[1]
	args := os.Args[2:]
	var err error
	switch sub {
	case "list":
		err = cmd.List(args)
	case "browse":
		err = cmd.Browse(args)
	case "popup":
		err = cmd.Popup(args)
	case "resume":
		err = cmd.Resume(args)
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", sub)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Println(`tsession — manage Copilot CLI sessions with tmux

Usage:
  tsession list                List recent sessions (text)
  tsession browse [query]      fzf picker (no popup)
  tsession popup               fzf picker designed for tmux popup
  tsession resume <session-id> Switch tmux to session, or copilot --resume
  tsession -h                  Show this help`)
}
