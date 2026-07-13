package cmd

import (
	"flag"
	"fmt"
	"os"

	"github.com/yarma/tsession/internal/remote"
)

// Remote dispatches `tsession remote <subcommand>`.
func Remote(args []string) error {
	fs := flag.NewFlagSet("remote", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: tsession remote <serve>")
	}
	switch fs.Arg(0) {
	case "serve":
		return remote.Serve(os.Stdin, os.Stdout)
	case "rpc":
		if fs.NArg() < 2 {
			return fmt.Errorf("usage: tsession remote rpc <method>")
		}
		return remote.ServeOneShot(fs.Arg(1), os.Stdout)
	default:
		return fmt.Errorf("unknown remote subcommand: %s", fs.Arg(0))
	}
}
