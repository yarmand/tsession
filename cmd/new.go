package cmd

import (
	"fmt"
)

// splitDashDash splits args at the first literal "--". Everything before is the
// command's own args; everything after is forwarded to copilot. When there is
// no "--", after is nil.
func splitDashDash(args []string) (before, after []string) {
	for i, a := range args {
		if a == "--" {
			return args[:i], args[i+1:]
		}
	}
	return args, nil
}

// validateNewArgs enforces that exactly one of branch / path is provided.
func validateNewArgs(branch, path string) error {
	switch {
	case branch == "" && path == "":
		return fmt.Errorf("usage: tsession new <branch> | --path <dir>")
	case branch != "" && path != "":
		return fmt.Errorf("provide either a branch or --path, not both")
	default:
		return nil
	}
}

// buildCopilotCommand builds the shell command run inside the tmux session.
// Every forwarded argument is shell-quoted so embedded spaces or metacharacters
// are passed to copilot intact.
func buildCopilotCommand(extra []string) string {
	cmd := "copilot"
	for _, a := range extra {
		cmd += " " + shellQuote(a)
	}
	return cmd
}
