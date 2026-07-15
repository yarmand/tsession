package remote

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/yarma/tsession/internal/config"
)

// defaultRepo is the GitHub repository used to resolve release assets.
const defaultRepo = "yarma/tsession"

// remoteBinDir is where installed binaries live on the remote host.
const remoteBinDir = ".tsession/remote-bin"

// UpdateOptions controls remote binary update policy.
type UpdateOptions struct {
	Force         bool
	CheckInterval time.Duration
}

type detectRuntimeFunc func(context.Context, config.Remote) (string, error)
type resolveReleaseFunc func(context.Context, string, string, string, *http.Client) (ResolvedAsset, error)

// EnsureRemoteBinary ensures a runtime-appropriate tsession binary is
// installed on the remote host, refreshing runtime/version resolution at
// most once per opts.CheckInterval (or immediately when opts.Force is set).
// It returns the remote path to the binary.
func EnsureRemoteBinary(ctx context.Context, r config.Remote, clientTag string, opts UpdateOptions) (string, error) {
	state, err := LoadUpdateState(r.Name)
	if err != nil {
		return "", err
	}
	return ensureRemoteBinaryWithDeps(ctx, r, clientTag, opts, state, DetectRuntime, ResolveRelease)
}

func ensureRemoteBinaryWithDeps(
	ctx context.Context,
	r config.Remote,
	clientTag string,
	opts UpdateOptions,
	state UpdateState,
	detectRuntime detectRuntimeFunc,
	resolveRelease resolveReleaseFunc,
) (string, error) {
	if !NeedsRefresh(state, time.Now().UTC(), opts.CheckInterval, opts.Force) && state.Version != "" {
		return remoteBinaryPath(state.Version), nil
	}

	runtime, err := detectRuntime(ctx, r)
	if err != nil {
		return "", err
	}
	asset, err := resolveRelease(ctx, defaultRepo, clientTag, runtime, http.DefaultClient)
	if err != nil {
		return "", err
	}
	path, err := InstallRemoteBinary(ctx, r, asset)
	if err != nil {
		return "", err
	}

	newState := UpdateState{
		LastCheckedAt: time.Now().UTC(),
		Runtime:       runtime,
		Version:       asset.Version,
		AssetName:     asset.AssetName,
	}
	if err := SaveUpdateState(r.Name, newState); err != nil {
		return "", err
	}
	return path, nil
}

// remoteBinaryPath returns the deterministic remote path for a given
// installed version.
func remoteBinaryPath(version string) string {
	return fmt.Sprintf("%s/%s/tsession", remoteBinDir, version)
}

// InstallRemoteBinary downloads and extracts the resolved release asset on
// the remote host, placing it at remoteBinaryPath(asset.Version), and
// returns that path.
func InstallRemoteBinary(ctx context.Context, r config.Remote, asset ResolvedAsset) (string, error) {
	if asset.DownloadURL == "" {
		return "", fmt.Errorf("install remote binary: empty download URL")
	}

	dest := remoteBinaryPath(asset.Version)
	script := installScript(dest, asset.DownloadURL)

	bin, args := r.GatherCommand()
	args = append(args, "bash", "-s")

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = strings.NewReader(script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", fmt.Errorf("install remote binary: %w: %s", err, msg)
		}
		return "", fmt.Errorf("install remote binary: %w", err)
	}
	return dest, nil
}

// installScript builds a POSIX shell script that downloads the release
// tarball to a versioned directory and extracts the tsession binary.
func installScript(dest, downloadURL string) string {
	destDir := dest[:strings.LastIndex(dest, "/")]
	return fmt.Sprintf(`set -e
mkdir -p %q
curl -fsSL %q -o %q.tar.gz
tar -xzf %q.tar.gz -C %q
rm -f %q.tar.gz
chmod +x %q
`, destDir, downloadURL, dest, dest, destDir, dest, dest)
}
