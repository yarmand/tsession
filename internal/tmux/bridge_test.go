package tmux

import (
	"errors"
	"reflect"
	"testing"
)

func TestEnsureBridgeCreatesPersistentSessionBeforeStartingCommand(t *testing.T) {
	oldRun := runTmux
	t.Cleanup(func() { runTmux = oldRun })

	var calls [][]string
	runTmux = func(args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		if reflect.DeepEqual(args, []string{"has-session", "-t", "bridge"}) {
			return nil, errTmuxSessionMissing
		}
		return nil, nil
	}

	target, err := EnsureBridge(BridgeSpec{
		Name: "bridge", Path: "/tmp", Command: "ssh -t host",
		Origin: "mstudio", SessionID: "full-id",
	})
	if err != nil {
		t.Fatal(err)
	}
	if target != "bridge" {
		t.Fatalf("target = %q", target)
	}
	assertOrderedCalls(t, calls,
		[]string{"new-session", "-d", "-s", "bridge", "-c", "/tmp"},
		[]string{"set-option", "-w", "-t", "bridge", "remain-on-exit", "on"},
		[]string{"set-option", "-t", "bridge", "@tsession-origin", "mstudio"},
		[]string{"set-option", "-t", "bridge", "@tsession-session-id", "full-id"},
		[]string{"respawn-pane", "-k", "-c", "/tmp", "-t", "bridge", "ssh -t host"},
	)
}

func TestEnsureBridgeRespawnsDeadPane(t *testing.T) {
	oldRun := runTmux
	t.Cleanup(func() { runTmux = oldRun })
	var calls [][]string
	runTmux = bridgeRunner(&calls, "mstudio", "full-id", "1")

	_, err := EnsureBridge(BridgeSpec{
		Name: "bridge", Path: "/tmp", Command: "ssh -t host",
		Origin: "mstudio", SessionID: "full-id",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertCall(t, calls, []string{"respawn-pane", "-k", "-c", "/tmp", "-t", "bridge", "ssh -t host"})
}

func TestEnsureBridgeReusesLivePane(t *testing.T) {
	oldRun := runTmux
	t.Cleanup(func() { runTmux = oldRun })
	var calls [][]string
	runTmux = bridgeRunner(&calls, "mstudio", "full-id", "0")

	_, err := EnsureBridge(BridgeSpec{
		Name: "bridge", Path: "/tmp", Command: "ssh -t host",
		Origin: "mstudio", SessionID: "full-id",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertNoCommand(t, calls, "new-session")
	assertNoCommand(t, calls, "respawn-pane")
}

func TestEnsureBridgeRejectsIdentityCollision(t *testing.T) {
	oldRun := runTmux
	t.Cleanup(func() { runTmux = oldRun })
	var calls [][]string
	runTmux = bridgeRunner(&calls, "other-host", "other-id", "0")

	_, err := EnsureBridge(BridgeSpec{
		Name: "bridge", Origin: "mstudio", SessionID: "full-id",
	})
	var collision *BridgeCollisionError
	if !errors.As(err, &collision) {
		t.Fatalf("error = %v, want BridgeCollisionError", err)
	}
}

func TestEnsureBridgePropagatesUnexpectedInspectionError(t *testing.T) {
	oldRun := runTmux
	t.Cleanup(func() { runTmux = oldRun })
	runTmux = func(args ...string) ([]byte, error) {
		return nil, errors.New("tmux unavailable")
	}

	_, err := EnsureBridge(BridgeSpec{Name: "bridge"})
	if err == nil || err.Error() != "inspect bridge bridge: tmux unavailable" {
		t.Fatalf("error = %v", err)
	}
}

func TestTmuxSessionMissingRecognizesOnlyMissingDiagnostics(t *testing.T) {
	for _, message := range []string{
		"can't find session: bridge",
		"no server running on /tmp/tmux-501/default",
		"failed to connect to server",
		"error connecting to /tmp/tmux-501/default (No such file or directory)",
	} {
		if !tmuxSessionMissing([]byte(message)) {
			t.Errorf("tmuxSessionMissing(%q) = false", message)
		}
	}
	if tmuxSessionMissing([]byte("permission denied")) {
		t.Fatal("permission error classified as a missing session")
	}
}

func bridgeRunner(calls *[][]string, origin, sessionID, dead string) func(...string) ([]byte, error) {
	return func(args ...string) ([]byte, error) {
		*calls = append(*calls, append([]string(nil), args...))
		switch args[0] {
		case "has-session":
			return nil, nil
		case "show-option":
			if args[len(args)-1] == "@tsession-origin" {
				return []byte(origin + "\n"), nil
			}
			return []byte(sessionID + "\n"), nil
		case "display-message":
			return []byte(dead + "\n"), nil
		default:
			return nil, nil
		}
	}
}

func assertCall(t *testing.T, calls [][]string, want []string) {
	t.Helper()
	for _, call := range calls {
		if reflect.DeepEqual(call, want) {
			return
		}
	}
	t.Fatalf("missing call %v in %v", want, calls)
}

func assertOrderedCalls(t *testing.T, got [][]string, wants ...[]string) {
	t.Helper()
	next := 0
	for _, call := range got {
		if next < len(wants) && reflect.DeepEqual(call, wants[next]) {
			next++
		}
	}
	if next != len(wants) {
		t.Fatalf("calls %v do not contain ordered sequence %v", got, wants)
	}
}

func assertNoCommand(t *testing.T, calls [][]string, command string) {
	t.Helper()
	for _, call := range calls {
		if len(call) > 0 && call[0] == command {
			t.Fatalf("unexpected %s call: %v", command, call)
		}
	}
}
