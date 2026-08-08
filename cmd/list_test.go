package cmd

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yarma/tsession/internal/config"
	"github.com/yarma/tsession/internal/remote"
	"github.com/yarma/tsession/internal/reponames"
	"github.com/yarma/tsession/internal/sessions"
)

func captureListOutput(t *testing.T, args []string) (string, error) {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = oldStdout })

	runErr := List(args)
	if err := w.Close(); err != nil {
		t.Fatalf("stdout close error = %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll(stdout) error = %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("stdout reader close error = %v", err)
	}
	os.Stdout = oldStdout

	return string(data), runErr
}

func TestListShort_UsesRepositoryAliasesAcrossLocalAndRemoteSections(t *testing.T) {
	writeConfigFile(t, `remotes:
  - name: devbox
    host: devbox.example.com
`)
	if err := reponames.Set("https://github.com/example/repository-aliases", "team-repo"); err != nil {
		t.Fatalf("reponames.Set() error = %v", err)
	}

	oldLoadAllLive := loadAllLiveFn
	oldFetch := fetchRemoteSessions
	t.Cleanup(func() {
		loadAllLiveFn = oldLoadAllLive
		fetchRemoteSessions = oldFetch
	})

	loadAllLiveFn = func(time.Duration) ([]sessions.Session, error) {
		return []sessions.Session{{
			ID:         "local",
			CWD:        filepath.Join("/worktrees", "feat-local"),
			Repository: "git@github.com:example/repository-aliases.git",
			Summary:    "local summary",
			UpdatedAt:  time.Now().UTC(),
			State:      sessions.StateWorking,
		}}, nil
	}
	fetchRemoteSessions = func(ctx context.Context, remotes []config.Remote, maxAge, timeout time.Duration, opts remote.FetchOptions) (map[string][]sessions.Session, []string) {
		return map[string][]sessions.Session{
			"devbox": {{
				ID:         "remote",
				Origin:     "devbox",
				CWD:        filepath.Join("/remote", "feat-remote"),
				Repository: "https://github.com/example/repository-aliases.git",
				Summary:    "remote summary",
				UpdatedAt:  time.Now().UTC(),
				State:      sessions.StateActiveIdle,
			}},
		}, nil
	}

	got, err := captureListOutput(t, []string{"--short", "--no-color", "--no-cache"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	for _, want := range []string{
		"── Local ",
		"── devbox ",
		"[team-repo]feat-local",
		"[team-repo]feat-remote",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("List() output missing %q:\n%s", want, got)
		}
	}
}

func TestListShort_PropagatesRepositoryAliasLoadError(t *testing.T) {
	homeRoot := t.TempDir()
	badHome := filepath.Join(homeRoot, "home")
	if err := os.WriteFile(badHome, []byte("not-a-directory"), 0o644); err != nil {
		t.Fatalf("WriteFile(badHome) error = %v", err)
	}
	t.Setenv("HOME", badHome)

	oldLoadAllLive := loadAllLiveFn
	t.Cleanup(func() { loadAllLiveFn = oldLoadAllLive })
	loadAllLiveFn = func(time.Duration) ([]sessions.Session, error) {
		return []sessions.Session{{
			ID:         "local",
			CWD:        filepath.Join("/worktrees", "feat-local"),
			Repository: "https://github.com/example/repository-aliases.git",
			Summary:    "local summary",
			UpdatedAt:  time.Now().UTC(),
			State:      sessions.StateWorking,
		}}, nil
	}

	_, err := captureListOutput(t, []string{"--short", "--local-only", "--no-color", "--no-cache"})
	if err == nil {
		t.Fatal("List() error = nil, want repository alias load error")
	}
}
