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

const defaultRemoteCheckInterval = 24 * time.Hour

// FetchOptions carries per-fetch remote binary update policy.
type FetchOptions struct {
	ClientTag     string
	CheckInterval time.Duration
	ForceUpdate   bool
}

// runRemoteCmd executes a single shell command on the given remote over its
// configured transport (ssh, codespace, or devcontainer) and returns the
// command's combined stdout. It is a package-level var so tests can stub it.
var runRemoteCmd = func(ctx context.Context, r config.Remote, cmd string) ([]byte, error) {
	bin, args, stdin := remoteShellInvocation(r, cmd)

	c := exec.CommandContext(ctx, bin, args...)
	c.Stdin = strings.NewReader(stdin)
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

func remoteShellInvocation(r config.Remote, cmd string) (string, []string, string) {
	bin, args := r.GatherCommand()
	args = append(args, "bash", "-l", "-s")
	return bin, args, "set -e\n" + cmd + "\n"
}

var ensureRemoteBinaryFn = EnsureRemoteBinary

// RequestSnapshot asks the remote for a fresh active-only session snapshot
// via a one-shot `tsession remote rpc snapshot` invocation.
func RequestSnapshot(ctx context.Context, r config.Remote) (*SnapshotPayload, error) {
	return requestSnapshot(ctx, r, "tsession")
}

func requestSnapshot(ctx context.Context, r config.Remote, binaryPath string) (*SnapshotPayload, error) {
	out, err := runRemoteCmd(ctx, r, shellQuote(binaryPath)+" remote rpc snapshot")
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

// EnsureDaemonAndSnapshot ensures the remote binary is installed and returns a
// merged, maxAge-filtered one-shot snapshot of that remote's active sessions.
func EnsureDaemonAndSnapshot(ctx context.Context, r config.Remote, opts FetchOptions, maxAge time.Duration) ([]sessions.Session, error) {
	checkInterval := opts.CheckInterval
	if checkInterval <= 0 {
		checkInterval = defaultRemoteCheckInterval
	}
	binaryPath, err := ensureRemoteBinaryFn(ctx, r, opts.ClientTag, UpdateOptions{
		Force:         opts.ForceUpdate,
		CheckInterval: checkInterval,
	})
	if err != nil {
		return nil, fmt.Errorf("ensure remote binary: %w", err)
	}
	payload, err := requestSnapshot(ctx, r, binaryPath)
	if err != nil {
		return nil, err
	}
	return payload.ToSessions(r.Name, maxAge), nil
}
