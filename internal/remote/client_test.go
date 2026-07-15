package remote

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/yarma/tsession/internal/config"
)

func TestEnsureDaemonAndSnapshot_StartsTmuxDaemonAndRequestsSnapshot(t *testing.T) {
	oldRunRemoteCmd := runRemoteCmd
	defer func() { runRemoteCmd = oldRunRemoteCmd }()

	var calls []string
	runRemoteCmd = func(ctx context.Context, r config.Remote, cmd string) ([]byte, error) {
		calls = append(calls, cmd)
		switch {
		case strings.Contains(cmd, "tmux has-session -t tsessiond"):
			return nil, errors.New("no such session")
		case strings.Contains(cmd, "tmux new-session -Ad -s tsessiond"):
			return []byte(""), nil
		case strings.Contains(cmd, "remote rpc snapshot"):
			return []byte(`{"protocolVersion":1,"ok":true,"payload":{"sessions":[{"id":"abc","state":"working","summary":"demo"}]}}`), nil
		default:
			return nil, fmt.Errorf("unexpected cmd: %s", cmd)
		}
	}
	out, err := EnsureDaemonAndSnapshot(context.Background(), config.Remote{Name: "devbox", Host: "devbox"}, FetchOptions{ClientTag: "v1.2.3", CheckInterval: 24 * time.Hour}, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != "abc" {
		t.Fatalf("sessions = %+v, want snapshot session", out)
	}
	if len(calls) < 3 {
		t.Fatalf("calls = %v, expected daemon ensure + snapshot calls", calls)
	}
}

func TestEnsureDaemonAndSnapshot_SkipsStartWhenAlreadyRunning(t *testing.T) {
	oldRunRemoteCmd := runRemoteCmd
	defer func() { runRemoteCmd = oldRunRemoteCmd }()

	var calls []string
	runRemoteCmd = func(ctx context.Context, r config.Remote, cmd string) ([]byte, error) {
		calls = append(calls, cmd)
		switch {
		case strings.Contains(cmd, "tmux has-session -t tsessiond"):
			return []byte(""), nil
		case strings.Contains(cmd, "remote rpc snapshot"):
			return []byte(`{"protocolVersion":1,"ok":true,"payload":{"sessions":[]}}`), nil
		default:
			return nil, fmt.Errorf("unexpected cmd: %s", cmd)
		}
	}
	if _, err := EnsureDaemonAndSnapshot(context.Background(), config.Remote{Name: "devbox", Host: "devbox"}, FetchOptions{}, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	for _, c := range calls {
		if strings.Contains(c, "tmux new-session") {
			t.Fatalf("should not have started a new daemon session, calls = %v", calls)
		}
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %v, want has-session + snapshot only", calls)
	}
}

func TestEnsureDaemonAndSnapshot_PropagatesSnapshotError(t *testing.T) {
	oldRunRemoteCmd := runRemoteCmd
	defer func() { runRemoteCmd = oldRunRemoteCmd }()

	runRemoteCmd = func(ctx context.Context, r config.Remote, cmd string) ([]byte, error) {
		switch {
		case strings.Contains(cmd, "tmux has-session -t tsessiond"):
			return []byte(""), nil
		case strings.Contains(cmd, "remote rpc snapshot"):
			return nil, errors.New("ssh: connection refused")
		default:
			return nil, fmt.Errorf("unexpected cmd: %s", cmd)
		}
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
