// Package shellutil provides small shell-quoting and remote-copilot-resolution
// helpers shared by the CLI remote bridge (cmd/remote_bridge.go) and the web
// terminal's attach-command builder (internal/attachcmd). Keeping this logic
// in one place means both paths quote and resolve remote binaries identically
// rather than maintaining two copies that could drift.
package shellutil

import "strings"

// Quote wraps s in single quotes, escaping any embedded single quotes, so
// that it is safe to embed in shell commands built as plain strings.
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Join quotes each argument with Quote and joins them with spaces, producing
// a single shell command line safe to hand to `sh -c`.
func Join(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = Quote(arg)
	}
	return strings.Join(quoted, " ")
}

// InteractiveLoginCommand returns a POSIX-shell fragment that runs command
// after initializing the remote user's configured shell as an interactive
// login shell. The command itself runs under /bin/sh with the initialized
// environment, so callers can use portable shell syntax regardless of whether
// the user's shell is zsh, bash, fish, or csh.
func InteractiveLoginCommand(command string) string {
	inner := "exec /bin/sh -c " + Quote(command)
	return `remote_shell=${SHELL:-/bin/sh}; ` +
		`case "${remote_shell##*/}" in csh|tcsh) shell_flags=-ic ;; *) shell_flags=-lic ;; esac; ` +
		`exec "$remote_shell" "$shell_flags" ` + Quote(inner)
}

// CopilotResolverCommand returns a shell fragment that resolves the absolute
// path to the `copilot` binary using the remote's own interactive login
// shell, so PATH customizations in .bashrc/.zshrc are respected the same way
// they would be for a real interactive session. On success it leaves
// $copilot_bin set to the absolute path; on failure it prints a diagnostic to
// stderr and exits 127. Callers append their own command referencing
// "$copilot_bin".
func CopilotResolverCommand() string {
	return `copilot_bin=$(command -v copilot 2>/dev/null | tail -n 1); ` +
		`case "$copilot_bin" in /*) ;; *) echo 'copilot resolver did not return an absolute path' >&2; exit 127 ;; esac; ` +
		`if [ ! -x "$copilot_bin" ]; then echo 'copilot not found in remote interactive shell PATH' >&2; exit 127; fi; `
}
