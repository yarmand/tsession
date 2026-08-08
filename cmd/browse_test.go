package cmd

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yarma/tsession/internal/config"
	"github.com/yarma/tsession/internal/remote"
	"github.com/yarma/tsession/internal/reponames"
	"github.com/yarma/tsession/internal/sessions"
)

func TestShouldResumeAfterFzf_InsideTmuxUsesBindingOnly(t *testing.T) {
	if shouldResumeAfterFzf(true, "session-id") {
		t.Fatal("inside-tmux browse would resume a second time after fzf")
	}
}

func TestShouldResumeAfterFzf_OutsideTmuxAcceptsSelection(t *testing.T) {
	if !shouldResumeAfterFzf(false, "session-id") {
		t.Fatal("outside-tmux caller should own resume after accepting")
	}
}

func TestLaunchInTmuxRespawnsExistingNavigatorWithCurrentArgs(t *testing.T) {
	t.Setenv("HOME", "/Users/me")
	oldRun := runTmuxCommand
	t.Cleanup(func() { runTmuxCommand = oldRun })

	var calls [][]string
	var interactive []bool
	runTmuxCommand = func(args []string, attach bool) error {
		calls = append(calls, append([]string(nil), args...))
		interactive = append(interactive, attach)
		return nil
	}

	if err := launchInTmux([]string{"--target", "pick"}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 {
		t.Fatalf("calls = %v, want has-session, respawn-pane, attach-session", calls)
	}
	if want := []string{"has-session", "-t", "session-nav"}; !reflect.DeepEqual(calls[0], want) {
		t.Fatalf("has-session = %v, want %v", calls[0], want)
	}
	if got := calls[1]; len(got) != 7 ||
		!reflect.DeepEqual(got[:6], []string{"respawn-pane", "-k", "-t", "session-nav", "-c", "/Users/me"}) ||
		!strings.Contains(got[6], "browse '--target' 'pick'") {
		t.Fatalf("respawn = %v", got)
	}
	if want := []string{"attach-session", "-t", "session-nav"}; !reflect.DeepEqual(calls[2], want) {
		t.Fatalf("attach = %v, want %v", calls[2], want)
	}
	if !reflect.DeepEqual(interactive, []bool{false, false, true}) {
		t.Fatalf("interactive calls = %v", interactive)
	}
}

func TestEnterBindingRoutesTargetAndDisplaysResumeErrors(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux/default,1,0")
	got := enterBinding("/tmp/tsession", "/dev/ttys001")
	for _, want := range []string{
		"execute-silent(",
		"'/tmp/tsession' resume",
		"--target='/dev/ttys001'",
		"tmux display-message",
		"tsession: $err",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("binding %q does not contain %q", got, want)
		}
	}
}

func TestRunFzfOptsBindsDistinctSessionAndRepositoryRenameShortcuts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TMUX", "/tmp/tmux/default,1,0")

	oldLoadAllLive := loadAllLiveFn
	t.Cleanup(func() { loadAllLiveFn = oldLoadAllLive })
	loadAllLiveFn = func(time.Duration) ([]sessions.Session, error) {
		return nil, nil
	}

	binDir := t.TempDir()
	fzfPath := filepath.Join(binDir, "fzf")
	script := "#!/bin/sh\nprintf '%s\n' \"$@\"\n"
	if err := os.WriteFile(fzfPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(fzf) error = %v", err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	got, err := runFzfOpts(14*24*time.Hour, "", false, false, false, 0, true, false, "", false)
	if err != nil {
		t.Fatalf("runFzfOpts() error = %v", err)
	}

	for _, want := range []string{
		"--bind=ctrl-n:execute-silent(tmux display-popup -E -w 99% -h 5 ",
		" rename {2})+reload(",
		"--bind=ctrl-N:execute-silent(tmux display-popup -E -w 99% -h 5 ",
		" rename-repo {2})+reload(",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("fzf argv %q does not contain %q", got, want)
		}
	}
}

func TestInitialListBytes_UsesRepositoryAliasesAcrossLocalAndRemoteSections(t *testing.T) {
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

	got, err := initialListBytes(24*time.Hour, false, true, 0, false)
	if err != nil {
		t.Fatalf("initialListBytes() error = %v", err)
	}
	for _, want := range []string{
		"[team-repo]feat-local",
		"[team-repo]feat-remote",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("initialListBytes() output missing %q:\n%s", want, got)
		}
	}

	rows := make(map[string][]string)
	for _, line := range strings.Split(strings.TrimSpace(got), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) == 10 {
			rows[fields[1]] = fields
		}
	}
	if got := rows["local"][8]; got != "" {
		t.Fatalf("local legend field = %q, want empty", got)
	}
	if got := rows["remote"][2]; got != "https://github.com/example/repository-aliases.git" {
		t.Fatalf("remote repo field = %q, want original repository", got)
	}
	if got := rows["remote"][9]; got != "devbox" {
		t.Fatalf("remote origin field = %q, want devbox", got)
	}
}

func TestRunFzfOpts_ShortPreviewKeepsStableFieldPositions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	oldLoadAllLive := loadAllLiveFn
	t.Cleanup(func() { loadAllLiveFn = oldLoadAllLive })
	loadAllLiveFn = func(time.Duration) ([]sessions.Session, error) {
		return nil, nil
	}

	binDir := t.TempDir()
	fzfPath := filepath.Join(binDir, "fzf")
	script := "#!/bin/sh\nprintf '%s\n' \"$@\"\n"
	if err := os.WriteFile(fzfPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(fzf) error = %v", err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	got, err := runFzfOpts(14*24*time.Hour, "", false, false, true, 0, true, false, "", false)
	if err != nil {
		t.Fatalf("runFzfOpts() error = %v", err)
	}

	for _, want := range []string{
		"--accept-nth=2",
		"Repo: %s",
		"_ {2} {6} {7} {4} {3} {8} {9} {10}",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("fzf argv %q does not contain %q", got, want)
		}
	}
}
