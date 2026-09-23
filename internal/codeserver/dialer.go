package codeserver

import (
	"fmt"
	"net"
	"os/exec"
	"time"

	"github.com/yarma/tsession/internal/codecmd"
	"github.com/yarma/tsession/internal/config"
)

// PortReadyFor picks the right PortReadyFunc for a session given its origin
// (empty for local sessions) and resolved remote r: a direct local dial for
// local sessions, DevcontainerDialer for devcontainer remotes (which have no
// port-forwarding primitive), and RemoteTunnelDialer for everything else
// (ssh, codespace).
func PortReadyFor(origin string, r config.Remote) PortReadyFunc {
	if origin == "" {
		return LocalDialer
	}
	if r.Type == "devcontainer" {
		return DevcontainerDialer(r)
	}
	return RemoteTunnelDialer(r)
}

// LocalDialer is a PortReadyFunc for a codeserver instance running on this
// host: it dials the discovered loopback port directly, with no extra
// teardown.
func LocalDialer(port int) (dial func() (net.Conn, error), teardown func() error, err error) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	return func() (net.Conn, error) {
		return net.DialTimeout("tcp", addr, 5*time.Second)
	}, nil, nil
}

// RemoteTunnelDialer returns a PortReadyFunc for a codeserver instance
// running on remote r (ssh or codespace): once the instance reports its
// remote port, it establishes a dedicated, persistent port-forwarding
// process (see codecmd.TunnelCommand) from a freshly allocated local port to
// that remote port, waits for the forward to come up, and dials through it.
// The returned teardown kills the forwarding process.
//
// Devcontainer remotes have no port-forwarding equivalent to ssh -L (docker
// exec cannot forward ports); RemoteTunnelDialer returns an error if called
// for one. See internal/codeserver's devcontainer-specific dialer instead.
func RemoteTunnelDialer(r config.Remote) PortReadyFunc {
	return func(remotePort int) (dial func() (net.Conn, error), teardown func() error, err error) {
		localPort, err := freeLoopbackPort()
		if err != nil {
			return nil, nil, fmt.Errorf("codeserver: allocate local tunnel port: %w", err)
		}

		bin, args, ok, err := codecmd.TunnelCommand(r, localPort, remotePort)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			return nil, nil, fmt.Errorf("codeserver: remote type %q has no port-forwarding tunnel", r.Type)
		}

		cmd := exec.Command(bin, args...)
		if err := cmd.Start(); err != nil {
			return nil, nil, fmt.Errorf("codeserver: start tunnel: %w", err)
		}

		addr := fmt.Sprintf("127.0.0.1:%d", localPort)
		if err := waitForListener(addr, 15*time.Second); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return nil, nil, err
		}

		dial = func() (net.Conn, error) {
			return net.DialTimeout("tcp", addr, 5*time.Second)
		}
		teardown = func() error {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			_ = cmd.Wait()
			return nil
		}
		return dial, teardown, nil
	}
}

// freeLoopbackPort asks the OS for a free loopback port by briefly binding
// to port 0 and immediately releasing it. This has an inherent (very small)
// TOCTOU race — another process could bind the same port before the tunnel
// command does — but is the standard, good-enough approach for picking an
// ephemeral local port to forward through.
func freeLoopbackPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("codeserver: unexpected listener address type %T", l.Addr())
	}
	return addr.Port, nil
}

// waitForListener polls addr until a TCP connection succeeds or timeout
// elapses. Port-forwarding processes (ssh -L, gh codespace ports forward)
// negotiate asynchronously, so the forward isn't necessarily ready the
// instant the child process starts.
func waitForListener(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		lastErr = err
		time.Sleep(150 * time.Millisecond)
	}
	return fmt.Errorf("codeserver: tunnel did not come up on %s: %w", addr, lastErr)
}
