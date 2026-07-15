package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/yarma/tsession/internal/config"
	"github.com/yarma/tsession/internal/sessions"
)

// tsessiondSession is the fixed tmux session name used for the remote daemon.
const tsessiondSession = "tsessiond"

// FetchOptions carries per-fetch tuning knobs for the daemon-backed remote
// fetch path (binary update cadence, forced re-installs, etc). ClientTag,
// CheckInterval, and ForceUpdate are consumed by the remote binary update
// manager (a separate work stream); EnsureDaemonAndSnapshot accepts them
// today so that integration is a non-breaking follow-up.
type FetchOptions struct {
	ClientTag     string
	CheckInterval time.Duration
	ForceUpdate   bool
}

// runRemoteCmd executes a single shell command on the given remote over its
// configured transport (ssh, codespace, or devcontainer) and returns the
// command's combined stdout. It is a package-level var so tests can stub it.
var runRemoteCmd = func(ctx context.Context, r config.Remote, cmd string) ([]byte, error) {
	bin, args := r.GatherCommand()
	args = append(args, "bash", "-lc", cmd)

	c := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		label := r.Name
		if r.Host != "" {
			label = r.Host
		}
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return stdout.Bytes(), fmt.Errorf("%s %s: %w: %s", bin, label, err, msg)
		}
		return stdout.Bytes(), fmt.Errorf("%s %s: %w", bin, label, err)
	}
	return stdout.Bytes(), nil
}

// ensureRemoteDaemon makes sure a `tsessiond` tmux session running
// `tsession remote serve` exists on the remote, starting one if needed.
func ensureRemoteDaemon(ctx context.Context, r config.Remote) error {
	if _, err := runRemoteCmd(ctx, r, fmt.Sprintf("tmux has-session -t %s", tsessiondSession)); err == nil {
		return nil
	}
	if _, err := runRemoteCmd(ctx, r, fmt.Sprintf("tmux new-session -Ad -s %s 'tsession remote serve'", tsessiondSession)); err != nil {
		return fmt.Errorf("start remote daemon: %w", err)
	}
	return nil
}

// RequestSnapshot asks the remote for a fresh active-only session snapshot
// via a one-shot `tsession remote rpc snapshot` invocation.
func RequestSnapshot(ctx context.Context, r config.Remote) (*SnapshotPayload, error) {
	out, err := runRemoteCmd(ctx, r, "tsession remote rpc snapshot")
	if err != nil {
		return nil, fmt.Errorf("request snapshot: %w", err)
	}
	var resp RPCResponse
	if err := json.Unmarshal(bytes.TrimSpace(out), &resp); err != nil {
		return nil, fmt.Errorf("parse snapshot response: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("remote snapshot error: %s", resp.Error)
	}
	return &resp.Payload, nil
}

// EnsureDaemonAndSnapshot ensures the remote daemon is running and returns a
// merged, maxAge-filtered snapshot of that remote's active sessions.
func EnsureDaemonAndSnapshot(ctx context.Context, r config.Remote, opts FetchOptions, maxAge time.Duration) ([]sessions.Session, error) {
	if err := ensureRemoteDaemon(ctx, r); err != nil {
		return nil, err
	}
	payload, err := RequestSnapshot(ctx, r)
	if err != nil {
		return nil, err
	}
	return payload.ToSessions(r.Name, maxAge), nil
}
