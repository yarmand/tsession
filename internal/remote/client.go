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

// EnsureDaemonAndSnapshot uses a PATH-resolved remote tsession when available,
// falling back to installing one only when the remote host has no tsession in
// PATH. It ensures the watcher is running, then obtains active sessions through
// the same `tsession list --active` path used interactively.
func EnsureDaemonAndSnapshot(ctx context.Context, r config.Remote, opts FetchOptions, maxAge time.Duration) ([]sessions.Session, error) {
	binaryPath, err := remoteBinaryInPath(ctx, r)
	if err != nil {
		return nil, err
	}
	if binaryPath == "" {
		checkInterval := opts.CheckInterval
		if checkInterval <= 0 {
			checkInterval = defaultRemoteCheckInterval
		}
		binaryPath, err = ensureRemoteBinaryFn(ctx, r, opts.ClientTag, UpdateOptions{
			Force:         opts.ForceUpdate,
			CheckInterval: checkInterval,
		})
		if err != nil {
			return nil, fmt.Errorf("ensure remote binary: %w", err)
		}
	}

	out, err := runRemoteCmd(ctx, r, remoteListCommand(binaryPath, maxAge))
	if err != nil {
		return nil, fmt.Errorf("list remote sessions: %w", err)
	}
	var listed []sessions.Session
	if err := json.Unmarshal(bytes.TrimSpace(out), &listed); err != nil {
		return nil, fmt.Errorf("parse remote session list: %w", err)
	}
	return remoteSessions(r, listed), nil
}

func remoteBinaryInPath(ctx context.Context, r config.Remote) (string, error) {
	out, err := runRemoteCmd(ctx, r, `if command -v tsession >/dev/null 2>&1; then command -v tsession; fi`)
	if err != nil {
		return "", fmt.Errorf("find remote tsession: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func remoteListCommand(binaryPath string, maxAge time.Duration) string {
	binary := shellQuote(binaryPath)
	return binary + " watch --daemon >/dev/null && " +
		binary + " list --active --local-only --json --max-age=" + shellQuote(maxAge.String())
}

func remoteSessions(r config.Remote, listed []sessions.Session) []sessions.Session {
	out := make([]sessions.Session, 0, len(listed))
	for _, s := range listed {
		target := s.TmuxTarget
		if target == "" {
			target = s.TmuxName
		}
		s.Origin = r.Name
		s.RemoteHost = r.Endpoint()
		s.RemoteTmuxAvailable = target != ""
		s.RemoteTmuxTarget = target
		s.TmuxName = ""
		s.TmuxTarget = ""
		out = append(out, s)
	}
	return out
}
