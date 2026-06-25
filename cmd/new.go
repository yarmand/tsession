package cmd

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yarma/tsession/internal/tmux"
	"github.com/yarma/tsession/internal/worktree"
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

// New implements `tsession new`: create (or reuse) a git worktree, open a tmux
// session in it, and start copilot there.
//
//	tsession new <branch> [-- <copilot-args>...]
//	tsession new --path <dir> [-- <copilot-args>...]
func New(args []string) error {
	before, copilotArgs := splitDashDash(args)

	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	path := fs.String("path", "", "use an existing worktree at this directory instead of creating one")
	if err := fs.Parse(before); err != nil {
		return err
	}
	branch := fs.Arg(0)

	if err := validateNewArgs(branch, *path); err != nil {
		return err
	}

	wtPath, err := resolveWorktreePath(branch, *path)
	if err != nil {
		return err
	}

	name := filepath.Base(wtPath)
	sess, _ := tmux.ListSessions()
	resolved, resume := tmux.ResolveSessionName(name, wtPath, sess)

	if !resume {
		if err := tmux.NewSession(resolved, wtPath, buildCopilotCommand(copilotArgs)); err != nil {
			return fmt.Errorf("create tmux session: %w", err)
		}
	}

	return tmux.SwitchClientTarget(resolved, "")
}

// resolveWorktreePath returns the worktree directory: either the validated
// existing --path, or a freshly created worktree for branch.
func resolveWorktreePath(branch, path string) (string, error) {
	if path != "" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(abs)
		if err != nil {
			return "", fmt.Errorf("--path %q: %w", path, err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("--path %q is not a directory", path)
		}
		return abs, nil
	}
	return worktree.Create(branch)
}
