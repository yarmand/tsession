package tmux

import (
	"bytes"
	"errors"
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
	oldWait := waitForTargetClient
	oldOutput := targetWaitOutput
	t.Cleanup(func() {
		listClientsOutput = oldList
		pickClientOutput = oldPick
		waitForTargetClient = oldWait
		targetWaitOutput = oldOutput
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

func TestPickClientWaitsForEligibleClientThenConfirmsWithPicker(t *testing.T) {
	oldList := listClientsOutput
	oldPick := pickClientOutput
	oldWait := waitForTargetClient
	oldOutput := targetWaitOutput
	t.Cleanup(func() {
		listClientsOutput = oldList
		pickClientOutput = oldPick
		waitForTargetClient = oldWait
		targetWaitOutput = oldOutput
	})

	var listCalls int
	listClientsOutput = func() ([]byte, error) {
		listCalls++
		if listCalls <= 2 {
			return []byte("/dev/ttys000|session-nav\n"), nil
		}
		return []byte("/dev/ttys000|session-nav\n/dev/ttys001|remote-debug\n"), nil
	}
	var waits int
	waitForTargetClient = func() { waits++ }
	var messages bytes.Buffer
	targetWaitOutput = &messages
	pickClientOutput = func(input string) ([]byte, error) {
		if input != "/dev/ttys001|remote-debug\n" {
			t.Fatalf("picker input = %q", input)
		}
		return []byte("/dev/ttys001|remote-debug\n"), nil
	}

	got, err := ResolveTarget("pick")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/dev/ttys001" {
		t.Fatalf("target = %q, want /dev/ttys001", got)
	}
	if waits != 2 {
		t.Fatalf("waits = %d, want 2", waits)
	}
	if messages.String() != "Waiting for a non-navigation tmux client; open or attach one to continue...\n" {
		t.Fatalf("waiting message = %q", messages.String())
	}
}

func TestPickClientPropagatesListErrorWhileWaiting(t *testing.T) {
	oldList := listClientsOutput
	oldWait := waitForTargetClient
	oldOutput := targetWaitOutput
	t.Cleanup(func() {
		listClientsOutput = oldList
		waitForTargetClient = oldWait
		targetWaitOutput = oldOutput
	})

	var listCalls int
	listClientsOutput = func() ([]byte, error) {
		listCalls++
		if listCalls == 1 {
			return []byte("/dev/ttys000|session-nav\n"), nil
		}
		return nil, errors.New("test list-clients failure")
	}
	waitForTargetClient = func() {}
	targetWaitOutput = &bytes.Buffer{}

	_, err := ResolveTarget("pick")
	if err == nil || err.Error() != "list clients: test list-clients failure" {
		t.Fatalf("error = %v, want propagated list-clients error", err)
	}
}
