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
	wantArgs := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "devbox", "bash", "-l", "-s"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("args = %v, want %v", args, wantArgs)
	}
	if stdin != "set -e\ntmux has-session -t tsessiond\n" {
		t.Fatalf("stdin = %q", stdin)
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

func TestEnsureDaemonAndSnapshotDoesNotRequireRemoteTmux(t *testing.T) {
	oldRun := runRemoteCmd
	oldEnsure := ensureRemoteBinaryFn
	t.Cleanup(func() {
		runRemoteCmd = oldRun
		ensureRemoteBinaryFn = oldEnsure
	})

	ensureRemoteBinaryFn = func(context.Context, config.Remote, string, UpdateOptions) (string, error) {
		return ".tsession/remote-bin/v0.5.0/tsession", nil
	}
	var calls []string
	runRemoteCmd = func(_ context.Context, _ config.Remote, command string) ([]byte, error) {
		calls = append(calls, command)
		if strings.Contains(command, "tmux ") {
			return nil, fmt.Errorf("unexpected remote tmux dependency: %s", command)
		}
		return []byte(`{"protocolVersion":1,"ok":true,"payload":{"tmuxAvailable":false,"sessions":[]}}`), nil
	}

	_, err := EnsureDaemonAndSnapshot(context.Background(), config.Remote{Name: "container"}, FetchOptions{}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || !strings.Contains(calls[0], "remote rpc snapshot") {
		t.Fatalf("calls = %v, want one-shot snapshot only", calls)
	}
}

func TestEnsureDaemonAndSnapshot_UsesInstalledBinaryAndRequestsSnapshot(t *testing.T) {
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
		if !strings.Contains(cmd, "remote rpc snapshot") {
			return nil, fmt.Errorf("unexpected cmd: %s", cmd)
		}
		return []byte(`{"protocolVersion":1,"ok":true,"payload":{"sessions":[{"id":"abc","state":"working","summary":"demo"}]}}`), nil
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
	if len(calls) != 1 {
		t.Fatalf("calls = %v, want one snapshot call", calls)
	}
	for _, call := range calls {
		if !strings.Contains(call, ".tsession/remote-bin/v1.2.3/tsession") {
			t.Fatalf("remote command does not use installed binary: %s", call)
		}
	}
}

func TestEnsureDaemonAndSnapshot_PropagatesSnapshotError(t *testing.T) {
	stubRemoteBinary(t, "tsession")
	oldRunRemoteCmd := runRemoteCmd
	defer func() { runRemoteCmd = oldRunRemoteCmd }()

	runRemoteCmd = func(ctx context.Context, r config.Remote, cmd string) ([]byte, error) {
		if strings.Contains(cmd, "remote rpc snapshot") {
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
