// Package worktree creates git worktrees via a user-configurable script and
// reports the resulting worktree path.
package worktree

import (
	"os"
	"path/filepath"
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
