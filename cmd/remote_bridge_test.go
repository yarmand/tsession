package cmd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/yarma/tsession/internal/config"
	"github.com/yarma/tsession/internal/sessions"
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
	want := "exec copilot --resume='$(touch /tmp/tsession-injected)'"
	if got != want {
		t.Fatalf("resume command = %q, want %q", got, want)
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
	if !strings.Contains(got, "tmux new-session -d -s "+fallback+" "+shellQuote(remoteResumeCommand(s.ID))) {
		t.Fatalf("nested resume command is not safely quoted in %q", got)
	}
	if !strings.Contains(got, "exec tmux attach-session -t "+fallback) {
		t.Fatalf("fallback attach missing from %q", got)
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
	if args[len(args)-1] != "exec copilot --resume='abcdefgh-1234'" {
		t.Fatalf("remote command = %q", args[len(args)-1])
	}
}
