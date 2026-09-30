// Package worktree creates git worktrees via a user-configurable script and
// reports the resulting worktree path.
package worktree

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const defaultScript = `#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(git rev-parse --git-common-dir)/.." && pwd)"
wt_folder="${repo_root}.worktrees"
mkdir -p "$wt_folder"
wt_path="$(realpath "$wt_folder")/$1"
git worktree add -b "$USER/$1" "$wt_path"
echo "$wt_path"
`

// configHome returns the tsession config directory. Overridable in tests.
var configHome = func() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "tsession"), nil
}

// ScriptPath returns the path to the worktree-creation script.
func ScriptPath() (string, error) {
	dir, err := configHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "new-worktree.sh"), nil
}

// EnsureScript writes the default script (mode 0755) if it does not already
// exist. An existing script is never overwritten.
func EnsureScript() error {
	path, err := ScriptPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(defaultScript), 0o755)
}

// Create ensures the script exists, runs it with the branch name, and returns
// the worktree path printed as the last non-empty line of stdout. The script's
// stderr is streamed to the user. The script runs in the current working
// directory.
//
// When logw is non-nil, Create logs the script path and the exact command it
// runs, and tees the script's stdout to logw so verbose callers see everything
// the script prints (not only the final path line).
func Create(branch string, logw io.Writer) (string, error) {
	if err := EnsureScript(); err != nil {
		return "", err
	}
	path, err := ScriptPath()
	if err != nil {
		return "", err
	}

	if logw != nil {
		fmt.Fprintf(logw, "+ %s\n", shellJoin("bash", []string{path, branch}))
	}

	var stdout bytes.Buffer
	cmd := exec.Command("bash", path, branch)
	if logw != nil {
		cmd.Stdout = io.MultiWriter(&stdout, logw)
	} else {
		cmd.Stdout = &stdout
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("worktree script failed: %w", err)
	}

	line := lastNonEmptyLine(stdout.String())
	if line == "" {
		return "", fmt.Errorf("worktree script printed no path on stdout")
	}
	return line, nil
}

// shellJoin renders a command and its arguments for human-readable logging,
// single-quoting any argument that contains whitespace or quotes. Display only.
func shellJoin(name string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, name)
	for _, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\n'\"") {
			parts = append(parts, "'"+strings.ReplaceAll(a, "'", `'\''`)+"'")
		} else {
			parts = append(parts, a)
		}
	}
	return strings.Join(parts, " ")
}

func lastNonEmptyLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return ""
}
