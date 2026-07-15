package tmux

import (
	"reflect"
	"testing"
)

func TestFilterTargetClientsExcludesNavigationSession(t *testing.T) {
	got := FilterTargetClients(ParseClients(
		"/dev/ttys000|session-nav\n"+
			"/dev/ttys001|remote-debug\n"+
			"|headless\n",
	), "session-nav")

	want := []Client{{TTY: "/dev/ttys001", SessionName: "remote-debug"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("clients = %+v, want %+v", got, want)
	}
}

func TestPickClientAlwaysRunsPickerForOneEligibleClient(t *testing.T) {
	oldList := listClientsOutput
	oldPick := pickClientOutput
	t.Cleanup(func() {
		listClientsOutput = oldList
		pickClientOutput = oldPick
	})

	listClientsOutput = func() ([]byte, error) {
		return []byte("/dev/ttys000|session-nav\n/dev/ttys001|remote-debug\n"), nil
	}
	called := false
	pickClientOutput = func(input string) ([]byte, error) {
		called = true
		if input != "/dev/ttys001|remote-debug\n" {
			t.Fatalf("picker input = %q", input)
		}
		return []byte("/dev/ttys001|remote-debug\n"), nil
	}

	got, err := ResolveTarget("pick")
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("explicit pick did not open the picker")
	}
	if got != "/dev/ttys001" {
		t.Fatalf("target = %q, want /dev/ttys001", got)
	}
}
