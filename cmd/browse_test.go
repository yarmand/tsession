package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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
