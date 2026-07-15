package cmd

import (
	"reflect"
	"strings"
	"testing"
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
