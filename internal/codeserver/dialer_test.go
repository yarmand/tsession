package codeserver

import (
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yarma/tsession/internal/config"
)

func TestLocalDialer_ConnectsToRunningListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			conn.Close()
		}
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	dial, teardown, err := LocalDialer(port)
	if err != nil {
		t.Fatalf("LocalDialer: %v", err)
	}
	if teardown != nil {
		t.Error("LocalDialer should not need extra teardown")
	}

	conn, err := dial()
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.Close()
}

func TestLocalDialer_FailsWhenNothingListening(t *testing.T) {
	// Grab then release a port so it's very likely nothing is listening.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	dial, _, err := LocalDialer(port)
	if err != nil {
		t.Fatalf("LocalDialer: %v", err)
	}
	if _, err := dial(); err == nil {
		t.Fatal("expected dial to fail when nothing is listening")
	}
}

func TestFreeLoopbackPort_ReturnsUsablePort(t *testing.T) {
	port, err := freeLoopbackPort()
	if err != nil {
		t.Fatal(err)
	}
	if port <= 0 {
		t.Fatalf("port = %d, want positive", port)
	}
}

func TestWaitForListener_SucceedsOnceListening(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			conn.Close()
		}
	}()

	addr := ln.Addr().String()
	if err := waitForListener(addr, 2*time.Second); err != nil {
		t.Fatalf("waitForListener: %v", err)
	}
}

func TestWaitForListener_TimesOutWhenNothingListens(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	err = waitForListener(addr, 300*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestRemoteTunnelDialer_DevcontainerUnsupported(t *testing.T) {
	r := config.Remote{Name: "dc", Type: "devcontainer", Container: "myapp"}
	_, _, err := RemoteTunnelDialer(r, nil)(12345)
	if err == nil {
		t.Fatal("expected an error: devcontainer has no ssh -L equivalent")
	}
}

func TestRemoteTunnelDialer_LogsCommandBeforeRunning(t *testing.T) {
	r := config.Remote{Name: "box", Type: "ssh", SSHCommand: "/does/not/exist", Host: "box.example.com"}

	var loggedBin string
	var loggedArgs []string
	_, _, err := RemoteTunnelDialer(r, func(bin string, args []string) {
		loggedBin = bin
		loggedArgs = args
	})(59999)
	if err == nil {
		t.Fatal("expected an error: SSHCommand binary does not exist")
	}
	if loggedBin != "/does/not/exist" {
		t.Fatalf("logCmd bin = %q, want /does/not/exist", loggedBin)
	}
	found := false
	for _, a := range loggedArgs {
		if strings.Contains(a, "box.example.com") {
			found = true
		}
	}
	if !found {
		t.Fatalf("logCmd args %q did not mention the remote host", loggedArgs)
	}
}

func TestRemoteTunnelDialer_EstablishesForwardAndDials(t *testing.T) {
	// Use a fake "ssh" that ignores its arguments beyond the -L spec: it
	// parses "127.0.0.1:<local>:127.0.0.1:<remote>" out of argv, listens on
	// the local port, accepts one connection, and sleeps — a stand-in for
	// a real ssh -N -L tunnel, exercising RemoteTunnelDialer's polling and
	// teardown without a real remote host.
	script := `#!/bin/sh
for arg in "$@"; do
  case "$arg" in
    127.0.0.1:*:127.0.0.1:*)
      local_port=$(echo "$arg" | cut -d: -f2)
      ;;
  esac
done
exec python3 -c "
import socket
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(('127.0.0.1', int('$local_port')))
s.listen(5)
while True:
    conn, _ = s.accept()
    conn.close()
"
`
	dir := t.TempDir()
	fakeSSH := dir + "/fake-ssh.sh"
	if err := os.WriteFile(fakeSSH, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	r := config.Remote{Name: "box", Type: "ssh", SSHCommand: fakeSSH, Host: "irrelevant"}
	dial, teardown, err := RemoteTunnelDialer(r, nil)(59999)
	if err != nil {
		t.Fatalf("RemoteTunnelDialer: %v", err)
	}
	defer teardown()

	conn, err := dial()
	if err != nil {
		t.Fatalf("dial through tunnel: %v", err)
	}
	conn.Close()

	if err := teardown(); err != nil {
		t.Fatalf("teardown: %v", err)
	}
}
