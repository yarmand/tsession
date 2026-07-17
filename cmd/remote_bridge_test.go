package cmd

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/yarma/tsession/internal/config"
	"github.com/yarma/tsession/internal/sessions"
	"github.com/yarma/tsession/internal/tmux"
)

func TestRemoteBridgeNameIsStableAndSanitized(t *testing.T) {
	got1 := remoteBridgeName("My Studio!", "abcdefgh-1234-5678")
	got2 := remoteBridgeName("My Studio!", "abcdefgh-1234-5678")
	if got1 != got2 {
		t.Fatalf("bridge names differ: %q vs %q", got1, got2)
	}
	if !strings.HasPrefix(got1, "tsession-r-my-studio-") {
		t.Fatalf("bridge name = %q", got1)
	}
}

func TestRemoteResumeCommandQuotesSessionIDForTmuxShell(t *testing.T) {
	got := remoteResumeCommand("$(touch /tmp/tsession-injected)")
	probe := "exec /bin/sh -c " + shellQuote("command -v copilot")
	resolver := `remote_shell=${SHELL:-/bin/sh}; ` +
		`case "${remote_shell##*/}" in csh|tcsh) shell_flags=-ic ;; *) shell_flags=-lic ;; esac; ` +
		`copilot_bin=$("$remote_shell" "$shell_flags" ` + shellQuote(probe) + ` 2>/dev/null | tail -n 1); ` +
		`case "$copilot_bin" in /*) ;; *) echo 'copilot resolver did not return an absolute path' >&2; exit 127 ;; esac; ` +
		`if [ ! -x "$copilot_bin" ]; then echo 'copilot not found in remote interactive shell PATH' >&2; exit 127; fi; `
	want := resolver + `exec "$copilot_bin" --resume='$(touch /tmp/tsession-injected)'`
	if got != want {
		t.Fatalf("resume command = %q, want %q", got, want)
	}
}

func TestRemoteCopilotResolverSupportsCshInteractiveFlags(t *testing.T) {
	got := remoteCopilotResolverCommand()
	if !strings.Contains(got, `case "${remote_shell##*/}" in csh|tcsh) shell_flags=-ic`) {
		t.Fatalf("resolver does not select csh-compatible interactive flags: %q", got)
	}
	if !strings.Contains(got, `*) shell_flags=-lic`) {
		t.Fatalf("resolver does not preserve login interactive flags for Bourne shells: %q", got)
	}
}

func TestRemoteSessionShellCommandResolvesCopilotBeforeCreatingTmuxFallback(t *testing.T) {
	s := sessions.Session{ID: "abcdefgh-1234", RemoteTmuxAvailable: true}
	got := remoteSessionShellCommand(s)
	resolverEnd := strings.Index(got, "tmux new-session")
	if resolverEnd < 0 || !strings.Contains(got[:resolverEnd], "command -v copilot") {
		t.Fatalf("Copilot is not resolved before tmux fallback creation: %q", got)
	}
	if !strings.Contains(got, `-e TSESSION_COPILOT_BIN="$copilot_bin"`) {
		t.Fatalf("resolved Copilot path is not passed into remote tmux: %q", got)
	}
	resume := shellQuote(`exec "$TSESSION_COPILOT_BIN" --resume=` + shellQuote(s.ID))
	if !strings.Contains(got, resume) {
		t.Fatalf("remote tmux command does not use its Copilot environment path: %q", got)
	}
}

func TestRemoteSessionShellCommandCreatesFallbackTmuxSession(t *testing.T) {
	s := sessions.Session{
		ID:                  "id'; touch /tmp/tsession-injected #",
		RemoteTmuxAvailable: true,
	}
	got := remoteSessionShellCommand(s)
	fallback := shellQuote(remoteFallbackTmuxName(s.ID))
	if !strings.Contains(got, "tmux has-session -t "+fallback) {
		t.Fatalf("fallback lookup missing from %q", got)
	}
	if !strings.Contains(got, "tmux new-session -d -s "+fallback+
		` -e TSESSION_COPILOT_BIN="$copilot_bin" `+shellQuote(remoteTmuxResumeCommand(s.ID))) {
		t.Fatalf("nested resume command is not safely quoted in %q", got)
	}
	if !strings.Contains(got, "exec tmux attach-session -t "+fallback) {
		t.Fatalf("fallback attach missing from %q", got)
	}
	recheck := "|| { tmux has-session -t " + fallback + " 2>/dev/null || exit $?; }"
	if !strings.Contains(got, recheck) {
		t.Fatalf("concurrent fallback creation is not rechecked in %q", got)
	}
}

func TestRemoteSessionShellCommandQuotesExistingTarget(t *testing.T) {
	s := sessions.Session{
		ID:                  "abcdefgh-1234",
		RemoteTmuxAvailable: true,
		RemoteTmuxTarget:    "famstack:2.0'; touch /tmp/tsession-target-injected #",
	}
	got := remoteSessionShellCommand(s)
	if !strings.Contains(got, shellQuote(s.RemoteTmuxTarget)) {
		t.Fatalf("remote target is not safely quoted in %q", got)
	}
}

func TestRemoteBridgeCommandSSHAttachesExistingTarget(t *testing.T) {
	s := sessions.Session{
		ID:                  "abcdefgh-1234",
		RemoteTmuxAvailable: true,
		RemoteTmuxTarget:    "famstack:2.0",
	}
	bin, args, err := remoteBridgeCommand(s, config.Remote{Type: "ssh", Host: "mstudio"})
	if err != nil {
		t.Fatal(err)
	}
	if bin != "ssh" {
		t.Fatalf("binary = %q, want ssh", bin)
	}
	wantPrefix := []string{"-t", "mstudio"}
	if !reflect.DeepEqual(args[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("args = %v, want prefix %v", args, wantPrefix)
	}
	remoteCommand := args[len(args)-1]
	if !strings.Contains(remoteCommand, "tmux attach-session") ||
		!strings.Contains(remoteCommand, s.RemoteTmuxTarget) {
		t.Fatalf("remote command = %q", remoteCommand)
	}
	if !strings.Contains(remoteCommand, "pane_dead") {
		t.Fatalf("remote command does not reject dead target panes: %q", remoteCommand)
	}
	if !strings.Contains(remoteCommand, "tmux attach-session") ||
		!strings.Contains(remoteCommand, "&& exit 0; fi;") {
		t.Fatalf("existing-target attach cannot fall back after a race: %q", remoteCommand)
	}
	if !strings.Contains(remoteCommand, "tmux new-session -d -s") {
		t.Fatalf("stale-target fallback missing: %q", remoteCommand)
	}
}

func TestRemoteBridgeCommandCodespaceUsesConfiguredTransport(t *testing.T) {
	s := sessions.Session{
		ID:                  "abcdefgh-1234",
		RemoteTmuxAvailable: true,
		RemoteTmuxTarget:    "work:1.0",
	}
	bin, args, err := remoteBridgeCommand(s, config.Remote{
		Type: "codespace", Codespace: "my-cs",
	})
	if err != nil {
		t.Fatal(err)
	}
	if bin != "gh" {
		t.Fatalf("binary = %q, want gh", bin)
	}
	wantPrefix := []string{"codespace", "ssh", "--codespace", "my-cs", "-t", "--"}
	if !reflect.DeepEqual(args[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("args = %v, want prefix %v", args, wantPrefix)
	}
	if !strings.Contains(args[len(args)-1], "tmux attach-session") ||
		!strings.Contains(args[len(args)-1], "work:1.0") {
		t.Fatalf("remote command = %q", args[len(args)-1])
	}
}

func TestRemoteBridgeCommandDevcontainerWithoutTmuxResumesDirectly(t *testing.T) {
	s := sessions.Session{ID: "abcdefgh-1234", RemoteTmuxAvailable: false}
	bin, args, err := remoteBridgeCommand(s, config.Remote{
		Type: "devcontainer", Container: "app", User: "vscode",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := []string{"exec", "-it", "-u", "vscode", "app", "bash", "-lc"}
	if bin != "docker" || !reflect.DeepEqual(args[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("command = %s %v", bin, args)
	}
	if args[len(args)-1] != remoteResumeCommand("abcdefgh-1234") {
		t.Fatalf("remote command = %q", args[len(args)-1])
	}
}

func TestEnsureRemoteBridgeUsesStableAlternateNameOnCollision(t *testing.T) {
	t.Setenv("HOME", "/Users/me")
	oldEnsure := ensureBridgeFn
	t.Cleanup(func() { ensureBridgeFn = oldEnsure })

	var names []string
	ensureBridgeFn = func(spec tmux.BridgeSpec) (string, error) {
		names = append(names, spec.Name)
		if len(names) == 1 {
			return "", &tmux.BridgeCollisionError{Name: spec.Name}
		}
		return spec.Name, nil
	}

	session := sessions.Session{ID: "full-session-id", Origin: "mstudio"}
	got, err := ensureRemoteBridge(session, config.Remote{Type: "ssh", Host: "mstudio"})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] == names[1] {
		t.Fatalf("bridge names = %v, want stable alternate after collision", names)
	}
	if got != names[1] {
		t.Fatalf("bridge = %q, want %q", got, names[1])
	}

	var collision *tmux.BridgeCollisionError
	if errors.As(err, &collision) {
		t.Fatalf("collision was not recovered: %v", err)
	}
}
