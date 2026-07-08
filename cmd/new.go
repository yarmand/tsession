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

// validateNewArgs rejects providing both a branch and a path. Providing
// neither is allowed: the caller defaults to the current working directory.
func validateNewArgs(branch, path string) error {
	if branch != "" && path != "" {
		return fmt.Errorf("provide either a branch or --path, not both")
	}
	return nil
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

// parseNewArgs parses the pre-`--` args for `new`, returning the branch and the
// resolved path. When neither a branch nor a path is given, path defaults to the
// current working directory ("."). Both -p and --path set the path.
func parseNewArgs(before []string) (branch, path string, err error) {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	fs.StringVar(&path, "path", "", "use an existing worktree at this directory instead of creating one")
	fs.StringVar(&path, "p", "", "shorthand for --path")
	if err = fs.Parse(before); err != nil {
		return "", "", err
	}
	branch = fs.Arg(0)

	if err = validateNewArgs(branch, path); err != nil {
		return "", "", err
	}
	if branch == "" && path == "" {
		path = "."
	}
	return branch, path, nil
}

// New implements `tsession new`: create (or reuse) a git worktree, open a tmux
// session in it, and start copilot there.
//
//	tsession new <branch> [-- <copilot-args>...]
//	tsession new [-p|--path <dir>] [-- <copilot-args>...]
//
// With no branch and no path, the current working directory is used.
func New(args []string) error {
	before, copilotArgs := splitDashDash(args)

	branch, path, err := parseNewArgs(before)
	if err != nil {
		return err
	}

	wtPath, err := resolveWorktreePath(branch, path)
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
