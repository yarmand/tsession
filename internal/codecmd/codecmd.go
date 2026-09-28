// Package codecmd builds the command used to launch a `code serve-web`
// instance for a session's working directory (see internal/codeserver,
// which owns the actual process). It is deliberately pure: no process I/O,
// no filesystem access, just command/script construction — mirroring
// internal/attachcmd's split between "what command to run" and "how to run
// it".
//
// A local session runs `code serve-web` directly. A remote session runs the
// identical flags wrapped in the remote's configured SSH / Codespaces /
// devcontainer transport (see shellutil.WrapRemoteTransport), resolving the
// `code` binary via the remote's own interactive login shell unless an
// explicit override is configured (see config.Remote.CodeBinary).
package codecmd

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/yarma/tsession/internal/config"
	"github.com/yarma/tsession/internal/sessions"
	"github.com/yarma/tsession/internal/shellutil"
)

// Key returns a stable, opaque identifier for the (origin, sessionID) pair,
// suitable for use as a URL path segment (e.g. the VS Code server's
// --server-base-path and the reverse-proxy route matching it). It is stable
// across calls so re-selecting the same session's code view reuses the same
// running instance rather than starting a new one.
func Key(origin, sessionID string) string {
	sum := sha256.Sum256([]byte(origin + "\x00" + sessionID))
	return fmt.Sprintf("%x", sum[:6])
}

// LocalCodeBinary returns the `code` binary to invoke for a local session:
// cfg's top-level code_command override if set, otherwise the bare "code"
// (resolved from PATH by the caller's exec.Command).
func LocalCodeBinary(cfg *config.Config) string {
	if cfg != nil && cfg.CodeCommand != "" {
		return cfg.CodeCommand
	}
	return "code"
}

// Build returns the binary and arguments to execute (via os/exec, wrapped in
// a PTY by the caller so a remote pseudo-terminal is allocated where
// needed) in order to launch `code serve-web` for session s, serving under
// basePath (e.g. "/api/code/<key>") and persisting its own state under
// dataDir. For a local session (s.Origin == "") the returned command runs
// directly on this host and r is ignored. For a remote session, r must be
// the resolved config.Remote for s.Origin.
func Build(s sessions.Session, r config.Remote, cfg *config.Config, basePath, dataDir string) (string, []string, error) {
	if s.Origin == "" {
		return LocalCodeBinary(cfg), serveWebArgs(basePath, s.CWD, dataDir), nil
	}

	// dataDir is a path on the host running tsession. A remote has its own
	// filesystem and user, so sending it verbatim makes VS Code try to
	// create a directory that cannot exist there (e.g. /Users/... on Linux).
	// That fails the extension host outright, which breaks everything
	// depending on extensions — GitHub sign-in included. Give the remote a
	// path under its own $HOME instead, still scoped by session key.
	args := serveWebArgs(basePath, s.CWD, "")
	script := launchScript(r, cfg, args, RemoteDataDir(Key(s.Origin, s.ID)))
	return shellutil.WrapRemoteTransport(r, script)
}

// RemoteDataDir returns the --server-data-dir for a code server running on a
// remote host, as an unexpanded shell expression rooted at that host's own
// $HOME. It mirrors the local layout (~/.tsession/codeserver/<key>) so each
// session keeps separate VS Code state on either side.
func RemoteDataDir(key string) string {
	return `"$HOME/.tsession/codeserver/` + key + `"`
}

// serveWebArgs returns the `code serve-web` arguments shared by local and
// remote launches. --host 127.0.0.1 keeps the instance loopback-only on
// whichever host it runs on (matching tsession serve's own loopback-only
// posture); --port 0 lets the OS pick a free port, discovered afterward by
// scanning the process's stdout for VS Code's "Web UI available at" line.
//
// An empty dataDir omits --server-data-dir: remote launches append it in the
// script itself so it can reference the remote's own $HOME unquoted.
func serveWebArgs(basePath, cwd, dataDir string) []string {
	args := []string{
		"serve-web",
		"--host", "127.0.0.1",
		"--port", "0",
		"--without-connection-token",
		"--accept-server-license-terms",
		"--server-base-path", basePath,
	}
	if dataDir != "" {
		args = append(args, "--server-data-dir", dataDir)
	}
	if cwd != "" {
		args = append(args, "--default-folder", cwd)
	}
	return args
}

// launchScript builds the shell script (without remote-transport wrapping)
// that resolves the `code` binary — via r's explicit override if configured,
// otherwise via the remote's own interactive login shell PATH — and execs it
// with args plus a --server-data-dir of dataDir, which is inserted unquoted
// so the remote shell expands it. The directory is created first: VS Code
// aborts rather than creating missing parents.
func launchScript(r config.Remote, cfg *config.Config, args []string, dataDir string) string {
	tail := " " + shellutil.Join(args) + " --server-data-dir " + dataDir
	mkdir := "mkdir -p " + dataDir + "; "
	if bin := r.CodeBinary(cfg); bin != "" {
		return mkdir + "exec " + shellutil.Quote(bin) + tail
	}
	return shellutil.CodeResolverCommand() + mkdir + `exec "$code_bin"` + tail
}

// TunnelCommand returns the binary and arguments to run a persistent,
// non-interactive port forward from localPort on this host to remotePort as
// seen from r's side (127.0.0.1:remotePort, since code serve-web always
// binds --host 127.0.0.1 on the remote host). ok is false when r's
// transport type has no port-forwarding mechanism of this kind: devcontainer
// (docker exec has no equivalent to ssh -L; see internal/codeserver's
// devcontainer dialer for its stdio-relay alternative instead).
func TunnelCommand(r config.Remote, localPort, remotePort int) (bin string, args []string, ok bool, err error) {
	local := fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", localPort, remotePort)

	switch r.Type {
	case "", "ssh":
		sshCmd := r.SSHCommand
		if sshCmd == "" {
			sshCmd = "ssh"
		}
		parts := strings.Fields(sshCmd)
		fwdArgs := append([]string{}, parts[1:]...)
		fwdArgs = append(fwdArgs, "-N", "-L", local)
		if parts[0] == "ssh" {
			fwdArgs = append(fwdArgs, "-o", "BatchMode=yes")
		}
		if r.Host != "" {
			fwdArgs = append(fwdArgs, r.Host)
		}
		return parts[0], fwdArgs, true, nil
	case "codespace":
		return "gh", []string{
			"codespace", "ports", "forward",
			fmt.Sprintf("%d:%d", remotePort, localPort),
			"--codespace", r.Codespace,
		}, true, nil
	case "devcontainer":
		return "", nil, false, nil
	default:
		return "", nil, false, fmt.Errorf("codecmd: unsupported remote type %q", r.Type)
	}
}
