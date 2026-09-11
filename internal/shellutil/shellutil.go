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

// CopilotResolverCommand returns a shell fragment that resolves the absolute
// path to the `copilot` binary using the remote's own interactive login
// shell, so PATH customizations in .bashrc/.zshrc are respected the same way
// they would be for a real interactive session. On success it leaves
// $copilot_bin set to the absolute path; on failure it prints a diagnostic to
// stderr and exits 127. Callers append their own command referencing
// "$copilot_bin".
func CopilotResolverCommand() string {
	probe := "exec /bin/sh -c " + Quote("command -v copilot")
	return `remote_shell=${SHELL:-/bin/sh}; ` +
		`case "${remote_shell##*/}" in csh|tcsh) shell_flags=-ic ;; *) shell_flags=-lic ;; esac; ` +
		`copilot_bin=$("$remote_shell" "$shell_flags" ` + Quote(probe) + ` 2>/dev/null | tail -n 1); ` +
		`case "$copilot_bin" in /*) ;; *) echo 'copilot resolver did not return an absolute path' >&2; exit 127 ;; esac; ` +
		`if [ ! -x "$copilot_bin" ]; then echo 'copilot not found in remote interactive shell PATH' >&2; exit 127; fi; `
}
