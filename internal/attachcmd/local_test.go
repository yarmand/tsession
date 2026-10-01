package attachcmd

import (
	"strings"
	"testing"

	"github.com/yarma/tsession/internal/tmux"
)

func TestLocalSessionName_IsNotReapedAsWebSession(t *testing.T) {
	// ReapOrphanedLocal kills every tmux session with the web prefix at
	// server startup. The persistent local terminal must survive that, so
	// its name must never carry the prefix.
	if strings.HasPrefix(LocalSessionName, tmux.WebSessionPrefix) {
		t.Fatalf("LocalSessionName %q must not start with %q", LocalSessionName, tmux.WebSessionPrefix)
	}
}

func TestBuildLocal_EnsuresPersistentSessionThenGroupedAttach(t *testing.T) {
	bin, args, err := BuildLocal("/Users/someone")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bin != "sh" || len(args) != 2 || args[0] != "-c" {
		t.Fatalf("unexpected command shape: bin=%q args=%v", bin, args)
	}
	script := args[1]
	web := LocalWebSessionName()

	// The persistent session must be created in the home directory, with no
	// trailing command argument: `tmux new-session -d -s X -c DIR` with
	// nothing after DIR starts the user's own default shell, which is what
	// the spec calls for. The clause therefore ends immediately at "; ".
	ensure := "tmux has-session -t " + shQ(LocalSessionName) +
		" 2>/dev/null || tmux new-session -d -s " + shQ(LocalSessionName) +
		" -c " + shQ("/Users/someone") + "; "
	if !strings.HasPrefix(script, ensure) {
		t.Fatalf("script must start by ensuring the persistent session %q:\n%s", ensure, script)
	}

	// Everything after that clause is the ordinary grouped attach, so the
	// persistent session necessarily exists before it is mirrored.
	for _, want := range []string{
		"tmux has-session -t " + shQ(web),
		"tmux new-session -d -s " + shQ(web) + " -t " + shQ(LocalSessionName),
		"exec tmux attach-session -t " + shQ(web),
	} {
		if !strings.Contains(strings.TrimPrefix(script, ensure), want) {
			t.Errorf("grouped attach missing %q:\n%s", want, script)
		}
	}
}

func TestBuildLocal_EmptyHomeIsAnError(t *testing.T) {
	if _, _, err := BuildLocal(""); err == nil {
		t.Fatal("expected an error for an empty home directory")
	}
}

func TestBuildLocalKill_OnlyKillsTheGroupedSession(t *testing.T) {
	bin, args := BuildLocalKill()
	if bin != "sh" || len(args) != 2 || args[0] != "-c" {
		t.Fatalf("unexpected command shape: bin=%q args=%v", bin, args)
	}

	script := args[1]
	if !strings.Contains(script, "tmux kill-session -t "+shQ(LocalWebSessionName())) {
		t.Errorf("expected the grouped session to be killed:\n%s", script)
	}
	if strings.Contains(script, shQ(LocalSessionName)) {
		t.Fatalf("teardown must never kill the persistent session %q:\n%s", LocalSessionName, script)
	}
}

func TestBuildLocalRestore_SwitchesTheOwningClientBackToTheLocalGroup(t *testing.T) {
	bin, args, err := BuildLocalRestore(4321)
	if err != nil {
		t.Fatalf("BuildLocalRestore: %v", err)
	}
	if bin != "sh" || len(args) != 2 || args[0] != "-c" {
		t.Fatalf("unexpected command shape: bin=%q args=%v", bin, args)
	}
	for _, want := range []string{
		"tmux list-clients",
		"#{client_pid} #{client_tty}",
		`[ "$pid" = 4321 ]`,
		"tmux switch-client",
		"-t " + shQ(LocalWebSessionName()),
	} {
		if !strings.Contains(args[1], want) {
			t.Errorf("restore script missing %q:\n%s", want, args[1])
		}
	}
}

func TestBuildLocalRestore_RejectsInvalidPID(t *testing.T) {
	if _, _, err := BuildLocalRestore(0); err == nil {
		t.Fatal("expected an error for an invalid client PID")
	}
}
