package cmd

import (
	"fmt"
	"strings"
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

// needsShellQuoting reports whether s contains characters that require shell
// quoting (spaces, tabs, or shell metacharacters).
func needsShellQuoting(s string) bool {
	return strings.ContainsAny(s, " \t'\"\\$`!#&*()[]{};<>|~")
}

// buildCopilotCommand builds the shell command run inside the tmux session.
// Arguments that contain special characters are shell-quoted; safe arguments
// are appended as-is.
func buildCopilotCommand(extra []string) string {
	cmd := "copilot"
	for _, a := range extra {
		if needsShellQuoting(a) {
			cmd += " " + shellQuote(a)
		} else {
			cmd += " " + a
		}
	}
	return cmd
}
