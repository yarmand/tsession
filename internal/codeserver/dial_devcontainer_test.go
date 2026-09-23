package codeserver

import (
	"bytes"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yarma/tsession/internal/config"
)

// withFakeDocker prepends a directory containing an executable named
// "docker" (running script) to PATH for the duration of the test, so
// exec.Command("docker", ...) inside dialDevcontainerStdio resolves to it.
func withFakeDocker(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "docker")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)
	t.Cleanup(func() { os.Setenv("PATH", oldPath) })
}

func TestDevcontainerDialer_RelaysBidirectionally(t *testing.T) {
	// A fake "container-side" TCP echo server, standing in for the process
	// devcontainerRelayScript would connect to at 127.0.0.1:<port> inside
	// the real container.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn)
	}()
	port := ln.Addr().(*net.TCPAddr).Port

	// The fake "docker" drops the "exec -i <container>" prefix GatherCommand
	// produces and execs the remaining "sh -c <script>" directly on the test
	// host, so devcontainerRelayScript's nc/socat/python3 ladder runs for
	// real against the echo listener above.
	withFakeDocker(t, "#!/bin/sh\nshift 3\nexec \"$@\"\n")

	r := config.Remote{Name: "dc", Type: "devcontainer", Container: "myapp"}
	dial, teardown, err := DevcontainerDialer(r)(port)
	if err != nil {
		t.Fatalf("DevcontainerDialer: %v", err)
	}
	if teardown != nil {
		t.Error("DevcontainerDialer should not need extra teardown beyond each conn's Close")
	}

	conn, err := dial()
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	want := []byte("hello devcontainer\n")
	if _, err := conn.Write(want); err != nil {
		t.Fatalf("write: %v", err)
	}

	got := make([]byte, len(want))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("echo mismatch: got %q want %q", got, want)
	}
}

func TestDevcontainerDialer_SurfacesRelayError(t *testing.T) {
	// Simulates a container image with none of nc/socat/python3 installed:
	// the relay process exits immediately with a message on stderr instead
	// of ever connecting anywhere.
	withFakeDocker(t, "#!/bin/sh\necho 'tsession: no relay tool (nc, socat, or python3) found in devcontainer' >&2\nexit 127\n")

	r := config.Remote{Name: "dc", Type: "devcontainer", Container: "myapp"}
	dial, _, err := DevcontainerDialer(r)(59999)
	if err != nil {
		t.Fatalf("DevcontainerDialer: %v", err)
	}

	conn, err := dial()
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	buf := make([]byte, 64)
	_, readErr := conn.Read(buf)
	if readErr == nil {
		t.Fatal("expected the relay's stderr message to surface as a Read error")
	}
	if !strings.Contains(readErr.Error(), "no relay tool") {
		t.Fatalf("expected relay error message, got: %v", readErr)
	}
}
