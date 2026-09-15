// Package webterm manages a registry of long-lived pseudo-terminals (PTYs),
// one per (origin, session) key, each running a single child process for its
// entire lifetime. It is deliberately generic: it knows nothing about tmux,
// SSH, or session metadata. Callers (internal/webui, using
// internal/attachcmd to build the command) hand it a Spec describing what to
// run; webterm owns the PTY, buffers recent output for reconnecting clients,
// and fans live output out to any number of concurrently subscribed
// WebSocket clients.
package webterm

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

// ringBufferCapacity is the amount of recent output retained per terminal so
// a client reconnecting after a tab close or refresh sees recent history
// instead of a blank screen.
const ringBufferCapacity = 256 * 1024

// Key identifies one persistent terminal. Origin is "" for local sessions
// and the remote name (matching config.Remote.Name / sessions.Session.Origin)
// otherwise.
type Key struct {
	Origin string
	ID     string
}

// Spec describes the command a new Terminal should run and the teardown
// step to perform when the terminal is explicitly closed.
type Spec struct {
	// Bin and Args are passed directly to exec.Command.
	Bin  string
	Args []string

	// Rows and Cols set the initial PTY size. Zero values default to 24x80.
	Rows uint16
	Cols uint16

	// Teardown, if set, runs once when the terminal is closed via
	// Registry.Close or Registry.Shutdown, after the child process has been
	// signaled to exit. It is the caller's hook for e.g. running
	// `tmux kill-session` over the same transport the terminal used, so a
	// browser-driven close doesn't leave the grouped tmux session behind.
	// Errors are returned to the Close caller but do not prevent the
	// terminal from being removed from the registry.
	Teardown func() error
}

// Terminal is one persistent PTY + child process, plus the plumbing to
// replay recent output to newly (re)connected subscribers and fan out live
// output to all of them.
type Terminal struct {
	key  Key
	cmd  *exec.Cmd
	ptmx *os.File

	ring *ringBuffer

	mu       sync.Mutex
	subs     map[*subscriber]struct{}
	nextSubs int
	exited   bool
	exitErr  error
	closed   bool

	teardown func() error
	exitCh   chan struct{}
}

type subscriber struct {
	w io.Writer
}

// Registry owns a set of live Terminals keyed by Key.
type Registry struct {
	mu    sync.Mutex
	terms map[Key]*Terminal
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{terms: make(map[Key]*Terminal)}
}

// Attach returns the Terminal for key, starting spec's command if none
// exists yet. If a Terminal for key is already running, it is returned as-is
// and spec is ignored — this is what makes "click the same session twice"
// reattach to the same warm PTY rather than spawning a second one.
func (r *Registry) Attach(key Key, spec Spec) (*Terminal, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if t, ok := r.terms[key]; ok && !t.isClosed() {
		return t, nil
	}

	t, err := newTerminal(key, spec)
	if err != nil {
		return nil, err
	}
	r.terms[key] = t
	return t, nil
}

// Get returns the Terminal currently registered for key, if any.
func (r *Registry) Get(key Key) (*Terminal, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.terms[key]
	return t, ok
}

// Close terminates the terminal for key (if any), running its Spec's
// Teardown, and removes it from the registry.
func (r *Registry) Close(key Key) error {
	r.mu.Lock()
	t, ok := r.terms[key]
	delete(r.terms, key)
	r.mu.Unlock()

	if !ok {
		return nil
	}
	return t.close()
}

// Shutdown closes every terminal currently in the registry. Errors from
// individual terminals are joined together; Shutdown always attempts to
// close all of them regardless of earlier failures.
func (r *Registry) Shutdown() error {
	r.mu.Lock()
	terms := make([]*Terminal, 0, len(r.terms))
	for k, t := range r.terms {
		terms = append(terms, t)
		delete(r.terms, k)
	}
	r.mu.Unlock()

	var errs []error
	for _, t := range terms {
		if err := t.close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func newTerminal(key Key, spec Spec) (*Terminal, error) {
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
		return nil, fmt.Errorf("webterm: start %s: %w", spec.Bin, err)
	}

	t := &Terminal{
		key:      key,
		cmd:      cmd,
		ptmx:     ptmx,
		ring:     newRingBuffer(ringBufferCapacity),
		subs:     make(map[*subscriber]struct{}),
		teardown: spec.Teardown,
		exitCh:   make(chan struct{}),
	}

	go t.readLoop()
	go t.waitLoop()

	return t, nil
}

func (t *Terminal) readLoop() {
	buf := make([]byte, 32*1024)
	for {
		n, err := t.ptmx.Read(buf)
		if n > 0 {
			t.broadcast(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func (t *Terminal) waitLoop() {
	err := t.cmd.Wait()
	t.mu.Lock()
	t.exited = true
	t.exitErr = err
	t.mu.Unlock()
	t.broadcast([]byte("\r\n[process exited]\r\n"))
	close(t.exitCh)
}

func (t *Terminal) broadcast(p []byte) {
	t.ring.Write(p)

	t.mu.Lock()
	defer t.mu.Unlock()
	for s := range t.subs {
		if _, err := s.w.Write(p); err != nil {
			delete(t.subs, s)
		}
	}
}

// Subscribe registers w to receive all future output from the terminal, and
// first synchronously replays any output currently held in the ring buffer
// so a reconnecting client isn't shown a blank screen. It returns a function
// that removes w from the live fan-out set; callers should call it when
// their connection ends.
//
// Writes to w happen synchronously on whichever goroutine produced the
// output (the terminal's own read loop), so w must be safe to call from
// that goroutine — in practice, a single WebSocket connection's Write is
// only ever called from here, so no additional synchronization is needed by
// the caller.
func (t *Terminal) Subscribe(w io.Writer) (unsubscribe func(), err error) {
	replay := t.ring.Snapshot()
	if len(replay) > 0 {
		if _, err := w.Write(replay); err != nil {
			return nil, err
		}
	}

	s := &subscriber{w: w}
	t.mu.Lock()
	if t.exited {
		// The process already exited before this subscriber connected; the
		// "[process exited]" marker was broadcast to whoever was subscribed
		// at the time, so replay it explicitly here too.
		t.mu.Unlock()
		_, _ = w.Write([]byte("\r\n[process exited]\r\n"))
		t.mu.Lock()
	}
	t.subs[s] = struct{}{}
	t.mu.Unlock()

	return func() {
		t.mu.Lock()
		delete(t.subs, s)
		t.mu.Unlock()
	}, nil
}

// Write sends p to the PTY as input (e.g. client keystrokes).
func (t *Terminal) Write(p []byte) (int, error) {
	return t.ptmx.Write(p)
}

// Resize sets the PTY's window size.
func (t *Terminal) Resize(rows, cols uint16) error {
	return pty.Setsize(t.ptmx, &pty.Winsize{Rows: rows, Cols: cols})
}

// Done returns a channel that is closed once the child process has exited.
func (t *Terminal) Done() <-chan struct{} {
	return t.exitCh
}

// Exited reports whether the child process has already exited, and if so,
// the error cmd.Wait() returned (nil for a clean exit).
func (t *Terminal) Exited() (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.exited, t.exitErr
}

func (t *Terminal) isClosed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}

// close terminates the child process (if still running), closes the PTY,
// runs the Spec's Teardown hook, and marks the terminal closed. It is safe
// to call more than once.
func (t *Terminal) close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	exited := t.exited
	teardown := t.teardown
	t.mu.Unlock()

	if !exited && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
	}
	_ = t.ptmx.Close()

	if teardown != nil {
		return teardown()
	}
	return nil
}
