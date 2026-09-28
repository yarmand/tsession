// Package codeserver manages a registry of long-lived `code serve-web`
// child processes, one per (origin, session) key, so switching back to a
// session with an already-running VS Code web instance reattaches instead
// of relaunching. It is deliberately generic about transport: the caller
// supplies the fully-built launch command (see internal/codecmd) and a
// PortReady callback that turns a discovered listening port into a Dial
// function once the callback resolves whatever's needed to reach it — a
// direct loopback dial for a local instance, or a freshly-established
// tunnel for a remote one (see dialer.go).
package codeserver

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/creack/pty"
)

// defaultStartTimeout bounds how long an instance may stay "starting"
// before it is treated as failed and killed. code serve-web can be slow on
// its very first run (it downloads the server build), so this is
// deliberately generous rather than tuned for the common case.
const defaultStartTimeout = 5 * time.Minute

// Status is the lifecycle state of one codeserver Instance.
type Status string

const (
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusFailed   Status = "failed"
	StatusStopped  Status = "stopped"
)

// Key identifies one persistent VS Code server instance. Origin is "" for
// local sessions and the remote name (matching config.Remote.Name /
// sessions.Session.Origin) otherwise.
type Key struct {
	Origin string
	ID     string
}

// PortReadyFunc is called once when the launched process reports the port
// it is listening on (parsed from its "Web UI available at ..." line). It
// must resolve however is needed to make that port reachable from this host
// — a direct dial for a local instance, or establishing a tunnel for a
// remote one — and return a dial function plus an optional extra teardown
// (e.g. to kill a tunnel process) to run when the instance is stopped. It
// runs on the instance's own output-reading goroutine and must not block
// indefinitely.
type PortReadyFunc func(port int) (dial func() (net.Conn, error), teardown func() error, err error)

// Spec describes the command an Instance should run and how to reach it
// once it reports its port.
type Spec struct {
	// Bin and Args are passed directly to exec.Command.
	Bin  string
	Args []string

	// Rows and Cols set the initial PTY size. Zero values default to
	// 24x80. The PTY is never written to or resized after launch — it
	// exists only so remote transports needing a local tty (e.g. ssh -t)
	// work, and so stdout/stderr are combined into one readable stream.
	Rows, Cols uint16

	// PortReady resolves the discovered port into a Dial function. It is
	// required; Start returns an error without it.
	PortReady PortReadyFunc

	// Teardown, if set, runs once when the instance is stopped (via
	// Registry.Stop or Registry.Shutdown) or fails to start, after the
	// child process has been killed and any PortReady-provided teardown
	// has run. Typically nil here since most transport-specific teardown
	// belongs in the PortReady-returned teardown instead; this exists for
	// completeness/symmetry with internal/webterm.Spec.
	Teardown func() error

	// StartTimeout overrides defaultStartTimeout. Zero means use the
	// default.
	StartTimeout time.Duration
}

// webUIPortPattern matches VS Code CLI's startup line, e.g.
// "Web UI available at http://127.0.0.1:52561/api/code/abc123/?tkn=...".
var webUIPortPattern = regexp.MustCompile(`Web UI available at https?://[^\s:/]+:(\d+)`)

// ansiEscapePattern strips ANSI SGR/cursor escape sequences that may appear
// in the process's output — remote transports allocate a pseudo-terminal
// (ssh -t), which can make CLIs emit color codes they'd otherwise suppress.
var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// scanForPort extracts the listening port from one line of output, if
// present.
func scanForPort(line []byte) (int, bool) {
	clean := ansiEscapePattern.ReplaceAll(line, nil)
	m := webUIPortPattern.FindSubmatch(clean)
	if m == nil {
		return 0, false
	}
	port, err := strconv.Atoi(string(m[1]))
	if err != nil {
		return 0, false
	}
	return port, true
}

// Instance is one persistent `code serve-web` child process plus the
// plumbing to discover its port and dial it once running.
type Instance struct {
	key  Key
	spec Spec
	cmd  *exec.Cmd
	ptmx *os.File

	log *ringBuffer

	mu           sync.Mutex
	status       Status
	port         int
	err          error
	dial         func() (net.Conn, error)
	dialTeardown func() error

	timer  *time.Timer
	exitCh chan struct{}
}

// Status returns the instance's current lifecycle state, its discovered
// port (0 until running), a snapshot of recently captured output, and any
// error recorded on failure.
func (in *Instance) Status() (status Status, port int, logTail []byte, err error) {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.status, in.port, in.log.Snapshot(), in.err
}

// Dial returns a fresh connection to the running VS Code server. It fails
// if the instance is not currently in StatusRunning.
func (in *Instance) Dial() (net.Conn, error) {
	in.mu.Lock()
	status := in.status
	dial := in.dial
	in.mu.Unlock()
	if status != StatusRunning || dial == nil {
		return nil, fmt.Errorf("codeserver: instance not running (status=%s)", status)
	}
	return dial()
}

// Done returns a channel that is closed once the child process has exited
// (successfully, on error, or killed).
func (in *Instance) Done() <-chan struct{} {
	return in.exitCh
}

func (in *Instance) isActive() bool {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.status == StatusStarting || in.status == StatusRunning
}

func newInstance(key Key, spec Spec) (*Instance, error) {
	if spec.PortReady == nil {
		return nil, errors.New("codeserver: Spec.PortReady is required")
	}

	rows, cols := spec.Rows, spec.Cols
	if rows == 0 {
		rows = 24
	}
	if cols == 0 {
		cols = 80
	}

	cmd := exec.Command(spec.Bin, spec.Args...)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
	if err != nil {
		return nil, fmt.Errorf("codeserver: start %s: %w", spec.Bin, err)
	}

	in := &Instance{
		key:    key,
		spec:   spec,
		cmd:    cmd,
		ptmx:   ptmx,
		log:    newRingBuffer(logBufferCapacity),
		status: StatusStarting,
		exitCh: make(chan struct{}),
	}

	timeout := spec.StartTimeout
	if timeout <= 0 {
		timeout = defaultStartTimeout
	}
	in.timer = time.AfterFunc(timeout, in.onStartTimeout)

	go in.readLoop()
	go in.waitLoop()

	return in, nil
}

// readLoop copies the child's combined stdout/stderr into the log ring
// buffer, additionally scanning each complete line for the "Web UI
// available at" marker until the port has been found.
func (in *Instance) readLoop() {
	buf := make([]byte, 32*1024)
	var pending []byte
	for {
		n, err := in.ptmx.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			in.log.Write(chunk)
			if !in.isActive() {
				// Already resolved (running/failed/stopped); no need to
				// keep scanning, just keep draining so the child doesn't
				// block on a full pty buffer.
			} else {
				pending = append(pending, chunk...)
				pending = in.consumeLines(pending)
			}
		}
		if err != nil {
			return
		}
	}
}

// consumeLines scans pending for complete lines, checking each for the
// port marker, and returns the unconsumed remainder.
func (in *Instance) consumeLines(pending []byte) []byte {
	for {
		idx := bytes.IndexByte(pending, '\n')
		if idx < 0 {
			return pending
		}
		line := pending[:idx]
		pending = pending[idx+1:]
		if port, ok := scanForPort(line); ok {
			in.onPortFound(port)
			return pending
		}
	}
}

// onPortFound resolves spec.PortReady for the discovered port and, unless
// the instance has already left StatusStarting (e.g. raced with Stop),
// transitions to StatusRunning or StatusFailed.
func (in *Instance) onPortFound(port int) {
	in.mu.Lock()
	if in.status != StatusStarting {
		in.mu.Unlock()
		return
	}
	in.mu.Unlock()

	dial, teardown, err := in.spec.PortReady(port)

	in.mu.Lock()
	defer in.mu.Unlock()
	if in.status != StatusStarting {
		// Raced with Stop()/process exit while PortReady was running (e.g.
		// establishing a tunnel): undo whatever it just set up.
		if teardown != nil {
			_ = teardown()
		}
		return
	}

	in.timer.Stop()
	if err != nil {
		in.status = StatusFailed
		in.err = fmt.Errorf("codeserver: resolve dial for port %d: %w", port, err)
		return
	}
	in.port = port
	in.dial = dial
	in.dialTeardown = teardown
	in.status = StatusRunning
}

func (in *Instance) onStartTimeout() {
	in.mu.Lock()
	if in.status != StatusStarting {
		in.mu.Unlock()
		return
	}
	in.status = StatusFailed
	in.err = fmt.Errorf("codeserver: timed out waiting for %s to report its port", in.spec.Bin)
	in.mu.Unlock()

	if in.cmd.Process != nil {
		_ = in.cmd.Process.Kill()
	}
}

func (in *Instance) waitLoop() {
	err := in.cmd.Wait()

	in.mu.Lock()
	if in.status == StatusStarting || in.status == StatusRunning {
		in.status = StatusFailed
		if err != nil {
			in.err = fmt.Errorf("code serve-web exited: %w", err)
		} else {
			in.err = errors.New("code serve-web exited unexpectedly")
		}
	}
	in.mu.Unlock()

	close(in.exitCh)
}

// close terminates the child process (if still running), closes the PTY,
// runs any PortReady-provided teardown followed by spec.Teardown, and marks
// the instance stopped. Safe to call more than once.
func (in *Instance) close() error {
	in.mu.Lock()
	if in.status == StatusStopped {
		in.mu.Unlock()
		return nil
	}
	stillRunning := in.status == StatusStarting || in.status == StatusRunning
	in.status = StatusStopped
	dialTeardown := in.dialTeardown
	teardown := in.spec.Teardown
	in.mu.Unlock()

	in.timer.Stop()

	if stillRunning && in.cmd.Process != nil {
		_ = in.cmd.Process.Kill()
	}
	_ = in.ptmx.Close()

	var errs []error
	if dialTeardown != nil {
		if err := dialTeardown(); err != nil {
			errs = append(errs, err)
		}
	}
	if teardown != nil {
		if err := teardown(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Registry owns a set of live Instances keyed by Key.
type Registry struct {
	mu        sync.Mutex
	instances map[Key]*Instance
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{instances: make(map[Key]*Instance)}
}

// Start returns the Instance for key, launching spec's command if none
// exists yet or the existing one is no longer starting/running. If an
// active (starting or running) Instance for key already exists, it is
// returned as-is and spec is ignored — this is what makes "reactivate a
// session that already has a code view" reuse the existing VS Code server
// rather than launching a second one.
func (r *Registry) Start(key Key, spec Spec) (*Instance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if in, ok := r.instances[key]; ok && in.isActive() {
		return in, nil
	}

	in, err := newInstance(key, spec)
	if err != nil {
		return nil, err
	}
	r.instances[key] = in
	return in, nil
}

// Get returns the Instance currently registered for key, if any (regardless
// of its status).
func (r *Registry) Get(key Key) (*Instance, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	in, ok := r.instances[key]
	return in, ok
}

// Stop terminates the instance for key (if any) and removes it from the
// registry.
func (r *Registry) Stop(key Key) error {
	r.mu.Lock()
	in, ok := r.instances[key]
	delete(r.instances, key)
	r.mu.Unlock()

	if !ok {
		return nil
	}
	return in.close()
}

// Shutdown stops every instance currently in the registry. Errors from
// individual instances are joined together; Shutdown always attempts to
// stop all of them regardless of earlier failures.
func (r *Registry) Shutdown() error {
	r.mu.Lock()
	instances := make([]*Instance, 0, len(r.instances))
	for k, in := range r.instances {
		instances = append(instances, in)
		delete(r.instances, k)
	}
	r.mu.Unlock()

	var errs []error
	for _, in := range instances {
		if err := in.close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
