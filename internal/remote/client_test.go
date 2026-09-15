package remote

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yarma/tsession/internal/config"
)

func TestRemoteShellInvocation_SendsCommandOnStdin(t *testing.T) {
	bin, args, stdin := remoteShellInvocation(config.Remote{Name: "devbox", Host: "devbox"}, "tmux has-session -t tsessiond")

	if bin != "ssh" {
		t.Fatalf("binary = %q, want ssh", bin)
	}
	wantArgs := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "devbox", "sh", "-s"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("args = %v, want %v", args, wantArgs)
	}
	for _, want := range []string{
		`remote_shell=${SHELL:-/bin/sh}`,
		`*) shell_flags=-lic`,
		`exec "$remote_shell" "$shell_flags"`,
		remoteOutputMarker,
		"tmux has-session -t tsessiond",
	} {
		if !strings.Contains(stdin, want) {
			t.Errorf("stdin missing %q:\n%s", want, stdin)
		}
	}
}

func stubRemoteBinary(t *testing.T, path string) {
	t.Helper()
	old := ensureRemoteBinaryFn
	ensureRemoteBinaryFn = func(context.Context, config.Remote, string, UpdateOptions) (string, error) {
		return path, nil
	}
	t.Cleanup(func() { ensureRemoteBinaryFn = old })
}

func TestEnsureDaemonAndSnapshotPrefersPathBinaryAndListsActiveSessions(t *testing.T) {
	oldRun := runRemoteCmd
	oldEnsure := ensureRemoteBinaryFn
	t.Cleanup(func() {
		runRemoteCmd = oldRun
		ensureRemoteBinaryFn = oldEnsure
	})

	ensureRemoteBinaryFn = func(context.Context, config.Remote, string, UpdateOptions) (string, error) {
		t.Fatal("installer called even though tsession is available in PATH")
		return "", nil
	}
	var calls []string
	runRemoteCmd = func(_ context.Context, _ config.Remote, command string) ([]byte, error) {
		calls = append(calls, command)
		if strings.Contains(command, "command -v tsession") {
			return []byte("/usr/local/bin/tsession\n"), nil
		}
		if !strings.Contains(command, "watch --daemon") ||
			strings.Contains(command, "watch --daemon --active") ||
			!strings.Contains(command, "list --active --local-only --json") {
			return nil, fmt.Errorf("unexpected remote command: %s", command)
		}
		return []byte(`[{"ID":"abc","State":3,"TmuxName":"work","TmuxTarget":"work:1.0"}]`), nil
	}

	got, err := EnsureDaemonAndSnapshot(context.Background(), config.Remote{Name: "devbox", Host: "devbox.example.com"}, FetchOptions{}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %v, want PATH check followed by active list", calls)
	}
	if len(got) != 1 || got[0].Origin != "devbox" ||
		got[0].RemoteTmuxTarget != "work:1.0" || !got[0].RemoteTmuxAvailable {
		t.Fatalf("sessions = %+v", got)
	}
}

func TestEnsureDaemonAndSnapshotInstallsOnlyWhenPathBinaryIsAbsent(t *testing.T) {
	oldRunRemoteCmd := runRemoteCmd
	defer func() { runRemoteCmd = oldRunRemoteCmd }()
	oldEnsureRemoteBinary := ensureRemoteBinaryFn
	defer func() { ensureRemoteBinaryFn = oldEnsureRemoteBinary }()

	var calls []string
	var gotClientTag string
	ensureRemoteBinaryFn = func(ctx context.Context, r config.Remote, clientTag string, opts UpdateOptions) (string, error) {
		gotClientTag = clientTag
		return ".tsession/remote-bin/v1.2.3/tsession", nil
	}
	runRemoteCmd = func(ctx context.Context, r config.Remote, cmd string) ([]byte, error) {
		calls = append(calls, cmd)
		if strings.Contains(cmd, "command -v tsession") {
			return nil, nil
		}
		if !strings.Contains(cmd, ".tsession/remote-bin/v1.2.3/tsession") ||
			!strings.Contains(cmd, "list --active --local-only --json") {
			return nil, fmt.Errorf("unexpected cmd: %s", cmd)
		}
		return []byte(`[{"ID":"abc","State":5,"TmuxName":"work"}]`), nil
	}
	out, err := EnsureDaemonAndSnapshot(context.Background(), config.Remote{Name: "devbox", Host: "devbox"}, FetchOptions{ClientTag: "v1.2.3", CheckInterval: 24 * time.Hour}, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != "abc" {
		t.Fatalf("sessions = %+v, want snapshot session", out)
	}
	if gotClientTag != "v1.2.3" {
		t.Fatalf("installer client tag = %q, want v1.2.3", gotClientTag)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %v, want PATH check and list call", calls)
	}
	if !strings.Contains(calls[1], ".tsession/remote-bin/v1.2.3/tsession") {
		t.Fatalf("remote command does not use installed binary: %s", calls[1])
	}
}

func TestEnsureDaemonAndSnapshot_PropagatesListError(t *testing.T) {
	oldRunRemoteCmd := runRemoteCmd
	defer func() { runRemoteCmd = oldRunRemoteCmd }()

	runRemoteCmd = func(ctx context.Context, r config.Remote, cmd string) ([]byte, error) {
		if strings.Contains(cmd, "command -v tsession") {
			return []byte("/usr/bin/tsession\n"), nil
		}
		if strings.Contains(cmd, "list --active") {
			return nil, errors.New("ssh: connection refused")
		}
		return nil, fmt.Errorf("unexpected cmd: %s", cmd)
	}
	_, err := EnsureDaemonAndSnapshot(context.Background(), config.Remote{Name: "devbox", Host: "devbox"}, FetchOptions{}, 24*time.Hour)
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("err = %v, want connection refused error", err)
	}
}

func TestRequestSnapshot_ReturnsErrorOnNotOK(t *testing.T) {
	oldRunRemoteCmd := runRemoteCmd
	defer func() { runRemoteCmd = oldRunRemoteCmd }()

	runRemoteCmd = func(ctx context.Context, r config.Remote, cmd string) ([]byte, error) {
		return []byte(`{"protocolVersion":1,"ok":false,"error":"snapshot failed"}`), nil
	}
	_, err := RequestSnapshot(context.Background(), config.Remote{Name: "devbox", Host: "devbox"})
	if err == nil || !strings.Contains(err.Error(), "snapshot failed") {
		t.Fatalf("err = %v, want snapshot failed error", err)
	}
}
