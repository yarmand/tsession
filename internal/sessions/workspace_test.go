package sessions

import "testing"

func TestWorkspaceLabels_MainWorktree(t *testing.T) {
	repo, wt := workspaceLabels("/home/u/myrepo", "/home/u/myrepo/.git")
	if repo != "myrepo" || wt != "myrepo" {
		t.Fatalf("got (%q, %q), want (myrepo, myrepo)", repo, wt)
	}
}

func TestWorkspaceLabels_LinkedWorktree(t *testing.T) {
	// A linked worktree's git-common-dir points back at the main repo's .git.
	repo, wt := workspaceLabels("/home/u/myrepo-feature", "/home/u/myrepo/.git")
	if repo != "myrepo" || wt != "myrepo-feature" {
		t.Fatalf("got (%q, %q), want (myrepo, myrepo-feature)", repo, wt)
	}
}

func TestWorkspaceLabels_TrailingSlash(t *testing.T) {
	repo, wt := workspaceLabels("/home/u/myrepo", "/home/u/myrepo/.git/")
	if repo != "myrepo" || wt != "myrepo" {
		t.Fatalf("got (%q, %q), want (myrepo, myrepo)", repo, wt)
	}
}

func TestDeriveWorkspace_RelativeCommonDirFromSubdir(t *testing.T) {
	oldGitRevParse := gitRevParse
	t.Cleanup(func() {
		gitRevParse = oldGitRevParse
	})

	gitRevParse = func(cwd, arg string) (string, error) {
		switch arg {
		case "--show-toplevel":
			return "/home/u/myrepo", nil
		case "--git-common-dir":
			return "../../.git", nil
		default:
			t.Fatalf("unexpected rev-parse arg %q for cwd %q", arg, cwd)
			return "", nil
		}
	}

	repo, wt := DeriveWorkspace("/home/u/myrepo/internal/sessions")
	if repo != "myrepo" || wt != "myrepo" {
		t.Fatalf("got (%q, %q), want (myrepo, myrepo)", repo, wt)
	}
}
