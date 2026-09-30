package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/yarma/tsession/internal/config"
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

// buildAgentCommand builds the shell command run inside the tmux session. base
// is the agent-start command (from --cmd, config's agent_command, or the
// default) and is inserted verbatim so multi-word/flagged commands like
// "agency copilot --hub" work. Every forwarded argument (after `--`) is
// shell-quoted so embedded spaces or metacharacters reach the agent intact.
func buildAgentCommand(base string, extra []string) string {
	cmd := base
	for _, a := range extra {
		cmd += " " + shellQuote(a)
	}
	return cmd
}

// parseNewArgs parses the pre-`--` args for `new`, returning the branch and the
// resolved path. When neither a branch nor a path is given, path defaults to the
// current working directory ("."). Both -p and --path set the path.
func parseNewArgs(before []string) (branch, path, cmd string, verbose bool, err error) {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	fs.StringVar(&path, "path", "", "use an existing worktree at this directory instead of creating one")
	fs.StringVar(&path, "p", "", "shorthand for --path")
	fs.StringVar(&cmd, "cmd", "", "command to start the agent in the session (overrides config agent_command)")
	fs.BoolVar(&verbose, "verbose", false, "print every step, command, and tool stdout/stderr")
	fs.BoolVar(&verbose, "v", false, "shorthand for --verbose")
	if err = fs.Parse(before); err != nil {
		return "", "", "", false, err
	}
	branch = fs.Arg(0)

	if err = validateNewArgs(branch, path); err != nil {
		return "", "", "", false, err
	}
	if branch == "" && path == "" {
		path = "."
	}
	return branch, path, cmd, verbose, nil
}

// New implements `tsession new`: create (or reuse) a git worktree, open a tmux
// session in it, and start the configured agent there.
//
//	tsession new [-v] [--cmd <command>] <branch> [-- <agent-args>...]
//	tsession new [-v] [--cmd <command>] [-p|--path <dir>] [-- <agent-args>...]
//
// With no branch and no path, the current working directory is used. The agent
// command started in the session is, in order of precedence: --cmd, the
// `agent_command` config value, then config.DefaultAgentCommand. Pass -v (or
// --verbose) to print every step, the exact commands run, and the
// stdout/stderr of the tools tsession invokes.
func New(args []string) error {
	before, copilotArgs := splitDashDash(args)

	branch, path, cmdOverride, verbose, err := parseNewArgs(before)
	if err != nil {
		return err
	}

	var logw io.Writer
	if verbose {
		logw = os.Stderr
	}
	vlogf(logw, "new: branch=%q path=%q agent-args=%v\n", branch, path, copilotArgs)

	agentBase := cmdOverride
	agentSource := "--cmd flag"
	if agentBase == "" {
		cfg, cerr := loadConfig()
		if cerr != nil {
			vlogf(logw, "new: warning: load config: %v\n", cerr)
		}
		if cfg != nil && strings.TrimSpace(cfg.AgentCommand) != "" {
			agentBase = cfg.AgentCommand
			agentSource = "config agent_command"
		} else {
			agentBase = config.DefaultAgentCommand
			agentSource = "default"
		}
	}
	vlogf(logw, "new: agent command %q (source: %s)\n", agentBase, agentSource)

	wtPath, err := resolveWorktreePath(branch, path, logw)
	if err != nil {
		return err
	}
	vlogf(logw, "new: worktree path resolved to %q\n", wtPath)

	name := filepath.Base(wtPath)
	sess, err := tmux.ListSessions()
	if err != nil {
		vlogf(logw, "new: warning: list tmux sessions: %v\n", err)
	}
	vlogf(logw, "new: %d existing tmux session(s)\n", len(sess))
	resolved, resume := tmux.ResolveSessionName(name, wtPath, sess)
	vlogf(logw, "new: session name=%q resume=%t\n", resolved, resume)

	if !resume {
		agentCmd := buildAgentCommand(agentBase, copilotArgs)
		vlogf(logw, "new: starting command in session: %s\n", agentCmd)
		if err := tmux.NewSessionVerbose(resolved, wtPath, agentCmd, logw); err != nil {
			return fmt.Errorf("create tmux session: %w", err)
		}
		vlogf(logw, "new: note: the started command runs inside tmux; its own output is not captured here. Attach to session %q to see it.\n", resolved)
	} else {
		vlogf(logw, "new: reusing existing session %q; not starting a new command\n", resolved)
	}

	vlogf(logw, "new: switching client to session %q\n", resolved)
	return tmux.SwitchClientTargetVerbose(resolved, "", logw)
}

// vlogf writes a formatted verbose line to w when w is non-nil.
func vlogf(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	fmt.Fprintf(w, format, args...)
}

// resolveWorktreePath returns the worktree directory: either the validated
// existing --path, or a freshly created worktree for branch.
func resolveWorktreePath(branch, path string, logw io.Writer) (string, error) {
	if path != "" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		vlogf(logw, "new: using existing worktree at %q\n", abs)
		info, err := os.Stat(abs)
		if err != nil {
			return "", fmt.Errorf("--path %q: %w", path, err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("--path %q is not a directory", path)
		}
		return abs, nil
	}
	vlogf(logw, "new: creating worktree for branch %q\n", branch)
	return worktree.Create(branch, logw)
}
