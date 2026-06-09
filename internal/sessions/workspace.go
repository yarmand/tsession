package sessions

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// gitRevParse runs `git -C cwd rev-parse <arg>`; swappable in tests.
var gitRevParse = func(cwd, arg string) (string, error) {
	out, err := exec.Command("git", "-C", cwd, "rev-parse", arg).Output()
	return strings.TrimSpace(string(out)), err
}

// workspaceLabels derives (repo, worktree) display labels from a git toplevel
// path and the absolute git-common-dir. The worktree label is the basename of
// the toplevel; the repo label is the basename of the directory containing the
// common .git dir (which is shared by all worktrees of a repo).
func workspaceLabels(toplevel, commonDir string) (repo, worktree string) {
	worktree = filepath.Base(toplevel)
	cd := strings.TrimRight(commonDir, "/")
	repo = filepath.Base(filepath.Dir(cd))
	if repo == "" || repo == "." || repo == string(filepath.Separator) {
		repo = worktree
	}
	return repo, worktree
}

// DeriveWorkspace resolves the repo and worktree labels for a working dir.
// Falls back to the cwd basename when the dir is not inside a git repo.
func DeriveWorkspace(cwd string) (repo, worktree string) {
	top, err := gitRevParse(cwd, "--show-toplevel")
	if err != nil || top == "" {
		base := filepath.Base(cwd)
		return base, base
	}
	common, err := gitRevParse(cwd, "--git-common-dir")
	if err != nil || common == "" {
		base := filepath.Base(top)
		return base, base
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(cwd, common)
	}
	return workspaceLabels(top, common)
}
