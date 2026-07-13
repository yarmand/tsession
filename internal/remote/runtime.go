package remote

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/yarma/tsession/internal/config"
)

// unameScript prints `uname -s` and `uname -m` on separate lines.
const unameScript = "uname -s\nuname -m\n"

// DetectRuntime probes a remote host's OS/architecture and maps it to a
// supported release runtime identifier (e.g. "linux-amd64").
func DetectRuntime(ctx context.Context, r config.Remote) (string, error) {
	bin, args := r.GatherCommand()
	args = append(args, "bash", "-s")

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = strings.NewReader(unameScript)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", fmt.Errorf("detect runtime: %w: %s", err, msg)
		}
		return "", fmt.Errorf("detect runtime: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) < 2 {
		return "", fmt.Errorf("detect runtime: unexpected output %q", stdout.String())
	}
	return mapRuntime(strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1]))
}

// mapRuntime maps `uname -s`/`uname -m` output to a supported runtime
// identifier. Supported runtimes (v1): linux-amd64, linux-arm64, darwin-arm64.
func mapRuntime(unameS, unameM string) (string, error) {
	switch strings.ToLower(unameS) {
	case "linux":
		switch strings.ToLower(unameM) {
		case "x86_64", "amd64":
			return "linux-amd64", nil
		case "aarch64", "arm64":
			return "linux-arm64", nil
		}
	case "darwin":
		if strings.ToLower(unameM) == "arm64" {
			return "darwin-arm64", nil
		}
	}
	return "", fmt.Errorf("unsupported runtime: %s/%s", unameS, unameM)
}
