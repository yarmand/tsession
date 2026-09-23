package codeserver

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/yarma/tsession/internal/config"
)

// DevcontainerDialer returns a PortReadyFunc for a codeserver instance
// running inside devcontainer remote r. docker exec has no equivalent to
// ssh -L port forwarding, so unlike RemoteTunnelDialer there is no single
// persistent tunnel process to establish up front: instead, each Dial()
// call spawns a fresh `docker exec -i` process that relays its stdio to
// 127.0.0.1:remotePort inside the container (via whichever of nc, socat, or
// python3 is available there — see devcontainerRelayScript), adapted into a
// net.Conn. There is no extra teardown; each dialed connection's own Close
// tears down its relay process.
func DevcontainerDialer(r config.Remote) PortReadyFunc {
	return func(remotePort int) (dial func() (net.Conn, error), teardown func() error, err error) {
		dial = func() (net.Conn, error) {
			return dialDevcontainerStdio(r, remotePort)
		}
		return dial, nil, nil
	}
}

// devcontainerRelayScript returns a POSIX shell fragment that relays its own
// stdin/stdout to 127.0.0.1:port using the first of nc, socat, or a small
// python3 fallback found in the container, failing loudly with a clear,
// actionable message (rather than a silent blank connection) if none of the
// three exist.
func devcontainerRelayScript(port int) string {
	pyRelay := `import socket, sys, threading
s = socket.create_connection(("127.0.0.1", ` + fmt.Sprintf("%d", port) + `))
def to_sock():
    while True:
        data = sys.stdin.buffer.read(65536)
        if not data:
            break
        s.sendall(data)
    s.shutdown(socket.SHUT_WR)
t = threading.Thread(target=to_sock)
t.start()
while True:
    data = s.recv(65536)
    if not data:
        break
    sys.stdout.buffer.write(data)
    sys.stdout.buffer.flush()
t.join()
`
	return fmt.Sprintf(
		`if command -v nc >/dev/null 2>&1; then exec nc 127.0.0.1 %d; `+
			`elif command -v socat >/dev/null 2>&1; then exec socat - TCP:127.0.0.1:%d; `+
			`elif command -v python3 >/dev/null 2>&1; then exec python3 -c %s; `+
			`else echo 'tsession: no relay tool (nc, socat, or python3) found in devcontainer' >&2; exit 127; fi`,
		port, port, shellQuote(pyRelay))
}

// shellQuote is a tiny local single-quote escaper (matching
// shellutil.Quote's behavior) so devcontainerRelayScript doesn't need to
// import shellutil just for this one call.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// dialDevcontainerStdio spawns a `docker exec -i` process running
// devcontainerRelayScript(port) inside r's container and adapts its
// stdin/stdout into a net.Conn.
func dialDevcontainerStdio(r config.Remote, port int) (net.Conn, error) {
	bin, args := r.GatherCommand()
	args = append(args, "sh", "-c", devcontainerRelayScript(port))
	cmd := exec.Command(bin, args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("codeserver: devcontainer relay stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("codeserver: devcontainer relay stdout: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("codeserver: start devcontainer relay: %w", err)
	}

	return newStdioConn(cmd, stdin, stdout, &stderr), nil
}

// stdioConn adapts a child process's stdin/stdout pipes into a net.Conn, so
// http.Transport (via the reverse proxy) can treat a `docker exec` relay
// process exactly like a real TCP connection. Addressing and deadlines are
// not meaningful for a process pipe, so those methods are no-ops.
type stdioConn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr *bytes.Buffer

	done      chan struct{}
	closeOnce sync.Once
}

func newStdioConn(cmd *exec.Cmd, stdin io.WriteCloser, stdout io.ReadCloser, stderr *bytes.Buffer) *stdioConn {
	c := &stdioConn{cmd: cmd, stdin: stdin, stdout: stdout, stderr: stderr, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(c.done)
	}()
	return c
}

// Read surfaces the relay's stderr (e.g. "no relay tool found") as the
// error once the process has exited, instead of a bare, unexplained EOF —
// this is what turns a silent blank iframe into an actionable message in
// the code pane.
func (c *stdioConn) Read(p []byte) (int, error) {
	n, err := c.stdout.Read(p)
	if err != nil {
		// The process may have exited (closing its stdout, which is what
		// just produced err) microseconds before cmd.Wait() returns and
		// populates stderr/closes c.done; give it a brief grace period
		// rather than racing a read of c.stderr against Wait's internal
		// stderr-copy goroutine.
		select {
		case <-c.done:
			if msg := strings.TrimSpace(c.stderr.String()); msg != "" {
				return n, fmt.Errorf("codeserver: devcontainer relay: %s", msg)
			}
		case <-time.After(2 * time.Second):
		}
	}
	return n, err
}

func (c *stdioConn) Write(p []byte) (int, error) { return c.stdin.Write(p) }

func (c *stdioConn) Close() error {
	c.closeOnce.Do(func() {
		_ = c.stdin.Close()
		_ = c.stdout.Close()
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
	})
	<-c.done
	return nil
}

func (c *stdioConn) LocalAddr() net.Addr  { return stdioAddr{} }
func (c *stdioConn) RemoteAddr() net.Addr { return stdioAddr{} }

// Deadlines are meaningless for a process pipe; the caller (http.Transport)
// sets them defensively, so these are silently accepted rather than
// returning an error that would otherwise break every request.
func (c *stdioConn) SetDeadline(t time.Time) error      { return nil }
func (c *stdioConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *stdioConn) SetWriteDeadline(t time.Time) error { return nil }

type stdioAddr struct{}

func (stdioAddr) Network() string { return "stdio" }
func (stdioAddr) String() string  { return "docker-exec-stdio" }
