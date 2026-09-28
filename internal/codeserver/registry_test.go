package codeserver

import (
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// waitFor polls cond until it returns true or the timeout elapses, failing
// the test on timeout. Instance state changes arrive asynchronously via the
// PTY read loop goroutine, so tests must poll rather than assert
// immediately.
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

// echoPortSpec returns a Spec whose command prints VS Code's real startup
// line format for port, then sleeps, plus a PortReady that just records the
// port it was called with — enough to exercise the registry's port-scanning
// and status transitions without a real `code` binary.
func echoPortSpec(port int, portReady PortReadyFunc) Spec {
	return Spec{
		Bin:       "sh",
		Args:      []string{"-c", "echo 'Web UI available at http://127.0.0.1:" + strconv.Itoa(port) + "/'; sleep 5"},
		PortReady: portReady,
	}
}

func recordingPortReady(t *testing.T, gotPort *int) PortReadyFunc {
	var mu sync.Mutex
	return func(port int) (func() (net.Conn, error), func() error, error) {
		mu.Lock()
		*gotPort = port
		mu.Unlock()
		return func() (net.Conn, error) { return nil, nil }, nil, nil
	}
}

func TestRegistry_StartLaunchesAndReusesInstance(t *testing.T) {
	reg := NewRegistry()
	key := Key{Origin: "", ID: "s1"}
	var gotPort int
	spec := echoPortSpec(54321, recordingPortReady(t, &gotPort))

	in1, err := reg.Start(key, spec)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, 3*time.Second, func() bool {
		status, _, _, _ := in1.Status()
		return status == StatusRunning
	})
	if gotPort != 54321 {
		t.Fatalf("PortReady called with port %d, want 54321", gotPort)
	}

	in2, err := reg.Start(key, spec)
	if err != nil {
		t.Fatalf("Start (reuse): %v", err)
	}
	if in1 != in2 {
		t.Fatal("expected Start to reuse the existing active instance for the same key")
	}

	if err := reg.Stop(key); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestRegistry_StartRelaunchesAfterStop(t *testing.T) {
	reg := NewRegistry()
	key := Key{Origin: "", ID: "s2"}
	var port1, port2 int
	spec1 := echoPortSpec(11111, recordingPortReady(t, &port1))
	spec2 := echoPortSpec(22222, recordingPortReady(t, &port2))

	in1, err := reg.Start(key, spec1)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, 3*time.Second, func() bool {
		status, _, _, _ := in1.Status()
		return status == StatusRunning
	})
	if err := reg.Stop(key); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	in2, err := reg.Start(key, spec2)
	if err != nil {
		t.Fatalf("Start after Stop: %v", err)
	}
	if in1 == in2 {
		t.Fatal("expected a new instance after Stop, not the closed one")
	}
	waitFor(t, 3*time.Second, func() bool {
		status, _, _, _ := in2.Status()
		return status == StatusRunning
	})
	if port2 != 22222 {
		t.Fatalf("PortReady called with port %d, want 22222", port2)
	}
}

func TestInstance_DialFailsBeforeRunning(t *testing.T) {
	reg := NewRegistry()
	key := Key{Origin: "", ID: "s3"}
	spec := Spec{
		Bin:  "sleep",
		Args: []string{"5"},
		PortReady: func(port int) (func() (net.Conn, error), func() error, error) {
			return func() (net.Conn, error) { return nil, nil }, nil, nil
		},
	}
	in, err := reg.Start(key, spec)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer reg.Stop(key)

	if _, err := in.Dial(); err == nil {
		t.Fatal("expected Dial to fail while instance is still starting")
	}
}

func TestInstance_PortReadyErrorMarksFailed(t *testing.T) {
	reg := NewRegistry()
	key := Key{Origin: "", ID: "s4"}
	spec := echoPortSpec(33333, func(port int) (func() (net.Conn, error), func() error, error) {
		return nil, nil, errStub
	})

	in, err := reg.Start(key, spec)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer reg.Stop(key)

	waitFor(t, 3*time.Second, func() bool {
		status, _, _, _ := in.Status()
		return status == StatusFailed
	})
	_, _, _, statusErr := in.Status()
	if statusErr == nil {
		t.Fatal("expected an error recorded for failed instance")
	}
}

func TestInstance_ProcessExitBeforePortMarksFailed(t *testing.T) {
	reg := NewRegistry()
	key := Key{Origin: "", ID: "s5"}
	spec := Spec{
		Bin:  "sh",
		Args: []string{"-c", "echo boom; exit 1"},
		PortReady: func(port int) (func() (net.Conn, error), func() error, error) {
			return func() (net.Conn, error) { return nil, nil }, nil, nil
		},
	}
	in, err := reg.Start(key, spec)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer reg.Stop(key)

	select {
	case <-in.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("instance did not exit in time")
	}

	status, _, logTail, statusErr := in.Status()
	if status != StatusFailed {
		t.Fatalf("status = %s, want failed", status)
	}
	if statusErr == nil {
		t.Fatal("expected error recorded on unexpected exit")
	}
	if !strings.Contains(string(logTail), "boom") {
		t.Fatalf("expected log tail to include child output, got %q", logTail)
	}
}

func TestInstance_StartTimeoutMarksFailedAndKills(t *testing.T) {
	reg := NewRegistry()
	key := Key{Origin: "", ID: "s6"}
	spec := Spec{
		Bin:          "sleep",
		Args:         []string{"30"},
		StartTimeout: 50 * time.Millisecond,
		PortReady: func(port int) (func() (net.Conn, error), func() error, error) {
			return func() (net.Conn, error) { return nil, nil }, nil, nil
		},
	}
	in, err := reg.Start(key, spec)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer reg.Stop(key)

	waitFor(t, 3*time.Second, func() bool {
		status, _, _, _ := in.Status()
		return status == StatusFailed
	})
}

func TestRegistry_StopRunsTeardownAndRemovesInstance(t *testing.T) {
	reg := NewRegistry()
	key := Key{Origin: "", ID: "s7"}

	var dialTeardownRan, specTeardownRan bool
	var mu sync.Mutex
	spec := Spec{
		Bin:  "sleep",
		Args: []string{"5"},
		PortReady: func(port int) (func() (net.Conn, error), func() error, error) {
			return func() (net.Conn, error) { return nil, nil }, func() error {
				mu.Lock()
				dialTeardownRan = true
				mu.Unlock()
				return nil
			}, nil
		},
		Teardown: func() error {
			mu.Lock()
			specTeardownRan = true
			mu.Unlock()
			return nil
		},
	}

	in, err := reg.Start(key, echoPortSpecWithSpec(44444, spec))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, 3*time.Second, func() bool {
		status, _, _, _ := in.Status()
		return status == StatusRunning
	})

	if err := reg.Stop(key); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	mu.Lock()
	dt, st := dialTeardownRan, specTeardownRan
	mu.Unlock()
	if !dt {
		t.Error("expected PortReady-provided teardown to run on Stop")
	}
	if !st {
		t.Error("expected Spec.Teardown to run on Stop")
	}

	if _, ok := reg.Get(key); ok {
		t.Fatal("expected instance to be removed from registry after Stop")
	}
}

func TestRegistry_ShutdownStopsAllInstances(t *testing.T) {
	reg := NewRegistry()
	var stoppedCount int
	var mu sync.Mutex
	makeSpec := func(port int) Spec {
		return echoPortSpecWithSpec(port, Spec{
			Bin:  "sleep",
			Args: []string{"5"},
			PortReady: func(port int) (func() (net.Conn, error), func() error, error) {
				return func() (net.Conn, error) { return nil, nil }, nil, nil
			},
			Teardown: func() error {
				mu.Lock()
				stoppedCount++
				mu.Unlock()
				return nil
			},
		})
	}

	keys := []Key{{ID: "a"}, {ID: "b"}, {Origin: "remote1", ID: "c"}}
	for i, k := range keys {
		if _, err := reg.Start(k, makeSpec(50000+i)); err != nil {
			t.Fatalf("Start %v: %v", k, err)
		}
	}

	if err := reg.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	mu.Lock()
	got := stoppedCount
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

// echoPortSpecWithSpec layers echoPortSpec's launch command onto an
// already-built Spec (preserving its PortReady/Teardown), for tests that
// need to assert on teardown behavior specifically.
func echoPortSpecWithSpec(port int, spec Spec) Spec {
	base := echoPortSpec(port, spec.PortReady)
	spec.Bin = base.Bin
	spec.Args = base.Args
	return spec
}

var errStub = &stubError{"stub port-ready failure"}

type stubError struct{ msg string }

func (e *stubError) Error() string { return e.msg }
