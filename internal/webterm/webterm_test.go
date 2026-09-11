package webterm

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

// waitFor polls cond until it returns true or the timeout elapses, failing
// the test on timeout. Terminal output arrives asynchronously via the PTY
// read loop goroutine, so tests must poll rather than assert immediately.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !cond() {
		t.Fatal("condition not met before timeout")
	}
}

// syncBuffer is a goroutine-safe io.Writer for capturing subscriber output
// in tests.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestRegistry_AttachStartsAndReusesTerminal(t *testing.T) {
	reg := NewRegistry()
	key := Key{Origin: "", ID: "s1"}
	spec := Spec{Bin: "sh", Args: []string{"-c", "echo hello; sleep 5"}}

	t1, err := reg.Attach(key, spec)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	t2, err := reg.Attach(key, spec)
	if err != nil {
		t.Fatalf("Attach (reuse): %v", err)
	}
	if t1 != t2 {
		t.Fatal("expected Attach to reuse the existing terminal for the same key")
	}

	var out syncBuffer
	unsub, err := t1.Subscribe(&out)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer unsub()

	waitFor(t, 3*time.Second, func() bool {
		return strings.Contains(out.String(), "hello")
	})

	if err := reg.Close(key); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestTerminal_RingBufferReplaysOnLateSubscribe(t *testing.T) {
	reg := NewRegistry()
	key := Key{Origin: "", ID: "s2"}
	term, err := reg.Attach(key, Spec{Bin: "sh", Args: []string{"-c", "echo early-output; sleep 5"}})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	defer reg.Close(key)

	// Give the process a moment to produce output before anyone subscribes,
	// so this test actually exercises replay rather than live streaming.
	waitFor(t, 3*time.Second, func() bool {
		return len(term.ring.Snapshot()) > 0
	})

	var late syncBuffer
	unsub, err := term.Subscribe(&late)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer unsub()

	if !strings.Contains(late.String(), "early-output") {
		t.Fatalf("expected replay to include early output, got %q", late.String())
	}
}

func TestTerminal_WriteSendsInputToChild(t *testing.T) {
	reg := NewRegistry()
	key := Key{Origin: "", ID: "s3"}
	// cat echoes stdin back out via the pty.
	term, err := reg.Attach(key, Spec{Bin: "cat", Args: nil})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	defer reg.Close(key)

	var out syncBuffer
	unsub, err := term.Subscribe(&out)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer unsub()

	if _, err := term.Write([]byte("ping\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	waitFor(t, 3*time.Second, func() bool {
		return strings.Contains(out.String(), "ping")
	})
}

func TestTerminal_ResizeDoesNotError(t *testing.T) {
	reg := NewRegistry()
	key := Key{Origin: "", ID: "s4"}
	term, err := reg.Attach(key, Spec{Bin: "sleep", Args: []string{"5"}, Rows: 24, Cols: 80})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	defer reg.Close(key)

	if err := term.Resize(40, 120); err != nil {
		t.Fatalf("Resize: %v", err)
	}
}

func TestTerminal_ExitMarksDoneAndBroadcastsMarker(t *testing.T) {
	reg := NewRegistry()
	key := Key{Origin: "", ID: "s5"}
	term, err := reg.Attach(key, Spec{Bin: "sh", Args: []string{"-c", "exit 0"}})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	defer reg.Close(key)

	select {
	case <-term.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("terminal did not exit in time")
	}

	exited, _ := term.Exited()
	if !exited {
		t.Fatal("expected Exited() to report true after process exit")
	}

	var out syncBuffer
	unsub, err := term.Subscribe(&out)
	if err != nil {
		t.Fatalf("Subscribe after exit: %v", err)
	}
	defer unsub()

	if !strings.Contains(out.String(), "process exited") {
		t.Fatalf("expected exit marker in replay, got %q", out.String())
	}
}

func TestRegistry_CloseRunsTeardownAndRemovesTerminal(t *testing.T) {
	reg := NewRegistry()
	key := Key{Origin: "", ID: "s6"}

	var teardownRan bool
	var mu sync.Mutex
	spec := Spec{
		Bin:  "sleep",
		Args: []string{"5"},
		Teardown: func() error {
			mu.Lock()
			teardownRan = true
			mu.Unlock()
			return nil
		},
	}

	if _, err := reg.Attach(key, spec); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if err := reg.Close(key); err != nil {
		t.Fatalf("Close: %v", err)
	}

	mu.Lock()
	ran := teardownRan
	mu.Unlock()
	if !ran {
		t.Fatal("expected Teardown to run on Close")
	}

	if _, ok := reg.Get(key); ok {
		t.Fatal("expected terminal to be removed from registry after Close")
	}

	// Attaching again after Close should start a brand new terminal, not
	// reuse the closed one.
	t2, err := reg.Attach(key, Spec{Bin: "sleep", Args: []string{"1"}})
	if err != nil {
		t.Fatalf("Attach after Close: %v", err)
	}
	defer reg.Close(key)
	if t2.isClosed() {
		t.Fatal("newly attached terminal should not be closed")
	}
}

func TestRegistry_ShutdownClosesAllTerminals(t *testing.T) {
	reg := NewRegistry()
	var closedCount int
	var mu sync.Mutex
	makeSpec := func() Spec {
		return Spec{Bin: "sleep", Args: []string{"5"}, Teardown: func() error {
			mu.Lock()
			closedCount++
			mu.Unlock()
			return nil
		}}
	}

	keys := []Key{{ID: "a"}, {ID: "b"}, {Origin: "remote1", ID: "c"}}
	for _, k := range keys {
		if _, err := reg.Attach(k, makeSpec()); err != nil {
			t.Fatalf("Attach %v: %v", k, err)
		}
	}

	if err := reg.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	mu.Lock()
	got := closedCount
	mu.Unlock()
	if got != len(keys) {
		t.Fatalf("expected %d teardowns, got %d", len(keys), got)
	}
	for _, k := range keys {
		if _, ok := reg.Get(k); ok {
			t.Fatalf("expected %v to be removed after Shutdown", k)
		}
	}
}

func TestRingBuffer_DropsOldestBeyondCapacity(t *testing.T) {
	rb := newRingBuffer(10)
	rb.Write([]byte("0123456789"))
	rb.Write([]byte("X"))
	got := string(rb.Snapshot())
	if got != "123456789X" {
		t.Fatalf("expected oldest byte dropped, got %q", got)
	}
}
